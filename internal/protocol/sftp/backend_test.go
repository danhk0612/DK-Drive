package sftp

import (
	"net"
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
