//go:build windows

package mount

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"

	localcache "github.com/danhk0612/DK-Drive/internal/cache"
	"github.com/danhk0612/DK-Drive/internal/vfs"
	"github.com/winfsp/go-winfsp"
	"github.com/winfsp/go-winfsp/gofs"
)

// Session is used by the GUI only. Existing spike CLIs keep their lifecycle.
type Session struct {
	mu      sync.Mutex
	fs      *winfsp.FileSystem
	guard   *guardedFS
	backend vfs.Backend
}

func StartSession(backend vfs.Backend, options Options, metadata bool) (*Session, error) {
	store, err := localcache.New("")
	if err != nil {
		return nil, err
	}
	delegate := backend
	mode := gofs.AttribReadOnlyWindows
	if options.ReadOnly {
		delegate = vfs.NewReadOnlyBackend(backend)
		mode = gofs.AttribReadOnlyAlways
	}
	raw := newGoFileSystem(delegate, options.ReadOnly, store)
	guard := &guardedFS{FileSystem: raw, cache: store.Directory()}
	base, err := gofs.NewOptions(guard, gofs.WithCaseInsensitive(false), gofs.WithAttribReadOnlyTransMode(mode))
	if err != nil {
		return nil, err
	}
	if options.VolumeName != "" {
		if err := base.(winfsp.BehaviourSetVolumeLabel).SetVolumeLabel(nil, options.VolumeName, new(winfsp.FSP_FSCTL_VOLUME_INFO)); err != nil {
			return nil, err
		}
	}
	if metadata {
		base, err = NewMetadataBehaviour(base, raw, options.ReadOnly)
		if err != nil {
			return nil, err
		}
	}
	fs, err := winfsp.Mount(base, strings.ToUpper(options.DriveLetter)+":")
	if err != nil {
		return nil, err
	}
	return &Session{fs: fs, guard: guard, backend: backend}, nil
}

func (s *Session) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.fs == nil {
		return nil
	}
	if err := s.guard.prepareStop(); err != nil {
		return err
	}
	s.fs.Unmount()
	s.fs = nil
	// With no open handles or failed closes, protocol socket close errors cannot
	// leave staged writes behind. The drive has already been unmounted.
	_ = s.backend.Close()
	return nil
}

type guardedFS struct {
	gofs.FileSystem
	mu       sync.Mutex
	open     int
	stopping bool
	closeErr error
	cache    string
}

func (g *guardedFS) prepareStop() error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.open != 0 {
		return fmt.Errorf("파일/폴더 핸들 %d개가 열려 있습니다; 탐색기와 편집기를 닫고 다시 해제하세요", g.open)
	}
	if g.closeErr != nil {
		return fmt.Errorf("파일 닫기/업로드 실패가 있어 해제를 중단했습니다. 캐시 보존 위치: %s: %w", g.cache, g.closeErr)
	}
	g.stopping = true
	return nil
}

func (g *guardedFS) OpenFile(n string, f int, m os.FileMode) (gofs.File, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.stopping {
		return nil, os.ErrClosed
	}
	file, err := g.FileSystem.OpenFile(n, f, m)
	if err != nil {
		return nil, err
	}
	g.open++
	return &trackedFile{File: file, owner: g}, nil
}
func (g *guardedFS) Stat(n string) (os.FileInfo, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.stopping {
		return nil, os.ErrClosed
	}
	return g.FileSystem.Stat(n)
}
func (g *guardedFS) Mkdir(n string, m os.FileMode) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.stopping {
		return os.ErrClosed
	}
	return g.FileSystem.Mkdir(n, m)
}
func (g *guardedFS) Rename(a, b string) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.stopping {
		return os.ErrClosed
	}
	return g.FileSystem.Rename(a, b)
}
func (g *guardedFS) Remove(n string) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.stopping {
		return os.ErrClosed
	}
	return g.FileSystem.Remove(n)
}

type trackedFile struct {
	gofs.File
	owner  *guardedFS
	closed bool
}

func (f *trackedFile) Close() error {
	f.owner.mu.Lock()
	defer f.owner.mu.Unlock()
	if f.closed {
		return os.ErrClosed
	}
	err := f.File.Close()
	f.closed = true
	f.owner.open--
	f.owner.closeErr = errors.Join(f.owner.closeErr, err)
	return err
}
