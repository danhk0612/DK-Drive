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
	client    *pkgsftp.Client
	sshClient *ssh.Client
	root      string
}

func New(ctx context.Context, config Config) (*Backend, error) {
	if err := validateConfig(config); err != nil {
		return nil, err
	}
	if config.Timeout <= 0 {
		config.Timeout = defaultTimeout
	}

	address := net.JoinHostPort(config.Host, strconv.Itoa(int(config.Port)))
	connection, err := (&net.Dialer{Timeout: config.Timeout}).DialContext(ctx, "tcp", address)
	if err != nil {
		return nil, fmt.Errorf("SFTP 서버 연결 실패: %w", err)
	}

	if err := connection.SetDeadline(time.Now().Add(config.Timeout)); err != nil {
		connection.Close()
		return nil, fmt.Errorf("SFTP 연결 제한시간 설정 실패: %w", err)
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
		return nil, fmt.Errorf("SSH 인증 또는 호스트 키 검증 실패: %w", err)
	}
	if err := connection.SetDeadline(time.Time{}); err != nil {
		sshConnection.Close()
		return nil, fmt.Errorf("SFTP 연결 제한시간 해제 실패: %w", err)
	}

	sshClient := ssh.NewClient(sshConnection, channels, requests)
	client, err := pkgsftp.NewClient(sshClient)
	if err != nil {
		sshClient.Close()
		return nil, fmt.Errorf("SFTP 세션 시작 실패: %w", err)
	}

	root := normalizeRoot(config.Root)
	info, err := client.Stat(root)
	if err != nil {
		client.Close()
		sshClient.Close()
		return nil, fmt.Errorf("SFTP 원격 시작 경로 확인 실패: %w", err)
	}
	if !info.IsDir() {
		client.Close()
		sshClient.Close()
		return nil, fmt.Errorf("SFTP 원격 시작 경로가 폴더가 아닙니다: %s", root)
	}

	return &Backend{client: client, sshClient: sshClient, root: root}, nil
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
	info, err := backend.client.Stat(remote)
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
	items, err := backend.client.ReadDir(remote)
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
	return backend.client.Open(remote)
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
	return backend.client.OpenFile(remote, flags)
}

func (backend *Backend) Mkdir(ctx context.Context, name string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	remote, err := backend.remotePath(name)
	if err != nil {
		return err
	}
	return backend.client.Mkdir(remote)
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
		return backend.client.RemoveDirectory(remote)
	}
	return backend.client.Remove(remote)
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
	return backend.client.Rename(oldRemote, newRemote)
}

func (backend *Backend) SetModTime(ctx context.Context, name string, modTime time.Time) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	remote, err := backend.remotePath(name)
	if err != nil {
		return err
	}
	return backend.client.Chtimes(remote, modTime, modTime)
}

func (backend *Backend) Close() error {
	return errors.Join(backend.client.Close(), backend.sshClient.Close())
}

var _ vfs.Backend = (*Backend)(nil)
