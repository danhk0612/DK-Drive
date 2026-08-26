package sftp

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"errors"
	"net"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/crypto/ssh"
)

func TestValidateConfig(t *testing.T) {
	config := Config{
		Host:            "example.test",
		Port:            22,
		Username:        "tester",
		Password:        "secret",
		HostKeyCallback: func(string, net.Addr, ssh.PublicKey) error { return nil },
	}
	if err := validateConfig(config); err != nil {
		t.Fatalf("validateConfig(): %v", err)
	}
}

func TestValidateConfigAcceptsPrivateKey(t *testing.T) {
	signer := testSigner(t)
	config := Config{
		Host:            "example.test",
		Port:            22,
		Username:        "tester",
		Signer:          signer,
		HostKeyCallback: func(string, net.Addr, ssh.PublicKey) error { return nil },
	}
	if err := validateConfig(config); err != nil {
		t.Fatalf("validateConfig(): %v", err)
	}
	if methods := authenticationMethods(config); len(methods) != 1 {
		t.Fatalf("authenticationMethods() length = %d, want 1", len(methods))
	}
}

func TestValidateConfigRejectsMissingOrMultipleAuthenticationMethods(t *testing.T) {
	callback := func(string, net.Addr, ssh.PublicKey) error { return nil }
	tests := []Config{
		{Host: "example.test", Port: 22, Username: "tester", HostKeyCallback: callback},
		{Host: "example.test", Port: 22, Username: "tester", Password: "secret", Signer: testSigner(t), HostKeyCallback: callback},
	}
	for _, config := range tests {
		if err := validateConfig(config); err == nil {
			t.Fatal("validateConfig() returned nil error")
		}
	}
}

func TestLoadPrivateKey(t *testing.T) {
	privateKey := testPrivateKey(t, nil)
	signer, err := LoadPrivateKey(privateKey, nil)
	if err != nil {
		t.Fatalf("LoadPrivateKey(): %v", err)
	}
	if signer.PublicKey().Type() != ssh.KeyAlgoED25519 {
		t.Fatalf("key type = %q, want %q", signer.PublicKey().Type(), ssh.KeyAlgoED25519)
	}
}

func TestLoadEncryptedPrivateKey(t *testing.T) {
	passphrase := []byte("test-passphrase")
	privateKey := testPrivateKey(t, passphrase)
	_, err := LoadPrivateKey(privateKey, nil)
	var passphraseMissing *ssh.PassphraseMissingError
	if !errors.As(err, &passphraseMissing) {
		t.Fatalf("LoadPrivateKey() error = %v, want PassphraseMissingError", err)
	}
	if _, err := LoadPrivateKey(privateKey, passphrase); err != nil {
		t.Fatalf("LoadPrivateKey(passphrase): %v", err)
	}
}

func testSigner(t *testing.T) ssh.Signer {
	t.Helper()
	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(privateKey)
	if err != nil {
		t.Fatal(err)
	}
	return signer
}

func testPrivateKey(t *testing.T, passphrase []byte) string {
	t.Helper()
	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	var block *pem.Block
	if len(passphrase) == 0 {
		block, err = ssh.MarshalPrivateKey(privateKey, "DKDrive test")
	} else {
		block, err = ssh.MarshalPrivateKeyWithPassphrase(privateKey, "DKDrive test", passphrase)
	}
	if err != nil {
		t.Fatal(err)
	}
	filename := filepath.Join(t.TempDir(), "id_ed25519")
	if err := os.WriteFile(filename, pem.EncodeToMemory(block), 0o600); err != nil {
		t.Fatal(err)
	}
	return filename
}

func TestValidateConfigRequiresHostKeyCallback(t *testing.T) {
	config := Config{Host: "example.test", Port: 22, Username: "tester", Password: "secret"}
	if err := validateConfig(config); err == nil {
		t.Fatal("validateConfig() returned nil error")
	}
}

func TestRemotePath(t *testing.T) {
	backend := Backend{root: "/home/tester/data"}
	tests := []struct {
		name    string
		want    string
		wantErr bool
	}{
		{name: ".", want: "/home/tester/data"},
		{name: "folder/file.txt", want: "/home/tester/data/folder/file.txt"},
		{name: `folder\한글.txt`, want: "/home/tester/data/folder/한글.txt"},
		{name: "../secret", wantErr: true},
	}

	for _, test := range tests {
		got, err := backend.remotePath(test.name)
		if (err != nil) != test.wantErr {
			t.Fatalf("remotePath(%q) error = %v, wantErr %v", test.name, err, test.wantErr)
		}
		if got != test.want {
			t.Errorf("remotePath(%q) = %q, want %q", test.name, got, test.want)
		}
	}
}
