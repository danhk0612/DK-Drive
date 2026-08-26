//go:build windows

package mount

import (
	"context"
	"errors"
	"io"
	"os"
	"path"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/winfsp/go-winfsp/gofs"

	localcache "github.com/danhk0612/DK-Drive/internal/cache"
	"github.com/danhk0612/DK-Drive/internal/vfs"
)

const operationTimeout = 30 * time.Second

// NewGoFileSystem adapts the protocol-neutral backend to go-winfsp's gofs
// boundary. Writable files are staged locally and uploaded on Sync or Close.
func NewGoFileSystem(backend vfs.Backend, readOnly bool, cacheStore *localcache.Store) gofs.FileSystem {
	return newGoFileSystem(backend, readOnly, cacheStore)
}

func newGoFileSystem(backend vfs.Backend, readOnly bool, cacheStore *localcache.Store) *goFileSystem {
	return &goFileSystem{backend: backend, readOnly: readOnly, cacheStore: cacheStore}
}

type goFileSystem struct {
	backend    vfs.Backend
	readOnly   bool
	cacheStore *localcache.Store
	openFiles  sync.Map
}

func (filesystem *goFileSystem) OpenFile(name string, flag int, perm os.FileMode) (gofs.File, error) {
	if filesystem.readOnly && flag&(os.O_WRONLY|os.O_RDWR|os.O_CREATE|os.O_TRUNC|os.O_APPEND) != 0 {
		return nil, vfs.ErrReadOnly
	}
	name = cleanMountPath(name)
	entry, statErr := filesystem.stat(name)
	if statErr == nil && entry.IsDir() {
		if flag&(os.O_WRONLY|os.O_RDWR|os.O_TRUNC|os.O_APPEND) != 0 {
			return nil, syscall.EISDIR
		}
		return &directoryFile{filesystem: filesystem, name: name}, nil
	}
	if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
		return nil, statErr
	}
	exists := statErr == nil
	if exists && flag&os.O_CREATE != 0 && flag&os.O_EXCL != 0 {
		return nil, os.ErrExist
	}
	if !exists && flag&os.O_CREATE == 0 {
		return nil, os.ErrNotExist
	}

	temporary, err := filesystem.cacheStore.CreateStaging()
	if err != nil {
		return nil, err
	}
	temporaryPath := temporary.Name()
	if err := temporary.Close(); err != nil {
		os.Remove(temporaryPath)
		return nil, err
	}

	removeTemporary := true
	defer func() {
		if removeTemporary {
			os.Remove(temporaryPath)
		}
	}()

	if exists && flag&os.O_TRUNC == 0 {
		if err := filesystem.download(name, temporaryPath); err != nil {
			return nil, err
		}
	}

	localFlags := os.O_RDWR | os.O_CREATE
	localFlags |= flag & (os.O_TRUNC | os.O_APPEND)
	local, err := os.OpenFile(temporaryPath, localFlags, perm)
	if err != nil {
		return nil, err
	}

	dirty := !exists || flag&os.O_TRUNC != 0
	removeTemporary = false
	file := &stagedFile{
		File:           local,
		filesystem:     filesystem,
		name:           name,
		temporaryPath:  temporaryPath,
		mode:           entry.Mode,
		modTime:        entry.ModTime,
		dirty:          dirty,
		writePermitted: flag&(os.O_WRONLY|os.O_RDWR) != 0,
	}
	filesystem.openFiles.Store(file, struct{}{})
	return file, nil
}

func (filesystem *goFileSystem) Mkdir(name string, _ os.FileMode) error {
	ctx, cancel := operationContext()
	defer cancel()
	return filesystem.backend.Mkdir(ctx, cleanMountPath(name))
}

func (filesystem *goFileSystem) Stat(name string) (os.FileInfo, error) {
	entry, err := filesystem.stat(cleanMountPath(name))
	if err != nil {
		return nil, err
	}
	return entryInfo{entry: entry}, nil
}

func (filesystem *goFileSystem) Rename(source, target string) error {
	ctx, cancel := operationContext()
	defer cancel()
	return filesystem.backend.Rename(ctx, cleanMountPath(source), cleanMountPath(target))
}

func (filesystem *goFileSystem) Remove(name string) error {
	name = cleanMountPath(name)
	entry, err := filesystem.stat(name)
	if err != nil {
		return err
	}
	ctx, cancel := operationContext()
	defer cancel()
	return filesystem.backend.Remove(ctx, name, entry.IsDir())
}

