package cache

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	stagingPattern        = "staging-*"
	metadataSuffix        = ".recovery.json"
	MetadataVersion       = 1
	ReasonUploadFailed    = "upload_failed"
	ReasonForceDisconnect = "force_disconnect"
)

type RecoveryState string

const (
	StatePreserved       RecoveryState = "preserved"
	StateMissingMetadata RecoveryState = "missing_metadata"
	StateMissingStaging  RecoveryState = "missing_staging"
	StateInvalidMetadata RecoveryState = "invalid_metadata"
	StateUnsafeStaging   RecoveryState = "unsafe_staging"
)

type RecoveryMetadata struct {
	Version       int           `json:"version"`
	ProfileID     string        `json:"profile_id,omitempty"`
	ProfileName   string        `json:"profile_name,omitempty"`
	Protocol      string        `json:"protocol,omitempty"`
	RemotePath    string        `json:"remote_path"`
	StagingPath   string        `json:"staging_path"`
	Size          int64         `json:"size"`
	CreatedAt     time.Time     `json:"created_at"`
	UpdatedAt     time.Time     `json:"updated_at"`
	PreservedAt   time.Time     `json:"preserved_at"`
	Reason        string        `json:"reason"`
	LastError     string        `json:"last_error,omitempty"`
	RecoveryState RecoveryState `json:"recovery_state"`
}

type Preservation struct {
	ProfileID   string
	ProfileName string
	Protocol    string
	RemotePath  string
	StagingPath string
	CreatedAt   time.Time
	Reason      string
	LastError   error
}

type RecoveryItem struct {
	Metadata     RecoveryMetadata
	MetadataPath string
	Problem      string
}

type Store struct {
	directory string
}

func New(directory string) (*Store, error) {
	if strings.TrimSpace(directory) == "" {
		var err error
		directory, err = DefaultDirectory()
		if err != nil {
			return nil, err
		}
	}
	directory, err := filepath.Abs(directory)
	if err != nil {
		return nil, fmt.Errorf("캐시 경로 확인 실패: %w", err)
	}
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return nil, fmt.Errorf("캐시 폴더 생성 실패: %w", err)
	}
	return &Store{directory: directory}, nil
}

func DefaultDirectory() (string, error) {
	root, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("사용자 캐시 경로 확인 실패: %w", err)
	}
	return filepath.Join(root, "DKDrive", "Cache"), nil
}

func (store *Store) Directory() string {
	return store.directory
}

