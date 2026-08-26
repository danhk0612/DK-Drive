//go:build windows

package mount

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"testing"
	"time"

	localcache "github.com/danhk0612/DK-Drive/internal/cache"
	"github.com/danhk0612/DK-Drive/internal/vfs"
)

func TestReadOnlyGoFileSystemRejectsWritableOpen(t *testing.T) {
	store, err := localcache.New(t.TempDir())
	if err != nil {
		t.Fatalf("cache.New(): %v", err)
	}
	filesystem := NewGoFileSystem(nil, true, store)
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

func TestStagingFileRemovedAfterSuccessfulUpload(t *testing.T) {
	store, err := localcache.New(t.TempDir())
	if err != nil {
		t.Fatalf("cache.New(): %v", err)
	}
	filesystem := NewGoFileSystem(&stagingBackend{}, false, store)
	file, err := filesystem.OpenFile("new.txt", os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatalf("OpenFile(): %v", err)
	}
	if _, err := file.Write([]byte("test")); err != nil {
		t.Fatalf("Write(): %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("Close(): %v", err)
	}
	assertCacheEntryCount(t, store.Directory(), 0)
}

func TestStagingFilePreservedAfterUploadFailure(t *testing.T) {
	store, err := localcache.New(t.TempDir())
	if err != nil {
		t.Fatalf("cache.New(): %v", err)
	}
	wantErr := errors.New("upload failed")
	filesystem := NewGoFileSystem(&stagingBackend{openWriteErr: wantErr}, false, store)
	file, err := filesystem.OpenFile("new.txt", os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatalf("OpenFile(): %v", err)
	}
	if _, err := file.Write([]byte("preserve me")); err != nil {
		t.Fatalf("Write(): %v", err)
	}
	if err := file.Close(); !errors.Is(err, wantErr) {
		t.Fatalf("Close() error = %v, want %v", err, wantErr)
	}
	assertCacheEntryCount(t, store.Directory(), 1)
}

func assertCacheEntryCount(t *testing.T, directory string, want int) {
	t.Helper()
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatalf("ReadDir(): %v", err)
	}
	if len(entries) != want {
		t.Fatalf("cache entry count = %d, want %d", len(entries), want)
	}
}

type stagingBackend struct {
	openWriteErr error
}

func (backend *stagingBackend) Stat(context.Context, string) (vfs.Entry, error) {
	return vfs.Entry{}, os.ErrNotExist
}

func (backend *stagingBackend) ReadDir(context.Context, string) ([]vfs.Entry, error) {
	return nil, nil
}

func (backend *stagingBackend) OpenRead(context.Context, string) (io.ReadCloser, error) {
	return nil, os.ErrNotExist
}

func (backend *stagingBackend) OpenWrite(context.Context, string, vfs.WriteOptions) (vfs.WriteHandle, error) {
	if backend.openWriteErr != nil {
		return nil, backend.openWriteErr
	}
	return discardWriteHandle{}, nil
}

func (backend *stagingBackend) Mkdir(context.Context, string) error {
	return nil
}
func (backend *stagingBackend) Remove(context.Context, string, bool) error {
	return nil
}
func (backend *stagingBackend) Rename(context.Context, string, string) error {
	return nil
}
func (backend *stagingBackend) SetModTime(context.Context, string, time.Time) error {
	return nil
}
func (backend *stagingBackend) Close() error {
	return nil
}

type discardWriteHandle struct{}

func (discardWriteHandle) WriteAt(buffer []byte, _ int64) (int, error) {
	return len(buffer), nil
}
func (discardWriteHandle) Sync() error {
	return nil
}
func (discardWriteHandle) Close() error {
	return nil
}

func TestReadOnlyGoFileSystemUsesVFSReadOnlyError(t *testing.T) {
	if !errors.Is(vfs.ErrReadOnly, fs.ErrPermission) {
		t.Fatalf("ErrReadOnly = %v, want fs.ErrPermission", vfs.ErrReadOnly)
	}
}
