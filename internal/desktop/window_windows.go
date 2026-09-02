//go:build windows

package desktop

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"github.com/danhk0612/DK-Drive/internal/app"
	localcache "github.com/danhk0612/DK-Drive/internal/cache"
	"github.com/danhk0612/DK-Drive/internal/config"
	"github.com/danhk0612/DK-Drive/internal/connection"
	"github.com/danhk0612/DK-Drive/internal/credential"
	"golang.org/x/sys/windows"
)

const (
	idList = 100 + iota
	idNew
	idEditProfile
	idSave
	idDelete
	idConnect
	idDisconnect
	idConnectAll
	idDisconnectAll
	idExit
	idShow
	idCloseToTray
	idStartup
	idProtocol
	idTestConnection
	idDrive
	idBrowseKey
	idBrowseKnownHosts
	idTogglePassword
	idTogglePassphrase
	idRecovery
	idOpenCacheFolder
	idClearCache
	idProfileClose
)

var protocols = []string{"SFTP", "WebDAV HTTPS", "WebDAV HTTP (평문)", "FTP (평문)", "Explicit FTPS", "Implicit FTPS (실서버 미검증)"}
var active *window

type taskResult struct {
	err     error
	done    func(error)
	confirm func() bool
	answer  chan bool
}

type layoutRule uint8

const (
	layoutMoveX layoutRule = 1 << iota
	layoutMoveY
	layoutGrowX
	layoutGrowY
)

type controlLayout struct {
	handle uintptr
	bounds rect
	rule   layoutRule
}

type keyboardCommand uint8

const (
	keyboardNone keyboardCommand = iota
	keyboardSelectAll
	keyboardSave
	keyboardCancelEdit
)

type window struct {
	hwnd, font, icon, smallIcon                                                          uintptr
	scale                                                                                float64
	hasTray, busy, secretFailed, loading, dirty                                          bool
	taskbarCreated                                                                       uint32
	settings                                                                             config.Settings
	filename                                                                             string
	manager                                                                              *connection.Manager
	sessionSecrets                                                                       map[string]config.Secrets
	selected                                                                             int
	controls                                                                             []uintptr
	layout                                                                               []controlLayout
	baseClientWidth, baseClientHeight, minTrackWidth, minTrackHeight                     int32
	list, status, closeToTray, startup, newButton, editButton, saveButton                uintptr
	testButton                                                                           uintptr
	authHint                                                                             uintptr
	exitButton, cacheButton                                                              uintptr
	deleteButton, connectButton, disconnectButton, connectAllButton, disconnectAllButton uintptr
	name, protocol, drive, port, host, root, user, key, knownHosts, password, passphrase uintptr
	keyBrowse, knownHostsBrowse, passwordToggle, passphraseToggle                        uintptr
	readOnly, autoConnect, remember, insecure                                            uintptr
	passwordVisible, passphraseVisible                                                   bool
	results                                                                              chan taskResult
	recoveryItems                                                                        []localcache.RecoveryItem
	cacheUsage                                                                           localcache.Usage
}

// Run owns all HWNDs on one OS thread. Network calls never run in WndProc.
func Run(hidden bool) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	mutex, err := windows.CreateMutex(nil, false, utf(`Local\DKDrive.Desktop.0_5`))
	if mutex != 0 {
		defer windows.CloseHandle(mutex)
	}
	if errors.Is(err, windows.ERROR_ALREADY_EXISTS) {
		p := utf(className)
		h := call("FindWindowW", uintptr(unsafe.Pointer(p)), 0)
		runtime.KeepAlive(p)
		if h != 0 {
			call("ShowWindow", h, 9)
			call("SetForegroundWindow", h)
		}
		return nil
	}
	if err != nil {
		return err
	}
	filename, err := config.SettingsPath()
	if err != nil {
		return err
	}
	s, err := config.LoadSettings(filename)
	if err != nil {
		return err
	}
	cacheStore, err := localcache.New("")
	if err != nil {
		return err
	}
	recoveryItems, err := cacheStore.Scan()
	if err != nil {
		return err
	}
	cacheUsage, err := cacheStore.Usage(recoveryItems)
	if err != nil {
		return err
	}
	w := &window{settings: s, filename: filename, manager: connection.New(connection.Mount), selected: -1, sessionSecrets: map[string]config.Secrets{}, results: make(chan taskResult, 1), recoveryItems: recoveryItems, cacheUsage: cacheUsage}
	active = w
	defer func() { active = nil }()
	call("SetProcessDPIAware")
	w.scale = 1
	dpi := uintptr(96)
	if dpiProc := user32.NewProc("GetDpiForSystem"); dpiProc.Find() == nil {
		value, _, _ := dpiProc.Call()
		if value != 0 {
			dpi = value
		}
	}
	var area rect
	call("SystemParametersInfoW", 0x30, 0, uintptr(unsafe.Pointer(&area)), 0)
	w.scale = desktopScale(uint32(dpi), area.Right-area.Left, area.Bottom-area.Top)
	var instance windows.Handle
	err = windows.GetModuleHandleEx(0, nil, &instance)
	if err != nil {
		return err
	}
	w.icon, err = createAppIcon(uintptr(instance), 32)
	if err != nil {
		return err
	}
	defer call("DestroyIcon", w.icon)
	w.smallIcon, err = createAppIcon(uintptr(instance), 16)
	if err != nil {
		return err
	}
	defer call("DestroyIcon", w.smallIcon)
	class := windowClass{Size: uint32(unsafe.Sizeof(windowClass{})), Proc: syscall.NewCallback(wndProc), Instance: uintptr(instance), Icon: w.icon, SmallIcon: w.smallIcon, Cursor: call("LoadCursorW", 0, 32512), Background: 16, Name: utf(className)}
	if call("RegisterClassExW", uintptr(unsafe.Pointer(&class))) == 0 {
		return errors.New("창 클래스 등록 실패")
	}
	defer call("UnregisterClassW", uintptr(unsafe.Pointer(class.Name)), uintptr(instance))
	title := utf("DK-Drive " + app.Version + " — 연결 관리")
	h, _, createErr := user32.NewProc("CreateWindowExW").Call(0x10000, uintptr(unsafe.Pointer(class.Name)), uintptr(unsafe.Pointer(title)), wsOverlappedWindow, 0x80000000, 0x80000000, uintptr(w.px(650)), uintptr(w.px(670)), 0, 0, uintptr(instance), 0)
	runtime.KeepAlive(title)
	if h == 0 {
		return fmt.Errorf("창 생성 실패: %w", createErr)
	}
	w.hwnd = h
	var windowBounds rect
	if call("GetWindowRect", h, uintptr(unsafe.Pointer(&windowBounds))) != 0 {
		w.minTrackWidth = windowBounds.Right - windowBounds.Left
		w.minTrackHeight = windowBounds.Bottom - windowBounds.Top
	}
	defer call("DestroyWindow", h)
	face := utf("맑은 고딕")
	font, _, _ := gdi32.NewProc("CreateFontW").Call(uintptr(int32(-w.px(15))), 0, 0, 0, 400, 0, 0, 0, 1, 0, 0, 0, 0, uintptr(unsafe.Pointer(face)))
	runtime.KeepAlive(face)
	w.font = font
	defer gdi32.NewProc("DeleteObject").Call(font)
	if err := w.build(); err != nil {
		return err
	}
	p := utf("TaskbarCreated")
	w.taskbarCreated = uint32(call("RegisterWindowMessageW", uintptr(unsafe.Pointer(p))))
	runtime.KeepAlive(p)
	w.tray(true)
	defer w.tray(false)
	if !hidden || !w.hasTray {
		w.show()
	}
	w.refreshList()
	if !hidden || !w.hasTray {
		call("SetFocus", w.list)
	}
	call("PostMessageW", h, wmAutoConnect, 0, 0)
	var msg message
	for {
		r := int32(call("GetMessageW", uintptr(unsafe.Pointer(&msg)), 0, 0, 0))
		if r == -1 {
			return errors.New("Windows 메시지 처리 실패")
		}
		if r == 0 {
			break
		}
		if activeProfileEditor != nil && (msg.Window == activeProfileEditor.hwnd || call("IsChild", activeProfileEditor.hwnd, msg.Window) != 0) {
			if activeProfileEditor.owner.handleKeyboardMessage(&msg) {
				continue
			}
			if call("IsDialogMessageW", activeProfileEditor.hwnd, uintptr(unsafe.Pointer(&msg))) == 0 {
				call("TranslateMessage", uintptr(unsafe.Pointer(&msg)))
				call("DispatchMessageW", uintptr(unsafe.Pointer(&msg)))
			}
			continue
		}
		if activeRecovery != nil && (msg.Window == activeRecovery.hwnd || call("IsChild", activeRecovery.hwnd, msg.Window) != 0) {
			if call("IsDialogMessageW", activeRecovery.hwnd, uintptr(unsafe.Pointer(&msg))) == 0 {
				call("TranslateMessage", uintptr(unsafe.Pointer(&msg)))
				call("DispatchMessageW", uintptr(unsafe.Pointer(&msg)))
			}
			continue
		}
		if w.handleKeyboardMessage(&msg) {
			continue
		}
		if call("IsDialogMessageW", h, uintptr(unsafe.Pointer(&msg))) == 0 {
			call("TranslateMessage", uintptr(unsafe.Pointer(&msg)))
			call("DispatchMessageW", uintptr(unsafe.Pointer(&msg)))
		}
	}
	return nil
}

