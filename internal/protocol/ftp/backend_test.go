package ftp

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/textproto"
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
			defer backend.Close()

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
			if got := server.Count("PASS"); got != 1 {
				t.Fatalf("healthy connection login count = %d", got)
			}
		})
	}
}

func recoveryBackend(t *testing.T, server *testServer) *Backend {
	t.Helper()
	config := Config{
		Host: "127.0.0.1", Port: server.Port(), Username: "tester", Password: "secret",
		Root: "/home/tester", Timeout: 5 * time.Second, TLSMode: server.mode,
	}
	if server.mode != TLSNone {
		config.TLSConfig = &tls.Config{InsecureSkipVerify: true} // test certificate
	}
	backend, err := New(context.Background(), config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { backend.Close() })
	return backend
}

func TestBackendRecoversAfterUpload(t *testing.T) {
	certificate := testCertificate(t)
	for _, mode := range []TLSMode{TLSNone, TLSExplicit, TLSImplicit} {
		for _, fault := range []string{"after-STOR", "NOOP-421"} {
			t.Run(string(mode)+"/"+fault, func(t *testing.T) {
				server := newTestServer(t, mode, certificate, fault)
				t.Cleanup(server.Close)
				backend := recoveryBackend(t, server)
				ctx := context.Background()
				const name = "새 폴더/원본 파일.txt"
				const payload = "DKDrive FTP 쓰기 검증\n"
				handle, err := backend.OpenWrite(ctx, name, vfs.WriteOptions{Create: true, Truncate: true})
				if err != nil {
					t.Fatal(err)
				}
				if _, err := handle.WriteAt([]byte(payload), 0); err != nil {
					t.Fatal(err)
				}
				if err := handle.Close(); err != nil {
					t.Fatalf("upload: %v", err)
				}
				reader, err := backend.OpenRead(ctx, name)
				if err != nil {
					t.Fatalf("read after upload: %v", err)
				}
				content, readErr := io.ReadAll(reader)
				closeErr := reader.Close()
				if string(content) != payload || readErr != nil || closeErr != nil {
					t.Fatalf("read = %q, errors: %v, %v", content, readErr, closeErr)
				}
				if err := backend.Rename(ctx, name, "이동.txt"); err != nil {
					t.Fatal(err)
				}
				// A repeated Close must not consume a reply from the reused session.
				if err := reader.Close(); err != nil {
					t.Fatal(err)
				}
				if _, err := backend.Stat(ctx, "이동.txt"); err != nil {
					t.Fatal(err)
				}
				if err := backend.Remove(ctx, "이동.txt", false); err != nil {
					t.Fatal(err)
				}
				for command, want := range map[string]int{"PASS": 2, "CWD": 2, "PWD": 2, "STOR": 1, "RETR": 1, "RNTO": 1, "DELE": 1} {
					if got := server.Count(command); got != want {
						t.Errorf("%s count = %d, want %d", command, got, want)
					}
				}
				if mode == TLSExplicit && server.Count("AUTH") != 2 {
					t.Fatal("reconnect did not restore explicit TLS")
				}
			})
		}
	}
}

func TestBackendRetriesReadOnlyOperations(t *testing.T) {
	for _, command := range []string{"MLST", "MLSD", "RETR"} {
		t.Run(command, func(t *testing.T) {
			server := newTestServer(t, TLSNone, tls.Certificate{}, command+"-421")
			t.Cleanup(server.Close)
			backend := recoveryBackend(t, server)
			ctx := context.Background()
			var err error
			switch command {
			case "MLST":
				_, err = backend.Stat(ctx, "hello.txt")
			case "MLSD":
				_, err = backend.ReadDir(ctx, ".")
			case "RETR":
				var reader io.ReadCloser
				reader, err = backend.OpenRead(ctx, "hello.txt")
				if err == nil {
					_, copyErr := io.Copy(io.Discard, reader)
					err = errors.Join(copyErr, reader.Close())
				}
			}
			if err != nil || server.Count(command) != 2 || server.Count("PASS") != 2 {
				t.Fatalf("retry: err=%v, operations=%d, logins=%d", err, server.Count(command), server.Count("PASS"))
			}
		})
	}
}

