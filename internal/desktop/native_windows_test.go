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
