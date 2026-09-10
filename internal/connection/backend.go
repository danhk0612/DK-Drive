package connection

import (
	"context"
	"crypto/tls"
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/danhk0612/DK-Drive/internal/config"
	"github.com/danhk0612/DK-Drive/internal/diagnostics"
	ftpbackend "github.com/danhk0612/DK-Drive/internal/protocol/ftp"
	sftpbackend "github.com/danhk0612/DK-Drive/internal/protocol/sftp"
	webdavbackend "github.com/danhk0612/DK-Drive/internal/protocol/webdav"
	"github.com/danhk0612/DK-Drive/internal/vfs"
	"golang.org/x/crypto/ssh/knownhosts"
)

func OpenBackend(ctx context.Context, p config.Profile, s config.Secrets) (vfs.Backend, error) {
	protocol := string(p.Protocol)
	if p.Protocol == config.ProtocolFTPS {
		protocol = "explicit-ftps"
		if p.FTPSMode == "implicit-ftps" {
			protocol = "implicit-ftps"
		}
	}
	recorder, err := diagnostics.Open(os.Getenv("DKDRIVE_DIAGNOSTICS"), protocol)
	if err != nil {
		return nil, errors.New("진단 폴더를 생성하거나 열 수 없습니다")
	}
	backend, err := openBackend(ctx, p, s, recorder)
	if err != nil {
		_ = recorder.Close()
		return nil, err
	}
	return vfs.WithDiagnostics(backend, recorder), nil
}

func openBackend(ctx context.Context, p config.Profile, s config.Secrets, recorder *diagnostics.Recorder) (vfs.Backend, error) {
	if err := p.Validate(); err != nil {
		return nil, err
	}
	if p.AuthMethod == config.AuthPassword && s.Password == "" {
		return nil, errors.New("비밀번호를 입력하세요")
	}
	switch p.Protocol {
	case config.ProtocolSFTP:
		filename := p.KnownHosts
		if filename == "" {
			home, err := os.UserHomeDir()
			if err != nil {
				return nil, err
			}
			filename = filepath.Join(home, ".ssh", "known_hosts")
		}
		callback, err := knownhosts.New(filename)
		if err != nil {
			return nil, errors.New("known_hosts 파일을 확인하세요; 호스트 키 검증은 생략하지 않습니다")
		}
		c := sftpbackend.Config{Diagnostics: recorder, Host: p.Host, Port: p.Port, Username: p.Username, Root: p.RemotePath, Timeout: 10 * time.Second, HostKeyCallback: callback}
		if p.AuthMethod == config.AuthPrivateKey {
			c.Signer, err = sftpbackend.LoadPrivateKey(p.PrivateKey, []byte(s.Passphrase))
			if err != nil {
				return nil, err
			}
		} else {
			c.Password = s.Password
		}
		return sftpbackend.New(ctx, c)
	case config.ProtocolWebDAV:
		scheme := p.WebDAVScheme
		if scheme == "" {
			scheme = "https"
		}
		return webdavbackend.New(ctx, webdavbackend.Config{Diagnostics: recorder, Scheme: scheme, Host: p.Host, Port: p.Port, Username: p.Username, Password: s.Password, Root: p.RemotePath, Timeout: 30 * time.Second, InsecureSkipTLSVerify: p.InsecureSkipTLSVerify})
	case config.ProtocolFTP, config.ProtocolFTPS:
		c := ftpbackend.Config{Diagnostics: recorder, Host: p.Host, Port: p.Port, Username: p.Username, Password: s.Password, Root: p.RemotePath, Timeout: 30 * time.Second, TLSMode: ftpbackend.TLSNone}
		if p.Protocol == config.ProtocolFTPS {
			c.TLSMode = ftpbackend.TLSExplicit
			if p.FTPSMode == "implicit-ftps" {
				c.TLSMode = ftpbackend.TLSImplicit
			}
			c.TLSConfig = &tls.Config{InsecureSkipVerify: p.InsecureSkipTLSVerify}
		}
		return ftpbackend.New(ctx, c)
	}
	return nil, errors.New("지원하지 않는 프로토콜입니다")
}
