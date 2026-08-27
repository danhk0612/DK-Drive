//go:build windows

package credential

import (
	"errors"
	"runtime"
	"unsafe"

	"golang.org/x/sys/windows"
)

// DPAPI uses the current Windows user, never CRYPTPROTECT_LOCAL_MACHINE.
type DPAPI struct{}

func (DPAPI) Protect(data []byte) ([]byte, error)   { return transform(data, true) }
func (DPAPI) Unprotect(data []byte) ([]byte, error) { return transform(data, false) }

func transform(data []byte, encrypt bool) ([]byte, error) {
	if len(data) == 0 {
		return nil, errors.New("빈 자격 증명 데이터입니다")
	}
	in := windows.DataBlob{Size: uint32(len(data)), Data: &data[0]}
	var out windows.DataBlob
	var err error
	if encrypt {
		err = windows.CryptProtectData(&in, nil, nil, 0, nil, windows.CRYPTPROTECT_UI_FORBIDDEN, &out)
	} else {
		err = windows.CryptUnprotectData(&in, nil, nil, 0, nil, windows.CRYPTPROTECT_UI_FORBIDDEN, &out)
	}
	runtime.KeepAlive(data)
	if err != nil {
		return nil, err
	}
	defer windows.LocalFree(windows.Handle(unsafe.Pointer(out.Data)))
	buffer := unsafe.Slice(out.Data, out.Size)
	result := append([]byte(nil), buffer...)
	clear(buffer)
	return result, nil
}
