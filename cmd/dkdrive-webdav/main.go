package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"golang.org/x/term"

	webdavbackend "github.com/danhk0612/DK-Drive/internal/protocol/webdav"
)

func main() {
	scheme := flag.String("scheme", "https", "WebDAV 방식: http 또는 https")
	host := flag.String("host", "", "WebDAV 호스트")
	port := flag.Uint("port", 443, "WebDAV 포트")
	username := flag.String("user", "", "WebDAV 사용자명")
	root := flag.String("root", "/", "원격 시작 경로")
	readPath := flag.String("read", "", "내용을 표준 출력할 원격 파일 경로")
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
