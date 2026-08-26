package webdav

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/danhk0612/DK-Drive/internal/vfs"
)

func TestBackendListsAndReads(t *testing.T) {
	const username = "tester"
	const password = "secret"
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		gotUsername, gotPassword, ok := request.BasicAuth()
		if !ok || gotUsername != username || gotPassword != password {
			writer.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch {
		case request.Method == "PROPFIND" && request.URL.Path == "/dav/home" && request.Header.Get("Depth") == "0":
			writeMultistatus(writer, `<d:response><d:href>/dav/home/</d:href><d:propstat><d:prop><d:resourcetype><d:collection/></d:resourcetype><d:getlastmodified>Wed, 26 Aug 2026 01:00:00 GMT</d:getlastmodified></d:prop><d:status>HTTP/1.1 200 OK</d:status></d:propstat></d:response>`)
		case request.Method == "PROPFIND" && request.URL.Path == "/dav/home" && request.Header.Get("Depth") == "1":
			writeMultistatus(writer,
				`<d:response><d:href>/dav/home/</d:href><d:propstat><d:prop><d:resourcetype><d:collection/></d:resourcetype></d:prop><d:status>HTTP/1.1 200 OK</d:status></d:propstat></d:response>`+
					`<d:response><d:href>/dav/home/%ED%95%9C%EA%B8%80.txt</d:href><d:propstat><d:prop><d:resourcetype/><d:getcontentlength>7</d:getcontentlength><d:getlastmodified>Wed, 26 Aug 2026 02:00:00 GMT</d:getlastmodified></d:prop><d:status>HTTP/1.1 200 OK</d:status></d:propstat></d:response>`)
		case request.Method == http.MethodGet && request.URL.Path == "/dav/home/한글.txt":
			fmt.Fprint(writer, "content")
		default:
			http.Error(writer, "unexpected request", http.StatusBadRequest)
		}
	}))
	defer server.Close()

	backend := newTestBackend(t, server.URL, username, password)
	defer backend.Close()
	entries, err := backend.ReadDir(context.Background(), ".")
	if err != nil {
		t.Fatalf("ReadDir(): %v", err)
	}
	if len(entries) != 1 || entries[0].Name != "한글.txt" || entries[0].Size != 7 {
		t.Fatalf("ReadDir() = %#v", entries)
	}
	reader, err := backend.OpenRead(context.Background(), "한글.txt")
	if err != nil {
		t.Fatalf("OpenRead(): %v", err)
	}
	data, readErr := io.ReadAll(reader)
	closeErr := reader.Close()
	if readErr != nil || closeErr != nil {
		t.Fatalf("read error = %v, close error = %v", readErr, closeErr)
	}
	if string(data) != "content" {
		t.Fatalf("content = %q", data)
	}
}

func TestResourceURLRejectsParentTraversal(t *testing.T) {
	backend := Backend{baseURL: mustURL(t, "https://example.test/dav/home")}
	if _, err := backend.resourceURL("../secret"); err == nil {
		t.Fatal("resourceURL() returned nil error")
	}
}

