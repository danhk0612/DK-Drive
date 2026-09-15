//go:build windows && amd64

package main

import (
	"syscall"
	"testing"
)

// The test executable links the same .syso as the shipping executable.
func TestEmbeddedProductResources(t *testing.T) {
	kernel := syscall.NewLazyDLL("kernel32.dll")
	module, _, err := kernel.NewProc("GetModuleHandleW").Call(0)
	if module == 0 {
		t.Fatal(err)
	}
	find := kernel.NewProc("FindResourceW")
	for _, resource := range [][2]uintptr{{1, 16}, {1, 14}, {1, 3}, {2, 3}, {3, 3}} {
		handle, _, err := find.Call(module, resource[0], resource[1])
		if handle == 0 {
			t.Fatalf("missing resource id/type %v: %v", resource, err)
		}
	}
	// Load the actual group resource through the Windows icon decoder.
	icon, _, err := syscall.NewLazyDLL("user32.dll").NewProc("LoadImageW").Call(module, 1, 1, 32, 32, 0)
	if icon == 0 {
		t.Fatalf("invalid icon resource: %v", err)
	}
	syscall.NewLazyDLL("user32.dll").NewProc("DestroyIcon").Call(icon)
}
