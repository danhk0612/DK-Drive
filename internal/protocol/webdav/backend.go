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
	"path"
	"strconv"
	"strings"
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
	backend := &Backend{client: client, baseURL: baseURL, username: config.Username, password: config.Password}
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

func (backend *Backend) OpenWrite(context.Context, string, vfs.WriteOptions) (vfs.WriteHandle, error) {
	return nil, unsupported("파일 쓰기")
}

func (backend *Backend) Mkdir(context.Context, string) error {
	return unsupported("폴더 생성")
}

func (backend *Backend) Remove(context.Context, string, bool) error {
	return unsupported("삭제")
}

func (backend *Backend) Rename(context.Context, string, string) error {
	return unsupported("이동 및 이름 변경")
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

func unsupported(operation string) error {
	return fmt.Errorf("WebDAV %s: %w", operation, errors.ErrUnsupported)
}

func responseError(method string, response *http.Response) error {
	message, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
	detail := strings.TrimSpace(string(message))
	if detail == "" {
		return fmt.Errorf("WebDAV %s 응답: %s", method, response.Status)
	}
	return fmt.Errorf("WebDAV %s 응답: %s: %s", method, response.Status, detail)
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