func TestBackendWritesMovesAndDeletes(t *testing.T) {
	var uploaded string
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch {
		case request.Method == "PROPFIND" && request.URL.Path == "/dav/home":
			writeMultistatus(writer, `<d:response><d:href>/dav/home/</d:href><d:propstat><d:prop><d:resourcetype><d:collection/></d:resourcetype></d:prop><d:status>HTTP/1.1 200 OK</d:status></d:propstat></d:response>`)
		case request.Method == "MKCOL" && request.URL.Path == "/dav/home/test":
			writer.WriteHeader(http.StatusCreated)
		case request.Method == http.MethodPut && request.URL.Path == "/dav/home/test/source.txt":
			data, err := io.ReadAll(request.Body)
			if err != nil {
				t.Error(err)
			}
			uploaded = string(data)
			writer.WriteHeader(http.StatusCreated)
		case request.Method == "MOVE" && request.URL.Path == "/dav/home/test/source.txt":
			if !strings.HasSuffix(request.Header.Get("Destination"), "/dav/home/test/moved.txt") {
				t.Errorf("Destination = %q", request.Header.Get("Destination"))
			}
			writer.WriteHeader(http.StatusCreated)
		case request.Method == http.MethodDelete && (request.URL.Path == "/dav/home/test/moved.txt" || request.URL.Path == "/dav/home/test"):
			writer.WriteHeader(http.StatusNoContent)
		default:
			http.Error(writer, "unexpected request", http.StatusBadRequest)
		}
	}))
	defer server.Close()

	backend := newTestBackend(t, server.URL, "tester", "secret")
	defer backend.Close()
	ctx := context.Background()
	if err := backend.Mkdir(ctx, "test"); err != nil {
		t.Fatalf("Mkdir(): %v", err)
	}
	handle, err := backend.OpenWrite(ctx, "test/source.txt", vfs.WriteOptions{Create: true, Truncate: true})
	if err != nil {
		t.Fatalf("OpenWrite(): %v", err)
	}
	if _, err := handle.WriteAt([]byte("DKDrive WebDAV"), 0); err != nil {
		t.Fatalf("WriteAt(): %v", err)
	}
	if err := handle.Close(); err != nil {
		t.Fatalf("Close(): %v", err)
	}
	if uploaded != "DKDrive WebDAV" {
		t.Fatalf("uploaded = %q", uploaded)
	}
	if err := backend.Rename(ctx, "test/source.txt", "test/moved.txt"); err != nil {
		t.Fatalf("Rename(): %v", err)
	}
	if err := backend.Remove(ctx, "test/moved.txt", false); err != nil {
		t.Fatalf("Remove(file): %v", err)
	}
	if err := backend.Remove(ctx, "test", true); err != nil {
		t.Fatalf("Remove(directory): %v", err)
	}
}

func TestResponseErrorMapsFileSystemErrors(t *testing.T) {
	tests := []struct {
		status int
		want   error
	}{
		{status: http.StatusNotFound, want: fs.ErrNotExist},
		{status: http.StatusUnauthorized, want: fs.ErrPermission},
		{status: http.StatusForbidden, want: fs.ErrPermission},
		{status: http.StatusLocked, want: fs.ErrPermission},
		{status: http.StatusMethodNotAllowed, want: errors.ErrUnsupported},
	}
	for _, test := range tests {
		response := &http.Response{
			StatusCode: test.status,
			Status:     fmt.Sprintf("%d test", test.status),
			Body:       io.NopCloser(strings.NewReader("detail")),
		}
		if err := responseError("TEST", response); !errors.Is(err, test.want) {
			t.Errorf("responseError(%d) = %v, want %v", test.status, err, test.want)
		}
	}
}

func TestCapabilities(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method == "PROPFIND" {
			writeMultistatus(writer, `<d:response><d:href>/dav/home/</d:href><d:propstat><d:prop><d:resourcetype><d:collection/></d:resourcetype></d:prop><d:status>HTTP/1.1 200 OK</d:status></d:propstat></d:response>`)
			return
		}
		if request.Method != http.MethodOptions {
			http.Error(writer, "unexpected request", http.StatusBadRequest)
			return
		}
		writer.Header().Add("DAV", "1, 2")
		writer.Header().Add("Allow", "OPTIONS, PROPFIND, GET")
		writer.Header().Add("Allow", "PUT, DELETE, MOVE")
		writer.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	backend := newTestBackend(t, server.URL, "tester", "secret")
	defer backend.Close()
	capabilities, err := backend.Capabilities(context.Background())
	if err != nil {
		t.Fatalf("Capabilities(): %v", err)
	}
	if len(capabilities.DAVClasses) != 2 || capabilities.DAVClasses[0] != "1" || capabilities.DAVClasses[1] != "2" {
		t.Fatalf("DAV classes = %#v", capabilities.DAVClasses)
	}
	for _, method := range []string{"OPTIONS", "PROPFIND", "GET", "PUT", "DELETE", "MOVE"} {
		if !capabilities.Methods[method] {
			t.Errorf("method %s not reported", method)
		}
	}
}