func (filesystem *goFileSystem) stat(name string) (vfs.Entry, error) {
	ctx, cancel := operationContext()
	defer cancel()
	return filesystem.backend.Stat(ctx, name)
}

func (filesystem *goFileSystem) setModTime(name string, modTime time.Time) error {
	ctx, cancel := operationContext()
	defer cancel()
	if err := filesystem.backend.SetModTime(ctx, name, modTime); err != nil {
		return err
	}

	var updateErr error
	filesystem.openFiles.Range(func(key, _ any) bool {
		file := key.(*stagedFile)
		if file.name == name {
			if err := file.setModTime(modTime); err != nil && updateErr == nil {
				updateErr = err
			}
		}
		return true
	})
	return updateErr
}

func (filesystem *goFileSystem) setBackendModTime(name string, modTime time.Time) error {
	ctx, cancel := operationContext()
	defer cancel()
	return filesystem.backend.SetModTime(ctx, name, modTime)
}

func (filesystem *goFileSystem) download(name, destination string) error {
	ctx, cancel := operationContext()
	defer cancel()
	reader, err := filesystem.backend.OpenRead(ctx, name)
	if err != nil {
		return err
	}
	local, err := os.OpenFile(destination, os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		reader.Close()
		return err
	}
	_, copyErr := io.Copy(local, reader)
	return errors.Join(copyErr, local.Close(), reader.Close())
}

func (filesystem *goFileSystem) upload(name string, local *os.File) error {
	info, err := local.Stat()
	if err != nil {
		return err
	}
	ctx, cancel := operationContext()
	defer cancel()
	writer, err := filesystem.backend.OpenWrite(ctx, name, vfs.WriteOptions{
		Create: true, Truncate: true, Mode: info.Mode(),
	})
	if err != nil {
		return err
	}

	buffer := make([]byte, 256*1024)
	var offset int64
	var writeErr error
	for offset < info.Size() {
		want := int64(len(buffer))
		if remaining := info.Size() - offset; remaining < want {
			want = remaining
		}
		n, readErr := local.ReadAt(buffer[:want], offset)
		if readErr != nil && !errors.Is(readErr, io.EOF) {
			writeErr = readErr
			break
		}
		written, err := writer.WriteAt(buffer[:n], offset)
		if err != nil {
			writeErr = err
			break
		}
		if written != n {
			writeErr = io.ErrShortWrite
			break
		}
		offset += int64(n)
	}
	if writeErr == nil {
		writeErr = writer.Sync()
	}
	return errors.Join(writeErr, writer.Close())
}

type stagedFile struct {
	*os.File
	filesystem     *goFileSystem
	name           string
	temporaryPath  string
	mode           os.FileMode
	modTime        time.Time
	dirty          bool
	writePermitted bool
	requestedTime  time.Time
	closed         bool
	mutex          sync.Mutex
}

func (file *stagedFile) Write(buffer []byte) (int, error) {
	file.markDirty()
	return file.File.Write(buffer)
}

func (file *stagedFile) WriteAt(buffer []byte, offset int64) (int, error) {
	file.markDirty()
	return file.File.WriteAt(buffer, offset)
}

func (file *stagedFile) Truncate(size int64) error {
	file.markDirty()
	return file.File.Truncate(size)
}

func (file *stagedFile) Stat() (os.FileInfo, error) {
	file.mutex.Lock()
	defer file.mutex.Unlock()
	info, err := file.File.Stat()
	if err != nil {
		return nil, err
	}
	mode := file.mode
	if mode == 0 {
		mode = info.Mode()
	}
	modTime := file.modTime
	if modTime.IsZero() || file.dirty {
		modTime = info.ModTime()
	}
	return stagedInfo{FileInfo: info, name: path.Base(file.name), mode: mode, modTime: modTime}, nil
}

func (file *stagedFile) Readdir(int) ([]os.FileInfo, error) {
	return nil, syscall.ENOTDIR
}

func (file *stagedFile) Sync() error {
	file.mutex.Lock()
	defer file.mutex.Unlock()
	if file.closed {
		return os.ErrClosed
	}
	if err := file.File.Sync(); err != nil {
		return err
	}
	if !file.dirty {
		return nil
	}
	if err := file.filesystem.upload(file.name, file.File); err != nil {
		return err
	}
	if !file.requestedTime.IsZero() {
		if err := file.filesystem.setBackendModTime(file.name, file.requestedTime); err != nil {
			return err
		}
	}
	file.dirty = false
	if file.requestedTime.IsZero() {
		file.modTime = time.Now()
	} else {
		file.modTime = file.requestedTime
	}
	return nil
}