func (w *window) px(v int) int32 { return int32(float64(v) * w.scale) }

func (w *window) dialogOwner() uintptr {
	if activeProfileEditor != nil && activeProfileEditor.owner == w {
		return activeProfileEditor.hwnd
	}
	return w.hwnd
}

func desktopScale(dpi uint32, workWidth, workHeight int32) float64 {
	scale := float64(dpi) / 96
	if scale <= 0 {
		scale = 1
	}
	if workWidth > 24 && workHeight > 24 {
		scale = min(scale, float64(workWidth-24)/650, float64(workHeight-24)/670)
	}
	return max(scale, 0.75)
}

func layoutRuleFor(class string, x, y, width, id int) layoutRule {
	if id == idList {
		return layoutGrowX | layoutGrowY
	}
	if class == "EDIT" && y == 540 {
		return layoutMoveY | layoutGrowX
	}
	if y >= 352 {
		return layoutMoveY
	}
	return 0
}

func resizeBounds(bounds rect, rule layoutRule, deltaX, deltaY int32) rect {
	result := bounds
	if rule&layoutMoveX != 0 {
		result.Left += deltaX
		result.Right += deltaX
	}
	if rule&layoutMoveY != 0 {
		result.Top += deltaY
		result.Bottom += deltaY
	}
	if rule&layoutGrowX != 0 {
		result.Right += deltaX
	}
	if rule&layoutGrowY != 0 {
		result.Bottom += deltaY
	}
	return result
}

func keyboardCommandFor(key uintptr, controlDown, dirty bool) keyboardCommand {
	if controlDown && key == 'A' {
		return keyboardSelectAll
	}
	if dirty && controlDown && key == 'S' {
		return keyboardSave
	}
	if dirty && key == 0x1b { // VK_ESCAPE
		return keyboardCancelEdit
	}
	return keyboardNone
}

func wndProc(h uintptr, msg uint32, wp, lp uintptr) uintptr {
	w := active
	if w != nil {
		switch msg {
		case wmCommand:
			w.command(int(wp&0xffff), int(wp>>16), lp)
			return 0
		case wmNotify:
			if w.handleListNotification(lp) {
				return 0
			}
		case wmSize:
			w.resize(int32(lp&0xffff), int32((lp>>16)&0xffff))
			return 0
		case wmGetMinMaxInfo:
			if lp != 0 && w.minTrackWidth > 0 && w.minTrackHeight > 0 {
				pointer := *(*unsafe.Pointer)(unsafe.Pointer(&lp))
				limits := (*minMaxInfo)(pointer)
				limits.MinTrackSize = point{X: w.minTrackWidth, Y: w.minTrackHeight}
				return 0
			}
		case wmClose:
			if w.busy {
				alert(h, "연결 작업이 끝난 뒤 다시 시도하세요")
				return 0
			}
			if w.settings.CloseToTray && w.hasTray {
				call("ShowWindow", h, 0)
			} else {
				w.exit()
			}
			return 0
		case wmDestroy:
			call("PostQuitMessage", 0)
			return 0
		case wmTaskDone:
			select {
			case r := <-w.results:
				if r.confirm != nil {
					r.answer <- r.confirm()
					return 0
				}
				w.setBusy(false)
				w.refreshList()
				r.done(r.err)
			default:
			}
			return 0
		case wmAutoConnect:
			w.connectAll(true)
			return 0
		case wmTray:
			if lp == 0x203 {
				w.show()
			} else if lp == 0x205 {
				w.trayMenu()
			}
			return 0
		case 0x11: // WM_QUERYENDSESSION: never discard live mounts during logoff.
			if w.busy || (activeRecovery != nil && activeRecovery.busy) || w.anyConnected() {
				w.show()
				setText(w.status, "Windows 종료 전에 DK-Drive에서 모든 드라이브를 해제하고 종료하세요.")
				return 0
			}
			return 1
		}
		if w.taskbarCreated != 0 && msg == w.taskbarCreated {
			if !w.tray(true) {
				w.show()
			}
			return 0
		}
	}
	return call("DefWindowProcW", h, uintptr(msg), wp, lp)
}