func TestBackendDoesNotReplayMutations(t *testing.T) {
	for _, command := range []string{"STOR", "RNTO", "DELE", "MKD", "MFMT"} {
		t.Run(command, func(t *testing.T) {
			fault := command + "-close"
			if command == "STOR" {
				fault = "STOR-no-reply" // The bytes persisted, but completion was lost.
			}
			server := newTestServer(t, TLSNone, tls.Certificate{}, fault)
			t.Cleanup(server.Close)
			backend := recoveryBackend(t, server)
			ctx := context.Background()
			var err error
			switch command {
			case "STOR":
				handle, openErr := backend.OpenWrite(ctx, "upload.txt", vfs.WriteOptions{Create: true, Truncate: true})
				if openErr != nil {
					t.Fatal(openErr)
				}
				if _, err := handle.WriteAt([]byte("uploaded"), 0); err != nil {
					t.Fatal(err)
				}
				err = handle.Close()
				if server.Uploaded() != "uploaded" {
					t.Fatal("server did not receive the upload")
				}
			case "RNTO":
				err = backend.Rename(ctx, "hello.txt", "renamed.txt")
			case "DELE":
				err = backend.Remove(ctx, "hello.txt", false)
			case "MKD":
				err = backend.Mkdir(ctx, "folder")
			case "MFMT":
				err = backend.SetModTime(ctx, "hello.txt", time.Now())
			}
			if err == nil || server.Count(command) != 1 || server.Count("PASS") != 1 {
				t.Fatalf("mutation was hidden/replayed: err=%v, operations=%d, logins=%d", err, server.Count(command), server.Count("PASS"))
			}
			if _, err := backend.Stat(ctx, "hello.txt"); err != nil {
				t.Fatalf("next operation did not recover: %v", err)
			}
			if server.Count(command) != 1 || server.Count("PASS") != 2 {
				t.Fatal("unexpected mutation replay or login count")
			}
		})
	}
}

func TestBackendRecoveryGuards(t *testing.T) {
	t.Run("root unchanged", func(t *testing.T) {
		server := newTestServer(t, TLSNone, tls.Certificate{}, "MLST-close", "root-change")
		t.Cleanup(server.Close)
		backend := recoveryBackend(t, server)
		if _, err := backend.Stat(context.Background(), "hello.txt"); err == nil {
			t.Fatal("accepted changed root")
		}
		if backend.root != "/home/tester" || server.Count("MLST") != 1 {
			t.Fatal("operation continued under changed root")
		}
	})
	t.Run("permission denied", func(t *testing.T) {
		server := newTestServer(t, TLSNone, tls.Certificate{}, "RETR-550")
		t.Cleanup(server.Close)
		backend := recoveryBackend(t, server)
		if _, err := backend.OpenRead(context.Background(), "hello.txt"); err == nil {
			t.Fatal("expected permission error")
		}
		if server.Count("PASS") != 1 || server.Count("RETR") != 1 {
			t.Fatal("retried permission failure")
		}
	})
	t.Run("canceled and closed", func(t *testing.T) {
		server := newTestServer(t, TLSNone, tls.Certificate{})
		t.Cleanup(server.Close)
		backend := recoveryBackend(t, server)
		backend.needsProbe = true
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if _, err := backend.Stat(ctx, "hello.txt"); !errors.Is(err, context.Canceled) {
			t.Fatalf("canceled: %v", err)
		}
		backend.Close()
		if _, err := backend.ReadDir(context.Background(), "."); !errors.Is(err, net.ErrClosed) {
			t.Fatalf("closed: %v", err)
		}
		if server.Count("PASS") != 1 || server.Count("NOOP") != 0 {
			t.Fatal("network used after cancellation/close")
		}
	})
}

