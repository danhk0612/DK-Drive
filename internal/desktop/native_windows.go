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
var comdlg32 = windows.NewLazySystemDLL("comdlg32.dll")
var comctl32 = windows.NewLazySystemDLL("comctl32.dll")

const (
	wmCommand                   = 0x111
	wmNotify                    = 0x4e
	wmSize                      = 0x5
	wmGetMinMaxInfo             = 0x24
	wmKeyDown                   = 0x100
	wmClose                     = 0x10
	wmDestroy                   = 2
	wmTaskDone                  = 0x8001
	wmTray                      = 0x8002
	wmAutoConnect               = 0x8003
	bsCheck                     = 3
	bsDefault                   = 1
	wsOverlappedWindow          = 0x00cf0000
	bmGetCheck                  = 0xf0
	bmSetCheck                  = 0xf1
	emSetSel                    = 0xb1
	cbAddString                 = 0x143
	cbGetCurSel                 = 0x147
	cbReset                     = 0x14b
	cbSetCurSel                 = 0x14e
	cbGetDroppedState           = 0x157
	lvmFirst                    = 0x1000
	lvmDeleteAllItems           = lvmFirst + 9
	lvmEnsureVisible            = lvmFirst + 19
	lvmSetItemState             = lvmFirst + 43
	lvmSetExtendedListViewStyle = lvmFirst + 54
	lvmInsertItemW              = lvmFirst + 77
	lvmInsertColumnW            = lvmFirst + 97
	lvmSetItemTextW             = lvmFirst + 116
	lvcfFormat                  = 0x1
	lvcfWidth                   = 0x2
	lvcfText                    = 0x4
	lvcfSubItem                 = 0x8
	lvifText                    = 0x1
	lvifState                   = 0x8
	lvisFocused                 = 0x1
	lvisSelected                = 0x2
	lvnItemChanged              = -101
)

