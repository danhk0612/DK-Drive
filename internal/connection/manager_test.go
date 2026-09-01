package connection

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/danhk0612/DK-Drive/internal/config"
)

type fakeSession struct {
	err    error
	closed atomic.Int32
}

func (s *fakeSession) Close() error { s.closed.Add(1); return s.err }
func profile(letter string) config.Profile {
	return config.Profile{Name: "test", Protocol: config.ProtocolSFTP, DriveLetter: letter, Host: "example.test", Port: 22, Username: "test", AuthMethod: config.AuthPassword}
}

func TestManagerMultipleConnectionsAndCloseFailure(t *testing.T) {
	busy := errors.New("open handles")
	x, y := &fakeSession{err: busy}, &fakeSession{}
	m := New(func(_ context.Context, _ string, p config.Profile, _ config.Secrets) (Session, error) {
		if p.DriveLetter == "X" {
			return x, nil
		}
		return y, nil
	})
	if err := m.Connect(context.Background(), "one", profile("X"), config.Secrets{}); err != nil {
		t.Fatal(err)
	}
	if err := m.Connect(context.Background(), "two", profile("Y"), config.Secrets{}); err != nil {
		t.Fatal(err)
	}
	if err := m.Connect(context.Background(), "third", profile("x"), config.Secrets{}); err == nil {
		t.Fatal("duplicate drive accepted")
	}
	if err := m.Connect(context.Background(), "one", profile("Z"), config.Secrets{}); err == nil {
		t.Fatal("duplicate ID accepted")
	}
	if err := m.CloseAll(); !errors.Is(err, busy) {
		t.Fatal(err)
	}
	if m.State("one") != "연결됨" || m.State("two") != "연결 안 됨" {
		t.Fatal("wrong state after partial failure")
	}
	x.err = nil
	if err := m.Disconnect("one"); err != nil {
		t.Fatal(err)
	}
	if x.closed.Load() != 2 || y.closed.Load() != 1 {
		t.Fatal("incorrect close count")
	}
}

func TestManagerReservesDriveWhileConnecting(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	m := New(func(context.Context, string, config.Profile, config.Secrets) (Session, error) {
		close(entered)
		<-release
		return &fakeSession{}, nil
	})
	done := make(chan error, 1)
	go func() { done <- m.Connect(context.Background(), "first", profile("X"), config.Secrets{}) }()
	<-entered
	if m.State("first") != "연결 중" {
		t.Fatal("not connecting")
	}
	if err := m.Connect(context.Background(), "second", profile("x"), config.Secrets{}); err == nil {
		t.Fatal("reservation lost")
	}
	if err := m.Disconnect("first"); err == nil {
		t.Fatal("disconnect during connect accepted")
	}
	if _, err := m.ForceDisconnect("first"); err == nil {
		t.Fatal("force disconnect during connect accepted")
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if err := m.CloseAll(); err != nil {
		t.Fatal(err)
	}
}

func TestManagerConnectFailureAllowsRetry(t *testing.T) {
	calls := 0
	m := New(func(context.Context, string, config.Profile, config.Secrets) (Session, error) {
		calls++
		if calls == 1 {
			return nil, context.DeadlineExceeded
		}
		return &fakeSession{}, nil
	})
	if err := m.Connect(context.Background(), "id", profile("X"), config.Secrets{}); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	if m.State("id") != "연결 안 됨" {
		t.Fatal("failed reservation retained")
	}
	if err := m.Connect(context.Background(), "id", profile("X"), config.Secrets{}); err != nil {
		t.Fatal(err)
	}
}

func TestManagerPassesProfileIDToFactory(t *testing.T) {
	var got string
	m := New(func(_ context.Context, id string, _ config.Profile, _ config.Secrets) (Session, error) {
		got = id
		return &fakeSession{}, nil
	})
	if err := m.Connect(context.Background(), "profile-id", profile("X"), config.Secrets{}); err != nil {
		t.Fatal(err)
	}
	if got != "profile-id" {
		t.Fatalf("factory profile ID = %q", got)
	}
}

func TestManagerForceUnsupportedKeepsReservation(t *testing.T) {
	s := &fakeSession{}
	m := New(func(context.Context, string, config.Profile, config.Secrets) (Session, error) { return s, nil })
	if err := m.Connect(context.Background(), "id", profile("X"), config.Secrets{}); err != nil {
		t.Fatal(err)
	}
	if _, err := m.ForceDisconnect("id"); err == nil {
		t.Fatal("unsupported force succeeded")
	}
	if m.State("id") != "연결됨" || s.closed.Load() != 0 {
		t.Fatal("unexpected fallback/removed session")
	}
	if err := m.Connect(context.Background(), "other", profile("X"), config.Secrets{}); err == nil {
		t.Fatal("lost drive reservation")
	}
}
