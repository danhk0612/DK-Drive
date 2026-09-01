//go:build windows

package desktop

import (
	"errors"
	"fmt"
	"path"
	"strings"
	"syscall"
	"unsafe"

	localcache "github.com/danhk0612/DK-Drive/internal/cache"
	"golang.org/x/sys/windows"
)

const (
	recoveryClassName = "DKDrive.Recovery.0_6"
	idRecoveryList    = 200 + iota
	idRecoveryRefresh
	idRecoveryFolder
	idRecoveryExport
	idRecoveryDelete
	idRecoveryClose
)

var activeRecovery *recoveryDialog

type recoveryDialog struct {
	hwnd, owner, font uintptr
	scale             float64
	ownerWindow       *window
	store             *localcache.Store
	items             []localcache.RecoveryItem
	selected          int
	list, detail      uintptr
	refreshButton     uintptr
	folderButton      uintptr
	exportButton      uintptr
	deleteButton      uintptr
	closeButton       uintptr
}

func showRecoveryDialog(owner *window) error {
	store, err := localcache.New("")
	if err != nil {
		return err
	}
	dialog := &recoveryDialog{owner: owner.hwnd, font: owner.font, scale: owner.scale, ownerWindow: owner, store: store, selected: -1}
	items, err := store.Scan()
	if err != nil {
		return err
	}
	dialog.items = items
	var instance windows.Handle
	if err := windows.GetModuleHandleEx(0, nil, &instance); err != nil {
		return err
	}
	class := windowClass{
		Size: uint32(unsafe.Sizeof(windowClass{})), Proc: syscall.NewCallback(recoveryWndProc),
		Instance: uintptr(instance), Icon: owner.icon, SmallIcon: owner.smallIcon,
		Cursor: call("LoadCursorW", 0, 32512), Background: 16, Name: utf(recoveryClassName),
	}
	if call("RegisterClassExW", uintptr(unsafe.Pointer(&class))) == 0 {
		return errors.New("복구 창 클래스 등록 실패")
	}
	defer call("UnregisterClassW", uintptr(unsafe.Pointer(class.Name)), uintptr(instance))
	title := utf("DK-Drive — 보존 캐시 복구")
	h, _, createErr := user32.NewProc("CreateWindowExW").Call(
		0x10000, uintptr(unsafe.Pointer(class.Name)), uintptr(unsafe.Pointer(title)),
		wsOverlappedWindow, 0x80000000, 0x80000000,
		uintptr(dialog.px(920)), uintptr(dialog.px(620)), owner.hwnd, 0, uintptr(instance), 0,
	)
	if h == 0 {
		return fmt.Errorf("보존 캐시 복구 창 생성 실패: %w", createErr)
	}
	dialog.hwnd = h
	activeRecovery = dialog
	defer func() { activeRecovery = nil }()
	if err := dialog.build(); err != nil {
		call("DestroyWindow", h)
		return err
	}
	dialog.refresh()
	call("EnableWindow", owner.hwnd, 0)
	defer func() {
		call("EnableWindow", owner.hwnd, 1)
		call("SetForegroundWindow", owner.hwnd)
		owner.recoveryItems = dialog.items
		owner.updateRecoveryButton()
	}()
	call("ShowWindow", h, 5)
	call("SetForegroundWindow", h)
	var msg message
	for call("IsWindow", h) != 0 {
		result := int32(call("GetMessageW", uintptr(unsafe.Pointer(&msg)), 0, 0, 0))
		if result == -1 {
			call("DestroyWindow", h)
			return errors.New("복구 창 메시지 처리 실패")
		}
		if result == 0 {
			call("DestroyWindow", h)
			call("PostQuitMessage", 0)
			break
		}
		if call("IsDialogMessageW", h, uintptr(unsafe.Pointer(&msg))) == 0 {
			call("TranslateMessage", uintptr(unsafe.Pointer(&msg)))
			call("DispatchMessageW", uintptr(unsafe.Pointer(&msg)))
		}
	}
	return nil
}

func (dialog *recoveryDialog) px(value int) int32 {
	return int32(float64(value) * dialog.scale)
}