// Clear removes every cache entry while preserving the cache directory itself.
// Callers must ensure that no active mount is using the store.
func (store *Store) Clear() (int, error) {
	info, err := os.Lstat(store.directory)
	if err != nil {
		return 0, fmt.Errorf("캐시 폴더 확인 실패: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return 0, fmt.Errorf("캐시 경로가 안전한 일반 폴더가 아닙니다: %s", store.directory)
	}
	entries, err := os.ReadDir(store.directory)
	if err != nil {
		return 0, fmt.Errorf("캐시 폴더 읽기 실패: %w", err)
	}
	removed := 0
	var result error
	for _, entry := range entries {
		target := filepath.Join(store.directory, entry.Name())
		if err := os.RemoveAll(target); err != nil {
			result = errors.Join(result, fmt.Errorf("%s: %w", entry.Name(), err))
			continue
		}
		removed++
	}
	if result != nil {
		return removed, fmt.Errorf("일부 캐시 항목을 삭제하지 못했습니다: %w", result)
	}
	return removed, nil
}

func (store *Store) CreateStaging() (*os.File, error) {
	file, err := os.CreateTemp(store.directory, stagingPattern)
	if err != nil {
		return nil, fmt.Errorf("캐시 임시 파일 생성 실패: %w", err)
	}
	return file, nil
}

func (store *Store) Preserve(value Preservation) (RecoveryItem, error) {
	stagingPath, err := store.safeStagingPath(value.StagingPath)
	if err != nil {
		return RecoveryItem{}, err
	}
	info, err := os.Lstat(stagingPath)
	if err != nil {
		return RecoveryItem{}, fmt.Errorf("보존 캐시 파일 확인 실패: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return RecoveryItem{}, fmt.Errorf("일반 파일이 아닌 캐시는 보존 항목으로 사용할 수 없습니다: %s", stagingPath)
	}
	now := time.Now().UTC()
	createdAt := value.CreatedAt.UTC()
	if createdAt.IsZero() {
		createdAt = info.ModTime().UTC()
	}
	metadata := RecoveryMetadata{
		Version: MetadataVersion, ProfileID: value.ProfileID,
		ProfileName: value.ProfileName, Protocol: value.Protocol,
		RemotePath: value.RemotePath, StagingPath: stagingPath,
		Size: info.Size(), CreatedAt: createdAt, UpdatedAt: info.ModTime().UTC(),
		PreservedAt: now, Reason: value.Reason, RecoveryState: StatePreserved,
	}
	if value.LastError != nil {
		metadata.LastError = value.LastError.Error()
	}
	metadataPath := stagingPath + metadataSuffix
	if err := store.writeMetadataAtomic(metadataPath, metadata); err != nil {
		return RecoveryItem{}, err
	}
	return RecoveryItem{Metadata: metadata, MetadataPath: metadataPath}, nil
}

func (store *Store) Scan() ([]RecoveryItem, error) {
	entries, err := os.ReadDir(store.directory)
	if err != nil {
		return nil, fmt.Errorf("캐시 폴더 검사 실패: %w", err)
	}
	items := make([]RecoveryItem, 0)
	seenStaging := make(map[string]bool)
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), metadataSuffix) {
			continue
		}
		metadataPath := filepath.Join(store.directory, entry.Name())
		expectedStaging := strings.TrimSuffix(metadataPath, metadataSuffix)
		seenStaging[expectedStaging] = true
		items = append(items, store.scanMetadata(metadataPath, expectedStaging))
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasPrefix(entry.Name(), "staging-") || strings.HasSuffix(entry.Name(), metadataSuffix) {
			continue
		}
		stagingPath := filepath.Join(store.directory, entry.Name())
		if seenStaging[stagingPath] {
			continue
		}
		info, infoErr := entry.Info()
		item := RecoveryItem{Metadata: RecoveryMetadata{
			Version: MetadataVersion, StagingPath: stagingPath,
			RecoveryState: StateMissingMetadata,
		}}
		if entry.Type()&os.ModeSymlink != 0 {
			item.Metadata.RecoveryState = StateUnsafeStaging
			item.Problem = "심볼릭 링크 캐시는 복구 대상으로 사용할 수 없습니다"
		} else if infoErr != nil {
			item.Problem = infoErr.Error()
		} else {
			item.Metadata.Size = info.Size()
			item.Metadata.CreatedAt = info.ModTime().UTC()
			item.Metadata.UpdatedAt = info.ModTime().UTC()
		}
		items = append(items, item)
	}
	return items, nil
}