func (w *window) build() error {
	w.loading = true
	defer func() { w.loading = false }()
	if !initializeListView() {
		return errors.New("Windows 목록 보기 초기화 실패")
	}
	var err error
	add := func(class, text string, style uintptr, x, y, ww, hh, id int) uintptr {
		if err != nil {
			return 0
		}
		var h uintptr
		h, err = w.createControl(class, text, style, x, y, ww, hh, id)
		if h != 0 {
			w.layout = append(w.layout, controlLayout{
				handle: h,
				bounds: rect{Left: w.px(x), Top: w.px(y), Right: w.px(x + ww), Bottom: w.px(y + hh)},
				rule:   layoutRuleFor(class, x, y, ww, id),
			})
		}
		if class != "STATIC" {
			w.controls = append(w.controls, h)
		}
		return h
	}
	label := func(text string, x, y, ww int) { add("STATIC", text, 0, x, y, ww, 22, 0) }
	button := func(text string, x, y, ww, id int) uintptr {
		return add("BUTTON", text, 0x10000, x, y, ww, 28, id)
	}
	checkbox := func(text string, x, y, ww, id int) uintptr {
		return add("BUTTON", text, 0x10000|bsCheck, x, y, ww, 25, id)
	}
	label("등록된 연결", 16, 14, 240)
	w.list = add("SysListView32", "", 0x10000|0x800000|0x0001|0x0004|0x0008, 16, 40, 588, 300, idList)
	send(w.list, lvmSetExtendedListViewStyle, 0, 0x1|0x20|0x10000)
	for i, column := range []struct {
		title string
		width int
	}{{"드라이브", 70}, {"연결 이름", 225}, {"프로토콜", 135}, {"상태", 140}} {
		if !listViewAddColumn(w.list, i, int(w.px(column.width)), column.title) {
			return errors.New("프로필 목록 열 생성 실패")
		}
	}
	w.newButton = button("새 연결", 16, 352, 108, idNew)
	w.editButton = button("설정", 132, 352, 108, idEditProfile)
	w.deleteButton = button("삭제", 248, 352, 108, idDelete)
	w.connectButton = button("선택 연결", 364, 352, 108, idConnect)
	w.disconnectButton = button("선택 해제", 480, 352, 108, idDisconnect)
	w.connectAllButton = button("전체 연결", 16, 390, 282, idConnectAll)
	w.disconnectAllButton = button("전체 해제", 306, 390, 282, idDisconnectAll)
	w.closeToTray = checkbox("창 닫으면 트레이로", 16, 435, 300, idCloseToTray)
	w.startup = checkbox("Windows 로그인 시 실행", 16, 463, 300, idStartup)
	w.cacheButton = button("보존 캐시", 16, 500, 282, idRecovery)
	w.exitButton = button("프로그램 종료", 306, 500, 282, idExit)
	w.status = add("EDIT", "준비됨", 0x800000|0x800|4|0x40, 16, 540, 572, 50, 0)
	w.setTabOrder([]uintptr{
		w.list, w.newButton, w.editButton, w.deleteButton, w.connectButton, w.disconnectButton,
		w.connectAllButton, w.disconnectAllButton, w.closeToTray, w.startup, w.cacheButton, w.exitButton,
	})
	var client rect
	if call("GetClientRect", w.hwnd, uintptr(unsafe.Pointer(&client))) != 0 {
		w.baseClientWidth = client.Right - client.Left
		w.baseClientHeight = client.Bottom - client.Top
	}
	check(w.closeToTray, w.settings.CloseToTray)
	enabled, startErr := startupEnabled()
	if startErr != nil {
		return startErr
	}
	check(w.startup, enabled)
	w.updateRecoveryButton()
	w.clearDirty()
	return err
}

func (w *window) resize(width, height int32) {
	if width <= 0 || height <= 0 || w.baseClientWidth <= 0 || w.baseClientHeight <= 0 {
		return
	}
	deltaX := max(width-w.baseClientWidth, 0)
	deltaY := max(height-w.baseClientHeight, 0)
	for _, item := range w.layout {
		bounds := resizeBounds(item.bounds, item.rule, deltaX, deltaY)
		call("MoveWindow", item.handle, uintptr(bounds.Left), uintptr(bounds.Top),
			uintptr(bounds.Right-bounds.Left), uintptr(bounds.Bottom-bounds.Top), 1)
	}
}

func (w *window) setTabOrder(handles []uintptr) {
	var previous uintptr
	for _, handle := range handles {
		if handle == 0 {
			continue
		}
		call("SetWindowPos", handle, previous, 0, 0, 0, 0, 0x1|0x2|0x10)
		previous = handle
	}
}

func (w *window) handleKeyboardMessage(msg *message) bool {
	if msg.ID != wmKeyDown || w.busy {
		return false
	}
	if msg.WParam == 0x1b && (send(w.protocol, cbGetDroppedState, 0, 0) != 0 || send(w.drive, cbGetDroppedState, 0, 0) != 0) {
		return false
	}
	controlDown := int16(call("GetKeyState", 0x11)) < 0 // VK_CONTROL
	switch keyboardCommandFor(msg.WParam, controlDown, w.dirty) {
	case keyboardSelectAll:
		focused := call("GetFocus")
		if !w.isTextInput(focused) {
			return false
		}
		send(focused, emSetSel, 0, ^uintptr(0))
		return true
	case keyboardSave:
		w.save()
		return true
	case keyboardCancelEdit:
		w.cancelEditorChanges()
		return true
	default:
		return false
	}
}

func (w *window) isTextInput(h uintptr) bool {
	for _, input := range []uintptr{
		w.name, w.port, w.host, w.root, w.user,
		w.key, w.knownHosts, w.password, w.passphrase,
	} {
		if h == input {
			return true
		}
	}
	return false
}

func (w *window) cancelEditorChanges() {
	if !w.dirty || box(w.dialogOwner(), "저장되지 않은 연결 설정 변경을 버릴까요?", 0x134) != 6 {
		return
	}
	if w.selected >= 0 {
		w.loadEditor(w.selected)
		return
	}
	w.newProfile()
}

func (w *window) markDirty() {
	if w.loading || w.dirty {
		return
	}
	w.dirty = true
	setText(w.saveButton, "저장 *")
	setText(w.status, "저장되지 않은 연결 설정 변경 사항이 있습니다.")
	w.updateActionButtons()
}

func (w *window) isProfileInput(h uintptr) bool {
	for _, input := range []uintptr{
		w.name, w.drive, w.port, w.host, w.root, w.user,
		w.key, w.knownHosts, w.password, w.passphrase,
		w.readOnly, w.autoConnect, w.remember, w.insecure,
	} {
		if h == input {
			return true
		}
	}
	return false
}