func (dialog *recoveryDialog) createControl(class, text string, style uintptr, x, y, width, height, id int) (uintptr, error) {
	classText, valueText := utf(class), utf(text)
	h, _, err := user32.NewProc("CreateWindowExW").Call(
		0, uintptr(unsafe.Pointer(classText)), uintptr(unsafe.Pointer(valueText)),
		0x40000000|0x10000000|style, uintptr(dialog.px(x)), uintptr(dialog.px(y)),
		uintptr(dialog.px(width)), uintptr(dialog.px(height)), dialog.hwnd, uintptr(id), 0, 0,
	)
	if h == 0 {
		return 0, fmt.Errorf("복구 UI 컨트롤 생성 실패: %w", err)
	}
	send(h, 0x30, dialog.font, 1)
	return h, nil
}

func (dialog *recoveryDialog) build() error {
	var err error
	add := func(class, text string, style uintptr, x, y, width, height, id int) uintptr {
		if err != nil {
			return 0
		}
		var h uintptr
		h, err = dialog.createControl(class, text, style, x, y, width, height, id)
		return h
	}
	add("STATIC", "보존된 캐시는 자동 업로드·삭제하지 않습니다. 내용을 확인한 뒤 내보내거나 명시적으로 삭제하세요.", 0, 16, 14, 870, 22, 0)
	dialog.list = add("SysListView32", "", 0x800000|0x0001|0x0004|0x0008, 16, 42, 870, 330, idRecoveryList)
	send(dialog.list, lvmSetExtendedListViewStyle, 0, 0x1|0x20|0x10000)
	columns := []struct {
		title string
		width int
	}{
		{"상태", 120}, {"프로필", 150}, {"프로토콜", 90},
		{"원격 경로", 270}, {"크기", 90}, {"보존 시각", 150},
	}
	for index, column := range columns {
		if !listViewAddColumn(dialog.list, index, int(dialog.px(column.width)), column.title) {
			return errors.New("보존 캐시 목록 열 생성 실패")
		}
	}
	dialog.detail = add("EDIT", "", 0x800000|0x800|4|0x40, 16, 386, 870, 130, 0)
	dialog.refreshButton = add("BUTTON", "새로 고침", 0, 16, 532, 110, 30, idRecoveryRefresh)
	dialog.folderButton = add("BUTTON", "캐시 폴더 열기", 0, 140, 532, 140, 30, idRecoveryFolder)
	dialog.exportButton = add("BUTTON", "선택 항목 내보내기…", 0, 294, 532, 190, 30, idRecoveryExport)
	dialog.deleteButton = add("BUTTON", "선택 항목 삭제", 0, 498, 532, 145, 30, idRecoveryDelete)
	dialog.closeButton = add("BUTTON", "닫기", bsDefault, 756, 532, 130, 30, idRecoveryClose)
	return err
}

func (dialog *recoveryDialog) refresh() {
	items, err := dialog.store.Scan()
	if err != nil {
		alert(dialog.hwnd, err.Error())
		return
	}
	dialog.items = items
	if dialog.selected >= len(items) {
		dialog.selected = -1
	}
	send(dialog.list, lvmDeleteAllItems, 0, 0)
	for index, item := range items {
		metadata := item.Metadata
		profile := metadata.ProfileName
		if profile == "" {
			profile = "(확인 불가)"
		}
		preservedAt := "-"
		if !metadata.PreservedAt.IsZero() {
			preservedAt = metadata.PreservedAt.Local().Format("2006-01-02 15:04:05")
		}
		listViewAddRow(dialog.list, index, []string{
			recoveryStateText(metadata.RecoveryState), profile, recoveryProtocolText(metadata.Protocol),
			metadata.RemotePath, formatByteSize(metadata.Size), preservedAt,
		})
	}
	if dialog.selected < 0 && len(items) > 0 {
		dialog.selected = 0
	}
	listViewSelect(dialog.list, dialog.selected)
	dialog.updateDetails()
}

func (dialog *recoveryDialog) updateDetails() {
	if dialog.selected < 0 || dialog.selected >= len(dialog.items) {
		setText(dialog.detail, "보존 캐시 항목이 없습니다.")
		call("EnableWindow", dialog.exportButton, 0)
		call("EnableWindow", dialog.deleteButton, 0)
		return
	}
	item := dialog.items[dialog.selected]
	metadata := item.Metadata
	lines := []string{
		"상태: " + recoveryStateText(metadata.RecoveryState),
		"로컬 캐시: " + metadata.StagingPath,
		"메타데이터: " + emptyValue(item.MetadataPath),
		"보존 사유: " + recoveryReasonText(metadata.Reason),
	}
	if metadata.LastError != "" {
		lines = append(lines, "마지막 오류: "+metadata.LastError)
	}
	if item.Problem != "" {
		lines = append(lines, "검사 결과: "+item.Problem)
	}
	setText(dialog.detail, strings.Join(lines, "\r\n"))
	call("EnableWindow", dialog.exportButton, enabledWord(recoveryExportable(item)))
	call("EnableWindow", dialog.deleteButton, enabledWord(recoveryDeletable(item)))
}

