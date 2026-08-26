//go:build windows

package main

import "testing"

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
