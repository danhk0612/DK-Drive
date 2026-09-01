package cache

import (
	"encoding/json"
	"fmt"
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