func (w *window) clearDirty() {
	w.dirty = false
	setText(w.saveButton, "저장")
	w.updateActionButtons()
}

func (w *window) browsePath(target uintptr, title string, filter []uint16) {
	path, selected, err := chooseFile(w.dialogOwner(), title, strings.TrimSpace(getText(target)), filter)
	if err != nil {
		w.report(err)
		return
	}
	if !selected {
		return
	}
	setText(target, path)
	w.markDirty()
	w.updateProtocolControls()
}

func allowProfileChange(dirty bool, choice int, save func() bool) bool {
	if !dirty {
		return true
	}
	switch choice {
	case 6: // IDYES
		return save()
	case 7: // IDNO
		return true
	default: // IDCANCEL and window close
		return false
	}
}

func (w *window) confirmProfileChange() bool {
	if !w.dirty {
		return true
	}
	choice := box(w.dialogOwner(), "현재 연결 설정의 변경 사항을 저장할까요?\n\n예: 저장 후 닫기\n아니요: 변경 사항을 버리고 닫기\n취소: 설정 창 유지", 0x233)
	return allowProfileChange(true, int(choice), w.save)
}

func (w *window) setBusy(value bool) {
	w.busy = value
	var enabled uintptr = 1
	if value {
		enabled = 0
	}
	for _, h := range w.controls {
		call("EnableWindow", h, enabled)
	}
	if activeProfileEditor != nil {
		for _, h := range activeProfileEditor.controls {
			call("EnableWindow", h, enabled)
		}
	}
	call("EnableWindow", w.status, 1)
	if !value {
		w.updateProtocolControls()
		w.updateActionButtons()
	}
}

type protocolControlState struct {
	sftp       bool
	tls        bool
	password   bool
	passphrase bool
}

func enabledWord(value bool) uintptr {
	if value {
		return 1
	}
	return 0
}

func protocolControls(index int, hasPrivateKey bool) protocolControlState {
	sftp := index == 0
	return protocolControlState{
		sftp:       sftp,
		tls:        index == 1 || index == 4 || index == 5,
		password:   !sftp || !hasPrivateKey,
		passphrase: sftp && hasPrivateKey,
	}
}

func protocolControlHint(index int, hasPrivateKey bool) string {
	if index != 0 {
		return "비밀번호 인증 — SFTP 키 입력과 Passphrase는 사용하지 않습니다."
	}
	if hasPrivateKey {
		return "SFTP 개인키 인증 — 비밀번호는 사용하지 않습니다."
	}
	return "SFTP 비밀번호 인증 — 개인키를 선택하면 키 인증으로 전환됩니다."
}

func (w *window) updateProtocolControls() {
	if w.busy {
		return
	}
	index := int(send(w.protocol, cbGetCurSel, 0, 0))
	hasPrivateKey := strings.TrimSpace(getText(w.key)) != ""
	state := protocolControls(index, hasPrivateKey)
	setText(w.authHint, protocolControlHint(index, hasPrivateKey))
	if !state.password && w.passwordVisible {
		w.setSecretVisible(w.password, w.passwordToggle, &w.passwordVisible, false)
	}
	if !state.passphrase && w.passphraseVisible {
		w.setSecretVisible(w.passphrase, w.passphraseToggle, &w.passphraseVisible, false)
	}
	for _, h := range []uintptr{w.key, w.knownHosts, w.keyBrowse, w.knownHostsBrowse} {
		call("EnableWindow", h, enabledWord(state.sftp))
	}
	call("EnableWindow", w.password, enabledWord(state.password))
	call("EnableWindow", w.passphrase, enabledWord(state.passphrase))
	call("EnableWindow", w.passwordToggle, enabledWord(state.password))
	call("EnableWindow", w.passphraseToggle, enabledWord(state.passphrase))
	call("EnableWindow", w.insecure, enabledWord(state.tls))
}

func secretDisplay(visible bool) (uintptr, string) {
	if visible {
		return 0, "숨기기"
	}
	return uintptr('*'), "보기"
}

func (w *window) setSecretVisible(input, toggle uintptr, visible *bool, next bool) {
	*visible = next
	passwordCharacter, buttonText := secretDisplay(next)
	send(input, 0xcc, passwordCharacter, 0) // EM_SETPASSWORDCHAR
	setText(toggle, buttonText)
	call("InvalidateRect", input, 0, 1)
}

func (w *window) maskSecrets() {
	w.setSecretVisible(w.password, w.passwordToggle, &w.passwordVisible, false)
	w.setSecretVisible(w.passphrase, w.passphraseToggle, &w.passphraseVisible, false)
}

func (w *window) task(fn func() error, done func(error)) {
	if w.busy {
		return
	}
	w.setBusy(true)
	setText(w.status, "작업 중…")
	go func() {
		err := fn()
		w.results <- taskResult{err: err, done: done}
		call("PostMessageW", w.hwnd, wmTaskDone, 0, 0)
	}()
}

func (w *window) report(err error) {
	if err != nil {
		if activeProfileEditor == nil {
			w.show()
		}
		setText(w.status, err.Error())
		alert(w.dialogOwner(), err.Error())
	} else {
		setText(w.status, "작업 완료")
	}
}

func (w *window) reportConnectionError(id string, err error) {
	w.manager.RecordError(id, err)
	w.refreshList()
	w.report(err)
}

func (w *window) anyConnected() bool {
	for _, p := range w.settings.Profiles {
		if !connectionInactive(w.manager.State(p.ID)) {
			return true
		}
	}
	return false
}

func connectionInactive(state string) bool {
	return state == "연결 안 됨" || state == "오류"
}

type profileListRow struct {
	drive, name, protocol, state string
}

func profileProtocolName(p config.Profile) string {
	switch p.Protocol {
	case config.ProtocolSFTP:
		return "SFTP"
	case config.ProtocolWebDAV:
		if p.WebDAVScheme == "http" {
			return "WebDAV HTTP"
		}
		return "WebDAV HTTPS"
	case config.ProtocolFTP:
		return "FTP"
	case config.ProtocolFTPS:
		if p.FTPSMode == "implicit-ftps" {
			return "Implicit FTPS"
		}
		return "Explicit FTPS"
	default:
		return "알 수 없음"
	}
}

func profileListRows(profiles []config.SavedProfile, state func(string) string) []profileListRow {
	rows := make([]profileListRow, 0, len(profiles))
	for _, saved := range profiles {
		rows = append(rows, profileListRow{
			drive:    normalizedDriveLetter(saved.Profile.DriveLetter) + ":",
			name:     saved.Profile.Name,
			protocol: profileProtocolName(saved.Profile),
			state:    state(saved.ID),
		})
	}
	return rows
}

