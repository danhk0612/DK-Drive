//go:build windows

package connection

import (
	"context"
	"fmt"
	"github.com/danhk0612/DK-Drive/internal/config"
	"github.com/danhk0612/DK-Drive/internal/mount"
	"github.com/winfsp/go-winfsp"
	"golang.org/x/sys/windows"
	"strings"
)

func Mount(ctx context.Context, profileID string, p config.Profile, secret config.Secrets) (Session, error) {
	if err := p.Validate(); err != nil {
		return nil, err
	}
	mask, err := windows.GetLogicalDrives()
	if err != nil {
		return nil, err
	}
	letter := strings.ToUpper(p.DriveLetter)
	if mask&(1<<(letter[0]-'A')) != 0 {
		return nil, fmt.Errorf("%s: 드라이브는 이미 사용 중입니다", letter)
	}
	if err := winfsp.LoadWinFSP(); err != nil {
		return nil, fmt.Errorf("WinFsp 런타임을 불러올 수 없습니다. WinFsp 설치를 확인하세요: %w", err)
	}
	backend, err := OpenBackend(ctx, p, secret)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		backend.Close()
		return nil, err
	}
	s, err := mount.StartSession(backend, mount.Options{
		DriveLetter: letter, VolumeName: p.Name, ReadOnly: p.ReadOnly,
		ProfileID: profileID, ProfileName: p.Name, Protocol: string(p.Protocol), RemotePath: p.RemotePath,
	}, p.Protocol == config.ProtocolSFTP)
	if err != nil {
		backend.Close()
		return nil, err
	}
	return s, nil
}
