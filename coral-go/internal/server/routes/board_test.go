package routes

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/cdknorow/coral/internal/board"
)

func setupBoardTestServer(t *testing.T) (*httptest.Server, *BoardHandler) {
	t.Helper()

	dbPath := t.TempDir() + "/board_test.db"
	bs, err := board.NewStore(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { bs.Close() })

	handler := NewBoardHandler(bs)

	r := chi.NewRouter()
	r.Get("/api/board/projects", handler.ListProjects)
	r.Post("/api/board/{project}/subscribe", handler.Subscribe)
	r.Delete("/api/board/{project}/subscribe", handler.Unsubscribe)
	r.Post("/api/board/{project}/messages", handler.PostMessage)
	r.Get("/api/board/{project}/messages", handler.ReadMessages)
	r.Get("/api/board/{project}/messages/check", handler.CheckUnread)
	r.Get("/api/board/{project}/messages/all", handler.ListAllMessages)
	r.Delete("/api/board/{project}/messages/{messageID}", handler.DeleteMessage)
	r.Get("/api/board/{project}/subscribers", handler.ListSubscribers)
	r.Post("/api/board/{project}/pause", handler.PauseBoard)
	r.Post("/api/board/{project}/resume", handler.ResumeBoard)
	r.Get("/api/board/{project}/paused", handler.GetPaused)
	r.Delete("/api/board/{project}", handler.DeleteBoard)

	server := httptest.NewServer(r)
	t.Cleanup(server.Close)

	return server, handler
}

func postJSON(t *testing.T, url string, payload any) *http.Response {
	t.Helper()
	body, _ := json.Marshal(payload)
	resp, err := http.Post(url, "application/json", bytes.NewReader(body))
	require.NoError(t, err)
	return resp
}

func TestBoardPauseResume(t *testing.T) {
	server, _ := setupBoardTestServer(t)
	base := server.URL + "/api/board/testproject"

	// Initially not paused
	resp, err := http.Get(base + "/paused")
	require.NoError(t, err)
	defer resp.Body.Close()
	var body map[string]any
	json.NewDecoder(resp.Body).Decode(&body)
	assert.Equal(t, false, body["paused"])

	// Pause the board
	resp2 := postJSON(t, base+"/pause", nil)
	defer resp2.Body.Close()
	assert.Equal(t, http.StatusOK, resp2.StatusCode)
	var pauseBody map[string]any
	json.NewDecoder(resp2.Body).Decode(&pauseBody)
	assert.Equal(t, true, pauseBody["paused"])

	// Verify paused
	resp3, err := http.Get(base + "/paused")
	require.NoError(t, err)
	defer resp3.Body.Close()
	var body2 map[string]any
	json.NewDecoder(resp3.Body).Decode(&body2)
	assert.Equal(t, true, body2["paused"])

	// Resume
	resp4 := postJSON(t, base+"/resume", nil)
	defer resp4.Body.Close()
	assert.Equal(t, http.StatusOK, resp4.StatusCode)
	var resumeBody map[string]any
	json.NewDecoder(resp4.Body).Decode(&resumeBody)
	assert.Equal(t, false, resumeBody["paused"])

	// Verify not paused
	resp5, err := http.Get(base + "/paused")
	require.NoError(t, err)
	defer resp5.Body.Close()
	var body3 map[string]any
	json.NewDecoder(resp5.Body).Decode(&body3)
	assert.Equal(t, false, body3["paused"])
}

func TestBoardPause_ReadMessagesReturnsEmpty(t *testing.T) {
	server, _ := setupBoardTestServer(t)
	base := server.URL + "/api/board/testproject"

	// Subscribe a session
	resp := postJSON(t, base+"/subscribe", map[string]string{
		"session_id": "sess-1",
		"job_title":  "Dev",
	})
	resp.Body.Close()

	// Post a message
	resp2 := postJSON(t, base+"/messages", map[string]string{
		"session_id": "sess-1",
		"content":    "Hello world",
	})
	resp2.Body.Close()

	// Pause the board
	resp3 := postJSON(t, base+"/pause", nil)
	resp3.Body.Close()

	// ReadMessages should return empty array when paused
	resp4, err := http.Get(base + "/messages?session_id=sess-1")
	require.NoError(t, err)
	defer resp4.Body.Close()

	var messages []any
	json.NewDecoder(resp4.Body).Decode(&messages)
	assert.Empty(t, messages)
}