func (w *window) refreshList() {
	wasLoading := w.loading
	w.loading = true
	send(w.list, lvmDeleteAllItems, 0, 0)
	for i, row := range profileListRows(w.settings.Profiles, w.manager.State) {
		if !listViewAddRow(w.list, i, []string{row.drive, row.name, row.protocol, row.state}) {
			setText(w.status, "프로필 목록을 갱신하지 못했습니다.")
			break
		}
	}
	if w.selected >= 0 && w.selected < len(w.settings.Profiles) {
		listViewSelect(w.list, w.selected)
	} else {
		listViewSelect(w.list, -1)
	}
	w.loading = wasLoading
	w.updateActionButtons()
	w.updateTrayTooltip()
}

type profileButtonState struct {
	save, delete, connect, disconnect, connectAll, disconnectAll bool
}

func profileButtons(selected int, dirty bool, states []string) profileButtonState {
	result := profileButtonState{save: dirty}
	for _, state := range states {
		if connectionInactive(state) {
			result.connectAll = true
		} else {
			result.disconnectAll = true
		}
	}
	if selected < 0 || selected >= len(states) {
		return result
	}
	if connectionInactive(states[selected]) {
		result.delete = true
		result.connect = true
	} else {
		result.disconnect = true
	}
	return result
}

func (w *window) updateActionButtons() {
	if w.busy {
		return
	}
	states := make([]string, len(w.settings.Profiles))
	for i, saved := range w.settings.Profiles {
		states[i] = w.manager.State(saved.ID)
	}
	state := profileButtons(w.selected, w.dirty, states)
	for _, control := range []struct {
		handle  uintptr
		enabled bool
	}{
		{w.saveButton, state.save},
		{w.editButton, w.selected >= 0 && w.selected < len(w.settings.Profiles)},
		{w.deleteButton, state.delete},
		{w.connectButton, state.connect},
		{w.disconnectButton, state.disconnect},
		{w.connectAllButton, state.connectAll},
		{w.disconnectAllButton, state.disconnectAll},
	} {
		call("EnableWindow", control.handle, enabledWord(control.enabled))
	}
}

func (w *window) updateRecoveryButton() {
	setText(w.cacheButton, fmt.Sprintf("보존 캐시 (%d · %s)", len(w.recoveryItems), formatByteSize(w.cacheUsage.RecoveryBytes)))
}

func profileDiagnosticText(diagnostic connection.Diagnostic, usage localcache.ProfileUsage) string {
	result := fmt.Sprintf("상태: %s · 보존 캐시: %d개 / %s", diagnostic.State, usage.Items, formatByteSize(usage.Bytes))
	if diagnostic.LastError != "" {
		when := "시각 없음"
		if !diagnostic.ErrorAt.IsZero() {
			when = diagnostic.ErrorAt.Local().Format("2006-01-02 15:04:05")
		}
		result += "\r\n마지막 오류 (" + when + "): " + diagnostic.LastError
	}
	return result
}

func normalizedDriveLetter(value string) string {
	return strings.ToUpper(strings.TrimSuffix(strings.TrimSpace(value), ":"))
}

func trayProfileLabel(saved config.SavedProfile, state string) string {
	connected := "X"
	if state == "연결됨" {
		connected = "O"
	}
	return fmt.Sprintf("[%s] %s: %s", connected, normalizedDriveLetter(saved.Profile.DriveLetter), saved.Profile.Name)
}

func trayTooltip(profiles []config.SavedProfile, state func(string) string) string {
	letters := make([]string, 0, len(profiles))
	for _, saved := range profiles {
		if state(saved.ID) == "연결됨" {
			letters = append(letters, normalizedDriveLetter(saved.Profile.DriveLetter))
		}
	}
	slices.Sort(letters)
	if len(letters) == 0 {
		return "DK-Drive"
	}
	return "DK-Drive [" + strings.Join(letters, " ") + "]"
}

func driveLetterMask(letter string) uint32 {
	letter = normalizedDriveLetter(letter)
	if len(letter) != 1 || letter[0] < 'A' || letter[0] > 'Z' {
		return 0
	}
	return 1 << (letter[0] - 'A')
}

func availableDriveLetters(profiles []config.SavedProfile, selected int, used uint32) []string {
	reserved := make(map[string]bool, len(profiles))
	current := ""
	for i, saved := range profiles {
		letter := normalizedDriveLetter(saved.Profile.DriveLetter)
		if i == selected {
			current = letter
			continue
		}
		reserved[letter] = true
	}
	letters := make([]string, 0, 26)
	for ch := byte('A'); ch <= 'Z'; ch++ {
		letter := string(ch)
		if reserved[letter] {
			continue
		}
		if used&driveLetterMask(letter) != 0 && letter != current {
			continue
		}
		letters = append(letters, letter)
	}
	return letters
}

func validateDriveAssignment(letter, currentID string, profiles []config.SavedProfile, used uint32) error {
	letter = normalizedDriveLetter(letter)
	for _, saved := range profiles {
		if saved.ID != currentID && normalizedDriveLetter(saved.Profile.DriveLetter) == letter {
			return fmt.Errorf("드라이브 %s:는 다른 연결 프로필에서 사용 중입니다", letter)
		}
	}
	if used&driveLetterMask(letter) != 0 {
		return fmt.Errorf("드라이브 %s:는 Windows에서 이미 사용 중입니다", letter)
	}
	return nil
}

func (w *window) refreshDriveOptions(current string) {
	used, err := windows.GetLogicalDrives()
	if err != nil {
		used = 0
	}
	letters := availableDriveLetters(w.settings.Profiles, w.selected, used)
	current = normalizedDriveLetter(current)
	send(w.drive, cbReset, 0, 0)
	selected := -1
	for i, letter := range letters {
		sendText(w.drive, cbAddString, letter)
		if letter == current {
			selected = i
		}
	}
	if selected < 0 {
		for i, letter := range letters {
			if letter == "X" {
				selected = i
				break
			}
		}
	}
	if selected < 0 && len(letters) != 0 {
		selected = 0
	}
	if selected >= 0 {
		send(w.drive, cbSetCurSel, uintptr(selected), 0)
	}
}

func (w *window) newProfile() {
	w.loading = true
	defer func() { w.loading = false }()
	w.selected = -1
	w.secretFailed = false
	listViewSelect(w.list, -1)
	for _, h := range []uintptr{w.name, w.host, w.user, w.key, w.knownHosts, w.password, w.passphrase} {
		setText(h, "")
	}
	w.refreshDriveOptions("X")
	setText(w.port, "22")
	setText(w.root, "/")
	send(w.protocol, cbSetCurSel, 0, 0)
	for _, h := range []uintptr{w.readOnly, w.autoConnect, w.remember, w.insecure} {
		check(h, false)
	}
	w.maskSecrets()
	w.updateProtocolControls()
	setText(w.status, "새 연결 설정을 입력하세요.")
	w.clearDirty()
}

