//go:build windows

package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"time"

	"github.com/winfsp/go-winfsp"
	"github.com/winfsp/go-winfsp/gofs"
	"golang.org/x/crypto/ssh/knownhosts"
	"golang.org/x/term"

	"github.com/danhk0612/DK-Drive/internal/mount"
	sftpbackend "github.com/danhk0612/DK-Drive/internal/protocol/sftp"
)

func main() {
	host := flag.String("host", "", "SFTP 호스트")
	port := flag.Uint("port", 22, "SFTP 포트")
	username := flag.String("user", "", "SFTP 사용자명")
	root := flag.String("root", "/", "원격 시작 경로")
	mountpoint := flag.String("mount", "X:", "마운트할 드라이브 문자(예: X:)")
	knownHosts := flag.String("known-hosts", defaultKnownHosts(), "known_hosts 파일")
	flag.Parse()

	if !validMountpoint(*mountpoint) {
		fatal("드라이브 문자는 X: 형식이어야 합니다")
	}
	if *host == "" || *username == "" || *port == 0 || *port > 65535 {
		fatal("-host, -user와 올바른 -port 값이 필요합니다")
	}
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

	hostKeyCallback, err := knownhosts.New(*knownHosts)
	if err != nil {
		fatal("known_hosts 파일을 읽을 수 없습니다: %v", err)
	}
	connectCtx, cancelConnect := context.WithTimeout(context.Background(), 30*time.Second)
	backend, err := sftpbackend.New(connectCtx, sftpbackend.Config{
		Host: *host, Port: uint16(*port), Username: *username, Password: password,
		Root: *root, Timeout: 10 * time.Second, HostKeyCallback: hostKeyCallback,
	})
	cancelConnect()
	if err != nil {
		fatal("연결 실패: %v", err)
	}
	defer backend.Close()

	behaviour, err := gofs.NewOptions(
		mount.NewGoFileSystem(backend),
		gofs.WithCaseInsensitive(true),
		gofs.WithAttribReadOnlyTransMode(gofs.AttribReadOnlyBypass),
	)
	if err != nil {
		fatal("WinFsp 파일시스템 구성 실패: %v", err)
	}
	letter := strings.ToUpper(*mountpoint)
	filesystem, err := winfsp.Mount(behaviour, letter)
	if err != nil {
		fatal("WinFsp 마운트 실패: %v", err)
	}
	defer filesystem.Unmount()

	fmt.Printf("DKDrive SFTP 파일시스템을 %s에 마운트했습니다. 종료하려면 Ctrl+C를 누르세요.\n", letter)
	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, os.Interrupt)
	<-interrupt
}

func validMountpoint(value string) bool {
	if len(value) != 2 || value[1] != ':' {
		return false
	}
	letter := value[0]
	return letter >= 'A' && letter <= 'Z' || letter >= 'a' && letter <= 'z'
}

func defaultKnownHosts() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "known_hosts"
	}
	return filepath.Join(home, ".ssh", "known_hosts")
}

func fatal(format string, values ...any) {
	fmt.Fprintf(os.Stderr, "dkdrive-sftp-mount: "+format+"\n", values...)
	os.Exit(1)
}