func TestBoardPause_CheckUnreadReturnsZero(t *testing.T) {
	server, _ := setupBoardTestServer(t)
	base := server.URL + "/api/board/testproject"

	// Subscribe two sessions
	resp := postJSON(t, base+"/subscribe", map[string]string{
		"session_id": "sess-1",
		"job_title":  "Dev",
	})
	resp.Body.Close()
	resp2 := postJSON(t, base+"/subscribe", map[string]string{
		"session_id": "sess-2",
		"job_title":  "QA",
	})
	resp2.Body.Close()

	// Post from sess-1
	resp3 := postJSON(t, base+"/messages", map[string]string{
		"session_id": "sess-1",
		"content":    "Hello",
	})
	resp3.Body.Close()

	// Pause
	resp4 := postJSON(t, base+"/pause", nil)
	resp4.Body.Close()

	// CheckUnread for sess-2 should return 0 when paused
	resp5, err := http.Get(base + "/messages/check?session_id=sess-2")
	require.NoError(t, err)
	defer resp5.Body.Close()

	var body map[string]any
	json.NewDecoder(resp5.Body).Decode(&body)
	assert.Equal(t, float64(0), body["unread"])
}

func TestBoardDelete_ClearsPauseState(t *testing.T) {
	server, _ := setupBoardTestServer(t)
	base := server.URL + "/api/board/testproject"

	// Pause
	resp := postJSON(t, base+"/pause", nil)
	resp.Body.Close()

	// Delete the board
	req, _ := http.NewRequest("DELETE", base, nil)
	resp2, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp2.Body.Close()
	assert.Equal(t, http.StatusOK, resp2.StatusCode)

	// Paused state should be cleared
	resp3, err := http.Get(base + "/paused")
	require.NoError(t, err)
	defer resp3.Body.Close()
	var body map[string]any
	json.NewDecoder(resp3.Body).Decode(&body)
	assert.Equal(t, false, body["paused"])
}

func TestBoardSubscribeAndPost(t *testing.T) {
	server, _ := setupBoardTestServer(t)
	base := server.URL + "/api/board/myproject"

	// Subscribe
	resp := postJSON(t, base+"/subscribe", map[string]string{
		"session_id": "sess-abc",
		"job_title":  "Backend Dev",
	})
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	// List subscribers
	resp2, err := http.Get(base + "/subscribers")
	require.NoError(t, err)
	defer resp2.Body.Close()

	var subs []map[string]any
	json.NewDecoder(resp2.Body).Decode(&subs)
	assert.Len(t, subs, 1)
	assert.Equal(t, "Backend Dev", subs[0]["job_title"])

	// Post message
	resp3 := postJSON(t, base+"/messages", map[string]string{
		"session_id": "sess-abc",
		"content":    "Test message",
	})
	defer resp3.Body.Close()
	assert.Equal(t, http.StatusOK, resp3.StatusCode)

	var msg map[string]any
	json.NewDecoder(resp3.Body).Decode(&msg)
	assert.Equal(t, "Test message", msg["content"])
	assert.NotEmpty(t, msg["id"])

	// List all messages (dashboard format)
	resp4, err := http.Get(base + "/messages/all?format=dashboard")
	require.NoError(t, err)
	defer resp4.Body.Close()

	var allBody map[string]any
	json.NewDecoder(resp4.Body).Decode(&allBody)
	assert.Equal(t, float64(1), allBody["total"])
}

func TestBoardPostMessage_EmptyContent(t *testing.T) {
	server, _ := setupBoardTestServer(t)
	base := server.URL + "/api/board/myproject"

	resp := postJSON(t, base+"/messages", map[string]string{
		"session_id": "sess-1",
		"content":    "",
	})
	defer resp.Body.Close()
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestBoardSubscribe_MissingSessionID(t *testing.T) {
	server, _ := setupBoardTestServer(t)
	base := server.URL + "/api/board/myproject"

	resp := postJSON(t, base+"/subscribe", map[string]string{
		"job_title": "Dev",
	})
	defer resp.Body.Close()
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestBoardListProjects(t *testing.T) {
	server, _ := setupBoardTestServer(t)

	// Subscribe to two different projects
	resp := postJSON(t, server.URL+"/api/board/project1/subscribe", map[string]string{
		"session_id": "s1",
		"job_title":  "Dev",
	})
	resp.Body.Close()
	resp2 := postJSON(t, server.URL+"/api/board/project2/subscribe", map[string]string{
		"session_id": "s2",
		"job_title":  "QA",
	})
	resp2.Body.Close()

	// List projects
	resp3, err := http.Get(server.URL + "/api/board/projects")
	require.NoError(t, err)
	defer resp3.Body.Close()

	var projects []map[string]any
	json.NewDecoder(resp3.Body).Decode(&projects)
	assert.Len(t, projects, 2)
}
