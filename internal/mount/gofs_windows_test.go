//go:build windows

package mount

import (
	"errors"
	"io/fs"
	"os"
	"testing"

	"github.com/danhk0612/DK-Drive/internal/vfs"
)

func TestReadOnlyGoFileSystemRejectsWritableOpen(t *testing.T) {
	filesystem := NewGoFileSystem(nil, true)
	flags := []int{
		os.O_WRONLY,
		os.O_RDWR,
		os.O_CREATE | os.O_WRONLY,
		os.O_TRUNC | os.O_WRONLY,
		os.O_APPEND | os.O_WRONLY,
	}
	for _, flag := range flags {
		if _, err := filesystem.OpenFile("file.txt", flag, 0o644); !errors.Is(err, fs.ErrPermission) {
			t.Errorf("OpenFile(flag=%d) error = %v, want fs.ErrPermission", flag, err)
		}
	}
}

func TestReadOnlyGoFileSystemUsesVFSReadOnlyError(t *testing.T) {
	if !errors.Is(vfs.ErrReadOnly, fs.ErrPermission) {
		t.Fatalf("ErrReadOnly = %v, want fs.ErrPermission", vfs.ErrReadOnly)
	}
}
