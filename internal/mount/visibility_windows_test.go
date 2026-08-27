//go:build windows

package mount

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	localcache "github.com/danhk0612/DK-Drive/internal/cache"
	"github.com/danhk0612/DK-Drive/internal/vfs"
)

// Files become visible remotely only when the mount starts its upload.
type visibilityBackend struct {
	vfs.Backend
	root         string
	uploads      atomic.Int32
	openWriteErr error
	entered      chan struct{}
	release      chan struct{}
}

func (backend *visibilityBackend) Stat(_ context.Context, name string) (vfs.Entry, error) {
	info, err := os.Stat(filepath.Join(backend.root, name))
	if err != nil {
		return vfs.Entry{}, err
	}
	return vfs.Entry{Name: info.Name(), Size: info.Size(), Mode: info.Mode(), ModTime: info.ModTime()}, nil
}

func (backend *visibilityBackend) OpenRead(_ context.Context, name string) (io.ReadCloser, error) {
	return os.Open(filepath.Join(backend.root, name))
}

func (backend *visibilityBackend) OpenWrite(_ context.Context, name string, _ vfs.WriteOptions) (vfs.WriteHandle, error) {
	backend.uploads.Add(1)
	if backend.entered != nil {
		close(backend.entered)
		<-backend.release
	}
	if backend.openWriteErr != nil {
		return nil, backend.openWriteErr
	}
	return os.OpenFile(filepath.Join(backend.root, name), os.O_CREATE|os.O_TRUNC|os.O_RDWR, 0o600)
}

func visibilityFixture(t *testing.T) (*goFileSystem, *visibilityBackend) {
	t.Helper()
	store, err := localcache.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	backend := &visibilityBackend{root: t.TempDir()}
	return newGoFileSystem(backend, false, store), backend
}

func TestPathLookupPublishesPendingWrite(t *testing.T) {
	for _, operation := range []string{"stat", "open"} {
		t.Run(operation, func(t *testing.T) {
			filesystem, backend := visibilityFixture(t)
			const name = "한글 파일.txt"
			const content = "DKDrive FTP 마운트 쓰기 검증"
			writer, err := filesystem.OpenFile(name, os.O_CREATE|os.O_WRONLY, 0o600)
			if err != nil {
				t.Fatal(err)
			}
			defer writer.Close()
			if _, err := writer.Write([]byte(content)); err != nil {
				t.Fatal(err)
			}
			other, err := filesystem.OpenFile("unrelated.txt", os.O_CREATE|os.O_WRONLY, 0o600)
			if err != nil {
				t.Fatal(err)
			}
			defer other.Close()
			if _, err := backend.Stat(context.Background(), name); !os.IsNotExist(err) {
				t.Fatalf("file already uploaded before lookup: %v", err)
			}
			// Simulate the next pathname lookup before WinFsp delivers Close.
			switch operation {
			case "stat":
				info, err := filesystem.Stat(`.\` + name)
				if err != nil {
					t.Fatal(err)
				}
				if info.Size() != int64(len(content)) {
					t.Fatalf("size = %d", info.Size())
				}
			case "open":
				reader, err := filesystem.OpenFile(`.\`+name, os.O_RDONLY, 0)
				if err != nil {
					t.Fatal(err)
				}
				data, readErr := io.ReadAll(reader)
				closeErr := reader.Close()
				if string(data) != content || readErr != nil || closeErr != nil {
					t.Fatalf("read = %q, errors: %v, %v", data, readErr, closeErr)
				}
			}
			if backend.uploads.Load() != 1 {
				t.Fatalf("upload count = %d, want only the requested file", backend.uploads.Load())
			}
			if _, err := backend.Stat(context.Background(), "unrelated.txt"); !os.IsNotExist(err) {
				t.Fatalf("unrelated pending file was uploaded: %v", err)
			}
		})
	}
}

func TestPathLookupReportsUploadFailureAndPreservesStaging(t *testing.T) {
	filesystem, backend := visibilityFixture(t)
	backend.openWriteErr = errors.New("upload failed")
	writer, err := filesystem.OpenFile("pending.txt", os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	if _, err := writer.Write([]byte("preserve me")); err != nil {
		t.Fatal(err)
	}
	if _, err := filesystem.Stat("pending.txt"); !errors.Is(err, backend.openWriteErr) {
		t.Fatalf("Stat() = %v, want upload error rather than not-exist", err)
	}
	data, err := os.ReadFile(writer.(*stagedFile).temporaryPath)
	if err != nil || string(data) != "preserve me" {
		t.Fatalf("staging = %q, err=%v", data, err)
	}
}

func TestPathLookupPublishesSubsequentWritesAndTruncate(t *testing.T) {
	filesystem, backend := visibilityFixture(t)
	writer, err := filesystem.OpenFile("pending.txt", os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	for _, content := range []string{"first", "later"} {
		if _, err := writer.WriteAt([]byte(content), 0); err != nil {
			t.Fatal(err)
		}
		reader, err := filesystem.OpenFile("pending.txt", os.O_RDONLY, 0)
		if err != nil {
			t.Fatal(err)
		}
		data, readErr := io.ReadAll(reader)
		closeErr := reader.Close()
		if string(data) != content || readErr != nil || closeErr != nil {
			t.Fatalf("read = %q, want %q, errors: %v, %v", data, content, readErr, closeErr)
		}
	}
	if err := writer.Truncate(2); err != nil {
		t.Fatal(err)
	}
	info, err := filesystem.Stat("pending.txt")
	if err != nil || info.Size() != 2 {
		t.Fatalf("truncated info = %v, error = %v", info, err)
	}
	if backend.uploads.Load() != 3 {
		t.Fatalf("upload count = %d, want 3", backend.uploads.Load())
	}
}

func TestPathLookupWaitsForCloseUpload(t *testing.T) {
	filesystem, backend := visibilityFixture(t)
	backend.entered = make(chan struct{})
	backend.release = make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(backend.release) }) }
	defer unblock()
	writer, err := filesystem.OpenFile("pending.txt", os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Write([]byte("complete")); err != nil {
		t.Fatal(err)
	}
	closed := make(chan error, 1)
	go func() { closed <- writer.Close() }()
	select {
	case <-backend.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("upload did not start")
	}
	lookedUp := make(chan error, 1)
	go func() {
		info, err := filesystem.Stat("pending.txt")
		if err == nil && info.Size() != int64(len("complete")) {
			err = errors.New("incomplete file size")
		}
		lookedUp <- err
	}()
	select {
	case err := <-lookedUp:
		unblock()
		<-closed
		t.Fatalf("lookup returned before upload finished: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	unblock()
	if err := <-closed; err != nil {
		t.Fatal(err)
	}
	if err := <-lookedUp; err != nil {
		t.Fatal(err)
	}
	if backend.uploads.Load() != 1 {
		t.Fatalf("duplicate upload count = %d", backend.uploads.Load())
	}
}
