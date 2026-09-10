package vfs

import (
	"context"
	"fmt"
	"github.com/danhk0612/DK-Drive/internal/diagnostics"
	"os"
)

type measuredBackend struct {
	Backend
	recorder *diagnostics.Recorder
}

func WithDiagnostics(backend Backend, recorder *diagnostics.Recorder) Backend {
	if recorder == nil {
		return backend
	}
	return &measuredBackend{Backend: backend, recorder: recorder}
}
func (b *measuredBackend) Stat(ctx context.Context, name string) (Entry, error) {
	return diagnostics.Call(b.recorder, diagnostics.LogicalStat, func() (Entry, error) { return b.Backend.Stat(ctx, name) })
}
func (b *measuredBackend) ReadDir(ctx context.Context, name string) ([]Entry, error) {
	return diagnostics.Call(b.recorder, diagnostics.LogicalReadDir, func() ([]Entry, error) { return b.Backend.ReadDir(ctx, name) })
}
func (b *measuredBackend) Close() error {
	err := b.Backend.Close()
	// Diagnostic output must never change the actual disconnect result.
	if b.recorder.Close() != nil {
		fmt.Fprintln(os.Stderr, "DK-Drive metadata diagnostic report could not be written")
	}
	return err
}