type point struct{ X, Y int32 }
type rect struct{ Left, Top, Right, Bottom int32 }
type minMaxInfo struct {
	Reserved, MaxSize, MaxPosition, MinTrackSize, MaxTrackSize point
}
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
type openFileName struct {
	Size             uint32
	Owner            uintptr
	Instance         uintptr
	Filter           *uint16
	CustomFilter     *uint16
	MaxCustomFilter  uint32
	FilterIndex      uint32
	File             *uint16
	MaxFile          uint32
	FileTitle        *uint16
	MaxFileTitle     uint32
	InitialDirectory *uint16
	Title            *uint16
	Flags            uint32
	FileOffset       uint16
	FileExtension    uint16
	DefaultExtension *uint16
	CustomData       uintptr
	Hook             uintptr
	TemplateName     *uint16
	Reserved         uintptr
	ReservedFlags    uint32
	FlagsEx          uint32
}
type initCommonControls struct {
	Size    uint32
	Classes uint32
}
type listViewColumn struct {
	Mask                               uint32
	Format, Width                      int32
	Text                               *uint16
	TextMax, SubItem, Image, Order     int32
	MinWidth, DefaultWidth, IdealWidth int32
}
type listViewItem struct {
	Mask             uint32
	Item, SubItem    int32
	State, StateMask uint32
	Text             *uint16
	TextMax, Image   int32
	Param            uintptr
	Indent, GroupID  int32
	Columns          uint32
	ColumnIndices    *uint32
	ColumnFormats    *int32
	Group            int32
}
type notifyHeader struct {
	WindowFrom uintptr
	IDFrom     uintptr
	Code       int32
}
type notifyListView struct {
	Header             notifyHeader
	Item, SubItem      int32
	NewState, OldState uint32
	Changed            uint32
	Action             point
	Param              uintptr
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

func initializeListView() bool {
	value := initCommonControls{Size: uint32(unsafe.Sizeof(initCommonControls{})), Classes: 0x1}
	r, _, _ := comctl32.NewProc("InitCommonControlsEx").Call(uintptr(unsafe.Pointer(&value)))
	runtime.KeepAlive(value)
	return r != 0
}

func listViewAddColumn(h uintptr, index, width int, title string) bool {
	text := windows.StringToUTF16(title)
	column := listViewColumn{
		Mask:    lvcfFormat | lvcfWidth | lvcfText | lvcfSubItem,
		Width:   int32(width),
		Text:    &text[0],
		TextMax: int32(len(text)),
		SubItem: int32(index),
	}
	r := send(h, lvmInsertColumnW, uintptr(index), uintptr(unsafe.Pointer(&column)))
	runtime.KeepAlive(column)
	runtime.KeepAlive(text)
	return int32(r) != -1
}

func listViewAddRow(h uintptr, index int, values []string) bool {
	if len(values) == 0 {
		return false
	}
	text := windows.StringToUTF16(values[0])
	item := listViewItem{Mask: lvifText, Item: int32(index), Text: &text[0], TextMax: int32(len(text))}
	if int32(send(h, lvmInsertItemW, 0, uintptr(unsafe.Pointer(&item)))) == -1 {
		return false
	}
	runtime.KeepAlive(item)
	runtime.KeepAlive(text)
	for subItem, value := range values[1:] {
		text = windows.StringToUTF16(value)
		item = listViewItem{SubItem: int32(subItem + 1), Text: &text[0], TextMax: int32(len(text))}
		if send(h, lvmSetItemTextW, uintptr(index), uintptr(unsafe.Pointer(&item))) == 0 {
			return false
		}
		runtime.KeepAlive(item)
		runtime.KeepAlive(text)
	}
	return true
}

func listViewSelect(h uintptr, index int) {
	item := listViewItem{StateMask: lvisFocused | lvisSelected}
	send(h, lvmSetItemState, ^uintptr(0), uintptr(unsafe.Pointer(&item)))
	if index >= 0 {
		item.State = lvisFocused | lvisSelected
		send(h, lvmSetItemState, uintptr(index), uintptr(unsafe.Pointer(&item)))
		send(h, lvmEnsureVisible, uintptr(index), 0)
	}
	runtime.KeepAlive(item)
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
	p, t := utf(text), utf("DK-Drive")
	r := call("MessageBoxW", h, uintptr(unsafe.Pointer(p)), uintptr(unsafe.Pointer(t)), flags)
	runtime.KeepAlive(p)
	runtime.KeepAlive(t)
	return r
}

func fileDialogFilter(parts ...string) []uint16 {
	result := make([]uint16, 0)
	for _, part := range parts {
		result = append(result, windows.StringToUTF16(part)...)
	}
	return append(result, 0)
}

func chooseFile(owner uintptr, title, current string, filter []uint16) (string, bool, error) {
	buffer := make([]uint16, 32768)
	if current != "" {
		value := windows.StringToUTF16(current)
		if len(value) < len(buffer) {
			copy(buffer, value)
		}
	}
	titleText := windows.StringToUTF16(title)
	dialog := openFileName{
		Size:        uint32(unsafe.Sizeof(openFileName{})),
		Owner:       owner,
		Filter:      &filter[0],
		FilterIndex: 1,
		File:        &buffer[0],
		MaxFile:     uint32(len(buffer)),
		Title:       &titleText[0],
		Flags:       0x8 | 0x800 | 0x1000 | 0x80000 | 0x2000000,
	}
	result, _, callErr := comdlg32.NewProc("GetOpenFileNameW").Call(uintptr(unsafe.Pointer(&dialog)))
	runtime.KeepAlive(filter)
	runtime.KeepAlive(titleText)
	runtime.KeepAlive(buffer)
	if result != 0 {
		return windows.UTF16ToString(buffer), true, nil
	}
	code, _, _ := comdlg32.NewProc("CommDlgExtendedError").Call()
	if code == 0 {
		return "", false, nil
	}
	return "", false, fmt.Errorf("Windows 파일 선택 창 오류: 0x%04X: %w", code, callErr)
}

func chooseSaveFile(owner uintptr, title, suggested string) (string, bool, error) {
	buffer := make([]uint16, 32768)
	value := windows.StringToUTF16(suggested)
	if len(value) < len(buffer) {
		copy(buffer, value)
	}
	filter := fileDialogFilter("모든 파일", "*.*")
	titleText := windows.StringToUTF16(title)
	dialog := openFileName{
		Size: uint32(unsafe.Sizeof(openFileName{})), Owner: owner,
		Filter: &filter[0], FilterIndex: 1, File: &buffer[0], MaxFile: uint32(len(buffer)),
		Title: &titleText[0], Flags: 0x2 | 0x8 | 0x800 | 0x80000 | 0x2000000,
	}
	result, _, callErr := comdlg32.NewProc("GetSaveFileNameW").Call(uintptr(unsafe.Pointer(&dialog)))
	runtime.KeepAlive(filter)
	runtime.KeepAlive(titleText)
	runtime.KeepAlive(buffer)
	if result != 0 {
		return windows.UTF16ToString(buffer), true, nil
	}
	code, _, _ := comdlg32.NewProc("CommDlgExtendedError").Call()
	if code == 0 {
		return "", false, nil
	}
	return "", false, fmt.Errorf("Windows 저장 위치 선택 창 오류: 0x%04X: %w", code, callErr)
}

func openFolder(path string) error {
	verb, target := utf("open"), utf(path)
	result, _, callErr := shell32.NewProc("ShellExecuteW").Call(0, uintptr(unsafe.Pointer(verb)), uintptr(unsafe.Pointer(target)), 0, 0, 1)
	runtime.KeepAlive(verb)
	runtime.KeepAlive(target)
	if result <= 32 {
		return fmt.Errorf("캐시 폴더 열기 실패 (Windows 오류 %d): %w", result, callErr)
	}
	return nil
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
		return 0, fmt.Errorf("DK-Drive 아이콘 생성 실패: %w", err)
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
	copy(n.Tip[:], windows.StringToUTF16(trayTooltip(w.settings.Profiles, w.manager.State)))
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

func (w *window) updateTrayTooltip() {
	if !w.hasTray {
		return
	}
	n := notifyIcon{Window: w.hwnd, ID: 1, Flags: 4}
	n.Size = uint32(unsafe.Sizeof(n))
	copy(n.Tip[:], windows.StringToUTF16(trayTooltip(w.settings.Profiles, w.manager.State)))
	shell32.NewProc("Shell_NotifyIconW").Call(1, uintptr(unsafe.Pointer(&n)))
}

func (w *window) show() { call("ShowWindow", w.hwnd, 9); call("SetForegroundWindow", w.hwnd) }

func (w *window) trayMenu() {
	menu := call("CreatePopupMenu")
	if menu == 0 {
		return
	}
	defer call("DestroyMenu", menu)
	drives := call("CreatePopupMenu")
	cacheMenu := call("CreatePopupMenu")
	if drives == 0 || cacheMenu == 0 {
		if drives != 0 {
			call("DestroyMenu", drives)
		}
		if cacheMenu != 0 {
			call("DestroyMenu", cacheMenu)
		}
		return
	}
	add := func(target, flags, id uintptr, text string) {
		p := utf(text)
		call("AppendMenuW", target, flags, id, uintptr(unsafe.Pointer(p)))
		runtime.KeepAlive(p)
	}
	for i, p := range w.settings.Profiles {
		add(drives, 0, uintptr(1000+i), trayProfileLabel(p, w.manager.State(p.ID)))
	}
	if len(w.settings.Profiles) == 0 {
		add(drives, 0x1, 0, "(등록된 드라이브 없음)")
	}
	add(drives, 0x800, 0, "")
	add(drives, 0, idNew, "드라이브 추가")
	add(drives, 0, idConnectAll, "전체 연결")
	add(drives, 0, idDisconnectAll, "전체 해제")
	add(cacheMenu, 0, idRecovery, "캐시 확인")
	add(cacheMenu, 0, idOpenCacheFolder, "폴더 열기")
	add(cacheMenu, 0, idClearCache, "캐시 정리")

	add(menu, 0, idShow, "DK-Drive 실행")
	add(menu, 0x10, drives, "드라이브")
	add(menu, 0x10, cacheMenu, "캐시 관리")
	add(menu, 0, idExit, "종료")
	var pos point
	call("GetCursorPos", uintptr(unsafe.Pointer(&pos)))
	call("SetForegroundWindow", w.hwnd)
	id := call("TrackPopupMenu", menu, 0x100|2, uintptr(pos.X), uintptr(pos.Y), 0, w.hwnd, 0)
	call("PostMessageW", w.hwnd, 0, 0, 0)
	if id != 0 {
		w.command(int(id), 0, 0)
	}
}
