//go:build windows

package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"unsafe"

	"github.com/danhk0612/DK-Drive/internal/app"
	"github.com/danhk0612/DK-Drive/internal/prerequisite"
	"github.com/winfsp/go-winfsp"
	"golang.org/x/sys/windows"
)

func prerequisiteMessage(text string, flags uintptr) uintptr {
	p := windows.StringToUTF16Ptr(text)
	title := windows.StringToUTF16Ptr("DK-Drive / WinFsp")
	result, _, _ := windows.NewLazySystemDLL("user32.dll").NewProc("MessageBoxW").Call(0, uintptr(unsafe.Pointer(p)), uintptr(unsafe.Pointer(title)), flags)
	runtime.KeepAlive(p)
	runtime.KeepAlive(title)
	return result
}

func prepareWinFsp(hidden bool) (bool, error) {
	if err := winfsp.LoadWinFSP(); err == nil {
		return true, nil
	}
	if hidden {
		return false, errors.New("WinFsp를 사용할 수 없습니다. DK-Drive를 직접 실행하여 설치를 진행하세요")
	}
	exe, err := os.Executable()
	if err != nil {
		return false, err
	}
	info := prerequisite.WinFsp()
	installer := filepath.Join(filepath.Dir(exe), info.File)
	if err := info.Verify(installer); err != nil {
		return false, fmt.Errorf("WinFsp 설치 파일을 사용할 수 없습니다. 설치형 배포판을 다시 실행하거나 ZIP 전체를 풀고 실행하세요: %w", err)
	}
	prompt := "드라이브 연결에 필요한 WinFsp를 사용할 수 없습니다. 함께 제공된 공식 설치 프로그램을 실행할까요?\n\nWindows 관리자 승인이 필요할 수 있습니다. 설치를 마친 뒤 DK-Drive를 다시 실행하세요.\n\n" + app.WinFspNotice + "\n" + app.WinFspURL
	if prerequisiteMessage(prompt, 0x24) != 6 {
		return false, nil
	}
	system, err := windows.GetSystemDirectory()
	if err != nil {
		return false, err
	}
	err = exec.Command(filepath.Join(system, "msiexec.exe"), "/i", installer, "/norestart").Run()
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		switch exit.ExitCode() {
		case 1602:
			return false, nil // User cancelled the official installer.
		case 3010:
			prerequisiteMessage("WinFsp 설치가 완료됐습니다. Windows를 재시작한 뒤 DK-Drive를 실행하세요.", 0x40)
			return false, nil
		}
	}
	if err != nil {
		return false, fmt.Errorf("WinFsp 설치 실패: %w", err)
	}
	// The loader caches its first result, so installation is followed by a fresh launch.
	prerequisiteMessage("WinFsp 설치가 완료됐습니다. DK-Drive를 다시 실행하세요.", 0x40)
	return false, nil
}
