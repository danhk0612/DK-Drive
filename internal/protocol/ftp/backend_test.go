package ftp

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"fmt"
	"io"
	"math/big"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/danhk0612/DK-Drive/internal/vfs"
)

func TestValidateConfig(t *testing.T) {
	base := Config{Host: "example.test", Port: 21, Username: "tester", Password: "secret", TLSMode: TLSNone}
	if err := validateConfig(base); err != nil {
		t.Fatalf("validateConfig(): %v", err)
	}

	tests := []Config{
		{},
		{Host: "example.test", Username: "tester", Password: "secret", TLSMode: TLSNone},
		{Host: "example.test", Port: 21, Password: "secret", TLSMode: TLSNone},
		{Host: "example.test", Port: 21, Username: "tester", TLSMode: TLSNone},
		{Host: "example.test", Port: 21, Username: "tester", Password: "secret", TLSMode: "invalid"},
		{Host: "example.test", Port: 21, Username: "tester", Password: "secret", TLSMode: TLSExplicit},
	}
	for _, config := range tests {
		if err := validateConfig(config); err == nil {
			t.Fatalf("validateConfig(%+v) returned nil error", config)
		}
	}
}

func TestRemotePath(t *testing.T) {
	backend := Backend{root: "/home/tester/data"}
	tests := []struct {
		name    string
		want    string
		wantErr bool
	}{
		{name: ".", want: "/home/tester/data"},
		{name: "folder/file.txt", want: "/home/tester/data/folder/file.txt"},
		{name: `folder\한글.txt`, want: "/home/tester/data/folder/한글.txt"},
		{name: "../secret", wantErr: true},
	}
	for _, test := range tests {
		got, err := backend.remotePath(test.name)
		if (err != nil) != test.wantErr {
			t.Fatalf("remotePath(%q) error = %v, wantErr %v", test.name, err, test.wantErr)
		}
		if got != test.want {
			t.Errorf("remotePath(%q) = %q, want %q", test.name, got, test.want)
		}
	}
}

func TestBackendListsAndReadsOverFTPAndFTPS(t *testing.T) {
	certificate := testCertificate(t)
	tests := []TLSMode{TLSNone, TLSExplicit, TLSImplicit}
	for _, mode := range tests {
		t.Run(string(mode), func(t *testing.T) {
			server := newTestServer(t, mode, certificate)
			defer server.Close()

			config := Config{
				Host:     "127.0.0.1",
				Port:     server.Port(),
				Username: "tester",
				Password: "secret",
				Root:     "/home/tester",
				TLSMode:  mode,
				Timeout:  5 * time.Second,
			}
			if mode != TLSNone {
				config.TLSConfig = &tls.Config{InsecureSkipVerify: true} // test certificate
			}

			backend, err := New(context.Background(), config)
			if err != nil {
				t.Fatalf("New(): %v", err)
			}

			entries, err := backend.ReadDir(context.Background(), ".")
			if err != nil {
				t.Fatalf("ReadDir(): %v", err)
			}
			if len(entries) != 2 || entries[0].Name != "자료" || !entries[0].IsDir() || entries[1].Name != "hello.txt" {
				t.Fatalf("ReadDir() = %+v", entries)
			}

			entry, err := backend.Stat(context.Background(), "hello.txt")
			if err != nil {
				t.Fatalf("Stat(): %v", err)
			}
			if entry.Size != 16 || entry.IsDir() {
				t.Fatalf("Stat() = %+v", entry)
			}

			reader, err := backend.OpenRead(context.Background(), "hello.txt")
			if err != nil {
				t.Fatalf("OpenRead(): %v", err)
			}
			content, readErr := io.ReadAll(reader)
			closeErr := reader.Close()
			if readErr != nil || closeErr != nil {
				t.Fatalf("read errors = %v, %v", readErr, closeErr)
			}
			if string(content) != "DKDrive FTP test" {
				t.Fatalf("content = %q", content)
			}

			if err := backend.Mkdir(context.Background(), "새 폴더"); err != nil {
				t.Fatalf("Mkdir(): %v", err)
			}
			handle, err := backend.OpenWrite(context.Background(), "새 폴더/작성.txt", vfs.WriteOptions{Create: true, Truncate: true})
			if err != nil {
				t.Fatalf("OpenWrite(): %v", err)
			}
			if _, err := handle.WriteAt([]byte("FTP upload test"), 0); err != nil {
				t.Fatalf("WriteAt(): %v", err)
			}
			if err := handle.Close(); err != nil {
				t.Fatalf("write Close(): %v", err)
			}
			if got := server.Uploaded(); got != "FTP upload test" {
				t.Fatalf("uploaded = %q", got)
			}
			if err := backend.Rename(context.Background(), "새 폴더/작성.txt", "이동.txt"); err != nil {
				t.Fatalf("Rename(): %v", err)
			}
			if err := backend.SetModTime(context.Background(), "이동.txt", time.Now()); err != nil {
				t.Fatalf("SetModTime(): %v", err)
			}
			if err := backend.Remove(context.Background(), "이동.txt", false); err != nil {
				t.Fatalf("Remove(file): %v", err)
			}
			if err := backend.Remove(context.Background(), "새 폴더", true); err != nil {
				t.Fatalf("Remove(directory): %v", err)
			}
			if err := backend.Close(); err != nil {
				t.Fatalf("Close(): %v", err)
			}
		})
	}
}

type testServer struct {
	t           *testing.T
	listener    net.Listener
	mode        TLSMode
	certificate tls.Certificate
	done        chan struct{}
	mutex       sync.Mutex
	uploaded    string
}

