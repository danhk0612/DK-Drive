package recovery

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"strings"
	"time"

	localcache "github.com/danhk0612/DK-Drive/internal/cache"
	"github.com/danhk0612/DK-Drive/internal/vfs"
)

const timestampTolerance = 2 * time.Second

type RemoteState uint8

const (
	RemoteMissing RemoteState = iota
	RemoteSame
	RemoteConflict
)

// RelativePath converts the absolute display path stored in recovery metadata
// back to the path expected by a backend rooted at profileRoot.
func RelativePath(profileRoot, preservedPath string) (string, error) {
	root := cleanAbsolute(profileRoot)
	preserved := cleanAbsolute(preservedPath)
	if preserved == "/" {
		return "", errors.New("복구 대상 원격 경로가 파일을 가리키지 않습니다")
	}
	if root != "/" && preserved != root && !strings.HasPrefix(preserved, root+"/") {
		return "", fmt.Errorf("보존된 원격 경로가 선택한 프로필 루트 밖에 있습니다: %s", preservedPath)
	}
	relative := strings.TrimPrefix(preserved, root)
	relative = strings.TrimPrefix(relative, "/")
	if relative == "" || relative == "." {
		return "", errors.New("복구 대상 원격 경로가 파일을 가리키지 않습니다")
	}
	return relative, nil
}

func cleanAbsolute(value string) string {
	value = strings.ReplaceAll(value, "\\", "/")
	return path.Clean("/" + strings.TrimPrefix(value, "/"))
}

// Inspect compares size and modification time. Missing or imprecise timestamps
// are treated as a conflict rather than permission to overwrite.
func Inspect(ctx context.Context, store *localcache.Store, item localcache.RecoveryItem, backend vfs.Backend, remotePath string) (RemoteState, vfs.Entry, error) {
	file, localInfo, err := store.OpenRecovery(item)
	if err != nil {
		return RemoteConflict, vfs.Entry{}, err
	}
	if err := file.Close(); err != nil {
		return RemoteConflict, vfs.Entry{}, fmt.Errorf("재시도할 캐시 파일 닫기 실패: %w", err)
	}
	entry, err := backend.Stat(ctx, remotePath)
	if errors.Is(err, fs.ErrNotExist) || errors.Is(err, os.ErrNotExist) {
		return RemoteMissing, vfs.Entry{}, nil
	}
	if err != nil {
		return RemoteConflict, vfs.Entry{}, fmt.Errorf("원격 파일 확인 실패: %w", err)
	}
	if entry.IsDir() {
		return RemoteConflict, entry, errors.New("복구 대상 원격 경로에 폴더가 있습니다")
	}
	localTime := item.Metadata.UpdatedAt
	if localTime.IsZero() {
		localTime = localInfo.ModTime()
	}
	if entry.Size == localInfo.Size() && !localTime.IsZero() && !entry.ModTime.IsZero() && withinTolerance(localTime, entry.ModTime) {
		return RemoteSame, entry, nil
	}
	return RemoteConflict, entry, nil
}

func withinTolerance(left, right time.Time) bool {
	difference := left.Sub(right)
	if difference < 0 {
		difference = -difference
	}
	return difference <= timestampTolerance
}

// Upload publishes the preserved bytes and confirms the resulting remote size.
// It never deletes the recovery item.
func Upload(ctx context.Context, store *localcache.Store, item localcache.RecoveryItem, backend vfs.Backend, remotePath string) (vfs.Entry, error) {
	file, info, err := store.OpenRecovery(item)
	if err != nil {
		return vfs.Entry{}, err
	}
	defer file.Close()

	writer, err := backend.OpenWrite(ctx, remotePath, vfs.WriteOptions{Create: true, Truncate: true, Mode: info.Mode()})
	if err != nil {
		return vfs.Entry{}, fmt.Errorf("원격 재시도 시작 실패: %w", err)
	}
	buffer := make([]byte, 256*1024)
	var offset int64
	for offset < info.Size() {
		want := int64(len(buffer))
		if remaining := info.Size() - offset; remaining < want {
			want = remaining
		}
		read, readErr := file.ReadAt(buffer[:want], offset)
		if errors.Is(readErr, io.EOF) && int64(read) != want {
			return vfs.Entry{}, errors.Join(fmt.Errorf("보존 캐시 읽기 실패: %w", io.ErrUnexpectedEOF), writer.Close())
		}
		if readErr != nil {
			return vfs.Entry{}, errors.Join(fmt.Errorf("보존 캐시 읽기 실패: %w", readErr), writer.Close())
		}
		written, writeErr := writer.WriteAt(buffer[:read], offset)
		if writeErr != nil {
			return vfs.Entry{}, errors.Join(fmt.Errorf("원격 재시도 쓰기 실패: %w", writeErr), writer.Close())
		}
		if written != read {
			return vfs.Entry{}, errors.Join(io.ErrShortWrite, writer.Close())
		}
		offset += int64(read)
	}
	if err := errors.Join(writer.Sync(), writer.Close()); err != nil {
		return vfs.Entry{}, fmt.Errorf("원격 재시도 반영 실패: %w", err)
	}
	entry, err := backend.Stat(ctx, remotePath)
	if err != nil {
		return vfs.Entry{}, fmt.Errorf("재시도 결과 확인 실패: %w", err)
	}
	if entry.IsDir() || entry.Size != info.Size() {
		return entry, fmt.Errorf("재시도 결과 크기가 일치하지 않습니다: 로컬 %d B, 원격 %d B", info.Size(), entry.Size)
	}
	return entry, nil
}

func AlternatePath(remotePath string, at time.Time) string {
	remotePath = strings.ReplaceAll(remotePath, "\\", "/")
	directory, filename := path.Split(remotePath)
	extension := path.Ext(filename)
	base := strings.TrimSuffix(filename, extension)
	if base == "" {
		base = "recovered"
	}
	return path.Join(directory, base+".recovered-"+at.Format("20060102-150405")+extension)
}
