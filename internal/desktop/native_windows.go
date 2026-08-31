//go:build windows

package desktop

import (
	"fmt"
	"runtime"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

var user32 = windows.NewLazySystemDLL("user32.dll")
var shell32 = windows.NewLazySystemDLL("shell32.dll")
var gdi32 = windows.NewLazySystemDLL("gdi32.dll")

const (
	wmCommand     = 0x111
	wmClose       = 0x10
	wmDestroy     = 2
	wmTaskDone    = 0x8001
	wmTray        = 0x8002
	wmAutoConnect = 0x8003
	bsCheck       = 3
	bmGetCheck    = 0xf0
	bmSetCheck    = 0xf1
	cbAddString   = 0x143
	cbGetCurSel   = 0x147
	cbReset       = 0x14b
	cbSetCurSel   = 0x14e
	lbReset       = 0x184
	lbAddString   = 0x180
	lbGetCurSel   = 0x188
	lbSetCurSel   = 0x186
)

type point struct{ X, Y int32 }
type rect struct{ Left, Top, Right, Bottom int32 }
type message struct {
	Window         uintptr
	ID             uint32
	WParam, LParam uintptr
	Time           uint32
	Point          point
	Private        uint32
}
type windowClass struct {
	Size, Style                        uint32
	Proc                               uintptr
	ClassExtra, WindowExtra            int32
	Instance, Icon, Cursor, Background uintptr
	Menu, Name                         *uint16
	SmallIcon                          uintptr
}
type notifyIcon struct {
	Size                uint32
	Window              uintptr
	ID, Flags, Callback uint32
	Icon                uintptr
	Tip                 [128]uint16
	State, StateMask    uint32
	Info                [256]uint16
	Version             uint32
	InfoTitle           [64]uint16
	InfoFlags           uint32
	GUID                windows.GUID
	BalloonIcon         uintptr
}

// Pointer-valued Win32 arguments must escape before this Go wrapper can grow
// the stack; KeepAlive alone would not prevent stack relocation.
//
//go:uintptrescapes
func call(name string, args ...uintptr) uintptr {
	r, _, _ := user32.NewProc(name).Call(args...)
	return r
}
func utf(s string) *uint16 { return windows.StringToUTF16Ptr(strings.ReplaceAll(s, "\x00", "�")) }
func setText(h uintptr, s string) {
	p := utf(s)
	call("SetWindowTextW", h, uintptr(unsafe.Pointer(p)))
	runtime.KeepAlive(p)
}
func getText(h uintptr) string {
	n := call("GetWindowTextLengthW", h)
	b := make([]uint16, n+1)
	call("GetWindowTextW", h, uintptr(unsafe.Pointer(&b[0])), uintptr(len(b)))
	return windows.UTF16ToString(b)
}

//go:uintptrescapes
func send(h uintptr, msg uint32, w, l uintptr) uintptr {
	return call("SendMessageW", h, uintptr(msg), w, l)
}
func sendText(h uintptr, msg uint32, s string) uintptr {
	p := utf(s)
	r := send(h, msg, 0, uintptr(unsafe.Pointer(p)))
	runtime.KeepAlive(p)
	return r
}
func checked(h uintptr) bool { return send(h, bmGetCheck, 0, 0) == 1 }
func check(h uintptr, value bool) {
	var v uintptr
	if value {
		v = 1
	}
	send(h, bmSetCheck, v, 0)
}
func alert(h uintptr, text string) { box(h, text, 0x10) }
func box(h uintptr, text string, flags uintptr) uintptr {
	p, t := utf(text), utf("DKDrive")
	r := call("MessageBoxW", h, uintptr(unsafe.Pointer(p)), uintptr(unsafe.Pointer(t)), flags)
	runtime.KeepAlive(p)
	runtime.KeepAlive(t)
	return r
}

func appIconBits(size int) ([]byte, []byte) {
	andStride := ((size + 15) / 16) * 2
	andBits := make([]byte, andStride*size)
	for i := range andBits {
		andBits[i] = 0xff
	}
	xorBits := make([]byte, size*size*4)
	set := func(x, y int, r, g, b byte) {
		row := size - 1 - y
		andBits[row*andStride+x/8] &^= 0x80 >> uint(x%8)
		i := (row*size + x) * 4
		xorBits[i], xorBits[i+1], xorBits[i+2], xorBits[i+3] = b, g, r, 0xff
	}
	for y := range size {
		for x := range size {
			sx, sy := x*32/size, y*32/size
			cx, cy := min(max(sx, 6), 25), min(max(sy, 6), 25)
			if (sx-cx)*(sx-cx)+(sy-cy)*(sy-cy) > 16 {
				continue
			}
			r, g, b := byte(18), byte(104), byte(179)
			if sx <= 3 || sx >= 28 || sy <= 3 || sy >= 28 {
				r, g, b = 11, 79, 138
			}
			set(x, y, r, g, b)
		}
	}
	for y := range size {
		for x := range size {
			sx, sy := x*32/size, y*32/size
			d := (sx >= 8 && sx <= 11 && sy >= 7 && sy <= 23) ||
				(sx >= 10 && sx <= 19 && sy >= 7 && sy <= 10) ||
				(sx >= 10 && sx <= 19 && sy >= 20 && sy <= 23) ||
				(sx >= 19 && sx <= 22 && sy >= 10 && sy <= 20)
			if d {
				set(x, y, 255, 255, 255)
			}
		}
	}
	return andBits, xorBits
}

func createAppIcon(instance uintptr, size int) (uintptr, error) {
	andBits, xorBits := appIconBits(size)
	h, _, err := user32.NewProc("CreateIcon").Call(
		instance, uintptr(size), uintptr(size), 1, 32,
		uintptr(unsafe.Pointer(&andBits[0])), uintptr(unsafe.Pointer(&xorBits[0])),
	)
	runtime.KeepAlive(andBits)
	runtime.KeepAlive(xorBits)
	if h == 0 {
		return 0, fmt.Errorf("DKDrive 아이콘 생성 실패: %w", err)
	}
	return h, nil
}

const className = "DKDrive.Desktop.0_5"

func (w *window) createControl(class, text string, style uintptr, x, y, width, height int, id int) (uintptr, error) {
	cp, tp := utf(class), utf(text)
	r, _, err := user32.NewProc("CreateWindowExW").Call(0, uintptr(unsafe.Pointer(cp)), uintptr(unsafe.Pointer(tp)),
		0x40000000|0x10000000|style, uintptr(w.px(x)), uintptr(w.px(y)), uintptr(w.px(width)), uintptr(w.px(height)), w.hwnd, uintptr(id), 0, 0)
	runtime.KeepAlive(cp)
	runtime.KeepAlive(tp)
	if r == 0 {
		return 0, fmt.Errorf("UI 컨트롤 생성 실패: %w", err)
	}
	send(r, 0x30, w.font, 1)
	return r, nil
}

func (w *window) tray(add bool) bool {
	n := notifyIcon{Window: w.hwnd, ID: 1, Flags: 1 | 2 | 4, Callback: wmTray, Icon: w.icon}
	n.Size = uint32(unsafe.Sizeof(n))
	copy(n.Tip[:], windows.StringToUTF16("DKDrive — 드라이브 관리"))
	op := uintptr(2)
	if add {
		op = 0
	}
	r, _, _ := shell32.NewProc("Shell_NotifyIconW").Call(op, uintptr(unsafe.Pointer(&n)))
	if add && r != 0 {
		// Version 0 uses the WM_* mouse callback convention below.
		shell32.NewProc("Shell_NotifyIconW").Call(4, uintptr(unsafe.Pointer(&n)))
	}
	w.hasTray = add && r != 0
	return r != 0
}

func (w *window) show() { call("ShowWindow", w.hwnd, 9); call("SetForegroundWindow", w.hwnd) }

func (w *window) trayMenu() {
	menu := call("CreatePopupMenu")
	if menu == 0 {
		return
	}
	defer call("DestroyMenu", menu)
	add := func(id int, text string) {
		p := utf(text)
		call("AppendMenuW", menu, 0, uintptr(id), uintptr(unsafe.Pointer(p)))
		runtime.KeepAlive(p)
	}
	add(idShow, "창 열기 / 설정")
	add(idNew, "드라이브 추가")
	for i, p := range w.settings.Profiles {
		add(1000+i, fmt.Sprintf("%s: %s — %s", p.Profile.DriveLetter, p.Profile.Name, w.manager.State(p.ID)))
	}
	add(idConnectAll, "모든 드라이브 연결")
	add(idDisconnectAll, "모든 드라이브 해제")
	add(idExit, "종료")
	var pos point
	call("GetCursorPos", uintptr(unsafe.Pointer(&pos)))
	call("SetForegroundWindow", w.hwnd)
	id := call("TrackPopupMenu", menu, 0x100|2, uintptr(pos.X), uintptr(pos.Y), 0, w.hwnd, 0)
	call("PostMessageW", w.hwnd, 0, 0, 0)
	if id != 0 {
		w.command(int(id), 0, 0)
	}
}
