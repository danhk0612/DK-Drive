package cache

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const stagingPattern = "staging-*"

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