func (file *stagedFile) Close() error {
	syncErr := file.Sync()
	file.mutex.Lock()
	if file.closed {
		file.mutex.Unlock()
		return os.ErrClosed
	}
	file.closed = true
	closeErr := file.File.Close()
	file.mutex.Unlock()
	file.filesystem.openFiles.Delete(file)
	if syncErr == nil {
		return errors.Join(closeErr, os.Remove(file.temporaryPath))
	}
	return errors.Join(syncErr, closeErr)
}

func (file *stagedFile) setModTime(modTime time.Time) error {
	file.mutex.Lock()
	defer file.mutex.Unlock()
	if file.closed {
		return os.ErrClosed
	}
	if err := os.Chtimes(file.temporaryPath, modTime, modTime); err != nil {
		return err
	}
	file.modTime = modTime
	file.requestedTime = modTime
	return nil
}

func (file *stagedFile) markDirty() {
	file.mutex.Lock()
	defer file.mutex.Unlock()
	if file.writePermitted {
		file.dirty = true
		file.requestedTime = time.Time{}
	}
}

type directoryFile struct {
	filesystem *goFileSystem
	name       string
	entries    []os.FileInfo
	offset     int
	closed     bool
}

func (file *directoryFile) Read([]byte) (int, error) {
	return 0, syscall.EISDIR
}

func (file *directoryFile) ReadAt([]byte, int64) (int, error) {
	return 0, syscall.EISDIR
}

func (file *directoryFile) Write([]byte) (int, error) {
	return 0, syscall.EISDIR
}

func (file *directoryFile) WriteAt([]byte, int64) (int, error) {
	return 0, syscall.EISDIR
}

func (file *directoryFile) Seek(int64, int) (int64, error) {
	return 0, syscall.EISDIR
}

func (file *directoryFile) Truncate(int64) error {
	return syscall.EISDIR
}

func (file *directoryFile) Sync() error {
	return nil
}

func (file *directoryFile) Close() error {
	if file.closed {
		return os.ErrClosed
	}
	file.closed = true
	return nil
}

func (file *directoryFile) Stat() (os.FileInfo, error) {
	if file.closed {
		return nil, os.ErrClosed
	}
	return file.filesystem.Stat(file.name)
}

func (file *directoryFile) Readdir(count int) ([]os.FileInfo, error) {
	if file.closed {
		return nil, os.ErrClosed
	}
	if file.entries == nil {
		ctx, cancel := operationContext()
		defer cancel()
		entries, err := file.filesystem.backend.ReadDir(ctx, file.name)
		if err != nil {
			return nil, err
		}
		file.entries = make([]os.FileInfo, len(entries))
		for index, entry := range entries {
			file.entries[index] = entryInfo{entry: entry}
		}
	}
	if count <= 0 {
		entries := file.entries[file.offset:]
		file.offset = len(file.entries)
		return entries, nil
	}
	if file.offset >= len(file.entries) {
		return nil, io.EOF
	}
	end := file.offset + count
	if end > len(file.entries) {
		end = len(file.entries)
	}
	entries := file.entries[file.offset:end]
	file.offset = end
	return entries, nil
}

type entryInfo struct {
	entry vfs.Entry
}

func (info entryInfo) Name() string       { return info.entry.Name }
func (info entryInfo) Size() int64        { return info.entry.Size }
func (info entryInfo) Mode() os.FileMode  { return info.entry.Mode }
func (info entryInfo) ModTime() time.Time { return info.entry.ModTime }
func (info entryInfo) IsDir() bool        { return info.entry.IsDir() }
func (info entryInfo) Sys() any           { return nil }

type stagedInfo struct {
	os.FileInfo
	name    string
	mode    os.FileMode
	modTime time.Time
}

func (info stagedInfo) Name() string       { return info.name }
func (info stagedInfo) Mode() os.FileMode  { return info.mode }
func (info stagedInfo) ModTime() time.Time { return info.modTime }

func cleanMountPath(name string) string {
	name = strings.ReplaceAll(name, "\\", "/")
	name = strings.TrimPrefix(path.Clean("/"+name), "/")
	if name == "" {
		return "."
	}
	return name
}

func operationContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), operationTimeout)
}

var _ gofs.FileSystem = (*goFileSystem)(nil)
var _ gofs.File = (*stagedFile)(nil)
var _ gofs.File = (*directoryFile)(nil)
