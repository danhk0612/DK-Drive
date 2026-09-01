package desktop

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/danhk0612/DK-Drive/internal/config"
	"github.com/danhk0612/DK-Drive/internal/connection"
)

type disconnectSession struct {
	normalErr, forceErr error
	normal, forced      int
}

func (s *disconnectSession) Close() error { s.normal++; return s.normalErr }
func (s *disconnectSession) ForceClose() (string, error) {
	s.forced++
	return "cache retained", s.forceErr
}

func disconnectFixture(t *testing.T, sessions ...*disconnectSession) (*connection.Manager, []config.SavedProfile) {
	t.Helper()
	m := connection.New(func(_ context.Context, _ string, p config.Profile, _ config.Secrets) (connection.Session, error) {
		return sessions[int(p.DriveLetter[0]-'X')], nil
	})
	var profiles []config.SavedProfile
	for i := range sessions {
		letter := string(rune('X' + i))
		p := config.SavedProfile{ID: letter, Profile: config.Profile{
			Name: letter, DriveLetter: letter, Protocol: config.ProtocolSFTP,
			Host: "example.test", Port: 22, Username: "test", AuthMethod: config.AuthPassword,
		}}
		if err := m.Connect(context.Background(), p.ID, p.Profile, config.Secrets{}); err != nil {
			t.Fatal(err)
		}
		profiles = append(profiles, p)
	}
	return m, profiles
}

func TestDisconnectDoesNotPromptWhenSafe(t *testing.T) {
	s := &disconnectSession{}
	m, profiles := disconnectFixture(t, s)
	result := disconnectProfiles(m, profiles, func(config.SavedProfile, error) bool {
		t.Fatal("prompted after successful close")
		return true
	})
	if result.err != nil || result.forceMessage != "" || len(result.canceled) != 0 || s.normal != 1 || s.forced != 0 || m.State("X") != "연결 안 됨" {
		t.Fatal(result, s)
	}
}

func TestDisconnectConfirmationAndPartialFailure(t *testing.T) {
	busy := errors.New("open handle")
	x, y, z := &disconnectSession{normalErr: busy}, &disconnectSession{normalErr: busy}, &disconnectSession{}
	m, profiles := disconnectFixture(t, x, y, z)
	var prompted []string
	result := disconnectProfiles(m, profiles, func(p config.SavedProfile, failure error) bool {
		if !errors.Is(failure, busy) || m.State(p.ID) != "연결됨" {
			t.Fatal("prompt before normal failure restored state", failure)
		}
		prompted = append(prompted, p.ID)
		return p.ID == "Y"
	})
	if len(prompted) != 2 || prompted[0] != "X" || prompted[1] != "Y" {
		t.Fatal(prompted)
	}
	if result.err != nil || !strings.Contains(result.forceMessage, "Y: cache retained") {
		t.Fatal(result)
	}
	if len(result.canceled) != 1 || result.canceled[0] != "X (X:)" {
		t.Fatal("wrong cancellation result", result.canceled)
	}
	if x.forced != 0 || y.forced != 1 || z.forced != 0 || x.normal != 1 || y.normal != 1 || z.normal != 1 {
		t.Fatal("wrong calls", x, y, z)
	}
	if m.State("X") != "연결됨" || m.State("Y") != "연결 안 됨" || m.State("Z") != "연결 안 됨" {
		t.Fatal("wrong states")
	}
}

func TestDisconnectForceFailureKeepsSession(t *testing.T) {
	failure := errors.New("force failed")
	s := &disconnectSession{normalErr: errors.New("busy"), forceErr: failure}
	m, profiles := disconnectFixture(t, s)
	result := disconnectProfiles(m, profiles, func(config.SavedProfile, error) bool { return true })
	if !errors.Is(result.err, failure) || m.State("X") != "연결됨" {
		t.Fatal(result.err)
	}
}

func TestForcePromptNamesDriveAndDataRisk(t *testing.T) {
	p := config.SavedProfile{Profile: config.Profile{Name: "test profile", DriveLetter: "Y"}}
	text := forceDisconnectPrompt(p, errors.New("open handles"))
	for _, want := range []string{"test profile", "Y:", "open handles", "손실", "캐시", "자동 재업로드·복구는 하지 않습니다", "아니요: 연결 유지"} {
		if !strings.Contains(text, want) {
			t.Fatal("missing warning", want)
		}
	}
}