func newTestServer(t *testing.T, mode TLSMode, certificate tls.Certificate) *testServer {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := &testServer{t: t, listener: listener, mode: mode, certificate: certificate, done: make(chan struct{})}
	go server.serve()
	return server
}

func (server *testServer) Port() uint16 {
	return uint16(server.listener.Addr().(*net.TCPAddr).Port)
}

func (server *testServer) Close() {
	server.listener.Close()
	select {
	case <-server.done:
	case <-time.After(5 * time.Second):
		server.t.Fatal("FTP test server did not stop")
	}
}

func (server *testServer) Uploaded() string {
	server.mutex.Lock()
	defer server.mutex.Unlock()
	return server.uploaded
}

func (server *testServer) serve() {
	defer close(server.done)
	connection, err := server.listener.Accept()
	if err != nil {
		return
	}
	defer connection.Close()
	if server.mode == TLSImplicit {
		connection = tls.Server(connection, &tls.Config{Certificates: []tls.Certificate{server.certificate}})
	}
	server.handle(connection)
}

func (server *testServer) handle(connection net.Conn) {
	reader := bufio.NewReader(connection)
	writer := bufio.NewWriter(connection)
	writeReply(writer, "220 DKDrive test FTP ready")
	var dataListener net.Listener

	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return
		}
		line = strings.TrimSpace(line)
		command, argument, _ := strings.Cut(line, " ")
		switch strings.ToUpper(command) {
		case "AUTH":
			writeReply(writer, "234 AUTH TLS successful")
			connection = tls.Server(connection, &tls.Config{Certificates: []tls.Certificate{server.certificate}})
			reader = bufio.NewReader(connection)
			writer = bufio.NewWriter(connection)
		case "USER":
			writeReply(writer, "331 Password required")
		case "PASS":
			writeReply(writer, "230 Logged in")
		case "FEAT":
			writeReply(writer, "211-Features\r\n MLST type*;size*;modify*;\r\n MLSD\r\n UTF8\r\n MDTM\r\n MFMT\r\n211 End")
		case "TYPE", "OPTS", "PBSZ", "PROT":
			writeReply(writer, "200 Command okay")
		case "CWD":
			writeReply(writer, "250 Directory changed")
		case "PWD":
			writeReply(writer, `257 "/home/tester" is current directory`)
		case "EPSV":
			dataListener = server.newDataListener()
			port := dataListener.Addr().(*net.TCPAddr).Port
			writeReply(writer, fmt.Sprintf("229 Entering Extended Passive Mode (|||%d|)", port))
		case "MLSD":
			writeReply(writer, "150 Opening data connection")
			server.writeData(dataListener, "type=dir;modify=20260827010102; 자료\r\ntype=file;size=16;modify=20260827030405; hello.txt\r\n")
			writeReply(writer, "226 Transfer complete")
			dataListener = nil
		case "MLST":
			name := pathBase(argument)
			writeReply(writer, fmt.Sprintf("250-File details\r\n type=file;size=16;modify=20260827030405; %s\r\n250 End", name))
		case "RETR":
			writeReply(writer, "150 Opening data connection")
			server.writeData(dataListener, "DKDrive FTP test")
			writeReply(writer, "226 Transfer complete")
			dataListener = nil
		case "STOR":
			writeReply(writer, "150 Opening data connection")
			server.readData(dataListener)
			writeReply(writer, "226 Transfer complete")
			dataListener = nil
		case "MKD":
			writeReply(writer, `257 "directory" created`)
		case "RNFR":
			writeReply(writer, "350 File action pending")
		case "RNTO", "DELE", "RMD":
			writeReply(writer, "250 File action okay")
		case "MFMT":
			writeReply(writer, "213 Modify time set")
		case "QUIT":
			writeReply(writer, "221 Goodbye")
			return
		default:
			writeReply(writer, "502 Command not implemented")
		}
	}
}

func (server *testServer) newDataListener() net.Listener {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		server.t.Error(err)
	}
	return listener
}

func (server *testServer) writeData(listener net.Listener, content string) {
	if listener == nil {
		server.t.Error("missing data listener")
		return
	}
	defer listener.Close()
	connection, err := listener.Accept()
	if err != nil {
		server.t.Error(err)
		return
	}
	defer connection.Close()
	if server.mode != TLSNone {
		connection = tls.Server(connection, &tls.Config{Certificates: []tls.Certificate{server.certificate}})
	}
	if _, err := io.WriteString(connection, content); err != nil {
		server.t.Error(err)
	}
}

func (server *testServer) readData(listener net.Listener) {
	if listener == nil {
		server.t.Error("missing data listener")
		return
	}
	defer listener.Close()
	connection, err := listener.Accept()
	if err != nil {
		server.t.Error(err)
		return
	}
	defer connection.Close()
	if server.mode != TLSNone {
		connection = tls.Server(connection, &tls.Config{Certificates: []tls.Certificate{server.certificate}})
	}
	content, err := io.ReadAll(connection)
	if err != nil {
		server.t.Error(err)
		return
	}
	server.mutex.Lock()
	server.uploaded = string(content)
	server.mutex.Unlock()
}

func writeReply(writer *bufio.Writer, reply string) {
	fmt.Fprintf(writer, "%s\r\n", reply)
	writer.Flush()
}

func pathBase(value string) string {
	parts := strings.Split(strings.TrimRight(value, "/"), "/")
	return parts[len(parts)-1]
}

func testCertificate(t *testing.T) tls.Certificate {
	t.Helper()
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "localhost"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
	}
	der, err := x509.CreateCertificate(rand.Reader, &template, &template, &privateKey.PublicKey, privateKey)
	if err != nil {
		t.Fatal(err)
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: privateKey}
}
