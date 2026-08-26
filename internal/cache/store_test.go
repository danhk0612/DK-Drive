package cache

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNewCreatesCustomDirectory(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "nested", "cache")
	store, err := New(directory)
	if err != nil {
		t.Fatalf("New(): %v", err)
	}
	info, err := os.Stat(store.Directory())
	if err != nil {
		t.Fatalf("Stat(): %v", err)
	}
	if !info.IsDir() {
		t.Fatalf("cache path is not a directory: %s", store.Directory())
	}
}

func TestCreateStagingUsesStoreDirectory(t *testing.T) {
	store, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New(): %v", err)
	}
	file, err := store.CreateStaging()
	if err != nil {
		t.Fatalf("CreateStaging(): %v", err)
	}
	defer file.Close()
	if filepath.Dir(file.Name()) != store.Directory() {
		t.Fatalf("staging directory = %q, want %q", filepath.Dir(file.Name()), store.Directory())
	}
	if !strings.HasPrefix(filepath.Base(file.Name()), "staging-") {
		t.Fatalf("staging filename = %q", filepath.Base(file.Name()))
	}
}

func TestDefaultDirectoryUsesDKDriveCache(t *testing.T) {
	directory, err := DefaultDirectory()
	if err != nil {
		t.Fatalf("DefaultDirectory(): %v", err)
	}
	wantSuffix := filepath.Join("DKDrive", "Cache")
	if !strings.HasSuffix(directory, wantSuffix) {
		t.Fatalf("DefaultDirectory() = %q, want suffix %q", directory, wantSuffix)
	}
}