func TestLockAndUnlock(t *testing.T) {
	const token = "opaquelocktoken:dkdrive-test"
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.Method {
		case "PROPFIND":
			writeMultistatus(writer, `<d:response><d:href>/dav/home/</d:href><d:propstat><d:prop><d:resourcetype><d:collection/></d:resourcetype></d:prop><d:status>HTTP/1.1 200 OK</d:status></d:propstat></d:response>`)
		case "LOCK":
			if request.Header.Get("Depth") != "0" || request.Header.Get("Timeout") != "Second-60" {
				t.Errorf("LOCK headers: Depth=%q Timeout=%q", request.Header.Get("Depth"), request.Header.Get("Timeout"))
			}
			writer.Header().Set("Lock-Token", "<"+token+">")
			writer.WriteHeader(http.StatusOK)
		case "UNLOCK":
			if request.Header.Get("Lock-Token") != "<"+token+">" {
				t.Errorf("Lock-Token = %q", request.Header.Get("Lock-Token"))
			}
			writer.WriteHeader(http.StatusNoContent)
		default:
			http.Error(writer, "unexpected request", http.StatusBadRequest)
		}
	}))
	defer server.Close()

	backend := newTestBackend(t, server.URL, "tester", "secret")
	defer backend.Close()
	got, err := backend.Lock(context.Background(), "lock.txt", 60*time.Second)
	if err != nil {
		t.Fatalf("Lock(): %v", err)
	}
	if got != token {
		t.Fatalf("token = %q, want %q", got, token)
	}
	if err := backend.Unlock(context.Background(), "lock.txt", got); err != nil {
		t.Fatalf("Unlock(): %v", err)
	}
}

func TestValidateConfig(t *testing.T) {
	valid := Config{Scheme: "https", Host: "example.test", Port: 443, Username: "tester", Password: "secret"}
	if err := validateConfig(valid); err != nil {
		t.Fatalf("validateConfig(): %v", err)
	}
	invalid := []Config{
		{Scheme: "ftp", Host: "example.test", Port: 443, Username: "tester", Password: "secret"},
		{Scheme: "https", Port: 443, Username: "tester", Password: "secret"},
		{Scheme: "https", Host: "example.test", Username: "tester", Password: "secret"},
		{Scheme: "https", Host: "example.test", Port: 443, Password: "secret"},
		{Scheme: "https", Host: "example.test", Port: 443, Username: "tester"},
	}
	for _, config := range invalid {
		if err := validateConfig(config); err == nil {
			t.Fatalf("validateConfig(%#v) returned nil error", config)
		}
	}
}

func newTestBackend(t *testing.T, serverURL, username, password string) *Backend {
	t.Helper()
	parsed := mustURL(t, serverURL)
	port, err := strconv.ParseUint(parsed.Port(), 10, 16)
	if err != nil {
		t.Fatal(err)
	}
	backend, err := New(context.Background(), Config{
		Scheme: parsed.Scheme, Host: parsed.Hostname(), Port: uint16(port),
		Username: username, Password: password, Root: "/dav/home",
	})
	if err != nil {
		t.Fatalf("New(): %v", err)
	}
	return backend
}

func mustURL(t *testing.T, value string) *url.URL {
	t.Helper()
	parsed, err := url.Parse(value)
	if err != nil {
		t.Fatal(err)
	}
	return parsed
}

func writeMultistatus(writer http.ResponseWriter, responses string) {
	writer.Header().Set("Content-Type", "application/xml")
	writer.WriteHeader(http.StatusMultiStatus)
	fmt.Fprintf(writer, `<?xml version="1.0"?><d:multistatus xmlns:d="DAV:">%s</d:multistatus>`, strings.TrimSpace(responses))
}
