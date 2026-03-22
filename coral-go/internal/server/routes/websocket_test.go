package routes

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"nhooyr.io/websocket"
	"nhooyr.io/websocket/wsjson"

	"github.com/cdknorow/coral/internal/ptymanager"

	"github.com/cdknorow/coral/internal/config"
	"github.com/cdknorow/coral/internal/store"
)

func setupTestServer(t *testing.T) (*httptest.Server, *SessionsHandler) {
	t.Helper()

	cfg := &config.Config{
		WSPollIntervalS: 1,
		LogDir:          t.TempDir(),
	}

	dbPath := t.TempDir() + "/test.db"
	db, err := store.Open(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })

	ptyBackend := ptymanager.NewPTYBackend()
	terminal := ptymanager.NewPTYSessionTerminal(ptyBackend)
	handler := NewSessionsHandler(db, cfg, nil, terminal, nil)

	r := chi.NewRouter()
	r.Get("/ws/coral", handler.WSCoral)
	r.Get("/ws/terminal/{name}", handler.WSTerminal)

	server := httptest.NewServer(r)
	t.Cleanup(server.Close)

	return server, handler
}

func TestWSCoral_FirstMessageIsFullUpdate(t *testing.T) {
	server, _ := setupTestServer(t)

	// Convert http:// to ws://
	wsURL := "ws" + server.URL[4:] + "/ws/coral"

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	require.NoError(t, err)
	defer conn.CloseNow()

	// Read first message — should be coral_update
	var msg map[string]json.RawMessage
	err = wsjson.Read(ctx, conn, &msg)
	require.NoError(t, err)

	var msgType string
	json.Unmarshal(msg["type"], &msgType)
	assert.Equal(t, "coral_update", msgType)

	// Should have sessions array (even if empty)
	assert.Contains(t, msg, "sessions")
	assert.Contains(t, msg, "active_runs")

	conn.Close(websocket.StatusNormalClosure, "done")
}

func TestWSCoral_SubsequentDiffsOnlyOnChange(t *testing.T) {
	server, _ := setupTestServer(t)

	wsURL := "ws" + server.URL[4:] + "/ws/coral"
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()

	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	require.NoError(t, err)
	defer conn.CloseNow()

	// Read first message (full update)
	var msg1 map[string]json.RawMessage
	err = wsjson.Read(ctx, conn, &msg1)
	require.NoError(t, err)

	var msgType string
	json.Unmarshal(msg1["type"], &msgType)
	assert.Equal(t, "coral_update", msgType)

	// Wait for second poll — with no agents running, there should be no diff
	// (or a diff with empty changes). Use a short timeout.
	readCtx, readCancel := context.WithTimeout(ctx, 3*time.Second)
	defer readCancel()

	var msg2 map[string]json.RawMessage
	err = wsjson.Read(readCtx, conn, &msg2)
	if err != nil {
		// Timeout is expected — no diff sent when nothing changed
		t.Log("No diff sent (expected — no changes)")
	} else {
		json.Unmarshal(msg2["type"], &msgType)
		assert.Equal(t, "coral_diff", msgType)
	}

	conn.Close(websocket.StatusNormalClosure, "done")
}

// --- Sleeping sessions in WebSocket ---

func TestWSCoral_SleepingSessionIncluded(t *testing.T) {
	server, handler := setupTestServer(t)

	ctx := context.Background()

	// Register a sleeping session in the DB
	ss := store.NewSessionStore(handler.db)
	boardName := "test-board"
	err := ss.RegisterLiveSession(ctx, &store.LiveSession{
		SessionID:  "sleep-session-001",
		AgentType:  "claude",
		AgentName:  "sleepy-agent",
		WorkingDir: "/tmp/test",
		IsSleeping: 1,
		BoardName:  &boardName,
		CreatedAt:  "2026-01-01T00:00:00Z",
	})
	require.NoError(t, err)

	// Connect to WebSocket
	wsURL := "ws" + server.URL[4:] + "/ws/coral"
	wsCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, _, err := websocket.Dial(wsCtx, wsURL, nil)
	require.NoError(t, err)
	defer conn.CloseNow()

	// Read first message
	var msg map[string]json.RawMessage
	err = wsjson.Read(wsCtx, conn, &msg)
	require.NoError(t, err)

	var msgType string
	json.Unmarshal(msg["type"], &msgType)
	assert.Equal(t, "coral_update", msgType)

	// Parse sessions
	var sessions []map[string]any
	json.Unmarshal(msg["sessions"], &sessions)

	// Should include the sleeping session
	found := false
	for _, s := range sessions {
		if s["session_id"] == "sleep-session-001" {
			found = true
			assert.Equal(t, true, s["sleeping"])
			assert.Equal(t, "Sleeping", s["status"])
			assert.Equal(t, nil, s["tmux_session"])
			assert.Equal(t, "claude", s["agent_type"])
			assert.Equal(t, "sleepy-agent", s["name"])
			break
		}
	}
	assert.True(t, found, "sleeping session should be included in WS update")

	conn.Close(websocket.StatusNormalClosure, "done")
}

