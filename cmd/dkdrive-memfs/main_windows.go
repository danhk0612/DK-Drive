//go:build windows

package main

import (
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"

	"github.com/winfsp/go-winfsp"
	"github.com/winfsp/go-winfsp/gofs"
	"github.com/winfsp/go-winfsp/memfs"
)

func main() {
	mountpoint := flag.String("mount", "X:", "마운트할 드라이브 문자(예: X:)")
	flag.Parse()

	if !validMountpoint(*mountpoint) {
		fmt.Fprintln(os.Stderr, "dkdrive-memfs: 드라이브 문자는 X: 형식이어야 합니다")
		os.Exit(2)
	}

	filesystem, err := winfsp.Mount(
		gofs.New(memfs.New(memfs.WithCaseInsensitive(true))),
		strings.ToUpper(*mountpoint),
	)
	if err != nil {
		fmt.Fprintln(os.Stderr, "dkdrive-memfs: WinFsp 마운트 실패:", err)
		fmt.Fprintln(os.Stderr, "WinFsp 설치 여부와 드라이브 문자 사용 여부를 확인하세요.")
		os.Exit(1)
	}
	defer filesystem.Unmount()

	fmt.Printf("DK-Drive 메모리 파일시스템을 %s에 마운트했습니다. 종료하려면 Ctrl+C를 누르세요.\n", strings.ToUpper(*mountpoint))

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
