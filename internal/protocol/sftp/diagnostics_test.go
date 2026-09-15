package sftp

import (
	"context"
	"github.com/danhk0612/DK-Drive/internal/diagnostics"
	pkgsftp "github.com/pkg/sftp"
	"net"
	"testing"
)

func TestMetadataDiagnosticsWithSFTPServer(t *testing.T) {
	local, remote := net.Pipe()
	server, err := pkgsftp.NewServer(remote)
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	defer remote.Close()
	go func() { _ = server.Serve() }()
	client, err := pkgsftp.NewClientPipe(local, local)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	defer local.Close()
	r, err := diagnostics.Open(t.TempDir(), "sftp")
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	b := &Backend{client: client, root: ".", config: Config{Diagnostics: r}}
	if _, err := b.Stat(context.Background(), "."); err != nil {
		t.Fatal(err)
	}
	if _, err := b.ReadDir(context.Background(), "."); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := b.Stat(ctx, "."); err == nil {
		t.Fatal("expected cancellation")
	}
	report := r.Snapshot()
	if report.Metrics[diagnostics.SFTPStat].Calls != 1 || report.Metrics[diagnostics.SFTPReadDir].Calls != 1 {
		t.Fatalf("%+v", report)
	}
}
