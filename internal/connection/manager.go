package connection

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/danhk0612/DK-Drive/internal/config"
)

type Session interface{ Close() error }
type Factory func(context.Context, config.Profile, config.Secrets) (Session, error)

type entry struct {
	drive   string
	state   string
	session Session
}

type Manager struct {
	mu      sync.Mutex
	entries map[string]*entry
	factory Factory
}

func New(factory Factory) *Manager { return &Manager{entries: map[string]*entry{}, factory: factory} }

func (m *Manager) State(id string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if e := m.entries[id]; e != nil {
		return e.state
	}
	return "연결 안 됨"
}

func (m *Manager) Connect(ctx context.Context, id string, p config.Profile, secret config.Secrets) error {
	if id == "" {
		return errors.New("연결 ID가 필요합니다")
	}
	if err := p.Validate(); err != nil {
		return err
	}
	m.mu.Lock()
	if m.entries[id] != nil {
		m.mu.Unlock()
		return errors.New("이미 연결되었거나 작업 중입니다")
	}
	drive := strings.ToUpper(p.DriveLetter)
	for _, e := range m.entries {
		if e.drive == drive {
			m.mu.Unlock()
			return fmt.Errorf("드라이브 %s:는 이미 사용 중입니다", drive)
		}
	}
	e := &entry{drive: drive, state: "연결 중"}
	m.entries[id] = e
	m.mu.Unlock()
	s, err := m.factory(ctx, p, secret)
	m.mu.Lock()
	defer m.mu.Unlock()
	if err != nil {
		delete(m.entries, id)
		return err
	}
	e.session, e.state = s, "연결됨"
	return nil
}

func (m *Manager) Disconnect(id string) error {
	m.mu.Lock()
	e := m.entries[id]
	if e == nil {
		m.mu.Unlock()
		return nil
	}
	if e.state != "연결됨" {
		m.mu.Unlock()
		return errors.New("연결 작업이 끝난 뒤 다시 시도하세요")
	}
	e.state = "해제 중"
	m.mu.Unlock()
	err := e.session.Close()
	m.mu.Lock()
	defer m.mu.Unlock()
	if err != nil {
		e.state = "연결됨"
		return err
	}
	delete(m.entries, id)
	return nil
}

// CloseAll deliberately leaves sessions that refuse a safe close mounted.
func (m *Manager) CloseAll() error {
	m.mu.Lock()
	ids := make([]string, 0, len(m.entries))
	for id := range m.entries {
		ids = append(ids, id)
	}
	m.mu.Unlock()
	var result error
	for _, id := range ids {
		result = errors.Join(result, m.Disconnect(id))
	}
	return result
}
