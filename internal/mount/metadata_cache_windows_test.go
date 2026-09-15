//go:build windows

package mount

import (
	"errors"
	"os"
	"testing"

	"github.com/danhk0612/DK-Drive/internal/vfs"
)

func TestCachedMetadataPublishesDirtyStagingBeforeStat(t *testing.T) {
	raw, _ := visibilityFixture(t)
	raw.backend = vfs.NewMetadataCache(raw.backend)
	writer, err := raw.OpenFile("pending.txt", os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	for _, content := range []string{"first", "a much longer update"} {
		if _, err := writer.WriteAt([]byte(content), 0); err != nil {
			t.Fatal(err)
		}
		info, err := raw.Stat("pending.txt")
		if err != nil || info.Size() != int64(len(content)) {
			t.Fatalf("stale stat: %v %v", info, err)
		}
	}
}
func TestCachedMetadataPreservesFailedUploadAndForcedDetach(t *testing.T) {
	raw, backend := visibilityFixture(t)
	raw.backend = vfs.NewMetadataCache(raw.backend)
	backend.openWriteErr = errors.New("upload failure")
	g := &guardedFS{FileSystem: raw, cache: raw.cacheStore.Directory()}
	file, err := g.OpenFile("pending.txt", os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.Write([]byte("preserve")); err != nil {
		t.Fatal(err)
	}
	local := file.(*trackedFile).File.(*stagedFile).temporaryPath
	if err := file.Close(); !errors.Is(err, backend.openWriteErr) {
		t.Fatal(err)
	}
	if err := g.forceStop(); !errors.Is(err, backend.openWriteErr) {
		t.Fatal(err)
	}
	data, err := os.ReadFile(local)
	if err != nil || string(data) != "preserve" {
		t.Fatalf("lost staging %q %v", data, err)
	}
	if backend.uploads.Load() != 1 {
		t.Fatal("unexpected retry", backend.uploads.Load())
	}
}
