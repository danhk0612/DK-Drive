package ftp

import (
	"context"
	"crypto/tls"
	"github.com/danhk0612/DK-Drive/internal/diagnostics"
	"testing"
)

func TestMetadataDiagnosticsCountRetries(t *testing.T) {
	for _, command := range []string{"MLST", "MLSD"} {
		t.Run(command, func(t *testing.T) {
			server := newTestServer(t, TLSNone, tls.Certificate{}, command+"-421")
			defer server.Close()
			b := recoveryBackend(t, server)
			r, err := diagnostics.Open(t.TempDir(), "ftp")
			if err != nil {
				t.Fatal(err)
			}
			defer r.Close()
			b.config.Diagnostics = r
			op := diagnostics.FTPGetEntry
			if command == "MLST" {
				_, err = b.Stat(context.Background(), "hello.txt")
			} else {
				op = diagnostics.FTPList
				_, err = b.ReadDir(context.Background(), ".")
			}
			if err != nil {
				t.Fatal(err)
			}
			m := r.Snapshot().Metrics[op]
			if m.Calls != uint64(server.Count(command)) || m.Calls != 2 || m.Errors != 1 {
				t.Fatalf("metric=%+v, server=%d", m, server.Count(command))
			}
		})
	}
}
