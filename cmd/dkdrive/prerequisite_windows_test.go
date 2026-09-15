//go:build windows

package main

import (
	"os"
	"testing"
)

func TestInstalledWinFspBootstrap(t *testing.T) {
	if os.Getenv("DKDRIVE_TEST_INSTALLED_WINFSP") != "1" {
		t.Skip("requires installed official WinFsp runtime")
	}
	ready, err := prepareWinFsp(true)
	if err != nil || !ready {
		t.Fatalf("installed WinFsp was not loaded: ready=%v err=%v", ready, err)
	}
}
