package webdav

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
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
