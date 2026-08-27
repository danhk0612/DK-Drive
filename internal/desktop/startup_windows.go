//go:build windows

package desktop

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf16"

	"golang.org/x/sys/windows/registry"
)

const runKey = `Software\Microsoft\Windows\CurrentVersion\Run`

func startupCommand(exe string) (string, error) {
	if !filepath.IsAbs(exe) || strings.ContainsAny(exe, "\"\r\n\x00") {
		return "", errors.New("자동 시작 실행 파일 경로가 올바르지 않습니다")
	}
	s := `"` + exe + `" --tray`
	if len(utf16.Encode([]rune(s))) > 260 {
		return "", errors.New("실행 파일 경로가 너무 길어 Windows 자동 시작에 등록할 수 없습니다")
	}
	return s, nil
}

func startupEnabled() (bool, error) {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKey, registry.QUERY_VALUE)
	if errors.Is(err, registry.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	defer k.Close()
	value, _, err := k.GetStringValue("DKDrive")
	if errors.Is(err, registry.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return value != "", nil
}

func setStartup(enabled bool) error {
	if !enabled {
		k, err := registry.OpenKey(registry.CURRENT_USER, runKey, registry.SET_VALUE)
		if errors.Is(err, registry.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		defer k.Close()
		err = k.DeleteValue("DKDrive")
		if errors.Is(err, registry.ErrNotExist) {
			return nil
		}
		return err
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	value, err := startupCommand(exe)
	if err != nil {
		return err
	}
	k, _, err := registry.CreateKey(registry.CURRENT_USER, runKey, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	return k.SetStringValue("DKDrive", value)
}