func (store *Store) Export(item RecoveryItem, destination string) error {
	if item.Metadata.RecoveryState != StatePreserved && item.Metadata.RecoveryState != StateMissingMetadata {
		return fmt.Errorf("현재 복구 상태에서는 로컬 내보내기를 할 수 없습니다: %s", item.Metadata.RecoveryState)
	}
	sourcePath, err := store.safeStagingPath(item.Metadata.StagingPath)
	if err != nil {
		return err
	}
	sourceInfo, err := os.Lstat(sourcePath)
	if err != nil {
		return fmt.Errorf("내보낼 캐시 파일 확인 실패: %w", err)
	}
	if sourceInfo.Mode()&os.ModeSymlink != 0 || !sourceInfo.Mode().IsRegular() {
		return fmt.Errorf("일반 파일이 아닌 캐시는 내보낼 수 없습니다: %s", sourcePath)
	}
	destinationPath, err := filepath.Abs(destination)
	if err != nil {
		return fmt.Errorf("내보낼 위치 확인 실패: %w", err)
	}
	if store.contains(destinationPath) {
		return errors.New("보존 캐시 폴더 안으로는 내보낼 수 없습니다")
	}
	if destinationInfo, statErr := os.Lstat(destinationPath); statErr == nil {
		if destinationInfo.Mode()&os.ModeSymlink != 0 || os.SameFile(sourceInfo, destinationInfo) {
			return errors.New("캐시 원본 또는 심볼릭 링크에는 내보낼 수 없습니다")
		}
	} else if !os.IsNotExist(statErr) {
		return fmt.Errorf("내보낼 파일 확인 실패: %w", statErr)
	}
	source, err := os.Open(sourcePath)
	if err != nil {
		return fmt.Errorf("보존 캐시 열기 실패: %w", err)
	}
	destinationFile, err := os.OpenFile(destinationPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		_ = source.Close()
		return fmt.Errorf("내보낼 파일 생성 실패: %w", err)
	}
	_, copyErr := io.Copy(destinationFile, source)
	syncErr := destinationFile.Sync()
	closeErr := destinationFile.Close()
	sourceCloseErr := source.Close()
	if err := errors.Join(copyErr, syncErr, closeErr, sourceCloseErr); err != nil {
		return fmt.Errorf("보존 캐시 내보내기 실패: %w", err)
	}
	return nil
}

