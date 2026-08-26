package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/crypto/ssh/knownhosts"
	"golang.org/x/term"

	sftpbackend "github.com/danhk0612/DK-Drive/internal/protocol/sftp"
)

func main() {
	host := flag.String("host", "", "SFTP 호스트")
	port := flag.Uint("port", 22, "SFTP 포트")
	username := flag.String("user", "", "SFTP 사용자명")
	root := flag.String("root", "/", "원격 시작 경로")
	readPath := flag.String("read", "", "내용을 표준 출력할 원격 파일 경로")
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

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
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
