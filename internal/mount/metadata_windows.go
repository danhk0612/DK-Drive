//go:build windows

package mount

import (
	"errors"
	"fmt"
	"sync"
	"syscall"
	"time"

	"github.com/winfsp/go-winfsp"
	"github.com/winfsp/go-winfsp/gofs"
	"golang.org/x/sys/windows"
)

type goFSBehaviour interface {
	winfsp.BehaviourBase
	winfsp.BehaviourGetSecurityByName
	winfsp.BehaviourCreate
	winfsp.BehaviourOverwrite
	winfsp.BehaviourReadDirectory
	winfsp.BehaviourGetFileInfo
	winfsp.BehaviourGetSecurity
	winfsp.BehaviourGetVolumeInfo
	winfsp.BehaviourSetVolumeLabel
	winfsp.BehaviourSetBasicInfo
	winfsp.BehaviourSetFileSize
	winfsp.BehaviourRead
	winfsp.BehaviourWrite
	winfsp.BehaviourFlush
	winfsp.BehaviourCanDelete
	winfsp.BehaviourCleanup
	winfsp.BehaviourRename
	winfsp.BehaviourDefaultOptions
}

// metadataBehaviour adds the SFTP metadata operations that gofs does not
// expose. Embedding the complete gofs behaviour preserves all optional WinFsp
// callbacks when this adapter is passed to winfsp.Mount.
type metadataBehaviour struct {
	goFSBehaviour
	filesystem *goFileSystem
	readOnly   bool
	handles    sync.Map
}

func NewMetadataBehaviour(inner winfsp.BehaviourBase, filesystem gofs.FileSystem, readOnly bool) (winfsp.BehaviourBase, error) {
	delegate, ok := inner.(goFSBehaviour)
	if !ok {
		return nil, fmt.Errorf("지원되지 않는 gofs 동작 집합: %T", inner)
	}
	mountFileSystem, ok := filesystem.(*goFileSystem)
	if !ok {
		return nil, fmt.Errorf("지원되지 않는 마운트 파일시스템: %T", filesystem)
	}
	return &metadataBehaviour{goFSBehaviour: delegate, filesystem: mountFileSystem, readOnly: readOnly}, nil
}

func (behaviour *metadataBehaviour) Open(
	filesystem *winfsp.FileSystemRef,
	name string,
	createOptions, grantedAccess uint32,
	info *winfsp.FSP_FSCTL_FILE_INFO,
) (uintptr, error) {
	handle, err := behaviour.goFSBehaviour.Open(filesystem, name, createOptions, grantedAccess, info)
	if err == nil {
		behaviour.handles.Store(handle, cleanMountPath(name))
	}
	return handle, err
}

func (behaviour *metadataBehaviour) Create(
	filesystem *winfsp.FileSystemRef,
	name string,
	createOptions, grantedAccess, fileAttributes uint32,
	securityDescriptor *windows.SECURITY_DESCRIPTOR,
	allocationSize uint64,
	info *winfsp.FSP_FSCTL_FILE_INFO,
) (uintptr, error) {
	handle, err := behaviour.goFSBehaviour.Create(
		filesystem, name, createOptions, grantedAccess, fileAttributes,
		securityDescriptor, allocationSize, info,
	)
	if err == nil {
		behaviour.handles.Store(handle, cleanMountPath(name))
	}
	return handle, err
}

func (behaviour *metadataBehaviour) Close(filesystem *winfsp.FileSystemRef, file uintptr) {
	behaviour.handles.Delete(file)
	behaviour.goFSBehaviour.Close(filesystem, file)
}

func (behaviour *metadataBehaviour) Rename(
	filesystem *winfsp.FileSystemRef,
	file uintptr,
	source, target string,
	replaceIfExists bool,
) error {
	if err := behaviour.goFSBehaviour.Rename(filesystem, file, source, target, replaceIfExists); err != nil {
		return err
	}
	behaviour.handles.Store(file, cleanMountPath(target))
	return nil
}

func (behaviour *metadataBehaviour) SetBasicInfo(
	filesystem *winfsp.FileSystemRef,
	file uintptr,
	flags winfsp.SetBasicInfoFlags,
	attributes uint32,
	creationTime, lastAccessTime, lastWriteTime, changeTime uint64,
	info *winfsp.FSP_FSCTL_FILE_INFO,
) error {
	const supported = winfsp.SetBasicInfoAttributes | winfsp.SetBasicInfoLastWriteTime
	if behaviour.readOnly || flags&^supported != 0 {
		return behaviour.goFSBehaviour.SetBasicInfo(
			filesystem, file, flags, attributes,
			creationTime, lastAccessTime, lastWriteTime, changeTime, info,
		)
	}

	value, ok := behaviour.handles.Load(file)
	if !ok {
		return windows.STATUS_INVALID_HANDLE
	}
	name := value.(string)
	ctx, cancel := operationContext()
	defer cancel()

	if flags&winfsp.SetBasicInfoAttributes != 0 {
		entry, err := behaviour.filesystem.backend.Stat(ctx, name)
		if err != nil {
			return err
		}
		if !entry.IsDir() {
			readOnly := attributes&windows.FILE_ATTRIBUTE_READONLY != 0
			if err := behaviour.filesystem.backend.SetReadOnly(ctx, name, readOnly); err != nil {
				return err
			}
		}
	}
	if flags&winfsp.SetBasicInfoLastWriteTime != 0 {
		if err := behaviour.filesystem.setModTime(name, filetimeToTime(lastWriteTime)); err != nil {
			return err
		}
	}

	err := behaviour.goFSBehaviour.SetBasicInfo(
		filesystem, file, flags, attributes,
		creationTime, lastAccessTime, lastWriteTime, changeTime, info,
	)
	if err != nil && !errors.Is(err, windows.STATUS_ACCESS_DENIED) {
		return err
	}
	if flags&winfsp.SetBasicInfoAttributes != 0 {
		info.FileAttributes &^= windows.FILE_ATTRIBUTE_READONLY
		if attributes&windows.FILE_ATTRIBUTE_READONLY != 0 {
			info.FileAttributes &^= windows.FILE_ATTRIBUTE_NORMAL
			info.FileAttributes |= windows.FILE_ATTRIBUTE_READONLY
		} else if info.FileAttributes == 0 {
			info.FileAttributes = windows.FILE_ATTRIBUTE_NORMAL
		}
	}
	if flags&winfsp.SetBasicInfoLastWriteTime != 0 {
		info.LastWriteTime = lastWriteTime
		info.ChangeTime = lastWriteTime
	}
	return nil
}

func filetimeToTime(value uint64) time.Time {
	filetime := syscall.Filetime{
		LowDateTime:  uint32(value),
		HighDateTime: uint32(value >> 32),
	}
	return time.Unix(0, filetime.Nanoseconds())
}

var _ goFSBehaviour = (*metadataBehaviour)(nil)
