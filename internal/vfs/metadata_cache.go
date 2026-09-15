package vfs

import (
	"context"
	"errors"
	"io/fs"
	"path"
	"strings"
	"sync"
	"time"
)

const metadataTTL = 2 * time.Second
const missingTTL = 500 * time.Millisecond
const metadataMaxEntries = 4096
const metadataMaxBytes = 16 << 20

type metadataKey struct {
	name      string
	directory bool
}
type metadataValue struct {
	entry   Entry
	entries []Entry
	err     error
	expires time.Time
	bytes   int
}
type metadataFlightKey struct {
	metadataKey
	generation uint64
}
type metadataFlight struct {
	done  chan struct{}
	value metadataValue
}

type metadataCache struct {
	Backend
	mu         sync.Mutex
	values     map[metadataKey]metadataValue
	flights    map[metadataFlightKey]*metadataFlight
	generation uint64
	changing   int
	closed     bool
	bytes      int
	now        func() time.Time
}

// NewMetadataCache owns metadata only. Every connection must receive a new instance.
func NewMetadataCache(backend Backend) Backend {
	return &metadataCache{Backend: backend, values: make(map[metadataKey]metadataValue), flights: make(map[metadataFlightKey]*metadataFlight), now: time.Now}
}

func metadataPath(name string) (string, bool) {
	name = strings.ReplaceAll(name, "\\", "/")
	for _, part := range strings.Split(name, "/") {
		if part == ".." {
			return "", false
		}
	}
	return path.Clean("/" + name), true
}
func copyMetadata(v metadataValue) metadataValue {
	if v.entries != nil {
		v.entries = append([]Entry{}, v.entries...)
	}
	return v
}

// Only a single, unambiguous not-found error chain may be cached.
func definiteMissing(err error) bool {
	if err == nil {
		return false
	}
	for errors.Unwrap(err) != nil {
		err = errors.Unwrap(err)
	}
	return err == fs.ErrNotExist
}
func (c *metadataCache) drop(k metadataKey) {
	if v, ok := c.values[k]; ok {
		c.bytes -= v.bytes
		delete(c.values, k)
	}
}
func (c *metadataCache) put(k metadataKey, v metadataValue) {
	size := len(k.name) + len(v.entry.Name) + 128
	for _, e := range v.entries {
		size += len(e.Name) + 64
	}
	if size > metadataMaxBytes {
		return
	}
	c.drop(k)
	if len(c.values) >= metadataMaxEntries || c.bytes+size > metadataMaxBytes {
		// Bound retained memory without an eviction list or background worker.
		clear(c.values)
		c.bytes = 0
	}
	v.bytes = size
	c.values[k] = v
	c.bytes += size
}
func (c *metadataCache) query(ctx context.Context, name string, directory bool) metadataValue {
	normalized, valid := metadataPath(name)
	call := func() metadataValue {
		if directory {
			items, err := c.Backend.ReadDir(ctx, name)
			return metadataValue{entries: items, err: err}
		}
		entry, err := c.Backend.Stat(ctx, name)
		return metadataValue{entry: entry, err: err}
	}
	if !valid {
		return call()
	} // Preserve the backend's traversal rejection.
	k := metadataKey{normalized, directory}
	for {
		if err := ctx.Err(); err != nil {
			return metadataValue{err: err}
		}
		c.mu.Lock()
		if c.closed {
			c.mu.Unlock()
			return metadataValue{err: fs.ErrClosed}
		}
		if c.changing == 0 {
			if v, ok := c.values[k]; ok {
				if c.now().Before(v.expires) {
					c.mu.Unlock()
					return copyMetadata(v)
				}
				c.drop(k)
			}
		}
		fk := metadataFlightKey{k, c.generation}
		if f, ok := c.flights[fk]; ok && c.changing == 0 {
			c.mu.Unlock()
			select {
			case <-ctx.Done():
				return metadataValue{err: ctx.Err()}
			case <-f.done:
				// A leader's cancellation must not cancel a live waiter.
				if errors.Is(f.value.err, context.Canceled) || errors.Is(f.value.err, context.DeadlineExceeded) {
					continue
				}
				c.mu.Lock()
				fresh := !c.closed && c.generation == fk.generation
				c.mu.Unlock()
				if !fresh {
					continue
				}
				return copyMetadata(f.value)
			}
		}
		f := &metadataFlight{done: make(chan struct{})}
		// Do not merge calls overlapping a mutation.
		merge := c.changing == 0
		if merge {
			c.flights[fk] = f
		}
		c.mu.Unlock()
		v := call()
		c.mu.Lock()
		if !c.closed && c.changing == 0 && c.generation == fk.generation && ctx.Err() == nil && (v.err == nil || definiteMissing(v.err)) {
			ttl := metadataTTL
			if v.err != nil {
				ttl = missingTTL
			}
			v.expires = c.now().Add(ttl)
			stored := copyMetadata(v)
			if stored.err != nil {
				stored.err = fs.ErrNotExist
			}
			c.put(k, stored)
			if directory && v.err == nil {
				for _, e := range v.entries {
					// Never seed a path outside this directory from a malformed server name.
					if e.Name == "" || e.Name == "." || e.Name == ".." || strings.ContainsAny(e.Name, "/\\") {
						continue
					}
					child := metadataKey{path.Join(normalized, e.Name), false}
					c.put(child, metadataValue{entry: e, expires: v.expires})
				}
			}
		}
		f.value = copyMetadata(v)
		if merge {
			delete(c.flights, fk)
		}
		close(f.done)
		c.mu.Unlock()
		return v
	}
}
func (c *metadataCache) Stat(ctx context.Context, name string) (Entry, error) {
	v := c.query(ctx, name, false)
	return v.entry, v.err
}
func (c *metadataCache) ReadDir(ctx context.Context, name string) ([]Entry, error) {
	v := c.query(ctx, name, true)
	return v.entries, v.err
}

