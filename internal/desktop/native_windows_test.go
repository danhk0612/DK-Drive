//go:build windows

package desktop

import (
	"strings"
	"testing"
	"unsafe"

	"github.com/danhk0612/DK-Drive/internal/config"
)

func TestNativeLayoutAMD64(t *testing.T) {
	if unsafe.Sizeof(uintptr(0)) != 8 {
		t.Skip("64-bit desktop target")
	}
	if unsafe.Sizeof(notifyIcon{}) != 976 {
		t.Fatalf("NOTIFYICONDATAW size: %d", unsafe.Sizeof(notifyIcon{}))
	}
	if unsafe.Sizeof(windowClass{}) != 80 {
		t.Fatalf("WNDCLASSEXW size: %d", unsafe.Sizeof(windowClass{}))
	}
	if unsafe.Sizeof(message{}) != 48 {
		t.Fatalf("MSG size: %d", unsafe.Sizeof(message{}))
	}
	if unsafe.Sizeof(openFileName{}) != 152 {
		t.Fatalf("OPENFILENAMEW size: %d", unsafe.Sizeof(openFileName{}))
	}
	if unsafe.Sizeof(listViewColumn{}) != 56 {
		t.Fatalf("LVCOLUMNW size: %d", unsafe.Sizeof(listViewColumn{}))
	}
	if unsafe.Sizeof(listViewItem{}) != 88 {
		t.Fatalf("LVITEMW size: %d", unsafe.Sizeof(listViewItem{}))
	}
	if unsafe.Sizeof(notifyHeader{}) != 24 || unsafe.Sizeof(notifyListView{}) != 64 {
		t.Fatalf("list view notification sizes: header=%d item=%d", unsafe.Sizeof(notifyHeader{}), unsafe.Sizeof(notifyListView{}))
	}
}

