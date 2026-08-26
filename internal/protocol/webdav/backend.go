package webdav

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/danhk0612/DK-Drive/internal/vfs"
)

const defaultTimeout = 10 * time.Second

const propfindBody = `<?xml version="1.0" encoding="utf-8"?>
<d:propfind xmlns:d="DAV:">
  <d:prop>
    <d:resourcetype/>
    <d:getcontentlength/>
    <d:getlastmodified/>
  </d:prop>
</d:propfind>`

const lockBody = `<?xml version="1.0" encoding="utf-8"?>
<d:lockinfo xmlns:d="DAV:">
  <d:lockscope><d:exclusive/></d:lockscope>
  <d:locktype><d:write/></d:locktype>
  <d:owner><d:href>DKDrive</d:href></d:owner>
</d:lockinfo>`

type Config struct {
	Scheme     string
	Host       string
	Port       uint16
	Username   string
	Password   string
	Root       string
	Timeout    time.Duration
	HTTPClient *http.Client
}

type Backend struct {
	client   *http.Client
	baseURL  *url.URL
	username string
	password string
	timeout  time.Duration
}

type Capabilities struct {
	DAVClasses []string
	Methods    map[string]bool
}

func New(ctx context.Context, config Config) (*Backend, error) {
	if err := validateConfig(config); err != nil {
		return nil, err
	}
	if config.Timeout <= 0 {
		config.Timeout = defaultTimeout
	}
	client := config.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: config.Timeout}
	}
	baseURL := &url.URL{
		Scheme: strings.ToLower(config.Scheme),
		Host:   net.JoinHostPort(config.Host, strconv.Itoa(int(config.Port))),
		Path:   normalizeRoot(config.Root),
	}
	backend := &Backend{
		client: client, baseURL: baseURL, username: config.Username,
		password: config.Password, timeout: config.Timeout,
	}
	entries, err := backend.propfind(ctx, ".", "0")
	if err != nil {
		return nil, fmt.Errorf("WebDAV 원격 시작 경로 확인 실패: %w", err)
	}
	if len(entries) != 1 || !entries[0].IsDir() {
		return nil, fmt.Errorf("WebDAV 원격 시작 경로가 폴더가 아닙니다: %s", baseURL.Path)
	}
	return backend, nil
}

func validateConfig(config Config) error {
	if config.Scheme != "http" && config.Scheme != "https" {
		return errors.New("WebDAV 방식은 http 또는 https여야 합니다")
	}
	if strings.TrimSpace(config.Host) == "" {
		return errors.New("WebDAV 호스트가 필요합니다")
	}
	if config.Port == 0 {
		return errors.New("WebDAV 포트는 1 이상이어야 합니다")
	}
	if strings.TrimSpace(config.Username) == "" {
		return errors.New("WebDAV 사용자명이 필요합니다")
	}
	if config.Password == "" {
		return errors.New("WebDAV 비밀번호가 필요합니다")
	}
	return nil
}

func normalizeRoot(root string) string {
	if strings.TrimSpace(root) == "" {
		return "/"
	}
	return path.Clean("/" + strings.TrimPrefix(strings.ReplaceAll(root, "\\", "/"), "/"))
}

func (backend *Backend) resourceURL(name string) (*url.URL, error) {
	normalized := strings.ReplaceAll(name, "\\", "/")
	for _, component := range strings.Split(normalized, "/") {
		if component == ".." {
			return nil, errors.New("상위 원격 경로 접근은 허용되지 않습니다")
		}
	}
	relative := strings.TrimPrefix(path.Clean("/"+normalized), "/")
	resource := *backend.baseURL
	resource.Path = path.Join(backend.baseURL.Path, relative)
	return &resource, nil
}

func (backend *Backend) newRequest(ctx context.Context, method string, resource *url.URL, body io.Reader) (*http.Request, error) {
	request, err := http.NewRequestWithContext(ctx, method, resource.String(), body)
	if err != nil {
		return nil, err
	}
	request.SetBasicAuth(backend.username, backend.password)
	request.Header.Set("User-Agent", "DKDrive/0.3")
	return request, nil
}

