package vfs

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"sync"
	"testing"
	"time"
)

type metadataStub struct {
	Backend
	mu          sync.Mutex
	stats, dirs int
	size        int64
	err         error
	statHook    func(context.Context, string) (Entry, error)
	dirHook     func(context.Context, string) ([]Entry, error)
}

func (b *metadataStub) Stat(ctx context.Context, n string) (Entry, error) {
	b.mu.Lock()
	b.stats++
	size, err, hook := b.size, b.err, b.statHook
	b.mu.Unlock()
	if hook != nil {
		return hook(ctx, n)
	}
	return Entry{Name: n, Size: size}, err
}
func (b *metadataStub) ReadDir(ctx context.Context, n string) ([]Entry, error) {
	b.mu.Lock()
	b.dirs++
	err, hook := b.err, b.dirHook
	b.mu.Unlock()
	if hook != nil {
		return hook(ctx, n)
	}
	return []Entry{{Name: "a", Size: 1}, {Name: "b", Size: 2}, {Name: "sub", Mode: fs.ModeDir}}, err
}
func (b *metadataStub) change() error                                       { b.mu.Lock(); defer b.mu.Unlock(); b.size++; return b.err }
func (b *metadataStub) Mkdir(context.Context, string) error                 { return b.change() }
func (b *metadataStub) Remove(context.Context, string, bool) error          { return b.change() }
func (b *metadataStub) Rename(context.Context, string, string) error        { return b.change() }
func (b *metadataStub) SetModTime(context.Context, string, time.Time) error { return b.change() }
func (b *metadataStub) SetReadOnly(context.Context, string, bool) error     { return b.change() }
func (b *metadataStub) OpenWrite(context.Context, string, WriteOptions) (WriteHandle, error) {
	return &metadataHandle{b}, nil
}
func (b *metadataStub) Close() error { return nil }

type metadataHandle struct{ b *metadataStub }

func (h *metadataHandle) WriteAt(p []byte, _ int64) (int, error) { return len(p), h.b.change() }
func (h *metadataHandle) Sync() error                            { return h.b.change() }
func (h *metadataHandle) Close() error                           { return h.b.change() }
func (b *metadataStub) counts() (int, int)                       { b.mu.Lock(); defer b.mu.Unlock(); return b.stats, b.dirs }