// Invalidate both before and after an operation, including uncertain failures.
// Generation fencing prevents an older remote read from repopulating stale data.
func (c *metadataCache) invalidate(names ...string) {
	c.generation++
	for _, name := range names {
		n, valid := metadataPath(name)
		if !valid {
			clear(c.values)
			c.bytes = 0
			continue
		}
		for k := range c.values {
			if k.name == n || n == "/" || strings.HasPrefix(k.name, n+"/") || (k.directory && k.name == path.Dir(n)) {
				c.drop(k)
			}
		}
	}
}
func (c *metadataCache) begin(names ...string) func() {
	c.mu.Lock()
	c.changing++
	c.invalidate(names...)
	c.mu.Unlock()
	return func() { c.mu.Lock(); c.invalidate(names...); c.changing--; c.mu.Unlock() }
}
func (c *metadataCache) Mkdir(ctx context.Context, n string) error {
	done := c.begin(n)
	defer done()
	return c.Backend.Mkdir(ctx, n)
}
func (c *metadataCache) Remove(ctx context.Context, n string, d bool) error {
	done := c.begin(n)
	defer done()
	return c.Backend.Remove(ctx, n, d)
}
func (c *metadataCache) Rename(ctx context.Context, a, b string) error {
	done := c.begin(a, b)
	defer done()
	return c.Backend.Rename(ctx, a, b)
}
func (c *metadataCache) SetModTime(ctx context.Context, n string, t time.Time) error {
	done := c.begin(n)
	defer done()
	return c.Backend.SetModTime(ctx, n, t)
}
func (c *metadataCache) SetReadOnly(ctx context.Context, n string, r bool) error {
	done := c.begin(n)
	defer done()
	return c.Backend.SetReadOnly(ctx, n, r)
}
func (c *metadataCache) OpenWrite(ctx context.Context, n string, o WriteOptions) (WriteHandle, error) {
	done := c.begin(n)
	defer done()
	h, err := c.Backend.OpenWrite(ctx, n, o)
	if err != nil {
		return h, err
	}
	return &metadataWrite{WriteHandle: h, cache: c, name: n}, nil
}

type metadataWrite struct {
	WriteHandle
	cache *metadataCache
	name  string
}

func (h *metadataWrite) WriteAt(p []byte, o int64) (int, error) {
	done := h.cache.begin(h.name)
	defer done()
	return h.WriteHandle.WriteAt(p, o)
}
func (h *metadataWrite) Sync() error {
	done := h.cache.begin(h.name)
	defer done()
	return h.WriteHandle.Sync()
}
func (h *metadataWrite) Close() error {
	done := h.cache.begin(h.name)
	defer done()
	return h.WriteHandle.Close()
}
func (c *metadataCache) Close() error {
	c.mu.Lock()
	c.closed = true
	c.generation++
	clear(c.values)
	c.bytes = 0
	c.mu.Unlock()
	return c.Backend.Close()
}