func TestIsConnectionError(t *testing.T) {
	for _, err := range []error{io.EOF, io.ErrUnexpectedEOF, net.ErrClosed, &net.OpError{Op: "write", Err: errors.New("reset")}, &textproto.Error{Code: 421}} {
		if !isConnectionError(fmt.Errorf("wrapped: %w", err)) {
			t.Errorf("not recognized: %v", err)
		}
	}
	for _, err := range []error{nil, context.Canceled, &textproto.Error{Code: 550}, &textproto.Error{Code: 530}, errors.ErrUnsupported} {
		if isConnectionError(err) {
			t.Errorf("incorrectly recognized: %v", err)
		}
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
	stored      map[string]string
	commands    map[string]int
	faults      map[string]bool
	active      net.Conn
	stopping    bool
}

func newTestServer(t *testing.T, mode TLSMode, certificate tls.Certificate, faults ...string) *testServer {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := &testServer{t: t, listener: listener, mode: mode, certificate: certificate, done: make(chan struct{})}
	server.stored = make(map[string]string)
	server.commands = make(map[string]int)
	server.faults = make(map[string]bool)
	for _, fault := range faults {
		server.faults[fault] = true
	}
	go server.serve()
	return server
}

func (server *testServer) Port() uint16 {
	return uint16(server.listener.Addr().(*net.TCPAddr).Port)
}

func (server *testServer) Close() {
	server.mutex.Lock()
	server.stopping = true
	if server.active != nil {
		server.active.Close()
	}
	server.mutex.Unlock()
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
	for {
		connection, err := server.listener.Accept()
		if err != nil {
			return
		}
		server.mutex.Lock()
		if server.stopping {
			server.mutex.Unlock()
			connection.Close()
			return
		}
		server.active = connection
		server.mutex.Unlock()
		raw := connection
		connection.SetDeadline(time.Now().Add(10 * time.Second))
		if server.mode == TLSImplicit {
			connection = tls.Server(connection, &tls.Config{Certificates: []tls.Certificate{server.certificate}})
		}
		server.handle(connection)
		raw.Close()
	}
}

func (server *testServer) Count(command string) int {
	server.mutex.Lock()
	defer server.mutex.Unlock()
	return server.commands[command]
}

// Only the control-server goroutine consumes faults.
func (server *testServer) fault(name string) bool {
	if !server.faults[name] {
		return false
	}
	delete(server.faults, name)
	return true
}

func (server *testServer) handle(connection net.Conn) {
	reader := bufio.NewReader(connection)
	writer := bufio.NewWriter(connection)
	writeReply(writer, "220 DKDrive test FTP ready")
	var dataListener net.Listener
	defer func() {
		if dataListener != nil {
			dataListener.Close()
		}
	}()

	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return
		}
		line = strings.TrimSpace(line)
		command, argument, _ := strings.Cut(line, " ")
		server.mutex.Lock()
		server.commands[command]++
		server.mutex.Unlock()
		if server.fault(command + "-close") {
			return
		}
		if server.fault(command + "-421") {
			writeReply(writer, "421 Control connection closing")
			return
		}
		if server.fault(command + "-550") {
			writeReply(writer, "550 Access denied")
			continue
		}
		switch strings.ToUpper(command) {
		case "AUTH":
			writeReply(writer, "234 AUTH TLS successful")
			connection = tls.Server(connection, &tls.Config{Certificates: []tls.Certificate{server.certificate}})
			reader = bufio.NewReader(connection)
			writer = bufio.NewWriter(connection)
		case "USER":
			writeReply(writer, "331 Password required")
		case "PASS":
			if argument != "secret" {
				writeReply(writer, "530 Authentication failed")
				continue
			}
			writeReply(writer, "230 Logged in")
		case "FEAT":
			writeReply(writer, "211-Features\r\n MLST type*;size*;modify*;\r\n MLSD\r\n UTF8\r\n MDTM\r\n MFMT\r\n211 End")
		case "TYPE", "OPTS", "PBSZ", "PROT", "NOOP":
			writeReply(writer, "200 Command okay")
		case "CWD":
			if argument != "/home/tester" {
				server.t.Errorf("unexpected root: %q", argument)
			}
			writeReply(writer, "250 Directory changed")
		case "PWD":
			if server.Count("PASS") > 1 && server.fault("root-change") {
				writeReply(writer, `257 "/different" is current directory`)
				continue
			}
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
			server.mutex.Lock()
			content, ok := server.stored[argument]
			server.mutex.Unlock()
			if !ok {
				content = "DKDrive FTP test"
			}
			writeReply(writer, "150 Opening data connection")
			server.writeData(dataListener, content)
			writeReply(writer, "226 Transfer complete")
			dataListener = nil
		case "STOR":
			writeReply(writer, "150 Opening data connection")
			server.readData(dataListener, argument)
			dataListener = nil
			if server.fault("STOR-no-reply") {
				return
			}
			writeReply(writer, "226 Transfer complete")
			if server.fault("after-STOR") {
				return
			}
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

func (server *testServer) readData(listener net.Listener, remote string) {
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
	server.stored[remote] = string(content)
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
