package config

import (
	"errors"
	"fmt"
	"strings"
)

type Protocol string

const (
	ProtocolSFTP   Protocol = "sftp"
	ProtocolWebDAV Protocol = "webdav"
	ProtocolFTP    Protocol = "ftp"
	ProtocolFTPS   Protocol = "ftps"
)

type AuthMethod string

const (
	AuthPassword   AuthMethod = "password"
	AuthPrivateKey AuthMethod = "private_key"
)

type Profile struct {
	Name          string
	Protocol      Protocol
	DriveLetter   string
	VolumeName    string
	Host          string
	Port          uint16
	RemotePath    string
	Username      string
	AuthMethod    AuthMethod
	PrivateKey    string
	AutoConnect   bool
	ReadOnly      bool
	AutoReconnect bool
}

func (p Profile) Validate() error {
	if strings.TrimSpace(p.Name) == "" {
		return errors.New("연결 이름이 필요합니다")
	}
	if p.Protocol != ProtocolSFTP && p.Protocol != ProtocolWebDAV && p.Protocol != ProtocolFTP && p.Protocol != ProtocolFTPS {
		return fmt.Errorf("지원하지 않는 프로토콜입니다: %q", p.Protocol)
	}
	if len(p.DriveLetter) != 1 || !isASCIILetter(p.DriveLetter[0]) {
		return errors.New("드라이브 문자는 A부터 Z까지 한 글자여야 합니다")
	}
	if strings.TrimSpace(p.Host) == "" {
		return errors.New("호스트가 필요합니다")
	}
	if p.Port == 0 {
		return errors.New("포트는 1 이상이어야 합니다")
	}
	if strings.TrimSpace(p.Username) == "" {
		return errors.New("사용자명이 필요합니다")
	}
	if p.AuthMethod != AuthPassword && p.AuthMethod != AuthPrivateKey {
		return fmt.Errorf("지원하지 않는 인증 방식입니다: %q", p.AuthMethod)
	}
	if p.AuthMethod == AuthPrivateKey && strings.TrimSpace(p.PrivateKey) == "" {
		return errors.New("개인키 인증에는 개인키 경로가 필요합니다")
	}
	return nil
}

func isASCIILetter(value byte) bool {
	return value >= 'A' && value <= 'Z' || value >= 'a' && value <= 'z'
}
