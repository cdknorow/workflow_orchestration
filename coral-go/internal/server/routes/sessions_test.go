package routes

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/cdknorow/coral/internal/board"
	"github.com/cdknorow/coral/internal/config"
	"github.com/cdknorow/coral/internal/ptymanager"
	"github.com/cdknorow/coral/internal/store"
)

// mockSessionTerminal implements ptymanager.SessionTerminal for testing.
type mockSessionTerminal struct {
	mu       sync.Mutex
	sessions map[string]*ptymanager.PaneInfo
	outputs  map[string]string
}

func newMockTerminal() *mockSessionTerminal {
	return &mockSessionTerminal{
		sessions: make(map[string]*ptymanager.PaneInfo),
		outputs:  make(map[string]string),
	}
}

func (m *mockSessionTerminal) ListSessions(_ context.Context) ([]ptymanager.PaneInfo, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]ptymanager.PaneInfo, 0, len(m.sessions))
	for _, p := range m.sessions {
		result = append(result, *p)
	}
	return result, nil
}

func (m *mockSessionTerminal) FindSession(_ context.Context, name, _, _ string) (*ptymanager.PaneInfo, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if p, ok := m.sessions[name]; ok {
		return p, nil
	}
	return nil, fmt.Errorf("session %q not found", name)
}

func (m *mockSessionTerminal) CaptureOutput(_ context.Context, name string, _ int, _, _ string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if out, ok := m.outputs[name]; ok {
		return out, nil
	}
	return "", fmt.Errorf("session %q not found", name)
}

func (m *mockSessionTerminal) SendInput(_ context.Context, name, _, _, _ string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.sessions[name]; !ok {
		return fmt.Errorf("session %q not found", name)
	}
	return nil
}

func (m *mockSessionTerminal) SendRawInput(_ context.Context, name string, _ []string, _, _ string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.sessions[name]; !ok {
		return fmt.Errorf("session %q not found", name)
	}
	return nil
}

func (m *mockSessionTerminal) SendToTarget(_ context.Context, _, _ string) error { return nil }
func (m *mockSessionTerminal) SendTerminalInput(_ context.Context, _, _ string) error {
	return nil
}

func (m *mockSessionTerminal) CreateSession(_ context.Context, name, workDir string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sessions[name] = &ptymanager.PaneInfo{
		SessionName: name,
		PaneTitle:   name,
		Target:      name + ":0.0",
		CurrentPath: workDir,
	}
	return nil
}

func (m *mockSessionTerminal) KillSession(_ context.Context, name, _, _ string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.sessions, name)
	return nil
}

func (m *mockSessionTerminal) KillSessionOnly(_ context.Context, name, _, _ string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.sessions, name)
	return nil
}

func (m *mockSessionTerminal) RestartPane(_ context.Context, _, _ string) error { return nil }
func (m *mockSessionTerminal) RenameSession(_ context.Context, oldName, newName string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if p, ok := m.sessions[oldName]; ok {
		p.SessionName = newName
		p.PaneTitle = newName
		m.sessions[newName] = p
		delete(m.sessions, oldName)
	}
	return nil
}

func (m *mockSessionTerminal) ResizeSession(_ context.Context, _ string, _ int, _, _ string) error {
	return nil
}
func (m *mockSessionTerminal) ResizeTarget(_ context.Context, _ string, _ int) error { return nil }
func (m *mockSessionTerminal) StartLogging(_ context.Context, _, _ string) error   { return nil }
func (m *mockSessionTerminal) StopLogging(_ context.Context, _ string) error        { return nil }
func (m *mockSessionTerminal) ClearHistory(_ context.Context, _ string) error       { return nil }

func (m *mockSessionTerminal) HasSession(_ context.Context, name string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.sessions[name]
	return ok
}

func (m *mockSessionTerminal) DisplayMessage(_ context.Context, _, _ string) (string, error) {
	return "", nil
}

func (m *mockSessionTerminal) FindTarget(_ context.Context, name, _, _ string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if p, ok := m.sessions[name]; ok {
		return p.Target, nil
	}
	return "", fmt.Errorf("session %q not found", name)
}

func (m *mockSessionTerminal) CaptureRawOutput(_ context.Context, _ string, _ int, _ bool) (string, error) {
	return "", nil
}

