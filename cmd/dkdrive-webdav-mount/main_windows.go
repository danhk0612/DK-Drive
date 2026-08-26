//go:build windows

package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/winfsp/go-winfsp"
	"github.com/winfsp/go-winfsp/gofs"
	"golang.org/x/term"

	localcache "github.com/danhk0612/DK-Drive/internal/cache"
	"github.com/danhk0612/DK-Drive/internal/mount"
	webdavbackend "github.com/danhk0612/DK-Drive/internal/protocol/webdav"
	"github.com/danhk0612/DK-Drive/internal/vfs"
)

func main() {
	scheme := flag.String("scheme", "https", "WebDAV 방식: http 또는 https")
	host := flag.String("host", "", "WebDAV 호스트")
	port := flag.Uint("port", 443, "WebDAV 포트")
	username := flag.String("user", "", "WebDAV 사용자명")
	root := flag.String("root", "/", "원격 시작 경로")
	mountpoint := flag.String("mount", "X:", "마운트할 드라이브 문자(예: X:)")
	readOnly := flag.Bool("read-only", false, "원격 드라이브의 변경 작업 차단")
	cacheDirectory := flag.String("cache-dir", "", "로컬 캐시 폴더(기본값: %LOCALAPPDATA%\\DKDrive\\Cache)")
	flag.Parse()

	if !validMountpoint(*mountpoint) {
		fatal("드라이브 문자는 X: 형식이어야 합니다")
	}
	if *host == "" || *username == "" || *port == 0 || *port > 65535 {
		fatal("-host, -user와 올바른 -port 값이 필요합니다")
	}
	cacheStore, err := localcache.New(*cacheDirectory)
	if err != nil {
		fatal("캐시 설정 실패: %v", err)
	}
	password := os.Getenv("DKDRIVE_WEBDAV_PASSWORD")
	if password == "" {
		password = readSecret("WebDAV 비밀번호: ")
	}
	connectCtx, cancelConnect := context.WithTimeout(context.Background(), 30*time.Second)
	backend, err := webdavbackend.New(connectCtx, webdavbackend.Config{
		Scheme: *scheme, Host: *host, Port: uint16(*port), Username: *username,
		Password: password, Root: *root, Timeout: 30 * time.Second,
	})
	cancelConnect()
	if err != nil {
		fatal("연결 실패: %v", err)
	}
	defer backend.Close()

	mountBackend := vfs.Backend(backend)
	attributeMode := gofs.AttribReadOnlyWindows
	modeName := "읽기/쓰기 모드"
	if *readOnly {
		mountBackend = vfs.NewReadOnlyBackend(backend)
		attributeMode = gofs.AttribReadOnlyAlways
		modeName = "읽기 전용 모드"
	}
	mountFileSystem := mount.NewGoFileSystem(mountBackend, *readOnly, cacheStore)
	baseBehaviour, err := gofs.NewOptions(
		mountFileSystem,
		gofs.WithCaseInsensitive(false),
		gofs.WithAttribReadOnlyTransMode(attributeMode),
	)
	if err != nil {
		fatal("WinFsp 파일시스템 구성 실패: %v", err)
	}
	letter := strings.ToUpper(*mountpoint)
	filesystem, err := winfsp.Mount(baseBehaviour, letter)
	if err != nil {
		fatal("WinFsp 마운트 실패: %v", err)
	}
	defer filesystem.Unmount()

	fmt.Printf("DKDrive WebDAV 파일시스템을 %s에 %s로 마운트했습니다. 캐시: %s. 종료하려면 Ctrl+C를 누르세요.\n", letter, modeName, cacheStore.Directory())
	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, os.Interrupt)
	<-interrupt
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

func validMountpoint(value string) bool {
	if len(value) != 2 || value[1] != ':' {
		return false
	}
	letter := value[0]
	return letter >= 'A' && letter <= 'Z' || letter >= 'a' && letter <= 'z'
}

func fatal(format string, values ...any) {
	fmt.Fprintf(os.Stderr, "dkdrive-webdav-mount: "+format+"\n", values...)
	os.Exit(1)
}