func protocolIndex(p config.Profile) int {
	switch p.Protocol {
	case config.ProtocolSFTP:
		return 0
	case config.ProtocolWebDAV:
		if p.WebDAVScheme == "http" {
			return 2
		}
		return 1
	case config.ProtocolFTP:
		return 3
	case config.ProtocolFTPS:
		if p.FTPSMode == "implicit-ftps" {
			return 5
		}
		return 4
	}
	return 0
}

func (w *window) secrets(p config.SavedProfile) (config.Secrets, error) {
	if s, ok := w.sessionSecrets[p.ID]; ok {
		return s, nil
	}
	return config.OpenSecrets(credential.DPAPI{}, p.ProtectedSecret)
}

func (w *window) loadEditor(index int) {
	if index < 0 || index >= len(w.settings.Profiles) {
		return
	}
	if index != w.selected && !w.confirmProfileChange() {
		wasLoading := w.loading
		w.loading = true
		if w.selected >= 0 {
			listViewSelect(w.list, w.selected)
		} else {
			listViewSelect(w.list, -1)
		}
		w.loading = wasLoading
		return
	}
	w.loading = true
	defer func() { w.loading = false }()
	w.selected = index
	// Saving from the confirmation dialog rebuilds the list and restores the
	// profile being saved. Re-select the profile the user actually clicked.
	listViewSelect(w.list, index)
	p := w.settings.Profiles[index]
	v := p.Profile
	w.refreshDriveOptions(v.DriveLetter)
	for _, f := range []struct {
		h uintptr
		s string
	}{{w.name, v.Name}, {w.port, strconv.Itoa(int(v.Port))}, {w.host, v.Host}, {w.root, v.RemotePath}, {w.user, v.Username}, {w.key, v.PrivateKey}, {w.knownHosts, v.KnownHosts}} {
		setText(f.h, f.s)
	}
	send(w.protocol, cbSetCurSel, uintptr(protocolIndex(v)), 0)
	check(w.readOnly, v.ReadOnly)
	check(w.autoConnect, v.AutoConnect)
	check(w.insecure, v.InsecureSkipTLSVerify)
	check(w.remember, len(p.ProtectedSecret) > 0)
	s, err := w.secrets(p)
	w.secretFailed = err != nil
	setText(w.password, s.Password)
	setText(w.passphrase, s.Passphrase)
	w.maskSecrets()
	w.updateProtocolControls()
	if err != nil {
		w.reportConnectionError(p.ID, err)
	} else {
		setText(w.status, profileDiagnosticText(w.manager.Diagnostic(p.ID), w.cacheUsage.Profiles[p.ID]))
	}
	w.clearDirty()
}

func (w *window) readEditor() (config.Profile, config.Secrets, error) {
	port, err := strconv.ParseUint(strings.TrimSpace(getText(w.port)), 10, 16)
	if err != nil || port == 0 {
		return config.Profile{}, config.Secrets{}, errors.New("포트는 1~65535여야 합니다")
	}
	name := strings.TrimSpace(getText(w.name))
	p := config.Profile{Name: name, DriveLetter: normalizedDriveLetter(getText(w.drive)), Port: uint16(port), Host: strings.TrimSpace(getText(w.host)), RemotePath: getText(w.root), Username: getText(w.user), VolumeName: name, ReadOnly: checked(w.readOnly), AutoConnect: checked(w.autoConnect), AutoReconnect: true, AuthMethod: config.AuthPassword, InsecureSkipTLSVerify: checked(w.insecure)}
	switch int(send(w.protocol, cbGetCurSel, 0, 0)) {
	case 0:
		p.Protocol = config.ProtocolSFTP
		p.PrivateKey = strings.TrimSpace(getText(w.key))
		p.KnownHosts = strings.TrimSpace(getText(w.knownHosts))
		if p.PrivateKey != "" {
			p.AuthMethod = config.AuthPrivateKey
		}
	case 1, 2:
		p.Protocol = config.ProtocolWebDAV
		p.WebDAVScheme = "https"
		if send(w.protocol, cbGetCurSel, 0, 0) == 2 {
			p.WebDAVScheme = "http"
		}
	case 3:
		p.Protocol = config.ProtocolFTP
	case 4, 5:
		p.Protocol = config.ProtocolFTPS
		p.FTPSMode = "explicit-ftps"
		if send(w.protocol, cbGetCurSel, 0, 0) == 5 {
			p.FTPSMode = "implicit-ftps"
		}
	}
	secret := config.Secrets{Password: getText(w.password), Passphrase: getText(w.passphrase)}
	if p.AuthMethod == config.AuthPrivateKey {
		secret.Password = ""
	} else {
		secret.Passphrase = ""
	}
	return p, secret, p.Validate()
}

func (w *window) save() bool {
	p, secret, err := w.readEditor()
	if err != nil {
		w.report(err)
		return false
	}
	currentID := ""
	if w.selected >= 0 {
		currentID = w.settings.Profiles[w.selected].ID
		if !connectionInactive(w.manager.State(currentID)) {
			w.report(errors.New("설정 변경 전에 연결을 해제하세요"))
			return false
		}
	}
	used, driveErr := windows.GetLogicalDrives()
	if driveErr != nil {
		w.report(fmt.Errorf("Windows 드라이브 문자 확인 실패: %w", driveErr))
		return false
	}
	if err := validateDriveAssignment(p.DriveLetter, currentID, w.settings.Profiles, used); err != nil {
		w.report(err)
		return false
	}
	if p.AutoConnect && !checked(w.remember) && (p.AuthMethod == config.AuthPassword || secret.Passphrase != "") {
		w.report(errors.New("다음 실행 때 자동 연결하려면 비밀번호/Passphrase 저장을 선택하세요"))
		return false
	}
	if w.secretFailed && checked(w.remember) && secret.Password == "" && secret.Passphrase == "" {
		w.report(errors.New("새 자격 증명을 입력하거나 저장 체크를 해제하세요; 기존 암호화 값을 빈 값으로 바꾸지 않습니다"))
		return false
	}
	if p.InsecureSkipTLSVerify && box(w.dialogOwner(), "서버 신원과 인증서 신뢰 검증을 건너뜁니다. 신뢰할 수 있는 테스트 서버에만 사용하세요. 저장할까요?", 0x34) != 6 {
		return false
	}
	if (p.Protocol == config.ProtocolFTP || p.WebDAVScheme == "http") && box(w.dialogOwner(), "이 연결은 비밀번호와 파일 내용을 평문으로 전송합니다. 저장할까요?", 0x34) != 6 {
		return false
	}
	var saved config.SavedProfile
	if w.selected >= 0 {
		saved = w.settings.Profiles[w.selected]
	} else {
		saved.ID, err = config.NewID()
		if err != nil {
			w.report(err)
			return false
		}
	}
	saved.Profile = p
	saved.ProtectedSecret = nil
	if checked(w.remember) {
		saved.ProtectedSecret, err = config.SealSecrets(credential.DPAPI{}, secret)
		if err != nil {
			w.report(err)
			return false
		}
	}
	next := w.settings
	next.Profiles = append([]config.SavedProfile(nil), w.settings.Profiles...)
	i := w.selected
	if i < 0 {
		i = len(next.Profiles)
		next.Profiles = append(next.Profiles, saved)
	} else {
		next.Profiles[i] = saved
	}
	if err := config.SaveSettings(w.filename, next); err != nil {
		w.report(err)
		return false
	}
	w.settings = next
	w.selected = i
	w.sessionSecrets[saved.ID] = secret
	w.secretFailed = false
	w.refreshList()
	w.clearDirty()
	setText(w.status, "설정을 저장했습니다. 선택 연결로 마운트하세요.")
	return true
}