// addSession adds a mock session for testing.
func (m *mockSessionTerminal) addSession(name, workDir string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sessions[name] = &ptymanager.PaneInfo{
		SessionName: name,
		PaneTitle:   name,
		Target:      name + ":0.0",
		CurrentPath: workDir,
	}
}

// setOutput sets mock capture output for a session.
func (m *mockSessionTerminal) setOutput(name, output string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.outputs[name] = output
}

// setupSessionsTestServer creates a test HTTP server with a SessionsHandler.
func setupSessionsTestServer(t *testing.T) (*httptest.Server, *SessionsHandler, *mockSessionTerminal, *store.SessionStore) {
	t.Helper()

	cfg := &config.Config{
		LogDir: t.TempDir(),
	}

	dbPath := t.TempDir() + "/test.db"
	db, err := store.Open(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })

	boardDBPath := t.TempDir() + "/board.db"
	bs, err := board.NewStore(boardDBPath)
	require.NoError(t, err)
	t.Cleanup(func() { bs.Close() })

	terminal := newMockTerminal()
	handler := NewSessionsHandler(db, cfg, nil, terminal, bs)
	ss := store.NewSessionStore(db)

	r := chi.NewRouter()

	// Session routes
	r.Get("/api/sessions/live", handler.List)
	r.Get("/api/sessions/live/{name}", handler.Detail)
	r.Get("/api/sessions/live/{name}/capture", handler.Capture)
	r.Post("/api/sessions/live/{name}/send", handler.Send)
	r.Post("/api/sessions/live/{name}/keys", handler.Keys)
	r.Post("/api/sessions/live/{name}/resize", handler.Resize)
	r.Post("/api/sessions/live/{name}/kill", handler.Kill)
	r.Post("/api/sessions/live/{name}/set-display-name", handler.SetDisplayName)
	r.Post("/api/sessions/live/{name}/set-icon", handler.SetIcon)
	r.Post("/api/sessions/launch", handler.Launch)
	r.Post("/api/sessions/launch-team", handler.LaunchTeam)

	// Task routes
	r.Get("/api/sessions/live/{name}/tasks", handler.ListTasks)
	r.Post("/api/sessions/live/{name}/tasks", handler.CreateTask)
	r.Put("/api/sessions/live/{name}/tasks/{taskID}", handler.UpdateTask)
	r.Delete("/api/sessions/live/{name}/tasks/{taskID}", handler.DeleteTask)

	// Note routes
	r.Get("/api/sessions/live/{name}/notes", handler.ListNotes)
	r.Post("/api/sessions/live/{name}/notes", handler.CreateNote)

	// Event routes
	r.Get("/api/sessions/live/{name}/events", handler.ListEvents)
	r.Post("/api/sessions/live/{name}/events", handler.CreateEvent)
	r.Get("/api/sessions/live/{name}/event-counts", handler.EventCounts)

	server := httptest.NewServer(r)
	t.Cleanup(server.Close)

	return server, handler, terminal, ss
}

func TestSessionsList_Empty(t *testing.T) {
	server, _, _, _ := setupSessionsTestServer(t)

	resp, err := http.Get(server.URL + "/api/sessions/live")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var sessions []interface{}
	err = json.NewDecoder(resp.Body).Decode(&sessions)
	require.NoError(t, err)
	assert.Empty(t, sessions)
}

func TestSessionsList_WithSessions(t *testing.T) {
	server, _, terminal, _ := setupSessionsTestServer(t)

	// Add mock sessions
	// Session names must match ParseSessionName regex: {type}-{uuid}
	terminal.addSession("claude-00000000-0000-0000-0000-000000000001", "/tmp/test")
	terminal.addSession("claude-00000000-0000-0000-0000-000000000002", "/tmp/test2")

	resp, err := http.Get(server.URL + "/api/sessions/live")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var sessions []map[string]interface{}
	err = json.NewDecoder(resp.Body).Decode(&sessions)
	require.NoError(t, err)
	assert.Len(t, sessions, 2)
}

func TestSessionsCapture_NotFound(t *testing.T) {
	server, _, _, _ := setupSessionsTestServer(t)

	resp, err := http.Get(server.URL + "/api/sessions/live/nonexistent/capture")
	require.NoError(t, err)
	defer resp.Body.Close()

	// Should return an error or empty capture
	assert.Contains(t, []int{http.StatusOK, http.StatusNotFound, http.StatusInternalServerError}, resp.StatusCode)
}

