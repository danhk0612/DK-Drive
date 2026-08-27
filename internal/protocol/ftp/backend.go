package ftp

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math"
	"net"
	"path"
	"strings"
	"sync"
	"time"

	ftpclient "github.com/jlaffaye/ftp"

	"github.com/danhk0612/DK-Drive/internal/vfs"
)

const defaultTimeout = 10 * time.Second

type TLSMode string

const (
	TLSNone     TLSMode = "none"
	TLSExplicit TLSMode = "explicit"
	TLSImplicit TLSMode = "implicit"
)

type Config struct {
	Host      string
	Port      uint16
	Username  string
	Password  string
	Root      string
	Timeout   time.Duration
	TLSMode   TLSMode
	TLSConfig *tls.Config
}

type Backend struct {
	mutex  sync.Mutex
	client *ftpclient.ServerConn
	root   string
	closed bool
}

func New(ctx context.Context, config Config) (*Backend, error) {
	if err := validateConfig(config); err != nil {
		return nil, err
	}
	if config.Timeout <= 0 {
		config.Timeout = defaultTimeout
	}

	client, root, err := connect(ctx, config)
	if err != nil {
		return nil, err
	}
	return &Backend{client: client, root: root}, nil
}

func connect(ctx context.Context, config Config) (*ftpclient.ServerConn, string, error) {
	address := net.JoinHostPort(config.Host, fmt.Sprint(config.Port))
	options := []ftpclient.DialOption{
		ftpclient.DialWithContext(ctx),
		ftpclient.DialWithTimeout(config.Timeout),
		ftpclient.DialWithShutTimeout(config.Timeout),
	}
	if config.TLSMode != TLSNone {
		tlsConfig := config.TLSConfig.Clone()
		if tlsConfig.ServerName == "" {
			tlsConfig.ServerName = config.Host
		}
		if config.TLSMode == TLSExplicit {
			options = append(options, ftpclient.DialWithExplicitTLS(tlsConfig))
		} else {
			options = append(options, ftpclient.DialWithTLS(tlsConfig))
		}
	}

	client, err := ftpclient.Dial(address, options...)
	if err != nil {
		return nil, "", fmt.Errorf("FTP 서버 연결 실패: %w", err)
	}
	if err := client.Login(config.Username, config.Password); err != nil {
		client.Quit()
		return nil, "", fmt.Errorf("FTP 인증 실패: %w", err)
	}
	if err := client.ChangeDir(normalizeRoot(config.Root)); err != nil {
		client.Quit()
		return nil, "", fmt.Errorf("FTP 원격 시작 경로 확인 실패: %w", err)
	}
	root, err := client.CurrentDir()
	if err != nil {
		client.Quit()
		return nil, "", fmt.Errorf("FTP 원격 시작 경로 조회 실패: %w", err)
	}
	return client, normalizeRoot(root), nil
}

func validateConfig(config Config) error {
	if strings.TrimSpace(config.Host) == "" {
		return errors.New("FTP 호스트가 필요합니다")
	}
	if config.Port == 0 {
		return errors.New("FTP 포트는 1 이상이어야 합니다")
	}
	if strings.TrimSpace(config.Username) == "" {
		return errors.New("FTP 사용자명이 필요합니다")
	}
	if config.Password == "" {
		return errors.New("FTP 비밀번호가 필요합니다")
	}
	if config.TLSMode != TLSNone && config.TLSMode != TLSExplicit && config.TLSMode != TLSImplicit {
		return fmt.Errorf("지원하지 않는 FTP TLS 모드입니다: %q", config.TLSMode)
	}
	if config.TLSMode != TLSNone && config.TLSConfig == nil {
		return errors.New("FTPS에는 TLS 설정이 필요합니다")
	}
	return nil
}

func normalizeRoot(root string) string {
	if strings.TrimSpace(root) == "" {
		return "/"
	}
	return path.Clean("/" + strings.TrimPrefix(strings.ReplaceAll(root, "\\", "/"), "/"))
}

func (backend *Backend) remotePath(name string) (string, error) {
	normalized := strings.ReplaceAll(name, "\\", "/")
	for _, component := range strings.Split(normalized, "/") {
		if component == ".." {
			return "", errors.New("상위 원격 경로 접근은 허용되지 않습니다")
		}
	}
	relative := strings.TrimPrefix(path.Clean("/"+normalized), "/")
	return path.Join(backend.root, relative), nil
}