func (w *window) deleteProfile() {
	if w.selected < 0 {
		return
	}
	p := w.settings.Profiles[w.selected]
	if !connectionInactive(w.manager.State(p.ID)) {
		w.report(errors.New("연결을 해제한 뒤 설정을 삭제하세요"))
		return
	}
	if box(w.hwnd, "이 연결 설정을 삭제할까요? 원격 파일은 삭제하지 않습니다.", 0x34) != 6 {
		return
	}
	next := w.settings
	next.Profiles = append([]config.SavedProfile(nil), w.settings.Profiles...)
	next.Profiles = append(next.Profiles[:w.selected], next.Profiles[w.selected+1:]...)
	if err := config.SaveSettings(w.filename, next); err != nil {
		w.report(err)
		return
	}
	delete(w.sessionSecrets, p.ID)
	w.settings = next
	w.selected = -1
	w.refreshList()
	setText(w.status, "연결 설정을 삭제했습니다.")
}

func (w *window) connect(index int) {
	if index < 0 || index >= len(w.settings.Profiles) {
		w.report(errors.New("연결 설정을 먼저 저장하고 선택하세요"))
		return
	}
	p := w.settings.Profiles[index]
	used, driveErr := windows.GetLogicalDrives()
	if driveErr != nil {
		w.reportConnectionError(p.ID, fmt.Errorf("Windows 드라이브 문자 확인 실패: %w", driveErr))
		return
	}
	if err := validateDriveAssignment(p.Profile.DriveLetter, p.ID, w.settings.Profiles, used); err != nil {
		w.reportConnectionError(p.ID, err)
		return
	}
	s, err := w.secrets(p)
	if err != nil {
		w.reportConnectionError(p.ID, err)
		return
	}
	w.task(func() error {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		return w.manager.Connect(ctx, p.ID, p.Profile, s)
	}, w.report)
}

func (w *window) connectAll(auto bool) {
	profiles := append([]config.SavedProfile(nil), w.settings.Profiles...)
	w.task(func() error {
		var result error
		for _, p := range profiles {
			if auto && !p.Profile.AutoConnect {
				continue
			}
			if !connectionInactive(w.manager.State(p.ID)) {
				continue
			}
			used, driveErr := windows.GetLogicalDrives()
			if driveErr != nil {
				w.manager.RecordError(p.ID, driveErr)
				result = errors.Join(result, fmt.Errorf("%s: Windows 드라이브 문자 확인 실패: %w", p.Profile.Name, driveErr))
				continue
			}
			if driveErr = validateDriveAssignment(p.Profile.DriveLetter, p.ID, profiles, used); driveErr != nil {
				w.manager.RecordError(p.ID, driveErr)
				result = errors.Join(result, fmt.Errorf("%s: %w", p.Profile.Name, driveErr))
				continue
			}
			s, err := w.secrets(p)
			if err != nil {
				w.manager.RecordError(p.ID, err)
			}
			if err == nil {
				ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				err = w.manager.Connect(ctx, p.ID, p.Profile, s)
				cancel()
			}
			if err != nil {
				result = errors.Join(result, fmt.Errorf("%s: %w", p.Profile.Name, err))
			}
		}
		return result
	}, w.report)
}

func (w *window) exit() {
	if w.busy {
		return
	}
	if activeRecovery != nil && activeRecovery.busy {
		call("SetForegroundWindow", activeRecovery.hwnd)
		alert(activeRecovery.hwnd, "원격 재시도가 끝난 뒤 프로그램을 종료하세요")
		return
	}
	w.disconnect(w.settings.Profiles, true)
}

func (w *window) disconnect(profiles []config.SavedProfile, exiting bool) {
	profiles = append([]config.SavedProfile(nil), profiles...)
	var result disconnectResult
	w.task(func() error {
		result = disconnectProfiles(w.manager, profiles, func(p config.SavedProfile, failure error) bool {
			answer := make(chan bool, 1)
			w.results <- taskResult{answer: answer, confirm: func() bool {
				w.show()
				// MB_YESNO | MB_ICONWARNING | MB_DEFBUTTON2: No is default.
				return box(w.hwnd, forceDisconnectPrompt(p, failure), 0x134) == 6
			}}
			call("PostMessageW", w.hwnd, wmTaskDone, 0, 0)
			return <-answer
		})
		return result.err
	}, func(err error) {
		// Completion dialogs run a nested Windows message loop too. Keep tray
		// commands blocked until the result is acknowledged (especially on exit).
		w.setBusy(true)
		defer w.setBusy(false)
		if err != nil {
			w.report(err)
		} else if len(result.canceled) != 0 {
			setText(w.status, "강제 해제를 취소하여 연결을 유지했습니다: "+strings.Join(result.canceled, ", "))
		} else {
			w.report(nil)
		}
		if result.forceMessage != "" {
			w.show()
			setText(w.status, result.forceMessage)
			alert(w.hwnd, result.forceMessage)
		}
		if !exiting {
			if scanErr := w.refreshRecoveryItems(); scanErr != nil {
				alert(w.hwnd, scanErr.Error())
			}
		}
		if exiting && err == nil && len(result.canceled) == 0 {
			call("DestroyWindow", w.hwnd)
		}
	})
}

func (w *window) refreshRecoveryItems() error {
	store, err := localcache.New("")
	if err != nil {
		return err
	}
	items, err := store.Scan()
	if err != nil {
		return err
	}
	usage, err := store.Usage(items)
	if err != nil {
		return err
	}
	w.recoveryItems = items
	w.cacheUsage = usage
	w.updateRecoveryButton()
	return nil
}

