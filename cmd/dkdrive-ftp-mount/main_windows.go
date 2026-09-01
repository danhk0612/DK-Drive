//go:build windows

package main

import (
	"context"
	"crypto/tls"
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
	ftpbackend "github.com/danhk0612/DK-Drive/internal/protocol/ftp"
	"github.com/danhk0612/DK-Drive/internal/vfs"
)

func main() {
	host := flag.String("host", "", "FTP/FTPS 호스트")
	port := flag.Uint("port", 0, "FTP/FTPS 포트(기본값: FTP/Explicit 21, Implicit 990)")
	username := flag.String("user", "", "FTP/FTPS 사용자명")
	root := flag.String("root", "/", "원격 시작 경로")
	modeName := flag.String("mode", "ftp", "연결 모드: ftp, explicit-ftps, implicit-ftps")
	mountpoint := flag.String("mount", "X:", "마운트할 드라이브 문자(예: X:)")
	readOnly := flag.Bool("read-only", false, "원격 드라이브의 변경 작업 차단")
	cacheDirectory := flag.String("cache-dir", "", "로컬 캐시 폴더(기본값: %LOCALAPPDATA%\\DKDrive\\Cache)")
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
	if !validMountpoint(*mountpoint) {
		fatal("드라이브 문자는 X: 형식이어야 합니다")
	}
	if *host == "" || *username == "" || selectedPort > 65535 {
		fatal("-host, -user와 올바른 -port 값이 필요합니다")
	}
	if mode == ftpbackend.TLSNone && *insecureSkipTLSVerify {
		fatal("-insecure-skip-tls-verify는 FTPS 모드에서만 사용할 수 있습니다")
	}
	cacheStore, err := localcache.New(*cacheDirectory)
	if err != nil {
		fatal("캐시 설정 실패: %v", err)
	}

	password := os.Getenv("DKDRIVE_FTP_PASSWORD")
	if password == "" {
		password = readSecret("FTP 비밀번호: ")
	}
	var tlsConfig *tls.Config
	if mode != ftpbackend.TLSNone {
		tlsConfig = &tls.Config{InsecureSkipVerify: *insecureSkipTLSVerify}
	}
	connectCtx, cancelConnect := context.WithTimeout(context.Background(), 30*time.Second)
	backend, err := ftpbackend.New(connectCtx, ftpbackend.Config{
		Host: *host, Port: uint16(selectedPort), Username: *username, Password: password,
		Root: *root, Timeout: 30 * time.Second, TLSMode: mode, TLSConfig: tlsConfig,
	})
	cancelConnect()
	if err != nil {
		fatal("연결 실패: %v", err)
	}
	defer backend.Close()

	mountBackend := vfs.Backend(backend)
	attributeMode := gofs.AttribReadOnlyWindows
	modeDescription := "읽기/쓰기 모드"
	if *readOnly {
		mountBackend = vfs.NewReadOnlyBackend(backend)
		attributeMode = gofs.AttribReadOnlyAlways
		modeDescription = "읽기 전용 모드"
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

	fmt.Printf("DK-Drive FTP/FTPS 파일시스템을 %s에 %s로 마운트했습니다. 캐시: %s. 종료하려면 Ctrl+C를 누르세요.\n", letter, modeDescription, cacheStore.Directory())
	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, os.Interrupt)
	<-interrupt
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

func validMountpoint(value string) bool {
	if len(value) != 2 || value[1] != ':' {
		return false
	}
	letter := value[0]
	return letter >= 'A' && letter <= 'Z' || letter >= 'a' && letter <= 'z'
}

func fatal(format string, values ...any) {
	fmt.Fprintf(os.Stderr, "dkdrive-ftp-mount: "+format+"\n", values...)
	os.Exit(1)
}