func TestStartupCommandQuotesPath(t *testing.T) {
	got, err := startupCommand(`C:\Program Files\DKDrive\dkdrive.exe`)
	if err != nil || got != `"C:\Program Files\DKDrive\dkdrive.exe" --tray` {
		t.Fatal(got, err)
	}
	for _, p := range []string{`dkdrive.exe`, `C:\bad"name.exe`, "C:\\bad\nname.exe", `C:\` + strings.Repeat("a", 260) + ".exe"} {
		if _, err := startupCommand(p); err == nil {
			t.Fatal("accepted invalid startup path")
		}
	}
}

func TestProtocolDisplayMapping(t *testing.T) {
	for i, p := range []config.Profile{{Protocol: config.ProtocolSFTP}, {Protocol: config.ProtocolWebDAV}, {Protocol: config.ProtocolWebDAV, WebDAVScheme: "http"}, {Protocol: config.ProtocolFTP}, {Protocol: config.ProtocolFTPS}, {Protocol: config.ProtocolFTPS, FTPSMode: "implicit-ftps"}} {
		if got := protocolIndex(p); got != i {
			t.Fatalf("mapping %d: %d", i, got)
		}
	}
}

func TestProtocolControls(t *testing.T) {
	tests := []struct {
		name          string
		index         int
		hasPrivateKey bool
		want          protocolControlState
	}{
		{name: "SFTP password", index: 0, want: protocolControlState{sftp: true, password: true}},
		{name: "SFTP private key", index: 0, hasPrivateKey: true, want: protocolControlState{sftp: true, passphrase: true}},
		{name: "WebDAV HTTPS", index: 1, want: protocolControlState{tls: true, password: true}},
		{name: "WebDAV HTTP", index: 2, want: protocolControlState{password: true}},
		{name: "FTP", index: 3, want: protocolControlState{password: true}},
		{name: "Explicit FTPS", index: 4, want: protocolControlState{tls: true, password: true}},
		{name: "Implicit FTPS", index: 5, want: protocolControlState{tls: true, password: true}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := protocolControls(tt.index, tt.hasPrivateKey); got != tt.want {
				t.Fatalf("protocolControls() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestProfileListRows(t *testing.T) {
	profiles := []config.SavedProfile{
		{ID: "sftp", Profile: config.Profile{DriveLetter: "x", Name: "긴 한글 연결 이름", Protocol: config.ProtocolSFTP}},
		{ID: "webdav", Profile: config.Profile{DriveLetter: "Y", Name: "Web", Protocol: config.ProtocolWebDAV, WebDAVScheme: "http"}},
		{ID: "webdavs", Profile: config.Profile{DriveLetter: "Z", Name: "Web TLS", Protocol: config.ProtocolWebDAV}},
		{ID: "ftp", Profile: config.Profile{DriveLetter: "M", Name: "FTP", Protocol: config.ProtocolFTP}},
		{ID: "ftps", Profile: config.Profile{DriveLetter: "N", Name: "FTPS", Protocol: config.ProtocolFTPS}},
		{ID: "iftps", Profile: config.Profile{DriveLetter: "P", Name: "Implicit", Protocol: config.ProtocolFTPS, FTPSMode: "implicit-ftps"}},
	}
	rows := profileListRows(profiles, func(id string) string {
		if id == "sftp" {
			return "연결됨"
		}
		return "연결 안 됨"
	})
	if len(rows) != 6 {
		t.Fatalf("row count: %d", len(rows))
	}
	if rows[0] != (profileListRow{drive: "X:", name: "긴 한글 연결 이름", protocol: "SFTP", state: "연결됨"}) {
		t.Fatalf("SFTP row: %+v", rows[0])
	}
	if rows[1].protocol != "WebDAV HTTP" || rows[2].protocol != "WebDAV HTTPS" ||
		rows[3].protocol != "FTP" || rows[4].protocol != "Explicit FTPS" || rows[5].protocol != "Implicit FTPS" {
		t.Fatalf("protocol names: %+v", rows)
	}
}

func TestProfileButtons(t *testing.T) {
	tests := []struct {
		name     string
		selected int
		dirty    bool
		states   []string
		want     profileButtonState
	}{
		{name: "new dirty profile", selected: -1, dirty: true, want: profileButtonState{save: true}},
		{name: "disconnected selection", selected: 0, states: []string{"연결 안 됨"}, want: profileButtonState{delete: true, connect: true, connectAll: true}},
		{name: "connected selection", selected: 0, states: []string{"연결됨"}, want: profileButtonState{disconnect: true, disconnectAll: true}},
		{name: "mixed profiles", selected: 1, dirty: true, states: []string{"연결됨", "연결 안 됨"}, want: profileButtonState{save: true, delete: true, connect: true, connectAll: true, disconnectAll: true}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := profileButtons(tt.selected, tt.dirty, tt.states); got != tt.want {
				t.Fatalf("profileButtons() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestAvailableDriveLetters(t *testing.T) {
	profiles := []config.SavedProfile{
		{ID: "one", Profile: config.Profile{DriveLetter: "X"}},
		{ID: "two", Profile: config.Profile{DriveLetter: "Y"}},
	}
	letters := availableDriveLetters(profiles, 0, driveLetterMask("C")|driveLetterMask("X")|driveLetterMask("Z"))
	joined := strings.Join(letters, "")
	if strings.Contains(joined, "C") || strings.Contains(joined, "Y") || strings.Contains(joined, "Z") {
		t.Fatalf("unavailable letters included: %s", joined)
	}
	if !strings.Contains(joined, "X") {
		t.Fatalf("current profile letter missing: %s", joined)
	}
}

func TestValidateDriveAssignment(t *testing.T) {
	profiles := []config.SavedProfile{
		{ID: "one", Profile: config.Profile{DriveLetter: "X"}},
		{ID: "two", Profile: config.Profile{DriveLetter: "Y"}},
	}
	if err := validateDriveAssignment("X", "one", profiles, 0); err != nil {
		t.Fatalf("current letter rejected: %v", err)
	}
	if err := validateDriveAssignment("Y", "one", profiles, 0); err == nil {
		t.Fatal("duplicate profile letter accepted")
	}
	if err := validateDriveAssignment("Z", "one", profiles, driveLetterMask("Z")); err == nil {
		t.Fatal("Windows-used letter accepted")
	}
}

func TestFileDialogFilter(t *testing.T) {
	filter := fileDialogFilter("개인키 파일", "*.key", "모든 파일", "*.*")
	if len(filter) < 2 || filter[len(filter)-1] != 0 || filter[len(filter)-2] != 0 {
		t.Fatal("filter is not double-NUL terminated")
	}
	parts := 0
	for _, value := range filter[:len(filter)-1] {
		if value == 0 {
			parts++
		}
	}
	if parts != 4 {
		t.Fatalf("filter part count: %d", parts)
	}
}

func TestAllowProfileChange(t *testing.T) {
	tests := []struct {
		name       string
		dirty      bool
		choice     int
		saveResult bool
		want       bool
		wantSave   bool
	}{
		{name: "clean", dirty: false, choice: 6, want: true},
		{name: "save succeeds", dirty: true, choice: 6, saveResult: true, want: true, wantSave: true},
		{name: "save fails", dirty: true, choice: 6, wantSave: true},
		{name: "discard", dirty: true, choice: 7, want: true},
		{name: "cancel", dirty: true, choice: 2},
		{name: "dialog closed", dirty: true, choice: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			saved := false
			got := allowProfileChange(tt.dirty, tt.choice, func() bool {
				saved = true
				return tt.saveResult
			})
			if got != tt.want || saved != tt.wantSave {
				t.Fatalf("allow=%v save=%v; want allow=%v save=%v", got, saved, tt.want, tt.wantSave)
			}
		})
	}
}

func TestAppIconBits(t *testing.T) {
	for _, size := range []int{16, 32} {
		andBits, xorBits := appIconBits(size)
		if len(andBits) != ((size+15)/16)*2*size {
			t.Fatalf("%dpx AND mask size: %d", size, len(andBits))
		}
		if len(xorBits) != size*size*4 {
			t.Fatalf("%dpx XOR bitmap size: %d", size, len(xorBits))
		}
		if andBits[0]&0x80 == 0 {
			t.Fatalf("%dpx top-left corner is not transparent", size)
		}
		centerRow := size - 1 - size/2
		center := (centerRow*size + size/2) * 4
		if xorBits[center+3] != 0xff {
			t.Fatalf("%dpx center is not opaque", size)
		}
	}
}
