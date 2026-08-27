//go:build windows

package mount

import (
	"errors"
	"io"
	"os"
	"path/filepath"
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
	if err := g.forceStop(); !errors.Is(err, backend.openWriteErr) {
		t.Fatal("lost previous upload warning", err)
	}
	data, err = os.ReadFile(path)
	if err != nil || string(data) != "preserve" {
		t.Fatal("force deleted failed upload staging", err)
	}
	if backend.uploads.Load() != 1 {
		t.Fatal("force retried failed upload")
	}
}

func TestForceStopPreservesPendingWriteWithoutUpload(t *testing.T) {
	raw, backend := visibilityFixture(t)
	g := &guardedFS{FileSystem: raw, cache: raw.cacheStore.Directory()}
	f, err := g.OpenFile("pending.txt", os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Write([]byte("not uploaded")); err != nil {
		t.Fatal(err)
	}
	staged := f.(*trackedFile).File.(*stagedFile)
	if err := g.forceStop(); err != nil {
		t.Fatal(err)
	}
	if backend.uploads.Load() != 0 {
		t.Fatal("force uploaded data")
	}
	data, err := os.ReadFile(staged.temporaryPath)
	if err != nil || string(data) != "not uploaded" {
		t.Fatal("lost staging", string(data), err)
	}
	if _, err := os.Stat(filepath.Join(backend.root, "pending.txt")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("remote file changed", err)
	}
	if _, err := f.Write([]byte("late")); !errors.Is(err, os.ErrClosed) {
		t.Fatal("write after forced close", err)
	}
	if err := f.Sync(); !errors.Is(err, os.ErrClosed) {
		t.Fatal("sync after forced close", err)
	}
	if err := f.Close(); !errors.Is(err, os.ErrClosed) {
		t.Fatal("duplicate close", err)
	}
	if g.open != 0 || len(g.files) != 0 {
		t.Fatal("leaked tracked handles")
	}
	if _, err := g.OpenFile("pending.txt", os.O_RDONLY, 0); !errors.Is(err, os.ErrClosed) {
		t.Fatal(err)
	}
	if err := g.forceStop(); err != nil {
		t.Fatal("force not idempotent", err)
	}
	if _, err := os.Stat(staged.temporaryPath); err != nil {
		t.Fatal("duplicate close removed staging", err)
	}
}

func TestForceStopReadAndDirectoryHandles(t *testing.T) {
	raw, backend := visibilityFixture(t)
	if err := os.WriteFile(filepath.Join(backend.root, "original.txt"), []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	g := &guardedFS{FileSystem: raw}
	f, err := g.OpenFile("original.txt", os.O_RDONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	d, err := g.OpenFile(".", os.O_RDONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := g.forceStop(); err != nil {
		t.Fatal(err)
	}
	if _, err := io.ReadAll(f); !errors.Is(err, os.ErrClosed) {
		t.Fatal("old reader still usable", err)
	}
	if err := d.Close(); !errors.Is(err, os.ErrClosed) {
		t.Fatal(err)
	}
	if backend.uploads.Load() != 0 || g.open != 0 {
		t.Fatal("force wrote to backend or leaked handles")
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
