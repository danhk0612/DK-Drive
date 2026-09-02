//go:build windows

package mount

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"syscall"
	"testing"
	"time"

	"github.com/winfsp/go-winfsp/gofs"
	"golang.org/x/sys/windows"

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

func TestGoFileSystemStatReturnsOSNotExist(t *testing.T) {
	store, err := localcache.New(t.TempDir())
	if err != nil {
		t.Fatalf("cache.New(): %v", err)
	}
	filesystem := NewGoFileSystem(
		&stagingBackend{statErr: fmt.Errorf("remote stat: %w", fs.ErrNotExist)},
		false,
		store,
	)
	if _, err := filesystem.Stat("missing.txt"); !os.IsNotExist(err) {
		t.Fatalf("Stat() error = %v, want os.IsNotExist", err)
	}
}

func TestNewMetadataBehaviourWrapsGoFS(t *testing.T) {
	store, err := localcache.New(t.TempDir())
	if err != nil {
		t.Fatalf("cache.New(): %v", err)
	}
	backend := &stagingBackend{}
	filesystem := newGoFileSystem(backend, false, store)
	base, err := gofs.NewOptions(filesystem)
	if err != nil {
		t.Fatalf("gofs.NewOptions(): %v", err)
	}
	if _, err := NewMetadataBehaviour(base, filesystem, false); err != nil {
		t.Fatalf("NewMetadataBehaviour(): %v", err)
	}
}

func TestSetModTimeUpdatesOpenStagingFile(t *testing.T) {
	store, err := localcache.New(t.TempDir())
	if err != nil {
		t.Fatalf("cache.New(): %v", err)
	}
	backend := &stagingBackend{}
	filesystem := newGoFileSystem(backend, false, store)
	file, err := filesystem.OpenFile("new.txt", os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatalf("OpenFile(): %v", err)
	}
	if _, err := file.Write([]byte("test")); err != nil {
		t.Fatalf("Write(): %v", err)
	}
	want := time.Date(2026, time.January, 2, 3, 4, 6, 0, time.UTC)
	if err := filesystem.setModTime("new.txt", want); err != nil {
		t.Fatalf("setModTime(): %v", err)
	}
	info, err := file.Stat()
	if err != nil {
		t.Fatalf("Stat(): %v", err)
	}
	if !info.ModTime().Equal(want) {
		t.Fatalf("ModTime() = %v, want %v", info.ModTime(), want)
	}
	if info.Sys() != nil {
		t.Fatalf("Sys() = %T, want nil", info.Sys())
	}
	if err := file.Close(); err != nil {
		t.Fatalf("Close(): %v", err)
	}
	if !backend.modTime.Equal(want) {
		t.Fatalf("backend modTime = %v, want %v", backend.modTime, want)
	}
}

func TestFiletimeToTime(t *testing.T) {
	want := time.Date(2026, time.January, 2, 3, 4, 6, 0, time.UTC)
	filetime := syscall.NsecToFiletime(want.UnixNano())
	value := uint64(filetime.HighDateTime)<<32 | uint64(filetime.LowDateTime)
	if got := filetimeToTime(value); !got.Equal(want) {
		t.Fatalf("filetimeToTime() = %v, want %v", got, want)
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

func TestStagingFileRejectsWriteBeyondCacheLimit(t *testing.T) {
	store, err := localcache.NewWithLimits(t.TempDir(), localcache.Limits{MaxFileBytes: 5, MaxTotalBytes: 10})
	if err != nil {
		t.Fatal(err)
	}
	filesystem := NewGoFileSystem(&stagingBackend{}, false, store)
	file, err := filesystem.OpenFile("limited.txt", os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.Write([]byte("123456")); !errors.Is(err, syscall.ENOSPC) {
		t.Fatalf("Write() error = %v, want ENOSPC", err)
	} else {
		assertDiskFullStatus(t, err)
	}
	if err := file.Truncate(6); !errors.Is(err, syscall.ENOSPC) {
		t.Fatalf("Truncate() error = %v, want ENOSPC", err)
	} else {
		assertDiskFullStatus(t, err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
}

func assertDiskFullStatus(t *testing.T, err error) {
	t.Helper()
	var status windows.NTStatus
	if !errors.As(err, &status) || status != windows.STATUS_DISK_FULL {
		t.Fatalf("error = %v, want STATUS_DISK_FULL", err)
	}
}

func TestStagingFilePreservedAfterUploadFailure(t *testing.T) {
	store, err := localcache.New(t.TempDir())
	if err != nil {
		t.Fatalf("cache.New(): %v", err)
	}
	wantErr := errors.New("upload failed")
	filesystem := newGoFileSystem(&stagingBackend{openWriteErr: wantErr}, false, store)
	filesystem.recovery = RecoveryContext{
		ProfileID: "profile-1", ProfileName: "NAS 연결",
		Protocol: "sftp", RemotePath: "/home/",
	}
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
	assertCacheEntryCount(t, store.Directory(), 2)
	items, err := store.Scan()
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("recovery items = %+v", items)
	}
	metadata := items[0].Metadata
	if metadata.ProfileID != "profile-1" || metadata.ProfileName != "NAS 연결" || metadata.Protocol != "sftp" || metadata.RemotePath != "/home/new.txt" || metadata.Reason != localcache.ReasonUploadFailed || metadata.LastError == "" || metadata.RecoveryState != localcache.StatePreserved {
		t.Fatalf("recovery metadata = %+v", metadata)
	}
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
	statErr      error
	modTime      time.Time
}

func (backend *stagingBackend) Stat(context.Context, string) (vfs.Entry, error) {
	if backend.statErr != nil {
		return vfs.Entry{}, backend.statErr
	}
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
func (backend *stagingBackend) SetModTime(_ context.Context, _ string, modTime time.Time) error {
	backend.modTime = modTime
	return nil
}
func (backend *stagingBackend) SetReadOnly(context.Context, string, bool) error {
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

func TestRecoveryRemotePathIncludesProfileRoot(t *testing.T) {
	for _, test := range []struct {
		root, name, want string
	}{
		{"/home/", "folder/file.txt", "/home/folder/file.txt"},
		{"/", `folder\한글.txt`, "/folder/한글.txt"},
	} {
		if got := recoveryRemotePath(test.root, test.name); got != test.want {
			t.Errorf("recoveryRemotePath(%q, %q) = %q, want %q", test.root, test.name, got, test.want)
		}
	}
}
