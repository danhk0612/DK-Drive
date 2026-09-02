package recovery

import (
	"bytes"
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"sync"
	"testing"
	"time"

	localcache "github.com/danhk0612/DK-Drive/internal/cache"
	"github.com/danhk0612/DK-Drive/internal/vfs"
)

func TestRelativePath(t *testing.T) {
	tests := []struct {
		root, preserved, want string
	}{
		{"/home/user", "/home/user/folder/file.txt", "folder/file.txt"},
		{"/", "/folder/file.txt", "folder/file.txt"},
		{"home/user", `home\user\한글.txt`, "한글.txt"},
	}
	for _, test := range tests {
		got, err := RelativePath(test.root, test.preserved)
		if err != nil || got != test.want {
			t.Fatalf("RelativePath(%q, %q)=(%q, %v), want %q", test.root, test.preserved, got, err, test.want)
		}
	}
	if _, err := RelativePath("/home/user", "/other/file.txt"); err == nil {
		t.Fatal("path outside profile root was accepted")
	}
}

func TestInspectUsesSizeAndTime(t *testing.T) {
	store, item := recoveryFixture(t, []byte("preserved"))
	backend := newMemoryBackend()
	backend.entries["same.txt"] = memoryObject{data: []byte("preserved"), modTime: item.Metadata.UpdatedAt.Add(time.Second)}
	backend.entries["different-time.txt"] = memoryObject{data: []byte("preserved"), modTime: item.Metadata.UpdatedAt.Add(3 * time.Second)}
	backend.entries["different-size.txt"] = memoryObject{data: []byte("short"), modTime: item.Metadata.UpdatedAt}
	backend.entries["missing-time.txt"] = memoryObject{data: []byte("preserved")}

	tests := []struct {
		path string
		want RemoteState
	}{
		{"missing.txt", RemoteMissing},
		{"same.txt", RemoteSame},
		{"different-time.txt", RemoteConflict},
		{"different-size.txt", RemoteConflict},
		{"missing-time.txt", RemoteConflict},
	}
	for _, test := range tests {
		got, _, err := Inspect(context.Background(), store, item, backend, test.path)
		if err != nil || got != test.want {
			t.Fatalf("Inspect(%s)=(%v, %v), want %v", test.path, got, err, test.want)
		}
	}
}

func TestUploadPublishesAndKeepsRecoveryItem(t *testing.T) {
	data := bytes.Repeat([]byte("DK-Drive 재시도"), 40000)
	store, item := recoveryFixture(t, data)
	backend := newMemoryBackend()
	entry, err := Upload(context.Background(), store, item, backend, "folder/retry.txt")
	if err != nil {
		t.Fatal(err)
	}
	if entry.Size != int64(len(data)) || !bytes.Equal(backend.entries["folder/retry.txt"].data, data) {
		t.Fatal("remote bytes do not match recovery data")
	}
	items, err := store.Scan()
	if err != nil || len(items) != 1 || items[0].Metadata.RecoveryState != localcache.StatePreserved {
		t.Fatalf("recovery item changed after upload: items=%v err=%v", items, err)
	}
}

func TestUploadRejectsPrematureEOF(t *testing.T) {
	data := bytes.Repeat([]byte("cache"), 100)
	store, item := recoveryFixture(t, data)
	backend := newMemoryBackend()
	backend.onOpenWrite = func() {
		if err := os.Truncate(item.Metadata.StagingPath, int64(len(data)/2)); err != nil {
			t.Fatal(err)
		}
	}

	_, err := Upload(context.Background(), store, item, backend, "retry.txt")
	if !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("Upload error=%v, want io.ErrUnexpectedEOF", err)
	}
}

func TestSyncAndCloseDeduplicatesSameError(t *testing.T) {
	want := errors.New("WebDAV PUT 응답: 409 Conflict")
	writer := &failingWriter{syncErr: want, closeErr: errors.New(want.Error())}
	err := syncAndClose(writer)
	if err == nil || err.Error() != want.Error() {
		t.Fatalf("syncAndClose() = %v, want one %q", err, want)
	}
}

