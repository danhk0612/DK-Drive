//go:build windows

package desktop

import (
	"errors"
	"fmt"
	"runtime"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

const profileEditorClassName = "DKDrive.ProfileEditor.0_6"

var (
	activeProfileEditor          *profileEditorDialog
	profileEditorClassRegistered bool
	profileEditorWndProcCallback = syscall.NewCallback(profileEditorWndProc)
)

type profileEditorDialog struct {
	hwnd     uintptr
	owner    *window
	controls []uintptr
}

func showProfileEditor(owner *window, index int) error {
	if activeProfileEditor != nil && call("IsWindow", activeProfileEditor.hwnd) != 0 {
		call("ShowWindow", activeProfileEditor.hwnd, 5)
		call("SetForegroundWindow", activeProfileEditor.hwnd)
		return nil
	}
	var instance windows.Handle
	if err := windows.GetModuleHandleEx(0, nil, &instance); err != nil {
		return err
	}
	if !profileEditorClassRegistered {
		class := windowClass{
			Size: uint32(unsafe.Sizeof(windowClass{})), Proc: profileEditorWndProcCallback,
			Instance: uintptr(instance), Icon: owner.icon, SmallIcon: owner.smallIcon,
			Cursor: call("LoadCursorW", 0, 32512), Background: 16, Name: utf(profileEditorClassName),
		}
		if call("RegisterClassExW", uintptr(unsafe.Pointer(&class))) == 0 {
			return errors.New("연결 설정 창 클래스 등록 실패")
		}
		profileEditorClassRegistered = true
	}
	title := "DK-Drive — 새 연결 설정"
	if index >= 0 && index < len(owner.settings.Profiles) {
		title = "DK-Drive — " + owner.settings.Profiles[index].Profile.Name + " 설정"
	}
	classNameText, titleText := utf(profileEditorClassName), utf(title)
	h, _, createErr := user32.NewProc("CreateWindowExW").Call(
		0x10000, uintptr(unsafe.Pointer(classNameText)), uintptr(unsafe.Pointer(titleText)),
		wsOverlappedWindow&^(0x40000|0x10000), 0x80000000, 0x80000000, uintptr(owner.px(660)), uintptr(owner.px(625)),
		owner.hwnd, 0, uintptr(instance), 0,
	)
	runtime.KeepAlive(classNameText)
	runtime.KeepAlive(titleText)
	if h == 0 {
		return fmt.Errorf("연결 설정 창 생성 실패: %w", createErr)
	}
	dialog := &profileEditorDialog{hwnd: h, owner: owner}
	activeProfileEditor = dialog
	if err := dialog.build(); err != nil {
		call("DestroyWindow", h)
		return err
	}
	if index >= 0 {
		owner.selected = index
		owner.loadEditor(index)
	} else {
		owner.newProfile()
	}
	call("EnableWindow", owner.hwnd, 0)
	call("ShowWindow", h, 5)
	call("SetForegroundWindow", h)
	call("SetFocus", owner.name)
	return nil
}

func (dialog *profileEditorDialog) createControl(class, text string, style uintptr, x, y, width, height, id int) (uintptr, error) {
	classText, valueText := utf(class), utf(text)
	h, _, err := user32.NewProc("CreateWindowExW").Call(
		0, uintptr(unsafe.Pointer(classText)), uintptr(unsafe.Pointer(valueText)),
		0x40000000|0x10000000|style, uintptr(dialog.owner.px(x)), uintptr(dialog.owner.px(y)),
		uintptr(dialog.owner.px(width)), uintptr(dialog.owner.px(height)), dialog.hwnd, uintptr(id), 0, 0,
	)
	runtime.KeepAlive(classText)
	runtime.KeepAlive(valueText)
	if h == 0 {
		return 0, fmt.Errorf("연결 설정 UI 컨트롤 생성 실패: %w", err)
	}
	send(h, 0x30, dialog.owner.font, 1)
	return h, nil
}

func (dialog *profileEditorDialog) build() error {
	w := dialog.owner
	var buildErr error
	add := func(class, text string, style uintptr, x, y, width, height, id int) uintptr {
		if buildErr != nil {
			return 0
		}
		h, err := dialog.createControl(class, text, style, x, y, width, height, id)
		if err != nil {
			buildErr = err
			return 0
		}
		if class != "STATIC" {
			dialog.controls = append(dialog.controls, h)
		}
		return h
	}
	label := func(text string, x, y, width int) { add("STATIC", text, 0, x, y, width, 22, 0) }
	edit := func(text string, x, y, width int, secret bool) uintptr {
		style := uintptr(0x10000 | 0x800000 | 0x80)
		if secret {
			style |= 0x20
		}
		h := add("EDIT", text, style, x, y, width, 24, 0)
		send(h, 0xc5, 4096, 0)
		return h
	}
	button := func(text string, x, y, width, id int) uintptr {
		return add("BUTTON", text, 0x10000, x, y, width, 28, id)
	}
	checkbox := func(text string, x, y, width int) uintptr {
		return add("BUTTON", text, 0x10000|bsCheck, x, y, width, 25, 0)
	}
	label("연결 이름", 16, 18, 110)
	w.name = edit("", 136, 16, 280, false)
	label("드라이브", 436, 18, 80)
	w.drive = add("COMBOBOX", "", 0x10000|0x200000|3, 526, 16, 100, 400, idDrive)
	label("프로토콜", 16, 52, 110)
	w.protocol = add("COMBOBOX", "", 0x10000|0x200000|3, 136, 50, 280, 240, idProtocol)
	for _, protocol := range protocols {
		sendText(w.protocol, cbAddString, protocol)
	}
	label("포트", 436, 52, 80)
	w.port = edit("22", 526, 50, 100, false)
	fields := []struct {
		name   string
		target *uintptr
	}{{"호스트", &w.host}, {"원격 시작 경로", &w.root}, {"사용자명", &w.user}, {"SFTP 개인키", &w.key}, {"known_hosts", &w.knownHosts}, {"비밀번호", &w.password}, {"키 Passphrase", &w.passphrase}}
	for index, field := range fields {
		y := 84 + index*34
		label(field.name, 16, y+2, 115)
		width := 490
		if index >= 3 {
			width = 400
		}
		*field.target = edit("", 136, y, width, index >= 5)
		switch index {
		case 3:
			w.keyBrowse = button("찾아보기…", 546, y-2, 80, idBrowseKey)
		case 4:
			w.knownHostsBrowse = button("찾아보기…", 546, y-2, 80, idBrowseKnownHosts)
		case 5:
			w.passwordToggle = button("보기", 546, y-2, 80, idTogglePassword)
		case 6:
			w.passphraseToggle = button("보기", 546, y-2, 80, idTogglePassphrase)
		}
	}
	w.readOnly = checkbox("읽기 전용", 16, 328, 160)
	w.autoConnect = checkbox("프로그램 시작 시 자동 연결", 226, 328, 370)
	w.remember = checkbox("비밀번호·Passphrase 저장 (현재 Windows 사용자용 암호화)", 16, 358, 610)
	w.insecure = checkbox("TLS 인증서 검증 건너뛰기 (신뢰할 수 있는 테스트 서버 전용)", 16, 388, 610)
	w.authHint = add("STATIC", "", 0, 16, 423, 610, 22, 0)
	label("FTP/HTTP는 평문 전송. 강제 해제 시 미저장 데이터가 손실될 수 있습니다.", 16, 447, 610)
	w.saveButton = button("저장", 16, 486, 120, idSave)
	w.testButton = add("BUTTON", "연결 테스트", 0x10000|bsDefault, 144, 486, 140, 28, idTestConnection)
	button("닫기", 506, 486, 120, idProfileClose)
	w.setTabOrder([]uintptr{
		w.name, w.drive, w.protocol, w.port, w.host, w.root, w.user,
		w.key, w.keyBrowse, w.knownHosts, w.knownHostsBrowse,
		w.password, w.passwordToggle, w.passphrase, w.passphraseToggle,
		w.readOnly, w.autoConnect, w.remember, w.insecure, w.saveButton, w.testButton,
	})
	return buildErr
}

func (dialog *profileEditorDialog) close() {
	if dialog.owner.busy {
		alert(dialog.hwnd, "연결 테스트가 끝난 뒤 설정 창을 닫으세요")
		return
	}
	if !dialog.owner.confirmProfileChange() {
		return
	}
	call("DestroyWindow", dialog.hwnd)
}

func (dialog *profileEditorDialog) destroy() {
	w := dialog.owner
	for _, target := range []*uintptr{
		&w.saveButton, &w.testButton, &w.authHint, &w.name, &w.protocol, &w.drive, &w.port,
		&w.host, &w.root, &w.user, &w.key, &w.knownHosts, &w.password, &w.passphrase,
		&w.keyBrowse, &w.knownHostsBrowse, &w.passwordToggle, &w.passphraseToggle,
		&w.readOnly, &w.autoConnect, &w.remember, &w.insecure,
	} {
		*target = 0
	}
	w.dirty = false
	if activeProfileEditor == dialog {
		activeProfileEditor = nil
	}
	call("EnableWindow", w.hwnd, 1)
	call("SetForegroundWindow", w.hwnd)
	w.updateActionButtons()
}

func profileEditorWndProc(h uintptr, msg uint32, wp, lp uintptr) uintptr {
	dialog := activeProfileEditor
	if dialog == nil || dialog.hwnd != h {
		return call("DefWindowProcW", h, uintptr(msg), wp, lp)
	}
	switch msg {
	case wmCommand:
		if int(wp&0xffff) == idProfileClose {
			dialog.close()
		} else {
			dialog.owner.command(int(wp&0xffff), int(wp>>16), lp)
		}
		return 0
	case wmClose:
		dialog.close()
		return 0
	case wmDestroy:
		dialog.destroy()
		return 0
	}
	return call("DefWindowProcW", h, uintptr(msg), wp, lp)
}
