package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path"
	"slices"
	"strings"
	"time"

	"golang.org/x/term"

	webdavbackend "github.com/danhk0612/DK-Drive/internal/protocol/webdav"
	"github.com/danhk0612/DK-Drive/internal/vfs"
)

func main() {
	scheme := flag.String("scheme", "https", "WebDAV 방식: http 또는 https")
	host := flag.String("host", "", "WebDAV 호스트")
	port := flag.Uint("port", 443, "WebDAV 포트")
	username := flag.String("user", "", "WebDAV 사용자명")
	root := flag.String("root", "/", "원격 시작 경로")
	readPath := flag.String("read", "", "내용을 표준 출력할 원격 파일 경로")
	writeTest := flag.Bool("write-test", false, "격리된 임시 폴더에서 쓰기 작업 검증")
	showCapabilities := flag.Bool("capabilities", false, "서버가 광고하는 WebDAV 기능 조회")
	lockTest := flag.Bool("lock-test", false, "격리된 임시 파일에서 LOCK/UNLOCK 검증")
	flag.Parse()

	if *host == "" || *username == "" || *port == 0 || *port > 65535 {
		fatal("-host, -user와 올바른 -port 값이 필요합니다")
	}
	password := os.Getenv("DKDRIVE_WEBDAV_PASSWORD")
	if password == "" {
		password = readSecret("WebDAV 비밀번호: ")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	backend, err := webdavbackend.New(ctx, webdavbackend.Config{
		Scheme: *scheme, Host: *host, Port: uint16(*port), Username: *username,
		Password: password, Root: *root, Timeout: 10 * time.Second,
	})
	if err != nil {
		fatal("연결 실패: %v", err)
	}
	defer backend.Close()
	if *showCapabilities {
		capabilities, err := backend.Capabilities(ctx)
		if err != nil {
			fatal("기능 조회 실패: %v", err)
		}
		fmt.Printf("DAV 클래스: %s\n", strings.Join(capabilities.DAVClasses, ", "))
		methods := make([]string, 0, len(capabilities.Methods))
		for method := range capabilities.Methods {
			methods = append(methods, method)
		}
		slices.Sort(methods)
		fmt.Printf("허용 메서드: %s\n", strings.Join(methods, ", "))
		return
	}
	if *writeTest {
		if err := runWriteTest(ctx, backend); err != nil {
			fatal("쓰기 검증 실패: %v", err)
		}
		return
	}
	if *lockTest {
		if err := runLockTest(ctx, backend); err != nil {
			fatal("잠금 검증 실패: %v", err)
		}
		return
	}

	if *readPath != "" {
		file, err := backend.OpenRead(ctx, *readPath)
		if err != nil {
			fatal("파일 열기 실패: %v", err)
		}
		defer file.Close()
		if _, err := io.Copy(os.Stdout, file); err != nil {
			fatal("파일 읽기 실패: %v", err)
		}
		return
	}

	entries, err := backend.ReadDir(ctx, ".")
	if err != nil {
		fatal("디렉터리 조회 실패: %v", err)
	}
	for _, entry := range entries {
		kind := "FILE"
		if entry.IsDir() {
			kind = "DIR "
		}
		fmt.Printf("%s %12d %s %s\n", kind, entry.Size, entry.ModTime.Format(time.RFC3339), entry.Name)
	}
}

type lockTestBackend interface {
	OpenWrite(context.Context, string, vfs.WriteOptions) (vfs.WriteHandle, error)
	Lock(context.Context, string, time.Duration) (string, error)
	Unlock(context.Context, string, string) error
	Remove(context.Context, string, bool) error
}

func runLockTest(ctx context.Context, backend lockTestBackend) error {
	name := "DKDrive WebDAV 잠금 테스트 " + time.Now().Format("20060102-150405") + ".txt"
	handle, err := backend.OpenWrite(ctx, name, vfs.WriteOptions{Create: true, Truncate: true, Mode: 0o644})
	if err != nil {
		return fmt.Errorf("잠금 테스트 파일 생성: %w", err)
	}
	if _, err := handle.WriteAt([]byte("DKDrive WebDAV LOCK 검증\n"), 0); err != nil {
		handle.Close()
		return fmt.Errorf("잠금 테스트 파일 쓰기: %w", err)
	}
	if err := handle.Close(); err != nil {
		return fmt.Errorf("잠금 테스트 파일 완료: %w", err)
	}
	cleanup := true
	defer func() {
		if cleanup {
			backend.Remove(context.Background(), name, false)
		}
	}()

	token, err := backend.Lock(ctx, name, 60*time.Second)
	if err != nil {
		return err
	}
	if err := backend.Unlock(ctx, name, token); err != nil {
		return err
	}
	if err := backend.Remove(ctx, name, false); err != nil {
		return fmt.Errorf("잠금 테스트 파일 삭제: %w", err)
	}
	cleanup = false
	fmt.Println("WebDAV LOCK, UNLOCK 통과")
	return nil
}

type writeTestBackend interface {
	Mkdir(context.Context, string) error
	OpenWrite(context.Context, string, vfs.WriteOptions) (vfs.WriteHandle, error)
	OpenRead(context.Context, string) (io.ReadCloser, error)
	Rename(context.Context, string, string) error
	Remove(context.Context, string, bool) error
}

func runWriteTest(ctx context.Context, backend writeTestBackend) error {
	root := "DKDrive WebDAV 쓰기 테스트 " + time.Now().Format("20060102-150405")
	subdirectory := path.Join(root, "이동 대상")
	original := path.Join(root, "원본 파일.txt")
	moved := path.Join(subdirectory, "이름 변경 파일.txt")
	data := []byte("DKDrive WebDAV 쓰기 검증\n")

	fmt.Printf("임시 테스트 경로 생성: %s\n", root)
	if err := backend.Mkdir(ctx, root); err != nil {
		return fmt.Errorf("임시 폴더 생성: %w", err)
	}
	cleanup := true
	defer func() {
		if !cleanup {
			return
		}
		backend.Remove(context.Background(), moved, false)
		backend.Remove(context.Background(), original, false)
		backend.Remove(context.Background(), subdirectory, true)
		backend.Remove(context.Background(), root, true)
	}()
	if err := backend.Mkdir(ctx, subdirectory); err != nil {
		return fmt.Errorf("하위 폴더 생성: %w", err)
	}
	handle, err := backend.OpenWrite(ctx, original, vfs.WriteOptions{Create: true, Truncate: true, Mode: 0o644})
	if err != nil {
		return fmt.Errorf("파일 생성: %w", err)
	}
	written, writeErr := handle.WriteAt(data, 0)
	if writeErr == nil && written != len(data) {
		writeErr = io.ErrShortWrite
	}
	closeErr := handle.Close()
	if err := errors.Join(writeErr, closeErr); err != nil {
		return fmt.Errorf("파일 쓰기 완료: %w", err)
	}
	reader, err := backend.OpenRead(ctx, original)
	if err != nil {
		return fmt.Errorf("작성 파일 열기: %w", err)
	}
	readData, readErr := io.ReadAll(reader)
	closeReadErr := reader.Close()
	if err := errors.Join(readErr, closeReadErr); err != nil {
		return fmt.Errorf("작성 파일 읽기: %w", err)
	}
	if string(readData) != string(data) {
		return errors.New("작성 후 읽은 파일 내용이 일치하지 않습니다")
	}
	if err := backend.Rename(ctx, original, moved); err != nil {
		return fmt.Errorf("파일 이동 및 이름 변경: %w", err)
	}
	if err := backend.Remove(ctx, moved, false); err != nil {
		return fmt.Errorf("파일 삭제: %w", err)
	}
	if err := backend.Remove(ctx, subdirectory, true); err != nil {
		return fmt.Errorf("하위 폴더 삭제: %w", err)
	}
	if err := backend.Remove(ctx, root, true); err != nil {
		return fmt.Errorf("임시 폴더 삭제: %w", err)
	}
	cleanup = false
	fmt.Println("WebDAV 생성, 쓰기, 읽기 검증, 이동, 이름 변경, 삭제 통과")
	return nil
}

func readSecret(prompt string) string {
	fmt.Fprint(os.Stderr, prompt)
	input, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(os.Stderr)
	if err != nil {
		fatal("비밀번호 입력 실패: %v", err)
	}
	if len(input) == 0 {
		fatal("비밀번호가 필요합니다")
	}
	return string(input)
}

func fatal(format string, values ...any) {
	fmt.Fprintf(os.Stderr, "dkdrive-webdav: "+format+"\n", values...)
	os.Exit(1)
}
