package main

import (
	"context"
	"crypto/tls"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"golang.org/x/term"

	ftpbackend "github.com/danhk0612/DK-Drive/internal/protocol/ftp"
)

func main() {
	host := flag.String("host", "", "FTP/FTPS 호스트")
	port := flag.Uint("port", 0, "FTP/FTPS 포트(기본값: FTP/Explicit 21, Implicit 990)")
	username := flag.String("user", "", "FTP/FTPS 사용자명")
	root := flag.String("root", "/", "원격 시작 경로")
	modeName := flag.String("mode", "ftp", "연결 모드: ftp, explicit-ftps, implicit-ftps")
	readPath := flag.String("read", "", "내용을 표준 출력할 원격 파일 경로")
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
