package mount

import (
	"context"
	"errors"

	"github.com/danhk0612/DK-Drive/internal/vfs"
)

var ErrNotImplemented = errors.New("WinFsp 마운트 기술 검증이 아직 구현되지 않았습니다")

type Options struct {
	DriveLetter string
	VolumeName  string
	ReadOnly    bool
	ProfileID   string
	ProfileName string
	Protocol    string
	RemotePath  string
	CacheLimits CacheLimits
}

type CacheLimits struct {
	MaxFileBytes  int64
	MaxTotalBytes int64
}

type Manager interface {
	Mount(ctx context.Context, backend vfs.Backend, options Options) error
	Unmount(ctx context.Context, driveLetter string) error
}
