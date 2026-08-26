package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"time"

	"golang.org/x/crypto/ssh/knownhosts"
	"golang.org/x/term"

	sftpbackend "github.com/danhk0612/DK-Drive/internal/protocol/sftp"
	"github.com/danhk0612/DK-Drive/internal/vfs"
)

func main() {
	host := flag.String("host", "", "SFTP 호스트")
	port := flag.Uint("port", 22, "SFTP 포트")
	username := flag.String("user", "", "SFTP 사용자명")
	root := flag.String("root", "/", "원격 시작 경로")
	readPath := flag.String("read", "", "내용을 표준 출력할 원격 파일 경로")
	writeTest := flag.Bool("write-test", false, "격리된 임시 폴더에서 쓰기 작업 검증")
	knownHosts := flag.String("known-hosts", defaultKnownHosts(), "known_hosts 파일")
	flag.Parse()

	password := os.Getenv("DKDRIVE_SFTP_PASSWORD")
	if password == "" {
		fmt.Fprint(os.Stderr, "SFTP 비밀번호: ")
		input, err := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Fprintln(os.Stderr)
		if err != nil {
			fatal("비밀번호 입력 실패: %v", err)
		}
		password = string(input)
		if password == "" {
			fatal("SFTP 비밀번호가 필요합니다")
		}
	}
	if *host == "" || *username == "" || *port == 0 || *port > 65535 {
		fatal("-host, -user와 올바른 -port 값이 필요합니다")
	}

	hostKeyCallback, err := knownhosts.New(*knownHosts)
	if err != nil {
		fatal("known_hosts 파일을 읽을 수 없습니다: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	backend, err := sftpbackend.New(ctx, sftpbackend.Config{
		Host:            *host,
		Port:            uint16(*port),
		Username:        *username,
		Password:        password,
		Root:            *root,
		Timeout:         10 * time.Second,
		HostKeyCallback: hostKeyCallback,
	})
	if err != nil {
		fatal("연결 실패: %v", err)
	}
	defer backend.Close()

	if *writeTest {
		if err := runWriteTest(ctx, backend); err != nil {
			fatal("쓰기 검증 실패: %v", err)
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

type writeTestBackend interface {
	Mkdir(context.Context, string) error
	OpenWrite(context.Context, string, vfs.WriteOptions) (vfs.WriteHandle, error)
	OpenRead(context.Context, string) (io.ReadCloser, error)
	Rename(context.Context, string, string) error
	SetModTime(context.Context, string, time.Time) error
	Stat(context.Context, string) (vfs.Entry, error)
	Remove(context.Context, string, bool) error
}

func runWriteTest(ctx context.Context, backend writeTestBackend) error {
	root := "DKDrive 쓰기 테스트 " + time.Now().Format("20060102-150405")
	subdirectory := path.Join(root, "이동 대상")
	original := path.Join(root, "원본 파일.txt")
	moved := path.Join(subdirectory, "이름 변경 파일.txt")
	data := []byte("DKDrive SFTP 쓰기 검증\n")

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
	syncErr := handle.Sync()
	closeErr := handle.Close()
	if err := errors.Join(writeErr, syncErr, closeErr); err != nil {
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
	wantModTime := time.Now().Add(-time.Hour).Truncate(time.Second)
	if err := backend.SetModTime(ctx, moved, wantModTime); err != nil {
		return fmt.Errorf("수정 시간 설정: %w", err)
	}
	entry, err := backend.Stat(ctx, moved)
	if err != nil {
		return fmt.Errorf("이동 파일 속성 확인: %w", err)
	}
	if entry.Size != int64(len(data)) {
		return fmt.Errorf("파일 크기 = %d, 예상값 = %d", entry.Size, len(data))
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

	fmt.Println("SFTP 생성, 쓰기, 읽기 검증, 이동, 이름 변경, 수정 시간, 삭제 통과")
	return nil
}

func defaultKnownHosts() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "known_hosts"
	}
	return filepath.Join(home, ".ssh", "known_hosts")
}

func fatal(format string, values ...any) {
	fmt.Fprintf(os.Stderr, "dkdrive-sftp: "+format+"\n", values...)
	os.Exit(1)
}