func TestMetadataTTLAndExplorerPattern(t *testing.T) {
	b := &metadataStub{}
	c := NewMetadataCache(b).(*metadataCache)
	ctx := context.Background()
	now := time.Unix(1, 0)
	c.now = func() time.Time { return now }
	first, _ := c.ReadDir(ctx, "/photo")
	first[0].Name = "changed"
	for _, n := range []string{"a", "b", "sub"} {
		if _, err := c.Stat(ctx, "/photo/"+n); err != nil {
			t.Fatal(err)
		}
	}
	second, _ := c.ReadDir(ctx, `photo\.`)
	if second[0].Name != "a" {
		t.Fatal("caller changed cached list")
	}
	if s, d := b.counts(); s != 0 || d != 1 {
		t.Fatalf("pattern stat=%d dir=%d", s, d)
	}
	now = now.Add(metadataTTL)
	c.Stat(ctx, "/photo/a")
	c.ReadDir(ctx, "/photo")
	if s, d := b.counts(); s != 1 || d != 2 {
		t.Fatalf("expiry stat=%d dir=%d", s, d)
	}
	c.Stat(ctx, "/OTHER")
	c.Stat(ctx, "/other")
	if s, _ := b.counts(); s != 3 {
		t.Fatal("case folded")
	}
	other := NewMetadataCache(b)
	other.Stat(ctx, "/photo/a")
	if s, _ := b.counts(); s != 4 {
		t.Fatal("cache shared")
	}
}
func TestMetadataErrors(t *testing.T) {
	for _, err := range []error{fs.ErrNotExist, fmt.Errorf("missing: %w", fs.ErrNotExist), fs.ErrPermission, context.Canceled, context.DeadlineExceeded, errors.New("network disconnected"), errors.Join(fs.ErrNotExist, fs.ErrPermission)} {
		t.Run(err.Error(), func(t *testing.T) {
			b := &metadataStub{err: err}
			c := NewMetadataCache(b).(*metadataCache)
			ctx := context.Background()
			now := time.Unix(1, 0)
			c.now = func() time.Time { return now }
			for i := 0; i < 2; i++ {
				c.Stat(ctx, "/missing")
				c.ReadDir(ctx, "/missing")
			}
			want := 2
			if definiteMissing(err) {
				want = 1
			}
			if s, d := b.counts(); s != want || d != want {
				t.Fatalf("got %d %d want %d", s, d, want)
			}
			now = now.Add(missingTTL)
			c.Stat(ctx, "/missing")
			c.ReadDir(ctx, "/missing")
			if s, d := b.counts(); s != want+1 || d != want+1 {
				t.Fatal("negative expiry")
			}
		})
	}
}
func TestMetadataMutationInvalidation(t *testing.T) {
	ops := map[string]func(Backend) error{
		"mkdir":    func(c Backend) error { return c.Mkdir(context.Background(), "/p/sub") },
		"remove":   func(c Backend) error { return c.Remove(context.Background(), "/p/sub", true) },
		"rename":   func(c Backend) error { return c.Rename(context.Background(), "/p/sub", "/q/sub") },
		"time":     func(c Backend) error { return c.SetModTime(context.Background(), "/p/sub", time.Now()) },
		"readonly": func(c Backend) error { return c.SetReadOnly(context.Background(), "/p/sub", true) },
	}
	for name, op := range ops {
		for _, fail := range []bool{false, true} {
			t.Run(fmt.Sprint(name, fail), func(t *testing.T) {
				b := &metadataStub{}
				c := NewMetadataCache(b)
				ctx := context.Background()
				c.ReadDir(ctx, "/p")
				c.ReadDir(ctx, "/q")
				c.Stat(ctx, "/p/sub/child")
				c.Stat(ctx, "/q/sub/child")
				if fail {
					b.err = errors.New("partial remote mutation")
				}
				op(c)
				b.err = nil
				c.Stat(ctx, "/p/sub/child")
				c.ReadDir(ctx, "/p")
				if s, d := b.counts(); s != 3 || d != 3 {
					t.Fatalf("invalidation %d %d", s, d)
				}
				if name == "rename" {
					c.Stat(ctx, "/q/sub/child")
					c.ReadDir(ctx, "/q")
					if s, d := b.counts(); s != 4 || d != 4 {
						t.Fatalf("rename target %d %d", s, d)
					}
				}
			})
		}
	}
}
func TestMetadataWriteVisibility(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(fmt.Sprint(fail), func(t *testing.T) {
			b := &metadataStub{}
			c := NewMetadataCache(b)
			ctx := context.Background()
			c.Stat(ctx, "/a")
			h, err := c.OpenWrite(ctx, "/a", WriteOptions{Create: true})
			if err != nil {
				t.Fatal(err)
			}
			c.Stat(ctx, "/a")
			for _, op := range []func() error{func() error { _, e := h.WriteAt([]byte("x"), 0); return e }, h.Sync, h.Close} {
				if fail {
					b.err = errors.New("partially applied")
				}
				op()
				b.err = nil
				e, err := c.Stat(ctx, "/a")
				if err != nil || e.Size != b.size {
					t.Fatalf("stale write: %+v, %v", e, err)
				}
			}
			if s, _ := b.counts(); s != 5 {
				t.Fatalf("stat calls %d", s)
			}
		})
	}
}
func TestMetadataLateReadCannotRepopulateAfterMutation(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	b := &metadataStub{statHook: func(context.Context, string) (Entry, error) {
		once.Do(func() { close(started); <-release })
		return Entry{Size: 1}, nil
	}}
	c := NewMetadataCache(b).(*metadataCache)
	done := make(chan struct{})
	go func() { c.Stat(context.Background(), "/a"); close(done) }()
	<-started
	c.Remove(context.Background(), "/a", false)
	close(release)
	<-done
	b.mu.Lock()
	b.statHook = nil
	b.size = 2
	b.mu.Unlock()
	e, _ := c.Stat(context.Background(), "/a")
	if e.Size != 2 {
		t.Fatalf("stale %+v", e)
	}
}
func TestMetadataBoundsAndClose(t *testing.T) {
	b := &metadataStub{}
	c := NewMetadataCache(b).(*metadataCache)
	for i := 0; i < metadataMaxEntries+10; i++ {
		c.Stat(context.Background(), fmt.Sprint(i))
	}
	if len(c.values) > metadataMaxEntries || c.bytes > metadataMaxBytes {
		t.Fatal("unbounded")
	}
	c.Close()
	if len(c.values) != 0 || c.bytes != 0 {
		t.Fatal("close retained data")
	}
	if _, err := c.Stat(context.Background(), "x"); !errors.Is(err, fs.ErrClosed) {
		t.Fatal(err)
	}
}
func TestMetadataReadOnlyCombination(t *testing.T) {
	b := &metadataStub{}
	c := NewReadOnlyBackend(NewMetadataCache(b))
	ctx := context.Background()
	if err := c.Remove(ctx, "/a", false); !errors.Is(err, ErrReadOnly) {
		t.Fatal(err)
	}
	c.ReadDir(ctx, "/")
	c.Stat(ctx, "/a")
	if s, d := b.counts(); s != 0 || d != 1 {
		t.Fatalf("%d %d", s, d)
	}
}
