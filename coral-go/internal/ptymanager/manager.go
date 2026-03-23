package ptymanager

import (
	"fmt"
	"sync"
	"time"
)

// PTYBackend implements TerminalBackend using native PTY sessions.
type PTYBackend struct {
	mu       sync.RWMutex
	sessions map[string]*session
}

// NewPTYBackend creates a new PTY-based terminal backend.
func NewPTYBackend() *PTYBackend {
	return &PTYBackend{
		sessions: make(map[string]*session),
	}
}

func (m *PTYBackend) Spawn(name, agentType, workDir, sessionID, command string, cols, rows uint16) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.sessions[name]; exists {
		return fmt.Errorf("session %q already exists", name)
	}

	s, err := newSession(name, agentType, workDir, sessionID, command, cols, rows)
	if err != nil {
		return err
	}

	m.sessions[name] = s
	return nil
}

func (m *PTYBackend) Kill(name string) error {
	m.mu.Lock()
	s, ok := m.sessions[name]
	if !ok {
		m.mu.Unlock()
		return fmt.Errorf("session %q not found", name)
	}
	delete(m.sessions, name)
	m.mu.Unlock()

	return s.kill()
}

func (m *PTYBackend) Restart(name, command string) error {
	m.mu.RLock()
	old, ok := m.sessions[name]
	m.mu.RUnlock()

	if !ok {
		return fmt.Errorf("session %q not found", name)
	}

	agentType := old.agentType
	workDir := old.workingDir
	sessionID := old.sessionID

	// Kill old session
	m.mu.Lock()
	delete(m.sessions, name)
	m.mu.Unlock()
	old.kill()

	// Spawn new session with default terminal size
	return m.Spawn(name, agentType, workDir, sessionID, command, 200, 50)
}

func (m *PTYBackend) SendInput(name string, data []byte) error {
	m.mu.RLock()
	s, ok := m.sessions[name]
	m.mu.RUnlock()
	if !ok {
		return fmt.Errorf("session %q not found", name)
	}
	return s.sendInput(data)
}

// WaitReady blocks until the session's shell has produced output (prompt ready)
// or the timeout expires. Returns true if ready, false on timeout.
func (m *PTYBackend) WaitReady(name string, timeout time.Duration) bool {
	m.mu.RLock()
	s, ok := m.sessions[name]
	m.mu.RUnlock()
	if !ok {
		return false
	}

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		s.ringMu.Lock()
		n := len(s.ring)
		s.ringMu.Unlock()
		if n > 0 {
			return true
		}
		time.Sleep(50 * time.Millisecond)
	}
	return false
}

func (m *PTYBackend) Resize(name string, cols, rows uint16) error {
	m.mu.RLock()
	s, ok := m.sessions[name]
	m.mu.RUnlock()
	if !ok {
		return fmt.Errorf("session %q not found", name)
	}
	return s.resize(cols, rows)
}

func (m *PTYBackend) Subscribe(name, subscriberID string) (<-chan []byte, error) {
	m.mu.RLock()
	s, ok := m.sessions[name]
	m.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("session %q not found", name)
	}
	return s.subscribe(subscriberID), nil
}

func (m *PTYBackend) Unsubscribe(name, subscriberID string) {
	m.mu.RLock()
	s, ok := m.sessions[name]
	m.mu.RUnlock()
	if !ok {
		return
	}
	s.unsubscribe(subscriberID)
}

func (m *PTYBackend) CaptureContent(name string) (string, error) {
	m.mu.RLock()
	s, ok := m.sessions[name]
	m.mu.RUnlock()
	if !ok {
		return "", fmt.Errorf("session %q not found", name)
	}
	return s.captureContent(), nil
}

func (m *PTYBackend) ListSessions() []SessionInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()

	infos := make([]SessionInfo, 0, len(m.sessions))
	for _, s := range m.sessions {
		infos = append(infos, SessionInfo{
			AgentName:  s.name,
			AgentType:  s.agentType,
			SessionID:  s.sessionID,
			WorkingDir: s.workingDir,
			Running:    s.isRunning(),
		})
	}
	return infos
}

func (m *PTYBackend) IsRunning(name string) bool {
	m.mu.RLock()
	s, ok := m.sessions[name]
	m.mu.RUnlock()
	if !ok {
		return false
	}
	return s.isRunning()
}

func (m *PTYBackend) LogPath(name string) string {
	m.mu.RLock()
	s, ok := m.sessions[name]
	m.mu.RUnlock()
	if !ok {
		return ""
	}
	return s.logPath
}

func (m *PTYBackend) Close() error {
	m.mu.Lock()
	sessions := make([]*session, 0, len(m.sessions))
	for _, s := range m.sessions {
		sessions = append(sessions, s)
	}
	m.sessions = make(map[string]*session)
	m.mu.Unlock()

	for _, s := range sessions {
		s.kill()
	}
	return nil
}
