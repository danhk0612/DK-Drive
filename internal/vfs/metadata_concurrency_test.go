package vfs

import (
	"context"
	"errors"
	"io/fs"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
)

func TestMetadataMergesConcurrentFailuresAndRecovers(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		release := make(chan struct{})
		b := &metadataStub{statHook: func(context.Context, string) (Entry, error) { <-release; return Entry{}, fs.ErrPermission }}
		c := NewMetadataCache(b)
		var wg sync.WaitGroup
		for i := 0; i < 20; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_, err := c.Stat(context.Background(), "/a")
				if !errors.Is(err, fs.ErrPermission) {
					t.Error(err)
				}
			}()
		}
		synctest.Wait()
		if s, _ := b.counts(); s != 1 {
			t.Fatalf("concurrent calls %d", s)
		}
		close(release)
		wg.Wait()
		c.Stat(context.Background(), "/a")
		if s, _ := b.counts(); s != 2 {
			t.Fatalf("failed flight retained %d", s)
		}
	})
}
func TestMetadataDifferentPathsAndKindsRunInParallel(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		release := make(chan struct{})
		b := &metadataStub{statHook: func(context.Context, string) (Entry, error) { <-release; return Entry{}, nil }, dirHook: func(context.Context, string) ([]Entry, error) { <-release; return nil, nil }}
		c := NewMetadataCache(b)
		var wg sync.WaitGroup
		wg.Add(3)
		go func() { defer wg.Done(); c.Stat(context.Background(), "/a") }()
		go func() { defer wg.Done(); c.Stat(context.Background(), "/b") }()
		go func() { defer wg.Done(); c.ReadDir(context.Background(), "/a") }()
		synctest.Wait()
		if s, d := b.counts(); s != 2 || d != 1 {
			t.Fatalf("serialized or key collision: %d %d", s, d)
		}
		close(release)
		wg.Wait()
	})
}
func TestMetadataLeaderCancellationDoesNotCancelWaiter(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var calls atomic.Int32
		b := &metadataStub{statHook: func(ctx context.Context, _ string) (Entry, error) {
			if calls.Add(1) == 1 {
				<-ctx.Done()
				return Entry{}, ctx.Err()
			}
			return Entry{Size: 42}, nil
		}}
		c := NewMetadataCache(b)
		ctx, cancel := context.WithCancel(context.Background())
		var wg sync.WaitGroup
		wg.Add(1)
		go func() { defer wg.Done(); c.Stat(ctx, "/a") }()
		synctest.Wait()
		wg.Add(1)
		go func() {
			defer wg.Done()
			e, err := c.Stat(context.Background(), "/a")
			if err != nil || e.Size != 42 {
				t.Errorf("waiter %+v %v", e, err)
			}
		}()
		synctest.Wait()
		cancel()
		wg.Wait()
		if calls.Load() != 2 {
			t.Fatal(calls.Load())
		}
	})
}
func TestMetadataWaiterCancellationDoesNotCancelLeader(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		release := make(chan struct{})
		b := &metadataStub{statHook: func(context.Context, string) (Entry, error) { <-release; return Entry{Size: 42}, nil }}
		c := NewMetadataCache(b)
		var wg sync.WaitGroup
		wg.Add(1)
		go func() {
			defer wg.Done()
			e, err := c.Stat(context.Background(), "/a")
			if err != nil || e.Size != 42 {
				t.Error(err)
			}
		}()
		synctest.Wait()
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan struct{})
		go func() {
			defer close(done)
			_, err := c.Stat(ctx, "/a")
			if !errors.Is(err, context.Canceled) {
				t.Error(err)
			}
		}()
		synctest.Wait()
		cancel()
		<-done
		close(release)
		wg.Wait()
		if s, _ := b.counts(); s != 1 {
			t.Fatal(s)
		}
	})
}
func TestMetadataReadDirSingleflightCopiesResults(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		release := make(chan struct{})
		b := &metadataStub{dirHook: func(context.Context, string) ([]Entry, error) { <-release; return []Entry{{Name: "a"}}, nil }}
		c := NewMetadataCache(b)
		var wg sync.WaitGroup
		wg.Add(2)
		results := make(chan []Entry, 2)
		for i := 0; i < 2; i++ {
			go func() { defer wg.Done(); e, _ := c.ReadDir(context.Background(), "/p"); results <- e }()
		}
		synctest.Wait()
		if _, d := b.counts(); d != 1 {
			t.Fatal(d)
		}
		close(release)
		wg.Wait()
		a, bList := <-results, <-results
		a[0].Name = "changed"
		if bList[0].Name != "a" {
			t.Fatal("shared slice")
		}
	})
}