func TestWSCoral_SleepingSessionFields(t *testing.T) {
	server, handler := setupTestServer(t)

	ctx := context.Background()

	// Register sleeping session with display name and board
	ss := store.NewSessionStore(handler.db)
	boardName := "my-project"
	displayName := "My Agent"
	err := ss.RegisterLiveSession(ctx, &store.LiveSession{
		SessionID:   "sleep-fields-001",
		AgentType:   "gemini",
		AgentName:   "field-agent",
		WorkingDir:  "/tmp/fields",
		IsSleeping:  1,
		BoardName:   &boardName,
		DisplayName: &displayName,
		CreatedAt:   "2026-01-01T00:00:00Z",
	})
	require.NoError(t, err)

	wsURL := "ws" + server.URL[4:] + "/ws/coral"
	wsCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, _, err := websocket.Dial(wsCtx, wsURL, nil)
	require.NoError(t, err)
	defer conn.CloseNow()

	var msg map[string]json.RawMessage
	err = wsjson.Read(wsCtx, conn, &msg)
	require.NoError(t, err)

	var sessions []map[string]any
	json.Unmarshal(msg["sessions"], &sessions)

	for _, s := range sessions {
		if s["session_id"] == "sleep-fields-001" {
			assert.Equal(t, true, s["sleeping"])
			assert.Equal(t, "Sleeping", s["status"])
			assert.Equal(t, "My Agent", s["display_name"])
			assert.Equal(t, "my-project", s["board_project"])
			assert.Equal(t, false, s["waiting_for_input"])
			assert.Equal(t, false, s["done"])
			assert.Equal(t, false, s["working"])
			break
		}
	}

	conn.Close(websocket.StatusNormalClosure, "done")
}

func TestWSCoral_NonSleepingSessionExcludedFromSleepingList(t *testing.T) {
	server, handler := setupTestServer(t)

	ctx := context.Background()

	// Register a NON-sleeping session (IsSleeping=0) — should NOT appear
	// since there's no live tmux/PTY agent either
	ss := store.NewSessionStore(handler.db)
	err := ss.RegisterLiveSession(ctx, &store.LiveSession{
		SessionID:  "awake-session-001",
		AgentType:  "claude",
		AgentName:  "awake-agent",
		WorkingDir: "/tmp/awake",
		IsSleeping: 0,
		CreatedAt:  "2026-01-01T00:00:00Z",
	})
	require.NoError(t, err)

	wsURL := "ws" + server.URL[4:] + "/ws/coral"
	wsCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, _, err := websocket.Dial(wsCtx, wsURL, nil)
	require.NoError(t, err)
	defer conn.CloseNow()

	var msg map[string]json.RawMessage
	err = wsjson.Read(wsCtx, conn, &msg)
	require.NoError(t, err)

	var sessions []map[string]any
	json.Unmarshal(msg["sessions"], &sessions)

	// Non-sleeping session without a live agent should NOT appear
	for _, s := range sessions {
		if s["session_id"] == "awake-session-001" {
			t.Error("non-sleeping session without live agent should not appear in WS update")
		}
	}

	conn.Close(websocket.StatusNormalClosure, "done")
}

func TestWSCoral_SleepingSessionNotDuplicated(t *testing.T) {
	server, handler := setupTestServer(t)

	ctx := context.Background()

	// Register a sleeping session
	ss := store.NewSessionStore(handler.db)
	err := ss.RegisterLiveSession(ctx, &store.LiveSession{
		SessionID:  "dedup-session-001",
		AgentType:  "claude",
		AgentName:  "dedup-agent",
		WorkingDir: "/tmp/dedup",
		IsSleeping: 1,
		CreatedAt:  "2026-01-01T00:00:00Z",
	})
	require.NoError(t, err)

	wsURL := "ws" + server.URL[4:] + "/ws/coral"
	wsCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, _, err := websocket.Dial(wsCtx, wsURL, nil)
	require.NoError(t, err)
	defer conn.CloseNow()

	var msg map[string]json.RawMessage
	err = wsjson.Read(wsCtx, conn, &msg)
	require.NoError(t, err)

	var sessions []map[string]any
	json.Unmarshal(msg["sessions"], &sessions)

	// Count how many times this session appears
	count := 0
	for _, s := range sessions {
		if s["session_id"] == "dedup-session-001" {
			count++
		}
	}
	assert.Equal(t, 1, count, "sleeping session should appear exactly once (no duplicates)")

	conn.Close(websocket.StatusNormalClosure, "done")
}

func TestWSTerminal_RejectsMissingPane(t *testing.T) {
	server, _ := setupTestServer(t)

	wsURL := "ws" + server.URL[4:] + "/ws/terminal/nonexistent?session_id=fake-id&agent_type=claude"
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		// Expected — server should reject when pane not found
		t.Log("Connection rejected (expected — pane not found)")
		return
	}
	defer conn.CloseNow()

	// If accepted, it should close quickly with an error status
	_, _, err = conn.Read(ctx)
	assert.Error(t, err, "should close when pane not found")
}
