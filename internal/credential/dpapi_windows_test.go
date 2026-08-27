//go:build windows

package credential

import (
	"bytes"
	"testing"
)

func TestDPAPIUserRoundTrip(t *testing.T) {
	p := DPAPI{}
	plain := []byte("test-only-password 한글")
	sealed, err := p.Protect(plain)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(sealed, plain) || bytes.Contains(sealed, plain) {
		t.Fatal("plaintext persisted")
	}
	restored, err := p.Unprotect(sealed)
	if err != nil || !bytes.Equal(restored, plain) {
		t.Fatalf("roundtrip: %v", err)
	}
	sealed[len(sealed)-1] ^= 1
	if _, err := p.Unprotect(sealed); err == nil {
		t.Fatal("tampered blob accepted")
	}
	if _, err := p.Protect(nil); err == nil {
		t.Fatal("empty input accepted")
	}
}