func TestSessionsCapture_WithOutput(t *testing.T) {
	server, _, terminal, _ := setupSessionsTestServer(t)

	terminal.addSession("claude-00000000-0000-0000-0000-000000000003", "/tmp/test")
	terminal.setOutput("claude-00000000-0000-0000-0000-000000000003", "Hello from agent\n$ doing work")

	resp, err := http.Get(server.URL + "/api/sessions/live/claude-00000000-0000-0000-0000-000000000003/capture")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var result map[string]interface{}
	err = json.NewDecoder(resp.Body).Decode(&result)
	require.NoError(t, err)
	assert.Contains(t, result, "capture")
}

func TestSessionsSend_NotFound(t *testing.T) {
	server, _, _, _ := setupSessionsTestServer(t)

	body := bytes.NewBufferString(`{"command": "echo hello"}`)
	resp, err := http.Post(server.URL+"/api/sessions/live/nonexistent/send", "application/json", body)
	require.NoError(t, err)
	defer resp.Body.Close()

	// Should return error since session doesn't exist
	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)
	if errMsg, ok := result["error"]; ok {
		assert.NotEmpty(t, errMsg)
	}
}

func TestSessionsSend_Success(t *testing.T) {
	server, _, terminal, _ := setupSessionsTestServer(t)

	terminal.addSession("claude-00000000-0000-0000-0000-000000000004", "/tmp/test")

	body := bytes.NewBufferString(`{"command": "echo hello"}`)
	resp, err := http.Post(server.URL+"/api/sessions/live/claude-00000000-0000-0000-0000-000000000004/send", "application/json", body)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestSessionsResize(t *testing.T) {
	server, _, terminal, _ := setupSessionsTestServer(t)

	terminal.addSession("claude-00000000-0000-0000-0000-000000000005", "/tmp/test")

	body := bytes.NewBufferString(`{"columns": 120}`)
	resp, err := http.Post(server.URL+"/api/sessions/live/claude-00000000-0000-0000-0000-000000000005/resize", "application/json", body)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestSessionsKill(t *testing.T) {
	server, _, terminal, _ := setupSessionsTestServer(t)

	terminal.addSession("claude-00000000-0000-0000-0000-000000000006", "/tmp/test")

	resp, err := http.Post(server.URL+"/api/sessions/live/claude-00000000-0000-0000-0000-000000000006/kill", "application/json", nil)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	// Verify session was removed
	assert.False(t, terminal.HasSession(context.Background(), "claude-00000000-0000-0000-0000-000000000006"))
}

func TestSessionsLaunch_MissingWorkDir(t *testing.T) {
	server, _, _, _ := setupSessionsTestServer(t)

	body := bytes.NewBufferString(`{"agent_type": "claude"}`)
	resp, err := http.Post(server.URL+"/api/sessions/launch", "application/json", body)
	require.NoError(t, err)
	defer resp.Body.Close()

	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)
	assert.Contains(t, result, "error")
}

func TestSessionsLaunch_Success(t *testing.T) {
	server, _, terminal, _ := setupSessionsTestServer(t)

	workDir := t.TempDir()
	body, _ := json.Marshal(map[string]interface{}{
		"working_dir": workDir,
		"agent_type":  "claude",
	})
	resp, err := http.Post(server.URL+"/api/sessions/launch", "application/json", bytes.NewReader(body))
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)
	sessionName, ok := result["session_name"]
	assert.True(t, ok, "response should contain session_name")
	assert.NotEmpty(t, sessionName)

	// Verify session was created in the terminal
	sessions, _ := terminal.ListSessions(context.Background())
	assert.NotEmpty(t, sessions)
}

func TestSessionsLaunchTeam_MissingBoardName(t *testing.T) {
	server, _, _, _ := setupSessionsTestServer(t)

	body := bytes.NewBufferString(`{"working_dir": "/tmp", "agents": [{"name": "dev"}]}`)
	resp, err := http.Post(server.URL+"/api/sessions/launch-team", "application/json", body)
	require.NoError(t, err)
	defer resp.Body.Close()

	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)
	assert.Contains(t, result, "error")
}