func emptyValue(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return "(없음)"
}

func recoveryExportable(item localcache.RecoveryItem) bool {
	return item.Metadata.RecoveryState == localcache.StatePreserved || item.Metadata.RecoveryState == localcache.StateMissingMetadata
}

func recoveryDeletable(item localcache.RecoveryItem) bool {
	switch item.Metadata.RecoveryState {
	case localcache.StatePreserved, localcache.StateMissingMetadata, localcache.StateMissingStaging,
		localcache.StateInvalidMetadata, localcache.StateUnsafeStaging:
		return true
	default:
		return false
	}
}

func recoveryStateText(state localcache.RecoveryState) string {
	switch state {
	case localcache.StatePreserved:
		return "보존됨"
	case localcache.StateMissingMetadata:
		return "메타데이터 없음"
	case localcache.StateMissingStaging:
		return "캐시 파일 없음"
	case localcache.StateInvalidMetadata:
		return "메타데이터 오류"
	case localcache.StateUnsafeStaging:
		return "안전하지 않은 파일"
	default:
		return "알 수 없음"
	}
}

func recoveryReasonText(reason string) string {
	switch reason {
	case localcache.ReasonUploadFailed:
		return "업로드 실패"
	case localcache.ReasonForceDisconnect:
		return "사용자 승인 강제 해제"
	case "":
		return "(확인 불가)"
	default:
		return reason
	}
}

func recoveryProtocolText(protocol string) string {
	switch strings.ToLower(protocol) {
	case "sftp":
		return "SFTP"
	case "webdav":
		return "WebDAV"
	case "ftp":
		return "FTP"
	case "ftps":
		return "FTPS"
	case "":
		return "-"
	default:
		return protocol
	}
}

func formatByteSize(size int64) string {
	if size < 1024 {
		return fmt.Sprintf("%d B", size)
	}
	units := []string{"KB", "MB", "GB", "TB"}
	value := float64(size)
	for _, unit := range units {
		value /= 1024
		if value < 1024 || unit == units[len(units)-1] {
			return fmt.Sprintf("%.1f %s", value, unit)
		}
	}
	return fmt.Sprintf("%d B", size)
}

func recoverySuggestedName(item localcache.RecoveryItem) string {
	name := path.Base(strings.ReplaceAll(item.Metadata.RemotePath, "\\", "/"))
	if name == "." || name == "/" || name == "" {
		name = "DK-Drive 복구 파일.bin"
	}
	name = strings.Map(func(r rune) rune {
		if strings.ContainsRune(`<>:"/\|?*`, r) || r < 32 {
			return '_'
		}
		return r
	}, name)
	name = strings.TrimRight(name, ". ")
	if name == "" {
		return "DK-Drive 복구 파일.bin"
	}
	return name
}

func (dialog *recoveryDialog) exportSelected() {
	if dialog.selected < 0 || dialog.selected >= len(dialog.items) {
		return
	}
	item := dialog.items[dialog.selected]
	if !recoveryExportable(item) {
		return
	}
	destination, selected, err := chooseSaveFile(dialog.hwnd, "보존 캐시를 로컬 파일로 내보내기", recoverySuggestedName(item))
	if err != nil {
		alert(dialog.hwnd, err.Error())
		return
	}
	if !selected {
		return
	}
	if err := dialog.store.Export(item, destination); err != nil {
		alert(dialog.hwnd, err.Error()+"\n\n원본 캐시와 메타데이터는 그대로 보존했습니다.")
		return
	}
	box(dialog.hwnd, "로컬 파일로 내보냈습니다.\n\n"+destination+"\n\n원본 캐시는 삭제하지 않았습니다.", 0x40)
}

