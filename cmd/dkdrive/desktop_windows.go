//go:build windows

package main

import (
	"github.com/danhk0612/DK-Drive/internal/desktop"
	"golang.org/x/sys/windows"
	"runtime"
	"unsafe"
)

func runDesktop(hidden bool) error { return desktop.Run(hidden) }

func showStartupError(err error) {
	text, title := windows.StringToUTF16Ptr(err.Error()), windows.StringToUTF16Ptr("DKDrive 시작 실패")
	windows.NewLazySystemDLL("user32.dll").NewProc("MessageBoxW").Call(0, uintptr(unsafe.Pointer(text)), uintptr(unsafe.Pointer(title)), 0x10)
	runtime.KeepAlive(text)
	runtime.KeepAlive(title)
}