func (backend *Backend) Stat(ctx context.Context, name string) (vfs.Entry, error) {
	entries, err := backend.propfind(ctx, name, "0")
	if err != nil {
		return vfs.Entry{}, err
	}
	if len(entries) != 1 {
		return vfs.Entry{}, fmt.Errorf("WebDAV PROPFIND가 항목 %d개를 반환했습니다", len(entries))
	}
	return entries[0], nil
}

func (backend *Backend) Capabilities(ctx context.Context) (Capabilities, error) {
	request, err := backend.newRequest(ctx, http.MethodOptions, backend.baseURL, nil)
	if err != nil {
		return Capabilities{}, fmt.Errorf("WebDAV OPTIONS 요청 생성 실패: %w", err)
	}
	response, err := backend.client.Do(request)
	if err != nil {
		return Capabilities{}, fmt.Errorf("WebDAV OPTIONS 요청 실패: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return Capabilities{}, responseError("OPTIONS", response)
	}
	capabilities := Capabilities{Methods: make(map[string]bool)}
	for _, value := range response.Header.Values("DAV") {
		for _, item := range strings.Split(value, ",") {
			if item = strings.TrimSpace(item); item != "" {
				capabilities.DAVClasses = append(capabilities.DAVClasses, item)
			}
		}
	}
	for _, value := range response.Header.Values("Allow") {
		for _, item := range strings.Split(value, ",") {
			if item = strings.TrimSpace(item); item != "" {
				capabilities.Methods[strings.ToUpper(item)] = true
			}
		}
	}
	return capabilities, nil
}

func (backend *Backend) Lock(ctx context.Context, name string, timeout time.Duration) (string, error) {
	resource, err := backend.resourceURL(name)
	if err != nil {
		return "", err
	}
	request, err := backend.newRequest(ctx, "LOCK", resource, bytes.NewBufferString(lockBody))
	if err != nil {
		return "", fmt.Errorf("WebDAV LOCK 요청 생성 실패: %w", err)
	}
	request.Header.Set("Content-Type", "application/xml; charset=utf-8")
	request.Header.Set("Depth", "0")
	seconds := int64(timeout / time.Second)
	if seconds < 1 {
		seconds = 60
	}
	request.Header.Set("Timeout", fmt.Sprintf("Second-%d", seconds))
	response, err := backend.client.Do(request)
	if err != nil {
		return "", fmt.Errorf("WebDAV LOCK 요청 실패: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK && response.StatusCode != http.StatusCreated {
		return "", responseError("LOCK", response)
	}
	token := normalizeLockToken(response.Header.Get("Lock-Token"))
	if token == "" {
		var body lockResponse
		if err := xml.NewDecoder(response.Body).Decode(&body); err != nil {
			return "", fmt.Errorf("WebDAV LOCK 응답 해석 실패: %w", err)
		}
		token = normalizeLockToken(body.LockDiscovery.ActiveLock.LockToken.Href)
	}
	if token == "" {
		return "", errors.New("WebDAV LOCK 응답에 잠금 토큰이 없습니다")
	}
	return token, nil
}

func (backend *Backend) Unlock(ctx context.Context, name, token string) error {
	resource, err := backend.resourceURL(name)
	if err != nil {
		return err
	}
	token = normalizeLockToken(token)
	if token == "" {
		return errors.New("WebDAV UNLOCK에는 잠금 토큰이 필요합니다")
	}
	request, err := backend.newRequest(ctx, "UNLOCK", resource, nil)
	if err != nil {
		return fmt.Errorf("WebDAV UNLOCK 요청 생성 실패: %w", err)
	}
	request.Header.Set("Lock-Token", "<"+token+">")
	return backend.do(request, http.StatusOK, http.StatusNoContent)
}

func normalizeLockToken(token string) string {
	return strings.TrimSpace(strings.Trim(strings.TrimSpace(token), "<>"))
}

func (backend *Backend) ReadDir(ctx context.Context, name string) ([]vfs.Entry, error) {
	resource, err := backend.resourceURL(name)
	if err != nil {
		return nil, err
	}
	entries, err := backend.propfindURL(ctx, resource, "1")
	if err != nil {
		return nil, err
	}
	resourcePath := path.Clean(resource.Path)
	children := make([]vfs.Entry, 0, len(entries))
	for _, entry := range entries {
		if path.Clean(entry.hrefPath) == resourcePath {
			continue
		}
		children = append(children, entry.Entry)
	}
	return children, nil
}

func (backend *Backend) propfind(ctx context.Context, name, depth string) ([]vfs.Entry, error) {
	resource, err := backend.resourceURL(name)
	if err != nil {
		return nil, err
	}
	items, err := backend.propfindURL(ctx, resource, depth)
	if err != nil {
		return nil, err
	}
	entries := make([]vfs.Entry, 0, len(items))
	for _, item := range items {
		entries = append(entries, item.Entry)
	}
	return entries, nil
}

func (backend *Backend) propfindURL(ctx context.Context, resource *url.URL, depth string) ([]davEntry, error) {
	request, err := backend.newRequest(ctx, "PROPFIND", resource, bytes.NewBufferString(propfindBody))
	if err != nil {
		return nil, fmt.Errorf("WebDAV PROPFIND 요청 생성 실패: %w", err)
	}
	request.Header.Set("Depth", depth)
	request.Header.Set("Content-Type", "application/xml; charset=utf-8")
	response, err := backend.client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("WebDAV PROPFIND 요청 실패: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusMultiStatus {
		return nil, responseError("PROPFIND", response)
	}
	var multistatus multistatusResponse
	if err := xml.NewDecoder(response.Body).Decode(&multistatus); err != nil {
		return nil, fmt.Errorf("WebDAV PROPFIND 응답 해석 실패: %w", err)
	}
	entries := make([]davEntry, 0, len(multistatus.Responses))
	for _, item := range multistatus.Responses {
		entry, err := entryFromResponse(resource, item)
		if err != nil {
			return nil, err
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

func (backend *Backend) OpenRead(ctx context.Context, name string) (io.ReadCloser, error) {
	resource, err := backend.resourceURL(name)
	if err != nil {
		return nil, err
	}
	request, err := backend.newRequest(ctx, http.MethodGet, resource, nil)
	if err != nil {
		return nil, fmt.Errorf("WebDAV GET 요청 생성 실패: %w", err)
	}
	response, err := backend.client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("WebDAV GET 요청 실패: %w", err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		defer response.Body.Close()
		return nil, responseError("GET", response)
	}
	return response.Body, nil
}

func (backend *Backend) Mkdir(ctx context.Context, name string) error {
	resource, err := backend.resourceURL(name)
	if err != nil {
		return err
	}
	return backend.execute(ctx, "MKCOL", resource, nil, http.StatusCreated)
}

func (backend *Backend) Remove(ctx context.Context, name string, _ bool) error {
	resource, err := backend.resourceURL(name)
	if err != nil {
		return err
	}
	return backend.execute(ctx, http.MethodDelete, resource, nil, http.StatusOK, http.StatusNoContent)
}

func (backend *Backend) Rename(ctx context.Context, oldName, newName string) error {
	source, err := backend.resourceURL(oldName)
	if err != nil {
		return err
	}
	destination, err := backend.resourceURL(newName)
	if err != nil {
		return err
	}
	request, err := backend.newRequest(ctx, "MOVE", source, nil)
	if err != nil {
		return fmt.Errorf("WebDAV MOVE 요청 생성 실패: %w", err)
	}
	request.Header.Set("Destination", destination.String())
	request.Header.Set("Overwrite", "F")
	return backend.do(request, http.StatusCreated, http.StatusNoContent)
}

func (backend *Backend) SetModTime(context.Context, string, time.Time) error {
	return unsupported("수정 시간 설정")
}

func (backend *Backend) SetReadOnly(context.Context, string, bool) error {
	return unsupported("읽기 전용 속성 설정")
}

func (backend *Backend) Close() error {
	backend.client.CloseIdleConnections()
	return nil
}

func (backend *Backend) execute(ctx context.Context, method string, resource *url.URL, body io.Reader, statuses ...int) error {
	request, err := backend.newRequest(ctx, method, resource, body)
	if err != nil {
		return fmt.Errorf("WebDAV %s 요청 생성 실패: %w", method, err)
	}
	return backend.do(request, statuses...)
}

func (backend *Backend) do(request *http.Request, statuses ...int) error {
	response, err := backend.client.Do(request)
	if err != nil {
		return fmt.Errorf("WebDAV %s 요청 실패: %w", request.Method, err)
	}
	defer response.Body.Close()
	for _, status := range statuses {
		if response.StatusCode == status {
			return nil
		}
	}
	return responseError(request.Method, response)
}

func unsupported(operation string) error {
	return fmt.Errorf("WebDAV %s: %w", operation, errors.ErrUnsupported)
}

func responseError(method string, response *http.Response) error {
	body, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
	detail := strings.TrimSpace(string(body))
	message := fmt.Sprintf("WebDAV %s 응답: %s", method, response.Status)
	if detail != "" {
		message += ": " + detail
	}
	switch response.StatusCode {
	case http.StatusNotFound:
		return fmt.Errorf("%s: %w", message, fs.ErrNotExist)
	case http.StatusUnauthorized, http.StatusForbidden, http.StatusLocked:
		return fmt.Errorf("%s: %w", message, fs.ErrPermission)
	case http.StatusMethodNotAllowed, http.StatusNotImplemented:
		return fmt.Errorf("%s: %w", message, errors.ErrUnsupported)
	default:
		return errors.New(message)
	}
}

type putHandle struct {
	mutex    sync.Mutex
	backend  *Backend
	resource *url.URL
	file     *os.File
	path     string
	append   bool
	dirty    bool
	closed   bool
}

func (backend *Backend) OpenWrite(ctx context.Context, name string, options vfs.WriteOptions) (vfs.WriteHandle, error) {
	resource, err := backend.resourceURL(name)
	if err != nil {
		return nil, err
	}
	temporary, err := os.CreateTemp("", "dkdrive-webdav-*")
	if err != nil {
		return nil, fmt.Errorf("WebDAV 쓰기 임시 파일 생성 실패: %w", err)
	}
	handle := &putHandle{
		backend: backend, resource: resource, file: temporary, path: temporary.Name(),
		append: options.Append, dirty: options.Truncate,
	}
	cleanup := true
	defer func() {
		if cleanup {
			temporary.Close()
			os.Remove(temporary.Name())
		}
	}()

	if !options.Truncate {
		reader, openErr := backend.OpenRead(ctx, name)
		if openErr == nil {
			_, copyErr := io.Copy(temporary, reader)
			closeErr := reader.Close()
			if err := errors.Join(copyErr, closeErr); err != nil {
				return nil, fmt.Errorf("WebDAV 기존 파일 임시 저장 실패: %w", err)
			}
		} else if !options.Create || !errors.Is(openErr, fs.ErrNotExist) {
			return nil, openErr
		} else {
			handle.dirty = true
		}
	} else if !options.Create {
		if _, err := backend.Stat(ctx, name); err != nil {
			return nil, err
		}
	}
	cleanup = false
	return handle, nil
}

func (handle *putHandle) WriteAt(data []byte, offset int64) (int, error) {
	handle.mutex.Lock()
	defer handle.mutex.Unlock()
	if handle.closed {
		return 0, os.ErrClosed
	}
	if handle.append {
		info, err := handle.file.Stat()
		if err != nil {
			return 0, err
		}
		offset = info.Size()
	}
	written, err := handle.file.WriteAt(data, offset)
	if written > 0 {
		handle.dirty = true
	}
	return written, err
}

func (handle *putHandle) Sync() error {
	handle.mutex.Lock()
	defer handle.mutex.Unlock()
	return handle.syncLocked()
}

func (handle *putHandle) syncLocked() error {
	if handle.closed {
		return os.ErrClosed
	}
	if err := handle.file.Sync(); err != nil {
		return err
	}
	if !handle.dirty {
		return nil
	}
	info, err := handle.file.Stat()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), handle.backend.timeout)
	defer cancel()
	request, err := handle.backend.newRequest(ctx, http.MethodPut, handle.resource, io.NewSectionReader(handle.file, 0, info.Size()))
	if err != nil {
		return fmt.Errorf("WebDAV PUT 요청 생성 실패: %w", err)
	}
	request.ContentLength = info.Size()
	if err := handle.backend.do(request, http.StatusOK, http.StatusCreated, http.StatusNoContent); err != nil {
		return err
	}
	handle.dirty = false
	return nil
}

func (handle *putHandle) Close() error {
	handle.mutex.Lock()
	defer handle.mutex.Unlock()
	if handle.closed {
		return os.ErrClosed
	}
	syncErr := handle.syncLocked()
	handle.closed = true
	return errors.Join(syncErr, handle.file.Close(), os.Remove(handle.path))
}

type multistatusResponse struct {
	Responses []davResponse `xml:"response"`
}

type davResponse struct {
	Href      string        `xml:"href"`
	Propstats []davPropstat `xml:"propstat"`
}

type davPropstat struct {
	Status string      `xml:"status"`
	Prop   davProperty `xml:"prop"`
}

type davProperty struct {
	ResourceType  davResourceType `xml:"resourcetype"`
	ContentLength string          `xml:"getcontentlength"`
	LastModified  string          `xml:"getlastmodified"`
}

type davResourceType struct {
	Collection *struct{} `xml:"collection"`
}

type lockResponse struct {
	LockDiscovery struct {
		ActiveLock struct {
			LockToken struct {
				Href string `xml:"href"`
			} `xml:"locktoken"`
		} `xml:"activelock"`
	} `xml:"lockdiscovery"`
}

type davEntry struct {
	vfs.Entry
	hrefPath string
}

func entryFromResponse(resource *url.URL, response davResponse) (davEntry, error) {
	var property *davProperty
	for index := range response.Propstats {
		if strings.Contains(response.Propstats[index].Status, " 200 ") {
			property = &response.Propstats[index].Prop
			break
		}
	}
	if property == nil {
		return davEntry{}, fmt.Errorf("WebDAV PROPFIND 속성 응답에 성공 상태가 없습니다: %s", response.Href)
	}
	href, err := url.Parse(response.Href)
	if err != nil {
		return davEntry{}, fmt.Errorf("WebDAV 응답 경로 해석 실패 %q: %w", response.Href, err)
	}
	href = resource.ResolveReference(href)
	hrefPath := path.Clean(href.Path)
	name := path.Base(strings.TrimSuffix(hrefPath, "/"))
	mode := fs.FileMode(0o644)
	if property.ResourceType.Collection != nil {
		mode = fs.ModeDir | 0o755
	}
	var size int64
	if property.ContentLength != "" {
		size, err = strconv.ParseInt(strings.TrimSpace(property.ContentLength), 10, 64)
		if err != nil {
			return davEntry{}, fmt.Errorf("WebDAV 파일 크기 해석 실패 %q: %w", property.ContentLength, err)
		}
	}
	var modTime time.Time
	if property.LastModified != "" {
		modTime, err = http.ParseTime(strings.TrimSpace(property.LastModified))
		if err != nil {
			return davEntry{}, fmt.Errorf("WebDAV 수정 시간 해석 실패 %q: %w", property.LastModified, err)
		}
	}
	return davEntry{Entry: vfs.Entry{Name: name, Size: size, Mode: mode, ModTime: modTime}, hrefPath: hrefPath}, nil
}

var _ vfs.Backend = (*Backend)(nil)
