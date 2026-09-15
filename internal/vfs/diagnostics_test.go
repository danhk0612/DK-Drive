package vfs

import (
	"context"
	"errors"
	"github.com/danhk0612/DK-Drive/internal/diagnostics"
	"os"
	"testing"
)

type countingMetadata struct {
	Backend
	stats, lists, closes int
}

func (b *countingMetadata) Stat(context.Context, string) (Entry, error) {
	b.stats++
	return Entry{Name: "a.jpg"}, nil
}
func (b *countingMetadata) ReadDir(context.Context, string) ([]Entry, error) {
	b.lists++
	return []Entry{{Name: "a.jpg"}, {Name: "b.jpg"}, {Name: "subfolder", Mode: os.ModeDir}}, nil
}
func (b *countingMetadata) Close() error { b.closes++; return nil }
func TestDiagnosticsExplorerBaseline(t *testing.T) {
	raw := &countingMetadata{}
	r, err := diagnostics.Open(t.TempDir(), "webdav")
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	b := NewReadOnlyBackend(WithDiagnostics(raw, r))
	ctx := context.Background()
	if _, err := b.ReadDir(ctx, "/photo"); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"a.jpg", "b.jpg", "subfolder"} {
		if _, err := b.Stat(ctx, "/photo/"+name); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := b.ReadDir(ctx, "/photo"); err != nil {
		t.Fatal(err)
	}
	if err := b.Mkdir(ctx, "/photo/no"); !errors.Is(err, ErrReadOnly) {
		t.Fatal(err)
	}
	report := r.Snapshot()
	if raw.stats != 3 || raw.lists != 2 || report.Metrics[diagnostics.LogicalStat].Calls != 3 || report.Metrics[diagnostics.LogicalReadDir].Calls != 2 {
		t.Fatalf("baseline = %+v, raw=%+v", report, raw)
	}
	if err := b.Close(); err != nil || raw.closes != 1 {
		t.Fatalf("close: %v, %d", err, raw.closes)
	}
	if WithDiagnostics(raw, nil) != raw {
		t.Fatal("disabled diagnostics must preserve backend")
	}
}