func (w *window) clearCache() {
	if w.anyConnected() {
		w.report(errors.New("연결된 드라이브가 있으면 캐시를 정리할 수 없습니다. 모든 드라이브를 먼저 해제하세요"))
		return
	}
	if box(w.hwnd, "캐시 폴더의 모든 파일을 삭제할까요?\n\n보존된 복구 파일과 메타데이터가 모두 영구 삭제되며 되돌릴 수 없습니다.", 0x134) != 6 {
		return
	}
	store, err := localcache.New("")
	if err != nil {
		w.report(err)
		return
	}
	removed := 0
	w.task(func() error {
		var clearErr error
		removed, clearErr = store.Clear()
		return clearErr
	}, func(clearErr error) {
		scanErr := w.refreshRecoveryItems()
		if err := errors.Join(clearErr, scanErr); err != nil {
			w.report(err)
			return
		}
		setText(w.status, fmt.Sprintf("캐시 정리 완료: %d개 항목 삭제", removed))
	})
}

func (w *window) handleListNotification(value uintptr) bool {
	if value == 0 {
		return false
	}
	// Windows passes NMHDR as an ABI pointer-sized word. Reinterpret that word
	// without uintptr arithmetic and use it only during this callback.
	pointer := *(*unsafe.Pointer)(unsafe.Pointer(&value))
	header := (*notifyHeader)(pointer)
	if header.WindowFrom != w.list {
		return false
	}
	if header.Code == -3 { // NM_DBLCLK
		if w.selected >= 0 {
			if err := showProfileEditor(w, w.selected); err != nil {
				w.report(err)
			}
		}
		return true
	}
	if header.Code != lvnItemChanged {
		return false
	}
	if w.loading {
		return true
	}
	notification := (*notifyListView)(pointer)
	if notification.Item >= 0 && notification.Changed&lvifState != 0 &&
		notification.NewState&lvisSelected != 0 && notification.OldState&lvisSelected == 0 {
		w.selected = int(notification.Item)
		p := w.settings.Profiles[w.selected]
		setText(w.status, profileDiagnosticText(w.manager.Diagnostic(p.ID), w.cacheUsage.Profiles[p.ID]))
		w.updateActionButtons()
	}
	return true
}

func (w *window) command(id, notice int, control uintptr) {
	if id == idShow {
		w.show()
		return
	}
	if w.busy {
		return
	}
	if !w.loading && ((id == 0 && w.isProfileInput(control) && (notice == 0 || notice == 0x300)) ||
		((id == idProtocol || id == idDrive) && notice == 1)) {
		w.markDirty()
	}
	if !w.loading && control == w.key && notice == 0x300 {
		w.updateProtocolControls()
	}
	if id >= 1000 && id < 1000+len(w.settings.Profiles) {
		i := id - 1000
		p := w.settings.Profiles[i]
		if connectionInactive(w.manager.State(p.ID)) {
			w.connect(i)
		} else {
			w.disconnect([]config.SavedProfile{p}, false)
		}
		return
	}
	switch id {
	case idNew:
		w.show()
		if err := showProfileEditor(w, -1); err != nil {
			w.report(err)
		}
	case idEditProfile:
		if w.selected >= 0 {
			if err := showProfileEditor(w, w.selected); err != nil {
				w.report(err)
			}
		}
	case idSave:
		w.save()
	case idBrowseKey:
		w.browsePath(w.key, "SFTP 개인키 선택", fileDialogFilter(
			"개인키 파일 (*.pem;*.key;*.ppk)", "*.pem;*.key;*.ppk",
			"모든 파일 (*.*)", "*.*",
		))
	case idBrowseKnownHosts:
		w.browsePath(w.knownHosts, "SFTP known_hosts 선택", fileDialogFilter(
			"known_hosts 파일", "known_hosts*",
			"모든 파일 (*.*)", "*.*",
		))
	case idTogglePassword:
		w.setSecretVisible(w.password, w.passwordToggle, &w.passwordVisible, !w.passwordVisible)
		call("SetFocus", w.password)
	case idTogglePassphrase:
		w.setSecretVisible(w.passphrase, w.passphraseToggle, &w.passphraseVisible, !w.passphraseVisible)
		call("SetFocus", w.passphrase)
	case idTestConnection:
		p, s, err := w.readEditor()
		if err != nil {
			w.report(err)
			return
		}
		if (p.InsecureSkipTLSVerify || p.Protocol == config.ProtocolFTP || p.WebDAVScheme == "http") && box(w.dialogOwner(), "평문 전송 또는 인증서 검증 우회 설정입니다. 신뢰할 수 있는 테스트 서버에 연결할까요?", 0x34) != 6 {
			return
		}
		w.task(func() error {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			b, err := connection.OpenBackend(ctx, p, s)
			if err != nil {
				return err
			}
			return b.Close()
		}, func(err error) {
			w.report(err)
			if err == nil {
				setText(w.status, "연결·원격 시작 경로 확인 통과 (파일 변경·마운트 없음)")
				box(w.dialogOwner(), "연결·원격 시작 경로 확인을 통과했습니다.\n\n파일 변경이나 드라이브 연결은 수행하지 않았습니다.", 0x40)
			}
		})
	case idDelete:
		w.deleteProfile()
	case idConnect:
		w.connect(w.selected)
	case idDisconnect:
		if w.selected >= 0 {
			w.disconnect([]config.SavedProfile{w.settings.Profiles[w.selected]}, false)
		}
	case idConnectAll:
		w.connectAll(false)
	case idDisconnectAll:
		w.disconnect(w.settings.Profiles, false)
	case idRecovery:
		if err := showRecoveryDialog(w); err != nil {
			w.report(err)
		}
	case idOpenCacheFolder:
		store, err := localcache.New("")
		if err == nil {
			err = openFolder(store.Directory())
		}
		if err != nil {
			w.report(err)
		}
	case idClearCache:
		w.clearCache()
	case idExit:
		w.exit()
	case idProtocol:
		if notice == 1 {
			ports := []string{"22", "443", "80", "21", "21", "990"}
			i := int(send(w.protocol, cbGetCurSel, 0, 0))
			if i >= 0 && i < len(ports) {
				setText(w.port, ports[i])
				check(w.insecure, false)
				w.updateProtocolControls()
			}
		}
	case idCloseToTray:
		next := w.settings
		next.CloseToTray = checked(w.closeToTray)
		if err := config.SaveSettings(w.filename, next); err != nil {
			check(w.closeToTray, w.settings.CloseToTray)
			w.report(err)
		} else {
			w.settings = next
		}
	case idStartup:
		value := checked(w.startup)
		if err := setStartup(value); err != nil {
			check(w.startup, !value)
			w.report(err)
		}
	}
}
