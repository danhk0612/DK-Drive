//go:build windows

package main

import (
	"testing"

	ftpbackend "github.com/danhk0612/DK-Drive/internal/protocol/ftp"
)

func TestParseMode(t *testing.T) {
	tests := []struct {
		name string
		mode ftpbackend.TLSMode
		port uint16
	}{
		{name: "ftp", mode: ftpbackend.TLSNone, port: 21},
		{name: "explicit-ftps", mode: ftpbackend.TLSExplicit, port: 21},
		{name: "implicit-ftps", mode: ftpbackend.TLSImplicit, port: 990},
	}
	for _, test := range tests {
		mode, port, err := parseMode(test.name)
		if err != nil || mode != test.mode || port != test.port {
			t.Fatalf("parseMode(%q) = %q, %d, %v", test.name, mode, port, err)
		}
	}
	if _, _, err := parseMode("invalid"); err == nil {
		t.Fatal("parseMode(invalid) returned nil error")
	}
}

func TestValidMountpoint(t *testing.T) {
	for _, value := range []string{"X:", "d:"} {
		if !validMountpoint(value) {
			t.Errorf("validMountpoint(%q) = false", value)
		}
	}
	for _, value := range []string{"", "X", "XY:", "1:", `X:\`} {
		if validMountpoint(value) {
			t.Errorf("validMountpoint(%q) = true", value)
		}
	}
}
