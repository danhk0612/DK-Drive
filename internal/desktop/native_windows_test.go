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
