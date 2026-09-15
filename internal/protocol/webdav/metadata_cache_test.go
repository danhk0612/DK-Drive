package webdav

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/danhk0612/DK-Drive/internal/vfs"
)

func TestMetadataCacheEliminatesChildPropfind(t *testing.T) {
	var depth0, depth1 atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := `<d:response><d:href>/dav/home/</d:href><d:propstat><d:prop><d:resourcetype><d:collection/></d:resourcetype></d:prop><d:status>HTTP/1.1 200 OK</d:status></d:propstat></d:response>`
		if r.Header.Get("Depth") == "1" {
			depth1.Add(1)
			body += `<d:response><d:href>/dav/home/a.jpg</d:href><d:propstat><d:prop><d:resourcetype/><d:getcontentlength>42</d:getcontentlength></d:prop><d:status>HTTP/1.1 200 OK</d:status></d:propstat></d:response>`
		} else {
			depth0.Add(1)
		}
		writeMultistatus(w, body)
	}))
	defer server.Close()
	c := vfs.NewMetadataCache(newTestBackend(t, server.URL, "tester", "secret"))
	defer c.Close()
	ctx := context.Background()
	for i := 0; i < 2; i++ {
		if _, err := c.ReadDir(ctx, "/"); err != nil {
			t.Fatal(err)
		}
		e, err := c.Stat(ctx, "/a.jpg")
		if err != nil || e.Size != 42 {
			t.Fatalf("%+v %v", e, err)
		}
	}
	if depth0.Load() != 1 || depth1.Load() != 1 {
		t.Fatalf("Depth 0/1: %d/%d", depth0.Load(), depth1.Load())
	}
}
