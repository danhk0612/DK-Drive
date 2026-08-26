package vfs

import (
	"context"
	"io"
	"io/fs"
	"time"
)

// Backend is the protocol-neutral file operation boundary used by the mount
// layer. Implementations must be safe for concurrent calls unless documented
// otherwise.
type Backend interface {
	Stat(ctx context.Context, path string) (Entry, error)
	ReadDir(ctx context.Context, path string) ([]Entry, error)
	OpenRead(ctx context.Context, path string) (io.ReadCloser, error)
	OpenWrite(ctx context.Context, path string, options WriteOptions) (WriteHandle, error)
	Mkdir(ctx context.Context, path string) error
	Remove(ctx context.Context, path string, directory bool) error
	Rename(ctx context.Context, oldPath, newPath string) error
	SetModTime(ctx context.Context, path string, modTime time.Time) error
	Close() error
}

type Entry struct {
	Name    string
	Size    int64
	Mode    fs.FileMode
	ModTime time.Time
}

func (entry Entry) IsDir() bool {
	return entry.Mode.IsDir()
}

type WriteOptions struct {
	Create   bool
	Truncate bool
	Append   bool
	Mode     fs.FileMode
}

type WriteHandle interface {
	io.WriterAt
	Sync() error
	Close() error
}
