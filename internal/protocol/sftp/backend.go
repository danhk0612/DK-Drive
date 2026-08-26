package sftp

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"

	pkgsftp "github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"

	"github.com/danhk0612/DK-Drive/internal/vfs"
)

const defaultTimeout = 10 * time.Second

type Config struct {
	Host            string
	Port            uint16
	Username        string
	Password        string
	Signer          ssh.Signer
	Root            string
	Timeout         time.Duration
	HostKeyCallback ssh.HostKeyCallback
}

type Backend struct {
	mutex      sync.RWMutex
	client     *pkgsftp.Client
	sshClient  *ssh.Client
	config     Config
	root       string
	generation uint64
	closed     bool
}

func New(ctx context.Context, config Config) (*Backend, error) {
	if err := validateConfig(config); err != nil {
		return nil, err
	}
	if config.Timeout <= 0 {
		config.Timeout = defaultTimeout
	}

	client, sshClient, root, err := connect(ctx, config)
	if err != nil {
		return nil, err
	}
	return &Backend{client: client, sshClient: sshClient, config: config, root: root}, nil
}

func connect(ctx context.Context, config Config) (*pkgsftp.Client, *ssh.Client, string, error) {
	address := net.JoinHostPort(config.Host, strconv.Itoa(int(config.Port)))
	connection, err := (&net.Dialer{Timeout: config.Timeout}).DialContext(ctx, "tcp", address)
	if err != nil {
		return nil, nil, "", fmt.Errorf("SFTP 서버 연결 실패: %w", err)
	}

	if err := connection.SetDeadline(time.Now().Add(config.Timeout)); err != nil {
		connection.Close()
		return nil, nil, "", fmt.Errorf("SFTP 연결 제한시간 설정 실패: %w", err)
	}

	sshConfig := &ssh.ClientConfig{
		User:            config.Username,
		Auth:            authenticationMethods(config),
		HostKeyCallback: config.HostKeyCallback,
		Timeout:         config.Timeout,
	}
	sshConnection, channels, requests, err := ssh.NewClientConn(connection, address, sshConfig)
	if err != nil {
		connection.Close()
		return nil, nil, "", fmt.Errorf("SSH 인증 또는 호스트 키 검증 실패: %w", err)
	}
	if err := connection.SetDeadline(time.Time{}); err != nil {
		sshConnection.Close()
		return nil, nil, "", fmt.Errorf("SFTP 연결 제한시간 해제 실패: %w", err)
	}

	sshClient := ssh.NewClient(sshConnection, channels, requests)
	client, err := pkgsftp.NewClient(sshClient)
	if err != nil {
		sshClient.Close()
		return nil, nil, "", fmt.Errorf("SFTP 세션 시작 실패: %w", err)
	}

	root := normalizeRoot(config.Root)
	info, err := client.Stat(root)
	if err != nil {
		client.Close()
		sshClient.Close()
		return nil, nil, "", fmt.Errorf("SFTP 원격 시작 경로 확인 실패: %w", err)
	}
	if !info.IsDir() {
		client.Close()
		sshClient.Close()
		return nil, nil, "", fmt.Errorf("SFTP 원격 시작 경로가 폴더가 아닙니다: %s", root)
	}

	return client, sshClient, root, nil
}

func validateConfig(config Config) error {
	if strings.TrimSpace(config.Host) == "" {
		return errors.New("SFTP 호스트가 필요합니다")
	}
	if config.Port == 0 {
		return errors.New("SFTP 포트는 1 이상이어야 합니다")
	}
	if strings.TrimSpace(config.Username) == "" {
		return errors.New("SFTP 사용자명이 필요합니다")
	}
	if (config.Password == "") == (config.Signer == nil) {
		return errors.New("SFTP 인증 방식은 비밀번호 또는 개인키 중 하나여야 합니다")
	}
	if config.HostKeyCallback == nil {
		return errors.New("SFTP 호스트 키 검증 설정이 필요합니다")
	}
	return nil
}

func authenticationMethods(config Config) []ssh.AuthMethod {
	if config.Signer != nil {
		return []ssh.AuthMethod{ssh.PublicKeys(config.Signer)}
	}
	return []ssh.AuthMethod{ssh.Password(config.Password)}
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
	if err := ctx.Err(); err != nil {
		return vfs.Entry{}, err
	}
	remote, err := backend.remotePath(name)
	if err != nil {
		return vfs.Entry{}, err
	}
	info, err := withReconnect(ctx, backend, func(client *pkgsftp.Client) (os.FileInfo, error) {
		return client.Stat(remote)
	})
	if err != nil {
		return vfs.Entry{}, err
	}
	return entryFromInfo(info), nil
}

func (backend *Backend) ReadDir(ctx context.Context, name string) ([]vfs.Entry, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	remote, err := backend.remotePath(name)
	if err != nil {
		return nil, err
	}
	items, err := withReconnect(ctx, backend, func(client *pkgsftp.Client) ([]os.FileInfo, error) {
		return client.ReadDir(remote)
	})
	if err != nil {
		return nil, err
	}
	entries := make([]vfs.Entry, 0, len(items))
	for _, item := range items {
		entries = append(entries, entryFromInfo(item))
	}
	return entries, nil
}

func entryFromInfo(info os.FileInfo) vfs.Entry {
	return vfs.Entry{Name: info.Name(), Size: info.Size(), Mode: info.Mode(), ModTime: info.ModTime()}
}