func TestSyncAndCloseKeepsDifferentErrors(t *testing.T) {
	writer := &failingWriter{syncErr: errors.New("sync failed"), closeErr: errors.New("close failed")}
	err := syncAndClose(writer)
	if err == nil || err.Error() != "sync failed\nclose failed" {
		t.Fatalf("syncAndClose() = %v", err)
	}
}

func TestAlternatePath(t *testing.T) {
	at := time.Date(2026, 9, 1, 15, 4, 5, 0, time.UTC)
	if got := AlternatePath("folder/report.txt", at); got != "folder/report.recovered-20260901-150405.txt" {
		t.Fatal(got)
	}
}

func recoveryFixture(t *testing.T, data []byte) (*localcache.Store, localcache.RecoveryItem) {
	t.Helper()
	store, err := localcache.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	file, err := store.CreateStaging()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	item, err := store.Preserve(localcache.Preservation{StagingPath: file.Name(), RemotePath: "/folder/retry.txt"})
	if err != nil {
		t.Fatal(err)
	}
	return store, item
}

type memoryObject struct {
	data    []byte
	modTime time.Time
}

type memoryBackend struct {
	mu          sync.Mutex
	entries     map[string]memoryObject
	onOpenWrite func()
}

func newMemoryBackend() *memoryBackend { return &memoryBackend{entries: map[string]memoryObject{}} }

func (backend *memoryBackend) Stat(_ context.Context, name string) (vfs.Entry, error) {
	backend.mu.Lock()
	defer backend.mu.Unlock()
	value, ok := backend.entries[name]
	if !ok {
		return vfs.Entry{}, os.ErrNotExist
	}
	return vfs.Entry{Name: name, Size: int64(len(value.data)), Mode: 0o600, ModTime: value.modTime}, nil
}

func (backend *memoryBackend) OpenWrite(_ context.Context, name string, _ vfs.WriteOptions) (vfs.WriteHandle, error) {
	if backend.onOpenWrite != nil {
		backend.onOpenWrite()
	}
	return &memoryWriter{backend: backend, name: name}, nil
}

func (backend *memoryBackend) ReadDir(context.Context, string) ([]vfs.Entry, error) { return nil, nil }
func (backend *memoryBackend) OpenRead(context.Context, string) (io.ReadCloser, error) {
	return nil, fs.ErrNotExist
}
func (backend *memoryBackend) Mkdir(context.Context, string) error                 { return nil }
func (backend *memoryBackend) Remove(context.Context, string, bool) error          { return nil }
func (backend *memoryBackend) Rename(context.Context, string, string) error        { return nil }
func (backend *memoryBackend) SetModTime(context.Context, string, time.Time) error { return nil }
func (backend *memoryBackend) SetReadOnly(context.Context, string, bool) error     { return nil }
func (backend *memoryBackend) Close() error                                        { return nil }

type memoryWriter struct {
	backend *memoryBackend
	name    string
	data    []byte
}

type failingWriter struct {
	syncErr  error
	closeErr error
}

func (*failingWriter) WriteAt(data []byte, _ int64) (int, error) { return len(data), nil }
func (writer *failingWriter) Sync() error                        { return writer.syncErr }
func (writer *failingWriter) Close() error                       { return writer.closeErr }

func (writer *memoryWriter) WriteAt(data []byte, offset int64) (int, error) {
	end := int(offset) + len(data)
	if end > len(writer.data) {
		writer.data = append(writer.data, make([]byte, end-len(writer.data))...)
	}
	copy(writer.data[int(offset):], data)
	return len(data), nil
}

func (writer *memoryWriter) Sync() error { return nil }
func (writer *memoryWriter) Close() error {
	writer.backend.mu.Lock()
	defer writer.backend.mu.Unlock()
	writer.backend.entries[writer.name] = memoryObject{data: append([]byte(nil), writer.data...), modTime: time.Now()}
	return nil
}
