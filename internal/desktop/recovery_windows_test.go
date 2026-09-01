//go:build windows

package desktop

import (
	"testing"

	localcache "github.com/danhk0612/DK-Drive/internal/cache"
)

func TestRecoveryDisplayHelpers(t *testing.T) {
	if got := recoveryStateText(localcache.StateMissingMetadata); got != "메타데이터 없음" {
		t.Fatalf("state text = %q", got)
	}
	if got := formatByteSize(1536); got != "1.5 KB" {
		t.Fatalf("size text = %q", got)
	}
	if got := recoveryProtocolText("webdav"); got != "WebDAV" {
		t.Fatalf("protocol text = %q", got)
	}
	item := localcache.RecoveryItem{Metadata: localcache.RecoveryMetadata{
		RemotePath: "/home/잘못된:이름?.txt", RecoveryState: localcache.StatePreserved,
	}}
	if got := recoverySuggestedName(item); got != "잘못된_이름_.txt" {
		t.Fatalf("suggested name = %q", got)
	}
	if !recoveryExportable(item) {
		t.Fatal("preserved item is not exportable")
	}
	item.Metadata.RecoveryState = localcache.StateInvalidMetadata
	if recoveryExportable(item) {
		t.Fatal("invalid metadata item is exportable")
	}
}
