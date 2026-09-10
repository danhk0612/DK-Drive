package webdav

import (
	"context"
	"github.com/danhk0612/DK-Drive/internal/diagnostics"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestMetadataDiagnosticsCountsDepthAndFailures(t *testing.T) {
	count := map[string]uint64{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count[r.Header.Get("Depth")]++
		if r.Header.Get("Depth") == "1" {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		writeMultistatus(w, `<d:response><d:href>/dav/home/</d:href><d:propstat><d:prop><d:resourcetype><d:collection/></d:resourcetype></d:prop><d:status>HTTP/1.1 200 OK</d:status></d:propstat></d:response>`)
	}))
	defer server.Close()
	b := newTestBackend(t, server.URL, "tester", "secret")
	defer b.Close()
	r, err := diagnostics.Open(t.TempDir(), "webdav")
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	b.diagnostics = r
	if _, err := b.Stat(context.Background(), "."); err != nil {
		t.Fatal(err)
	}
	if _, err := b.ReadDir(context.Background(), "."); err == nil {
		t.Fatal("expected permission error")
	}
	report := r.Snapshot()
	if report.Metrics[diagnostics.WebDAVDepth0].Calls != count["0"]-1 || report.Metrics[diagnostics.WebDAVDepth1].Calls != count["1"] || report.Metrics[diagnostics.WebDAVDepth1].Errors != 1 {
		t.Fatalf("%+v, counts=%v", report, count)
	}
}
