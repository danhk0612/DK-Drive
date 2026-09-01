package main

import (
	"context"
	"crypto/tls"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path"
	"time"

	"golang.org/x/term"

	ftpbackend "github.com/danhk0612/DK-Drive/internal/protocol/ftp"
	"github.com/danhk0612/DK-Drive/internal/vfs"
)

func main() {
	host := flag.String("host", "", "FTP/FTPS 호스트")
	port := flag.Uint("port", 0, "FTP/FTPS 포트(기본값: FTP/Explicit 21, Implicit 990)")
	username := flag.String("user", "", "FTP/FTPS 사용자명")
	root := flag.String("root", "/", "원격 시작 경로")
	modeName := flag.String("mode", "ftp", "연결 모드: ftp, explicit-ftps, implicit-ftps")
	readPath := flag.String("read", "", "내용을 표준 출력할 원격 파일 경로")
	writeTest := flag.Bool("write-test", false, "격리된 임시 폴더에서 쓰기 작업 검증")
	insecureSkipTLSVerify := flag.Bool("insecure-skip-tls-verify", false, "FTPS 인증서 검증 건너뛰기(신뢰할 수 있는 테스트 서버 전용)")
	flag.Parse()

	mode, defaultPort, err := parseMode(*modeName)
	if err != nil {
		fatal("%v", err)
	}
	selectedPort := *port
	if selectedPort == 0 {
		selectedPort = uint(defaultPort)
	}
	if *host == "" || *username == "" || selectedPort > 65535 {
		fatal("-host, -user와 올바른 -port 값이 필요합니다")
	}
	if mode == ftpbackend.TLSNone && *insecureSkipTLSVerify {
		fatal("-insecure-skip-tls-verify는 FTPS 모드에서만 사용할 수 있습니다")
	}

	password := os.Getenv("DKDRIVE_FTP_PASSWORD")
	if password == "" {
		password = readSecret("FTP 비밀번호: ")
	}
	var tlsConfig *tls.Config
	if mode != ftpbackend.TLSNone {
		tlsConfig = &tls.Config{InsecureSkipVerify: *insecureSkipTLSVerify}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	backend, err := ftpbackend.New(ctx, ftpbackend.Config{
		Host:      *host,
		Port:      uint16(selectedPort),
		Username:  *username,
		Password:  password,
		Root:      *root,
		Timeout:   10 * time.Second,
		TLSMode:   mode,
		TLSConfig: tlsConfig,
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
		_, copyErr := io.Copy(os.Stdout, file)
		closeErr := file.Close()
		if err := errors.Join(copyErr, closeErr); err != nil {
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
	Stat(context.Context, string) (vfs.Entry, error)
	Remove(context.Context, string, bool) error
}

func runWriteTest(ctx context.Context, backend writeTestBackend) error {
	root := "DK-Drive FTP 쓰기 테스트 " + time.Now().Format("20060102-150405")
	subdirectory := path.Join(root, "이동 대상")
	original := path.Join(root, "원본 파일.txt")
	moved := path.Join(subdirectory, "이름 변경 파일.txt")
	data := []byte("DK-Drive FTP 쓰기 검증\n")

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

	fmt.Println("FTP 생성, 쓰기, 읽기 검증, 이동, 이름 변경, 삭제 통과")
	return nil
}

func parseMode(value string) (ftpbackend.TLSMode, uint16, error) {
	switch value {
	case "ftp":
		return ftpbackend.TLSNone, 21, nil
	case "explicit-ftps":
		return ftpbackend.TLSExplicit, 21, nil
	case "implicit-ftps":
		return ftpbackend.TLSImplicit, 990, nil
	default:
		return "", 0, fmt.Errorf("지원하지 않는 연결 모드입니다: %q", value)
	}
}

func readSecret(prompt string) string {
	fmt.Fprint(os.Stderr, prompt)
	input, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(os.Stderr)
	if err != nil {
		fatal("비밀번호 입력 실패: %v", err)
	}
	if len(input) == 0 {
		fatal("FTP 비밀번호가 필요합니다")
	}
	return string(input)
}

func fatal(format string, values ...any) {
	fmt.Fprintf(os.Stderr, "dkdrive-ftp: "+format+"\n", values...)
	os.Exit(1)
}
