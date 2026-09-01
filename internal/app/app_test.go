package app

import (
	"bytes"
	"strings"
	"testing"
)

func TestVersionCommand(t *testing.T) {
	var output bytes.Buffer
	if err := run([]string{"version"}, &output); err != nil {
		t.Fatalf("run(version): %v", err)
	}
	if got := strings.TrimSpace(output.String()); got != Version {
		t.Fatalf("version output = %q, want %q", got, Version)
	}
}

func TestDefaultMessageShowsCurrentStage(t *testing.T) {
	var output bytes.Buffer
	if err := run(nil, &output); err != nil {
		t.Fatalf("run(): %v", err)
	}
	message := output.String()
	if !strings.Contains(message, "DK-Drive "+Version) || !strings.Contains(message, "0.5 프로그램 기능") {
		t.Fatalf("default output = %q", message)
	}
}

func TestUnknownCommand(t *testing.T) {
	var output bytes.Buffer
	if err := run([]string{"unknown"}, &output); err == nil {
		t.Fatal("run(unknown) returned nil error")
	}
}