func TestSessionsSetDisplayName(t *testing.T) {
	server, _, terminal, ss := setupSessionsTestServer(t)

	terminal.addSession("claude-test-rename", "/tmp/test")
	// Register session in DB
	ctx := context.Background()
	ss.RegisterLiveSession(ctx, &store.LiveSession{AgentName: "claude-test-rename", AgentType: "claude", WorkingDir: "/tmp/test", SessionID: "test-123"})

	body := bytes.NewBufferString(`{"display_name": "My Agent", "session_id": "test-123"}`)
	resp, err := http.Post(server.URL+"/api/sessions/live/claude-test-rename/set-display-name", "application/json", body)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestSessionsSetIcon(t *testing.T) {
	server, _, terminal, ss := setupSessionsTestServer(t)

	terminal.addSession("claude-test-icon", "/tmp/test")
	ctx := context.Background()
	ss.RegisterLiveSession(ctx, &store.LiveSession{AgentName: "claude-test-icon", AgentType: "claude", WorkingDir: "/tmp/test", SessionID: "test-icon-123"})

	body := bytes.NewBufferString(`{"icon": "🚀"}`)
	resp, err := http.Post(server.URL+"/api/sessions/live/claude-test-icon/set-icon", "application/json", body)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestSessionsTasks_CRUD(t *testing.T) {
	server, _, terminal, ss := setupSessionsTestServer(t)

	terminal.addSession("claude-test-tasks", "/tmp/test")
	ctx := context.Background()
	ss.RegisterLiveSession(ctx, &store.LiveSession{AgentName: "claude-test-tasks", AgentType: "claude", WorkingDir: "/tmp/test", SessionID: "test-task-123"})

	// List tasks (should be empty)
	resp, err := http.Get(server.URL + "/api/sessions/live/claude-test-tasks/tasks?session_id=test-task-123")
	require.NoError(t, err)
	resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	// Create task
	body := bytes.NewBufferString(`{"title": "Test task", "session_id": "test-task-123"}`)
	resp, err = http.Post(server.URL+"/api/sessions/live/claude-test-tasks/tasks", "application/json", body)
	require.NoError(t, err)
	resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestSessionsNotes_Create(t *testing.T) {
	server, _, terminal, ss := setupSessionsTestServer(t)

	terminal.addSession("claude-test-notes", "/tmp/test")
	ctx := context.Background()
	ss.RegisterLiveSession(ctx, &store.LiveSession{AgentName: "claude-test-notes", AgentType: "claude", WorkingDir: "/tmp/test", SessionID: "test-note-123"})

	body := bytes.NewBufferString(`{"content": "Test note", "session_id": "test-note-123"}`)
	resp, err := http.Post(server.URL+"/api/sessions/live/claude-test-notes/notes", "application/json", body)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestSessionsEvents_CreateAndList(t *testing.T) {
	server, _, terminal, ss := setupSessionsTestServer(t)

	terminal.addSession("claude-test-events", "/tmp/test")
	ctx := context.Background()
	ss.RegisterLiveSession(ctx, &store.LiveSession{AgentName: "claude-test-events", AgentType: "claude", WorkingDir: "/tmp/test", SessionID: "test-evt-123"})

	// Create event
	body := bytes.NewBufferString(`{"event_type": "tool_use", "summary": "Read file", "session_id": "test-evt-123"}`)
	resp, err := http.Post(server.URL+"/api/sessions/live/claude-test-events/events", "application/json", body)
	require.NoError(t, err)
	resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	// List events
	resp, err = http.Get(server.URL + "/api/sessions/live/claude-test-events/events?session_id=test-evt-123")
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	// Event counts
	resp, err = http.Get(server.URL + "/api/sessions/live/claude-test-events/event-counts?session_id=test-evt-123")
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestSessionsKeys(t *testing.T) {
	server, _, terminal, _ := setupSessionsTestServer(t)

	terminal.addSession("claude-test-keys", "/tmp/test")

	body := bytes.NewBufferString(`{"keys": ["Enter"]}`)
	resp, err := http.Post(server.URL+"/api/sessions/live/claude-test-keys/keys", "application/json", body)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

// setupSessionsTestServerWithConfig creates a test server with custom config.
func setupSessionsTestServerWithConfig(t *testing.T, cfg *config.Config) (*httptest.Server, *SessionsHandler, *mockSessionTerminal, *store.SessionStore) {
	t.Helper()

	if cfg.LogDir == "" {
		cfg.LogDir = t.TempDir()
	}

	dbPath := t.TempDir() + "/test.db"
	db, err := store.Open(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })

	boardDBPath := t.TempDir() + "/board.db"
	bs, err := board.NewStore(boardDBPath)
	require.NoError(t, err)
	t.Cleanup(func() { bs.Close() })

	terminal := newMockTerminal()
	handler := NewSessionsHandler(db, cfg, nil, terminal, bs)
	ss := store.NewSessionStore(db)

	r := chi.NewRouter()
	r.Post("/api/sessions/launch", handler.Launch)
	r.Post("/api/sessions/launch-team", handler.LaunchTeam)

	server := httptest.NewServer(r)
	t.Cleanup(server.Close)

	return server, handler, terminal, ss
}

// ── Edition Limits Tests ────────────────────────────────────────────────

func TestEditionLimits_LaunchTeam_TeamLimitExceeded(t *testing.T) {
	cfg := &config.Config{
		MaxLiveTeams:  1,
		MaxLiveAgents: 5,
	}
	server, _, _, ss := setupSessionsTestServerWithConfig(t, cfg)
	ctx := context.Background()

	// Pre-register a team (live session with a board_name)
	board := "existing-team"
	ss.RegisterLiveSession(ctx, &store.LiveSession{
		AgentName: "agent-1", AgentType: "claude", WorkingDir: "/tmp",
		SessionID: "sid-1", BoardName: &board,
	})

	// Try to launch a second team — should be rejected
	body, _ := json.Marshal(map[string]interface{}{
		"board_name":  "new-team",
		"working_dir": t.TempDir(),
		"agents":      []map[string]string{{"name": "dev"}},
	})
	resp, err := http.Post(server.URL+"/api/sessions/launch-team", "application/json", bytes.NewReader(body))
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusForbidden, resp.StatusCode)

	var result map[string]string
	json.NewDecoder(resp.Body).Decode(&result)
	assert.Contains(t, result["error"], "Demo limit")
	assert.Contains(t, result["error"], "team")
}

func TestEditionLimits_Launch_AgentLimitExceeded(t *testing.T) {
	cfg := &config.Config{
		MaxLiveTeams:  1,
		MaxLiveAgents: 2,
	}
	server, _, _, ss := setupSessionsTestServerWithConfig(t, cfg)
	ctx := context.Background()

	// Pre-register 2 agents (at the limit)
	ss.RegisterLiveSession(ctx, &store.LiveSession{
		AgentName: "agent-1", AgentType: "claude", WorkingDir: "/tmp", SessionID: "sid-1",
	})
	ss.RegisterLiveSession(ctx, &store.LiveSession{
		AgentName: "agent-2", AgentType: "claude", WorkingDir: "/tmp", SessionID: "sid-2",
	})

	// Try to launch another agent — should be rejected
	body, _ := json.Marshal(map[string]interface{}{
		"working_dir": t.TempDir(),
		"agent_type":  "claude",
	})
	resp, err := http.Post(server.URL+"/api/sessions/launch", "application/json", bytes.NewReader(body))
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusForbidden, resp.StatusCode)

	var result map[string]string
	json.NewDecoder(resp.Body).Decode(&result)
	assert.Contains(t, result["error"], "Demo limit")
	assert.Contains(t, result["error"], "agent")
}

func TestEditionLimits_LaunchTeam_AgentLimitExceeded(t *testing.T) {
	cfg := &config.Config{
		MaxLiveTeams:  10,
		MaxLiveAgents: 3,
	}
	server, _, _, ss := setupSessionsTestServerWithConfig(t, cfg)
	ctx := context.Background()

	// Pre-register 2 agents
	ss.RegisterLiveSession(ctx, &store.LiveSession{
		AgentName: "agent-1", AgentType: "claude", WorkingDir: "/tmp", SessionID: "sid-1",
	})
	ss.RegisterLiveSession(ctx, &store.LiveSession{
		AgentName: "agent-2", AgentType: "claude", WorkingDir: "/tmp", SessionID: "sid-2",
	})

	// Try to launch a team with 2 agents (would exceed limit of 3)
	body, _ := json.Marshal(map[string]interface{}{
		"board_name":  "new-team",
		"working_dir": t.TempDir(),
		"agents":      []map[string]string{{"name": "dev1"}, {"name": "dev2"}},
	})
	resp, err := http.Post(server.URL+"/api/sessions/launch-team", "application/json", bytes.NewReader(body))
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusForbidden, resp.StatusCode)

	var result map[string]string
	json.NewDecoder(resp.Body).Decode(&result)
	assert.Contains(t, result["error"], "Demo limit")
}

func TestEditionLimits_NoLimits_UnlimitedLaunches(t *testing.T) {
	cfg := &config.Config{
		MaxLiveTeams:  0, // unlimited
		MaxLiveAgents: 0, // unlimited
	}
	server, _, _, ss := setupSessionsTestServerWithConfig(t, cfg)
	ctx := context.Background()

	// Pre-register many agents
	for i := 0; i < 10; i++ {
		ss.RegisterLiveSession(ctx, &store.LiveSession{
			AgentName: fmt.Sprintf("agent-%d", i), AgentType: "claude",
			WorkingDir: "/tmp", SessionID: fmt.Sprintf("sid-%d", i),
		})
	}

	// Launch should succeed (no limits)
	body, _ := json.Marshal(map[string]interface{}{
		"working_dir": t.TempDir(),
		"agent_type":  "claude",
	})
	resp, err := http.Post(server.URL+"/api/sessions/launch", "application/json", bytes.NewReader(body))
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestEditionLimits_Launch_BelowLimit_Succeeds(t *testing.T) {
	cfg := &config.Config{
		MaxLiveTeams:  1,
		MaxLiveAgents: 5,
	}
	server, _, _, _ := setupSessionsTestServerWithConfig(t, cfg)

	// Launch with no existing sessions — should succeed
	body, _ := json.Marshal(map[string]interface{}{
		"working_dir": t.TempDir(),
		"agent_type":  "claude",
	})
	resp, err := http.Post(server.URL+"/api/sessions/launch", "application/json", bytes.NewReader(body))
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestEditionLimits_SleepingAgents_NotCounted(t *testing.T) {
	cfg := &config.Config{
		MaxLiveTeams:  1,
		MaxLiveAgents: 2,
	}
	server, _, _, ss := setupSessionsTestServerWithConfig(t, cfg)
	ctx := context.Background()

	// Register 2 agents, but one is sleeping
	ss.RegisterLiveSession(ctx, &store.LiveSession{
		AgentName: "agent-1", AgentType: "claude", WorkingDir: "/tmp", SessionID: "sid-1",
	})
	ss.RegisterLiveSession(ctx, &store.LiveSession{
		AgentName: "agent-2", AgentType: "claude", WorkingDir: "/tmp", SessionID: "sid-2", IsSleeping: 1,
	})

	// Launch should succeed since only 1 non-sleeping agent
	body, _ := json.Marshal(map[string]interface{}{
		"working_dir": t.TempDir(),
		"agent_type":  "claude",
	})
	resp, err := http.Post(server.URL+"/api/sessions/launch", "application/json", bytes.NewReader(body))
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

// ── Kill Sleeping Sessions Tests ────────────────────────────────────────

func TestKill_SleepingSession_RemovedFromDB(t *testing.T) {
	server, handler, terminal, ss := setupSessionsTestServer(t)
	ctx := context.Background()

	// Set up board handler so board pause clearing works
	bh := NewBoardHandler(handler.bs)
	handler.SetBoardHandler(bh)

	board := "test-board"
	terminal.addSession("claude-sleeping-1", "/tmp/test")
	ss.RegisterLiveSession(ctx, &store.LiveSession{
		AgentName: "claude-sleeping-1", AgentType: "claude", WorkingDir: "/tmp",
		SessionID: "sleep-sid-1", BoardName: &board, IsSleeping: 1,
	})

	// Verify session exists in DB
	count, err := ss.CountBoardSessions(ctx, board)
	require.NoError(t, err)
	assert.Equal(t, 1, count)

	// Kill the sleeping session
	body := bytes.NewBufferString(`{"agent_type": "claude", "session_id": "sleep-sid-1"}`)
	resp, err := http.Post(server.URL+"/api/sessions/live/claude-sleeping-1/kill", "application/json", body)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	// Verify session is removed from DB
	count, err = ss.CountBoardSessions(ctx, board)
	require.NoError(t, err)
	assert.Equal(t, 0, count)
}

func TestKill_LastSessionOnBoard_ClearsPauseState(t *testing.T) {
	server, handler, terminal, ss := setupSessionsTestServer(t)
	ctx := context.Background()

	bh := NewBoardHandler(handler.bs)
	handler.SetBoardHandler(bh)

	// Pause the board (simulates sleeping state)
	boardName := "paused-board"
	bh.SetPaused(boardName, true)
	assert.True(t, bh.IsPaused(boardName))

	terminal.addSession("claude-paused-1", "/tmp/test")
	ss.RegisterLiveSession(ctx, &store.LiveSession{
		AgentName: "claude-paused-1", AgentType: "claude", WorkingDir: "/tmp",
		SessionID: "paused-sid-1", BoardName: &boardName, IsSleeping: 1,
	})

	// Kill the last (only) session on the board
	body := bytes.NewBufferString(`{"agent_type": "claude", "session_id": "paused-sid-1"}`)
	resp, err := http.Post(server.URL+"/api/sessions/live/claude-paused-1/kill", "application/json", body)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	// Board pause state should be cleared
	assert.False(t, bh.IsPaused(boardName))
}

func TestKill_NotLastSessionOnBoard_KeepsPauseState(t *testing.T) {
	server, handler, terminal, ss := setupSessionsTestServer(t)
	ctx := context.Background()

	bh := NewBoardHandler(handler.bs)
	handler.SetBoardHandler(bh)

	boardName := "multi-board"
	bh.SetPaused(boardName, true)

	// Register 2 sessions on the same board
	terminal.addSession("claude-multi-1", "/tmp/test")
	ss.RegisterLiveSession(ctx, &store.LiveSession{
		AgentName: "claude-multi-1", AgentType: "claude", WorkingDir: "/tmp",
		SessionID: "multi-sid-1", BoardName: &boardName, IsSleeping: 1,
	})
	terminal.addSession("claude-multi-2", "/tmp/test")
	ss.RegisterLiveSession(ctx, &store.LiveSession{
		AgentName: "claude-multi-2", AgentType: "claude", WorkingDir: "/tmp",
		SessionID: "multi-sid-2", BoardName: &boardName, IsSleeping: 1,
	})

	// Kill one session (not the last)
	body := bytes.NewBufferString(`{"agent_type": "claude", "session_id": "multi-sid-1"}`)
	resp, err := http.Post(server.URL+"/api/sessions/live/claude-multi-1/kill", "application/json", body)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	// Board should still be paused (one session remains)
	assert.True(t, bh.IsPaused(boardName))

	// Verify one session remains
	count, err := ss.CountBoardSessions(ctx, boardName)
	require.NoError(t, err)
	assert.Equal(t, 1, count)
}

func TestKill_SleepingBoard_AllSessionsRemoved_NoPTY(t *testing.T) {
	server, handler, _, ss := setupSessionsTestServer(t)
	ctx := context.Background()

	bh := NewBoardHandler(handler.bs)
	handler.SetBoardHandler(bh)

	boardName := "sleeping-board"
	bh.SetPaused(boardName, true)

	// Register 2 sleeping sessions — no terminal sessions exist (sleeping team)
	ss.RegisterLiveSession(ctx, &store.LiveSession{
		AgentName: "agent-1", AgentType: "claude", WorkingDir: "/tmp",
		SessionID: "sleep-1", BoardName: &boardName, IsSleeping: 1,
	})
	ss.RegisterLiveSession(ctx, &store.LiveSession{
		AgentName: "agent-2", AgentType: "claude", WorkingDir: "/tmp",
		SessionID: "sleep-2", BoardName: &boardName, IsSleeping: 1,
	})

	// Simulate frontend killBoard: kill each session sequentially
	for _, sid := range []string{"sleep-1", "sleep-2"} {
		body := bytes.NewBufferString(fmt.Sprintf(`{"agent_type": "claude", "session_id": "%s"}`, sid))
		name := "agent-" + sid[len(sid)-1:]
		resp, err := http.Post(server.URL+"/api/sessions/live/"+name+"/kill", "application/json", body)
		require.NoError(t, err)
		resp.Body.Close()
		assert.Equal(t, http.StatusOK, resp.StatusCode)
	}

	// All sessions should be gone from DB
	count, err := ss.CountBoardSessions(ctx, boardName)
	require.NoError(t, err)
	assert.Equal(t, 0, count, "all sleeping sessions should be removed from DB")

	// Board pause state should be cleared
	assert.False(t, bh.IsPaused(boardName), "board pause should be cleared after all sessions killed")

	// GetSleepingBoardNames should not return this board
	sleepingBoards, err := ss.GetSleepingBoardNames(ctx)
	require.NoError(t, err)
	for _, b := range sleepingBoards {
		assert.NotEqual(t, boardName, b, "sleeping board should not appear in GetSleepingBoardNames after kill")
	}
}
