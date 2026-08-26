package vfs

import (
	"context"
	"errors"
	"io/fs"
	"testing"
	"time"
)

func TestReadOnlyBackendRejectsMutations(t *testing.T) {
	backend := &readOnlyBackend{}
	ctx := context.Background()
	tests := []struct {
		name string
		run  func() error
	}{
		{name: "OpenWrite", run: func() error {
			_, err := backend.OpenWrite(ctx, "file.txt", WriteOptions{Create: true})
			return err
		}},
		{name: "Mkdir", run: func() error { return backend.Mkdir(ctx, "folder") }},
		{name: "RemoveFile", run: func() error { return backend.Remove(ctx, "file.txt", false) }},
		{name: "RemoveDirectory", run: func() error { return backend.Remove(ctx, "folder", true) }},
		{name: "Rename", run: func() error { return backend.Rename(ctx, "old", "new") }},
		{name: "SetModTime", run: func() error { return backend.SetModTime(ctx, "file.txt", time.Now()) }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := test.run(); !errors.Is(err, fs.ErrPermission) {
				t.Fatalf("error = %v, want fs.ErrPermission", err)
			}
		})
	}
}
