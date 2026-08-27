//go:build windows

package mount

import (
	"errors"
	"os"
	"testing"
)

func TestSessionGuardRequiresClosedHandlesAndPublishesWrites(t *testing.T) {
	raw, backend := visibilityFixture(t)
	g := &guardedFS{FileSystem: raw}
	f, err := g.OpenFile("test.txt", os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Write([]byte("save before detach")); err != nil {
		t.Fatal(err)
	}
	if err := g.prepareStop(); err == nil {
		t.Fatal("stopped with writable handle")
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	if backend.uploads.Load() != 1 {
		t.Fatal("write not published")
	}
	if err := g.prepareStop(); err != nil {
		t.Fatal(err)
	}
	if _, err := g.OpenFile("test.txt", os.O_RDONLY, 0); !errors.Is(err, os.ErrClosed) {
		t.Fatal("open after stop", err)
	}
	if _, err := g.Stat("test.txt"); !errors.Is(err, os.ErrClosed) {
		t.Fatal("stat after stop", err)
	}
	if err := g.Mkdir("dir", 0700); !errors.Is(err, os.ErrClosed) {
		t.Fatal(err)
	}
	if err := g.Rename("a", "b"); !errors.Is(err, os.ErrClosed) {
		t.Fatal(err)
	}
	if err := g.Remove("test.txt"); !errors.Is(err, os.ErrClosed) {
		t.Fatal(err)
	}
}

func TestSessionGuardRetainsFailedUpload(t *testing.T) {
	raw, backend := visibilityFixture(t)
	backend.openWriteErr = errors.New("upload failed")
	g := &guardedFS{FileSystem: raw, cache: raw.cacheStore.Directory()}
	f, err := g.OpenFile("test.txt", os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Write([]byte("preserve")); err != nil {
		t.Fatal(err)
	}
	path := f.(*trackedFile).File.(*stagedFile).temporaryPath
	if err := f.Close(); !errors.Is(err, backend.openWriteErr) {
		t.Fatal(err)
	}
	if err := g.prepareStop(); !errors.Is(err, backend.openWriteErr) {
		t.Fatal("failed close forgotten", err)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "preserve" {
		t.Fatal("staging lost", err)
	}
	if err := f.Close(); !errors.Is(err, os.ErrClosed) {
		t.Fatal(err)
	}
	if g.open != 0 {
		t.Fatal("double close corrupted handle count")
	}
}

func TestSessionGuardTracksDirectoryHandles(t *testing.T) {
	raw, _ := visibilityFixture(t)
	g := &guardedFS{FileSystem: raw}
	f, err := g.OpenFile(".", os.O_RDONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := g.prepareStop(); err == nil {
		t.Fatal("stopped with directory handle")
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	if err := g.prepareStop(); err != nil {
		t.Fatal(err)
	}
}