func (backend *Backend) OpenRead(ctx context.Context, name string) (io.ReadCloser, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	remote, err := backend.remotePath(name)
	if err != nil {
		return nil, err
	}
	return withReconnect(ctx, backend, func(client *pkgsftp.Client) (io.ReadCloser, error) {
		return client.Open(remote)
	})
}

func (backend *Backend) OpenWrite(ctx context.Context, name string, options vfs.WriteOptions) (vfs.WriteHandle, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	remote, err := backend.remotePath(name)
	if err != nil {
		return nil, err
	}
	flags := os.O_WRONLY
	if options.Create {
		flags |= os.O_CREATE
	}
	if options.Truncate {
		flags |= os.O_TRUNC
	}
	if options.Append {
		flags |= os.O_APPEND
	}
	return withReconnect(ctx, backend, func(client *pkgsftp.Client) (vfs.WriteHandle, error) {
		return client.OpenFile(remote, flags)
	})
}

func (backend *Backend) Mkdir(ctx context.Context, name string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	remote, err := backend.remotePath(name)
	if err != nil {
		return err
	}
	_, err = withReconnect(ctx, backend, func(client *pkgsftp.Client) (struct{}, error) {
		return struct{}{}, client.Mkdir(remote)
	})
	return err
}

func (backend *Backend) Remove(ctx context.Context, name string, directory bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	remote, err := backend.remotePath(name)
	if err != nil {
		return err
	}
	if directory {
		_, err = withReconnect(ctx, backend, func(client *pkgsftp.Client) (struct{}, error) {
			return struct{}{}, client.RemoveDirectory(remote)
		})
		return err
	}
	_, err = withReconnect(ctx, backend, func(client *pkgsftp.Client) (struct{}, error) {
		return struct{}{}, client.Remove(remote)
	})
	return err
}

func (backend *Backend) Rename(ctx context.Context, oldName, newName string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	oldRemote, err := backend.remotePath(oldName)
	if err != nil {
		return err
	}
	newRemote, err := backend.remotePath(newName)
	if err != nil {
		return err
	}
	_, err = withReconnect(ctx, backend, func(client *pkgsftp.Client) (struct{}, error) {
		return struct{}{}, client.Rename(oldRemote, newRemote)
	})
	return err
}

func (backend *Backend) SetModTime(ctx context.Context, name string, modTime time.Time) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	remote, err := backend.remotePath(name)
	if err != nil {
		return err
	}
	_, err = withReconnect(ctx, backend, func(client *pkgsftp.Client) (struct{}, error) {
		return struct{}{}, client.Chtimes(remote, modTime, modTime)
	})
	return err
}

func (backend *Backend) SetReadOnly(ctx context.Context, name string, readOnly bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	remote, err := backend.remotePath(name)
	if err != nil {
		return err
	}
	_, err = withReconnect(ctx, backend, func(client *pkgsftp.Client) (struct{}, error) {
		info, err := client.Stat(remote)
		if err != nil {
			return struct{}{}, err
		}
		mode := modeWithReadOnly(info.Mode(), readOnly)
		return struct{}{}, client.Chmod(remote, mode)
	})
	return err
}

func modeWithReadOnly(mode os.FileMode, readOnly bool) os.FileMode {
	mode = mode.Perm()
	if readOnly {
		return mode &^ 0o222
	}
	return mode | 0o200
}

func (backend *Backend) Close() error {
	backend.mutex.Lock()
	defer backend.mutex.Unlock()
	if backend.closed {
		return nil
	}
	backend.closed = true
	return errors.Join(backend.client.Close(), backend.sshClient.Close())
}

func withReconnect[T any](ctx context.Context, backend *Backend, operation func(*pkgsftp.Client) (T, error)) (T, error) {
	client, generation, err := backend.session()
	var zero T
	if err != nil {
		return zero, err
	}
	result, err := operation(client)
	if err == nil || !isConnectionError(err) {
		return result, err
	}
	client, err = backend.reconnect(ctx, generation)
	if err != nil {
		return zero, err
	}
	return operation(client)
}

func (backend *Backend) session() (*pkgsftp.Client, uint64, error) {
	backend.mutex.RLock()
	defer backend.mutex.RUnlock()
	if backend.closed {
		return nil, 0, net.ErrClosed
	}
	return backend.client, backend.generation, nil
}

func (backend *Backend) reconnect(ctx context.Context, failedGeneration uint64) (*pkgsftp.Client, error) {
	backend.mutex.Lock()
	defer backend.mutex.Unlock()
	if backend.closed {
		return nil, net.ErrClosed
	}
	if backend.generation != failedGeneration {
		return backend.client, nil
	}
	client, sshClient, _, err := connect(ctx, backend.config)
	if err != nil {
		return nil, fmt.Errorf("SFTP 재연결 실패: %w", err)
	}
	oldClient, oldSSHClient := backend.client, backend.sshClient
	backend.client, backend.sshClient = client, sshClient
	backend.generation++
	oldClient.Close()
	oldSSHClient.Close()
	return backend.client, nil
}

func isConnectionError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, net.ErrClosed) {
		return true
	}
	var networkError net.Error
	if errors.As(err, &networkError) {
		return true
	}
	message := strings.ToLower(err.Error())
	for _, marker := range []string{
		"broken pipe",
		"connection aborted",
		"connection lost",
		"connection reset",
		"ssh: disconnect",
		"use of closed network connection",
	} {
		if strings.Contains(message, marker) {
			return true
		}
	}
	return false
}

var _ vfs.Backend = (*Backend)(nil)
