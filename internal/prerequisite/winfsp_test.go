package prerequisite

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestInstallerIntegrity(t *testing.T) {
	p := filepath.Join(t.TempDir(), "installer.msi")
	data := []byte("trusted installer")
	if err := os.WriteFile(p, data, 0600); err != nil {
		t.Fatal(err)
	}
	i := Installer{SHA256: fmt.Sprintf("%x", sha256.Sum256(data))}
	if err := i.Verify(p); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("changed"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := i.Verify(p); err == nil {
		t.Fatal("modified installer accepted")
	}
	if err := i.Verify(p + "missing"); err == nil {
		t.Fatal("missing installer accepted")
	}
}