func (dialog *recoveryDialog) deleteSelected() {
	if dialog.selected < 0 || dialog.selected >= len(dialog.items) {
		return
	}
	if dialog.ownerWindow.anyConnected() {
		alert(dialog.hwnd, "연결된 드라이브가 있으면 복구 항목을 삭제할 수 없습니다. 모든 드라이브를 먼저 해제하세요.")
		return
	}
	item := dialog.items[dialog.selected]
	if !recoveryDeletable(item) {
		return
	}
	remotePath := emptyValue(item.Metadata.RemotePath)
	message := "선택한 보존 캐시 항목을 삭제할까요?\n\n원격 경로: " + remotePath +
		"\n로컬 캐시: " + item.Metadata.StagingPath +
		"\n크기: " + formatByteSize(item.Metadata.Size) +
		"\n\n로컬로 내보내지 않은 데이터가 영구 삭제되며 되돌릴 수 없습니다."
	if box(dialog.hwnd, message, 0x134) != 6 {
		return
	}
	if err := dialog.store.Delete(item); err != nil {
		dialog.refresh()
		alert(dialog.hwnd, err.Error())
		return
	}
	dialog.refresh()
	box(dialog.hwnd, "선택한 보존 캐시 항목을 삭제했습니다.", 0x40)
}

func (dialog *recoveryDialog) resize(width, height int32) {
	minimumWidth, minimumHeight := dialog.px(800), dialog.px(500)
	if width < minimumWidth || height < minimumHeight {
		return
	}
	margin := dialog.px(16)
	listTop := dialog.px(42)
	buttonHeight := dialog.px(30)
	buttonY := height - dialog.px(58)
	detailHeight := dialog.px(130)
	detailY := buttonY - dialog.px(16) - detailHeight
	listHeight := detailY - dialog.px(14) - listTop
	call("MoveWindow", dialog.list, uintptr(margin), uintptr(listTop), uintptr(width-2*margin), uintptr(listHeight), 1)
	call("MoveWindow", dialog.detail, uintptr(margin), uintptr(detailY), uintptr(width-2*margin), uintptr(detailHeight), 1)
	call("MoveWindow", dialog.refreshButton, uintptr(margin), uintptr(buttonY), uintptr(dialog.px(110)), uintptr(buttonHeight), 1)
	call("MoveWindow", dialog.folderButton, uintptr(dialog.px(140)), uintptr(buttonY), uintptr(dialog.px(140)), uintptr(buttonHeight), 1)
	call("MoveWindow", dialog.exportButton, uintptr(dialog.px(294)), uintptr(buttonY), uintptr(dialog.px(190)), uintptr(buttonHeight), 1)
	call("MoveWindow", dialog.deleteButton, uintptr(dialog.px(498)), uintptr(buttonY), uintptr(dialog.px(145)), uintptr(buttonHeight), 1)
	call("MoveWindow", dialog.closeButton, uintptr(width-dialog.px(146)), uintptr(buttonY), uintptr(dialog.px(130)), uintptr(buttonHeight), 1)
}

func recoveryWndProc(h uintptr, msg uint32, wp, lp uintptr) uintptr {
	dialog := activeRecovery
	if dialog == nil || dialog.hwnd != h {
		return call("DefWindowProcW", h, uintptr(msg), wp, lp)
	}
	switch msg {
	case wmCommand:
		switch int(wp & 0xffff) {
		case idRecoveryRefresh:
			dialog.refresh()
		case idRecoveryFolder:
			if err := openFolder(dialog.store.Directory()); err != nil {
				alert(h, err.Error())
			}
		case idRecoveryExport:
			dialog.exportSelected()
		case idRecoveryDelete:
			dialog.deleteSelected()
		case idRecoveryClose:
			call("DestroyWindow", h)
		}
		return 0
	case wmNotify:
		if lp != 0 {
			pointer := *(*unsafe.Pointer)(unsafe.Pointer(&lp))
			notification := (*notifyListView)(pointer)
			if notification.Header.WindowFrom == dialog.list && notification.Header.Code == lvnItemChanged && notification.Changed&lvifState != 0 && notification.NewState&lvisSelected != 0 && notification.Item >= 0 && int(notification.Item) < len(dialog.items) {
				dialog.selected = int(notification.Item)
				dialog.updateDetails()
			}
		}
		return 0
	case wmSize:
		dialog.resize(int32(lp&0xffff), int32((lp>>16)&0xffff))
		return 0
	case wmGetMinMaxInfo:
		if lp != 0 {
			pointer := *(*unsafe.Pointer)(unsafe.Pointer(&lp))
			limits := (*minMaxInfo)(pointer)
			limits.MinTrackSize = point{X: dialog.px(800), Y: dialog.px(500)}
			return 0
		}
	case wmClose:
		call("DestroyWindow", h)
		return 0
	}
	return call("DefWindowProcW", h, uintptr(msg), wp, lp)
}
