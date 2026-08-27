package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func savedFixture() SavedProfile {
	return SavedProfile{ID: "test-id", Profile: Profile{Name: "테스트", Protocol: ProtocolSFTP, DriveLetter: "X", Host: "example.test", Port: 22, Username: "tester", AuthMethod: AuthPassword}}
}

// Only a reversible test double; production uses DPAPI.
type testProtector struct{ fail bool }

func (p testProtector) Protect(b []byte) ([]byte, error) {
	if p.fail {
		return nil, errors.New("failed")
	}
	out := append([]byte(nil), b...)
	for i := range out {
		out[i] ^= 0xaa
	}
	return out, nil
}
func (p testProtector) Unprotect(b []byte) ([]byte, error) { return p.Protect(b) }

func TestSettingsRoundTripAndSecretSeparation(t *testing.T) {
	file := filepath.Join(t.TempDir(), "settings.json")
	s, err := LoadSettings(file)
	if err != nil || s.Version != 1 || !s.CloseToTray {
		t.Fatalf("default: %+v %v", s, err)
	}
	p := savedFixture()
	secret := Secrets{Password: "unique-password", Passphrase: "unique-passphrase"}
	p.ProtectedSecret, err = SealSecrets(testProtector{}, secret)
	if err != nil {
		t.Fatal(err)
	}
	s.Profiles = []SavedProfile{p}
	if err := SaveSettings(file, s); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(data, []byte(secret.Password)) || bytes.Contains(data, []byte(secret.Passphrase)) {
		t.Fatal("plaintext secret in settings")
	}
	got, err := LoadSettings(file)
	if err != nil {
		t.Fatal(err)
	}
	restored, err := OpenSecrets(testProtector{}, got.Profiles[0].ProtectedSecret)
	if err != nil || restored != secret {
		t.Fatalf("secret roundtrip: %+v %v", restored, err)
	}
	got.Profiles[0].ProtectedSecret = nil
	got.Profiles[0].Profile.Name = "변경"
	if err := SaveSettings(file, got); err != nil {
		t.Fatal(err)
	}
	got, err = LoadSettings(file)
	if err != nil || len(got.Profiles[0].ProtectedSecret) != 0 || got.Profiles[0].Profile.Name != "변경" {
		t.Fatalf("replace: %+v %v", got, err)
	}
}

func TestSettingsRejectInvalidWithoutReplacing(t *testing.T) {
	filename := filepath.Join(t.TempDir(), "settings.json")
	s := DefaultSettings()
	s.Profiles = []SavedProfile{savedFixture()}
	if err := SaveSettings(filename, s); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(filename)
	for _, kind := range []string{"version", "duplicate-id", "duplicate-drive", "invalid-profile"} {
		t.Run(kind, func(t *testing.T) {
			bad := s
			bad.Profiles = append([]SavedProfile(nil), s.Profiles...)
			switch kind {
			case "version":
				bad.Version = 2
			case "duplicate-id":
				p := savedFixture()
				p.Profile.DriveLetter = "Y"
				bad.Profiles = append(bad.Profiles, p)
			case "duplicate-drive":
				p := savedFixture()
				p.ID = "other"
				p.Profile.DriveLetter = "x"
				bad.Profiles = append(bad.Profiles, p)
			case "invalid-profile":
				bad.Profiles[0].Profile.Host = ""
			}
			if err := SaveSettings(filename, bad); err == nil {
				t.Fatal("accepted invalid settings")
			}
			after, _ := os.ReadFile(filename)
			if !bytes.Equal(before, after) {
				t.Fatal("original replaced")
			}
		})
	}
}

func TestLoadRejectsMalformedAndFutureData(t *testing.T) {
	for _, data := range []string{`{`, `{"Version":2}`, `{"Version":1,"Password":"not-allowed"}`, `{"Version":1} {}`, `{"Version":1} null`} {
		file := filepath.Join(t.TempDir(), "settings.json")
		if err := os.WriteFile(file, []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := LoadSettings(file); err == nil {
			t.Fatalf("accepted %s", data)
		}
	}
}

func TestSecretFailuresAndIDs(t *testing.T) {
	if _, err := SealSecrets(testProtector{fail: true}, Secrets{Password: "test"}); err == nil {
		t.Fatal("ignored protect error")
	}
	if _, err := OpenSecrets(testProtector{fail: true}, []byte{1}); err == nil {
		t.Fatal("ignored unprotect error")
	}
	if s, err := OpenSecrets(testProtector{}, nil); err != nil || s != (Secrets{}) {
		t.Fatal(s, err)
	}
	data, _ := json.Marshal(Secrets{Password: "nul\x00value"})
	sealed, _ := testProtector{}.Protect(data)
	if _, err := OpenSecrets(testProtector{}, sealed); err == nil {
		t.Fatal("accepted NUL")
	}
	a, err := NewID()
	if err != nil {
		t.Fatal(err)
	}
	b, err := NewID()
	if err != nil || len(a) != 32 || len(b) != 32 || a == b {
		t.Fatal("invalid IDs")
	}
}

func TestProfileProtocolOptions(t *testing.T) {
	for _, p := range []Profile{
		func() Profile {
			p := savedFixture().Profile
			p.Protocol = ProtocolFTP
			p.AuthMethod = AuthPrivateKey
			p.PrivateKey = "key"
			return p
		}(),
		func() Profile { p := savedFixture().Profile; p.InsecureSkipTLSVerify = true; return p }(),
		func() Profile {
			p := savedFixture().Profile
			p.Protocol = ProtocolWebDAV
			p.WebDAVScheme = "bad"
			return p
		}(),
		func() Profile { p := savedFixture().Profile; p.Protocol = ProtocolFTPS; p.FTPSMode = "bad"; return p }(),
		func() Profile { p := savedFixture().Profile; p.Host = "bad\x00host"; return p }(),
	} {
		if err := p.Validate(); err == nil {
			t.Fatalf("accepted %+v", p)
		}
	}
}
