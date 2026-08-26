package vfs

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"time"
)

var ErrReadOnly = fmt.Errorf("DKDrive 읽기 전용 연결: %w", fs.ErrPermission)

type readOnlyBackend struct {
	backend Backend
}

func NewReadOnlyBackend(backend Backend) Backend {
	return &readOnlyBackend{backend: backend}
}

func (backend *readOnlyBackend) Stat(ctx context.Context, name string) (Entry, error) {
	return backend.backend.Stat(ctx, name)
}

func (backend *readOnlyBackend) ReadDir(ctx context.Context, name string) ([]Entry, error) {
	return backend.backend.ReadDir(ctx, name)
}

func (backend *readOnlyBackend) OpenRead(ctx context.Context, name string) (io.ReadCloser, error) {
	return backend.backend.OpenRead(ctx, name)
}

func (backend *readOnlyBackend) OpenWrite(context.Context, string, WriteOptions) (WriteHandle, error) {
	return nil, ErrReadOnly
}

func (backend *readOnlyBackend) Mkdir(context.Context, string) error {
	return ErrReadOnly
}

func (backend *readOnlyBackend) Remove(context.Context, string, bool) error {
	return ErrReadOnly
}

func (backend *readOnlyBackend) Rename(context.Context, string, string) error {
	return ErrReadOnly
}

func (backend *readOnlyBackend) SetModTime(context.Context, string, time.Time) error {
	return ErrReadOnly
}

func (backend *readOnlyBackend) SetReadOnly(context.Context, string, bool) error {
	return ErrReadOnly
}

func (backend *readOnlyBackend) Close() error {
	return backend.backend.Close()
}

var _ Backend = (*readOnlyBackend)(nil)