func (backend *Backend) Stat(ctx context.Context, name string) (vfs.Entry, error) {
	remote, err := backend.remotePath(name)
	if err != nil {
		return vfs.Entry{}, err
	}
	client, err := backend.lock(ctx)
	if err != nil {
		return vfs.Entry{}, err
	}
	defer backend.mutex.Unlock()

	if remote == backend.root {
		return vfs.Entry{Name: path.Base(backend.root), Mode: fs.ModeDir | 0o755}, nil
	}
	entry, statErr := client.GetEntry(remote)
	if statErr == nil {
		return entryFromFTP(entry), nil
	}
	entries, listErr := client.List(path.Dir(remote))
	if listErr != nil {
		return vfs.Entry{}, fmt.Errorf("FTP 경로 조회 실패: %w", errors.Join(statErr, listErr))
	}
	for _, item := range entries {
		if item.Name == path.Base(remote) {
			return entryFromFTP(item), nil
		}
	}
	return vfs.Entry{}, fs.ErrNotExist
}

func (backend *Backend) ReadDir(ctx context.Context, name string) ([]vfs.Entry, error) {
	remote, err := backend.remotePath(name)
	if err != nil {
		return nil, err
	}
	client, err := backend.lock(ctx)
	if err != nil {
		return nil, err
	}
	defer backend.mutex.Unlock()

	items, err := client.List(remote)
	if err != nil {
		return nil, fmt.Errorf("FTP 디렉터리 조회 실패: %w", err)
	}
	entries := make([]vfs.Entry, 0, len(items))
	for _, item := range items {
		if item.Name != "." && item.Name != ".." {
			entries = append(entries, entryFromFTP(item))
		}
	}
	return entries, nil
}

func entryFromFTP(entry *ftpclient.Entry) vfs.Entry {
	mode := fs.FileMode(0o644)
	if entry.Type == ftpclient.EntryTypeFolder {
		mode = fs.ModeDir | 0o755
	} else if entry.Type == ftpclient.EntryTypeLink {
		mode = fs.ModeSymlink | 0o777
	}
	size := int64(entry.Size)
	if entry.Size > math.MaxInt64 {
		size = math.MaxInt64
	}
	return vfs.Entry{Name: entry.Name, Size: size, Mode: mode, ModTime: entry.Time}
}

func (backend *Backend) OpenRead(ctx context.Context, name string) (io.ReadCloser, error) {
	remote, err := backend.remotePath(name)
	if err != nil {
		return nil, err
	}
	client, err := backend.lock(ctx)
	if err != nil {
		return nil, err
	}
	response, err := client.Retr(remote)
	if err != nil {
		backend.mutex.Unlock()
		return nil, fmt.Errorf("FTP 파일 읽기 시작 실패: %w", err)
	}
	return &lockedReadCloser{ReadCloser: response, unlock: backend.mutex.Unlock}, nil
}

type lockedReadCloser struct {
	io.ReadCloser
	once   sync.Once
	unlock func()
}

func (reader *lockedReadCloser) Close() error {
	err := reader.ReadCloser.Close()
	reader.once.Do(reader.unlock)
	return err
}

func (backend *Backend) OpenWrite(context.Context, string, vfs.WriteOptions) (vfs.WriteHandle, error) {
	return nil, errors.ErrUnsupported
}

func (backend *Backend) Mkdir(context.Context, string) error {
	return errors.ErrUnsupported
}

func (backend *Backend) Remove(context.Context, string, bool) error {
	return errors.ErrUnsupported
}

func (backend *Backend) Rename(context.Context, string, string) error {
	return errors.ErrUnsupported
}

func (backend *Backend) SetModTime(context.Context, string, time.Time) error {
	return errors.ErrUnsupported
}

func (backend *Backend) SetReadOnly(context.Context, string, bool) error {
	return errors.ErrUnsupported
}

func (backend *Backend) Close() error {
	backend.mutex.Lock()
	defer backend.mutex.Unlock()
	if backend.closed {
		return nil
	}
	backend.closed = true
	return backend.client.Quit()
}

func (backend *Backend) lock(ctx context.Context) (*ftpclient.ServerConn, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	backend.mutex.Lock()
	if backend.closed {
		backend.mutex.Unlock()
		return nil, net.ErrClosed
	}
	return backend.client, nil
}

var _ vfs.Backend = (*Backend)(nil)
