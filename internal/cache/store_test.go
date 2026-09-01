package cache

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
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

func TestPreserveWritesVersionedMetadataAtomically(t *testing.T) {
	store, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	file, err := store.CreateStaging()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString("preserved data"); err != nil {
		t.Fatal(err)
	}
	createdAt := time.Date(2026, 9, 1, 1, 2, 3, 0, time.UTC)
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	item, err := store.Preserve(Preservation{
		ProfileID: "profile-id", ProfileName: "NAS 연결", Protocol: "webdav",
		RemotePath: "/home/한글 파일.txt", StagingPath: file.Name(),
		CreatedAt: createdAt, Reason: "upload_failed", LastError: os.ErrPermission,
	})
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(item.MetadataPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "password") || strings.Contains(string(data), "passphrase") {
		t.Fatalf("secret field written: %s", data)
	}
	var metadata RecoveryMetadata
	if err := json.Unmarshal(data, &metadata); err != nil {
		t.Fatalf("metadata is partial JSON: %v", err)
	}
	if metadata.Version != MetadataVersion || metadata.ProfileID != "profile-id" || metadata.RemotePath != "/home/한글 파일.txt" || metadata.Size != int64(len("preserved data")) || metadata.Reason != "upload_failed" || metadata.RecoveryState != StatePreserved {
		t.Fatalf("metadata = %+v", metadata)
	}
	if !metadata.CreatedAt.Equal(createdAt) || metadata.LastError == "" {
		t.Fatalf("metadata times/error = %+v", metadata)
	}
	entries, err := os.ReadDir(store.Directory())
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".recovery-metadata-") {
			t.Fatalf("atomic temporary file left behind: %s", entry.Name())
		}
	}
}

func TestScanFindsPreservedAndOrphanedEntries(t *testing.T) {
	store, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	preserved, err := store.CreateStaging()
	if err != nil {
		t.Fatal(err)
	}
	if err := preserved.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Preserve(Preservation{StagingPath: preserved.Name(), RemotePath: "/saved.txt", Reason: "force_disconnect"}); err != nil {
		t.Fatal(err)
	}
	orphan, err := store.CreateStaging()
	if err != nil {
		t.Fatal(err)
	}
	if err := orphan.Close(); err != nil {
		t.Fatal(err)
	}
	items, err := store.Scan()
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("items = %+v", items)
	}
	states := map[RecoveryState]bool{}
	for _, item := range items {
		states[item.Metadata.RecoveryState] = true
	}
	if !states[StatePreserved] || !states[StateMissingMetadata] {
		t.Fatalf("states = %+v", states)
	}
}

func TestScanReportsMetadataWithoutStaging(t *testing.T) {
	store, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	file, err := store.CreateStaging()
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	item, err := store.Preserve(Preservation{StagingPath: file.Name(), RemotePath: "/gone.txt", Reason: "upload_failed"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(file.Name()); err != nil {
		t.Fatal(err)
	}
	items, err := store.Scan()
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].MetadataPath != item.MetadataPath || items[0].Metadata.RecoveryState != StateMissingStaging {
		t.Fatalf("items = %+v", items)
	}
}

func TestScanRejectsMetadataPathOutsideCache(t *testing.T) {
	root := t.TempDir()
	store, err := New(filepath.Join(root, "cache"))
	if err != nil {
		t.Fatal(err)
	}
	file, err := store.CreateStaging()
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	metadata := RecoveryMetadata{
		Version: MetadataVersion, RemotePath: "/remote.txt",
		StagingPath: filepath.Join(root, "outside.txt"), RecoveryState: StatePreserved,
	}
	if err := store.writeMetadataAtomic(file.Name()+metadataSuffix, metadata); err != nil {
		t.Fatal(err)
	}
	items, err := store.Scan()
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Metadata.RecoveryState != StateInvalidMetadata || items[0].Problem == "" {
		t.Fatalf("items = %+v", items)
	}
	if items[0].Metadata.StagingPath != file.Name() {
		t.Fatalf("untrusted path retained: %q", items[0].Metadata.StagingPath)
	}
}

func TestScanDoesNotFollowStagingSymlink(t *testing.T) {
	root := t.TempDir()
	store, err := New(filepath.Join(root, "cache"))
	if err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(root, "outside.txt")
	if err := os.WriteFile(outside, []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(store.Directory(), "staging-link")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	items, err := store.Scan()
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Metadata.RecoveryState != StateUnsafeStaging || items[0].Problem == "" {
		t.Fatalf("items = %+v", items)
	}
}

func TestExportCopiesBytesAndPreservesRecoveryItem(t *testing.T) {
	store, err := New(filepath.Join(t.TempDir(), "cache"))
	if err != nil {
		t.Fatal(err)
	}
	file, err := store.CreateStaging()
	if err != nil {
		t.Fatal(err)
	}
	want := []byte("한글\x00binary\xff")
	if _, err := file.Write(want); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	item, err := store.Preserve(Preservation{StagingPath: file.Name(), RemotePath: "/한글 파일.bin", Reason: ReasonUploadFailed})
	if err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(filepath.Dir(store.Directory()), "내보낸 파일.bin")
	if err := store.Export(item, destination); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(destination)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Fatalf("exported bytes = %q, want %q", got, want)
	}
	if _, err := os.Stat(file.Name()); err != nil {
		t.Fatalf("staging removed: %v", err)
	}
	if _, err := os.Stat(item.MetadataPath); err != nil {
		t.Fatalf("metadata removed: %v", err)
	}
}

func TestExportRejectsCacheDestinationAndUnavailableState(t *testing.T) {
	root := t.TempDir()
	store, err := New(filepath.Join(root, "cache"))
	if err != nil {
		t.Fatal(err)
	}
	file, err := store.CreateStaging()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString("keep"); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	item := RecoveryItem{Metadata: RecoveryMetadata{StagingPath: file.Name(), RecoveryState: StateMissingMetadata}}
	if err := store.Export(item, filepath.Join(store.Directory(), "copy.txt")); err == nil {
		t.Fatal("export inside cache accepted")
	}
	item.Metadata.RecoveryState = StateInvalidMetadata
	if err := store.Export(item, filepath.Join(root, "copy.txt")); err == nil {
		t.Fatal("invalid recovery item exported")
	}
	got, err := os.ReadFile(file.Name())
	if err != nil || string(got) != "keep" {
		t.Fatalf("source changed: %q, %v", got, err)
	}
}