// OpenRecovery opens a scanned recovery item's staging file after repeating
// the same path and file-type checks used by export and delete operations.
func (store *Store) OpenRecovery(item RecoveryItem) (*os.File, os.FileInfo, error) {
	if item.Metadata.RecoveryState != StatePreserved {
		return nil, nil, fmt.Errorf("현재 복구 상태에서는 원격 재시도를 할 수 없습니다: %s", item.Metadata.RecoveryState)
	}
	stagingPath, err := store.safeStagingPath(item.Metadata.StagingPath)
	if err != nil {
		return nil, nil, err
	}
	info, err := os.Lstat(stagingPath)
	if err != nil {
		return nil, nil, fmt.Errorf("재시도할 캐시 파일 확인 실패: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return nil, nil, fmt.Errorf("일반 파일이 아닌 캐시는 원격 재시도에 사용할 수 없습니다: %s", stagingPath)
	}
	file, err := os.Open(stagingPath)
	if err != nil {
		return nil, nil, fmt.Errorf("재시도할 캐시 파일 열기 실패: %w", err)
	}
	return file, info, nil
}

// Delete removes one scanned recovery item. Callers must ensure that no active
// mount is using the store because metadata-less staging files can be live.
func (store *Store) Delete(item RecoveryItem) error {
	switch item.Metadata.RecoveryState {
	case StatePreserved, StateMissingMetadata, StateMissingStaging, StateInvalidMetadata, StateUnsafeStaging:
	default:
		return fmt.Errorf("삭제할 수 없는 복구 상태입니다: %s", item.Metadata.RecoveryState)
	}

	stagingPath, err := store.safeStagingPath(item.Metadata.StagingPath)
	if err != nil {
		return err
	}
	expectedMetadata := stagingPath + metadataSuffix
	metadataPath := ""
	if item.MetadataPath != "" {
		metadataPath, err = filepath.Abs(item.MetadataPath)
		if err != nil {
			return fmt.Errorf("복구 메타데이터 경로 확인 실패: %w", err)
		}
		if metadataPath != expectedMetadata {
			return errors.New("선택 항목의 캐시 파일과 메타데이터 경로가 일치하지 않습니다")
		}
	}

	if err := removeRecoveryFile(stagingPath); err != nil {
		return fmt.Errorf("보존 캐시 파일 삭제 실패: %w", err)
	}
	if metadataPath != "" {
		if err := removeRecoveryFile(metadataPath); err != nil {
			return fmt.Errorf("복구 메타데이터 삭제 실패: %w", err)
		}
	}
	return nil
}

func removeRecoveryFile(filename string) error {
	info, err := os.Lstat(filename)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.IsDir() {
		return fmt.Errorf("디렉터리는 복구 항목으로 삭제할 수 없습니다: %s", filename)
	}
	return os.Remove(filename)
}

func (store *Store) scanMetadata(metadataPath, expectedStaging string) RecoveryItem {
	item := RecoveryItem{MetadataPath: metadataPath}
	data, err := os.ReadFile(metadataPath)
	if err != nil {
		item.Metadata = RecoveryMetadata{Version: MetadataVersion, StagingPath: expectedStaging, RecoveryState: StateInvalidMetadata}
		item.Problem = err.Error()
		return item
	}
	if err := json.Unmarshal(data, &item.Metadata); err != nil {
		item.Metadata = RecoveryMetadata{Version: MetadataVersion, StagingPath: expectedStaging, RecoveryState: StateInvalidMetadata}
		item.Problem = "메타데이터 JSON 오류: " + err.Error()
		return item
	}
	actualStaging, err := store.safeStagingPath(item.Metadata.StagingPath)
	if err != nil || actualStaging != expectedStaging || item.Metadata.Version != MetadataVersion || item.Metadata.RecoveryState != StatePreserved {
		item.Metadata.StagingPath = expectedStaging
		item.Metadata.RecoveryState = StateInvalidMetadata
		switch {
		case err != nil:
			item.Problem = err.Error()
		case actualStaging != expectedStaging:
			item.Problem = "메타데이터와 연결된 캐시 파일 경로가 일치하지 않습니다"
		case item.Metadata.Version != MetadataVersion:
			item.Problem = fmt.Sprintf("지원하지 않는 캐시 메타데이터 버전입니다: %d", item.Metadata.Version)
		default:
			item.Problem = fmt.Sprintf("지원하지 않는 복구 상태입니다: %q", item.Metadata.RecoveryState)
		}
		return item
	}
	info, err := os.Lstat(expectedStaging)
	if err != nil {
		if os.IsNotExist(err) {
			item.Metadata.RecoveryState = StateMissingStaging
			item.Problem = "메타데이터에 연결된 캐시 파일이 없습니다"
		} else {
			item.Metadata.RecoveryState = StateInvalidMetadata
			item.Problem = err.Error()
		}
		return item
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		item.Metadata.RecoveryState = StateUnsafeStaging
		item.Problem = "일반 파일이 아닌 캐시는 복구 대상으로 사용할 수 없습니다"
		return item
	}
	item.Metadata.Size = info.Size()
	return item
}

func (store *Store) writeMetadataAtomic(destination string, metadata RecoveryMetadata) error {
	temporary, err := os.CreateTemp(store.directory, ".recovery-metadata-*.tmp")
	if err != nil {
		return fmt.Errorf("복구 메타데이터 임시 파일 생성 실패: %w", err)
	}
	temporaryPath := temporary.Name()
	removeTemporary := true
	defer func() {
		if removeTemporary {
			_ = os.Remove(temporaryPath)
		}
	}()
	encoder := json.NewEncoder(temporary)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(metadata); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("복구 메타데이터 작성 실패: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("복구 메타데이터 동기화 실패: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("복구 메타데이터 닫기 실패: %w", err)
	}
	if err := os.Rename(temporaryPath, destination); err != nil {
		return fmt.Errorf("복구 메타데이터 반영 실패: %w", err)
	}
	removeTemporary = false
	return nil
}

func (store *Store) safeStagingPath(value string) (string, error) {
	absolute, err := filepath.Abs(value)
	if err != nil {
		return "", fmt.Errorf("보존 캐시 경로 확인 실패: %w", err)
	}
	relative, err := filepath.Rel(store.directory, absolute)
	if err != nil || relative == "." || filepath.IsAbs(relative) || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.Dir(relative) != "." || !strings.HasPrefix(filepath.Base(relative), "staging-") {
		return "", fmt.Errorf("캐시 루트 밖의 스테이징 경로는 사용할 수 없습니다: %s", value)
	}
	return filepath.Join(store.directory, relative), nil
}

func (store *Store) contains(value string) bool {
	relative, err := filepath.Rel(store.directory, value)
	return err == nil && !filepath.IsAbs(relative) && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}
