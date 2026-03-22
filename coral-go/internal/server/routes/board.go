package routes

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/cdknorow/coral/internal/board"
)

// BoardHandler handles message board HTTP endpoints.
type BoardHandler struct {
	bs     *board.Store
	mu     sync.RWMutex
	paused map[string]bool // in-memory set of paused project names
}

func NewBoardHandler(bs *board.Store) *BoardHandler {
	return &BoardHandler{
		bs:     bs,
		paused: make(map[string]bool),
	}
}

func (h *BoardHandler) isPaused(project string) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.paused[project]
}

// SetPaused programmatically pauses or resumes a board (used by sleep/wake and startup).
func (h *BoardHandler) SetPaused(project string, paused bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if paused {
		h.paused[project] = true
	} else {
		delete(h.paused, project)
	}
}

// IsPaused returns whether a board is paused (exported for use by notifier).
func (h *BoardHandler) IsPaused(project string) bool {
	return h.isPaused(project)
}

// ListProjects returns all boards with subscriber and message counts.
// GET /api/board/projects
func (h *BoardHandler) ListProjects(w http.ResponseWriter, r *http.Request) {
	projects, err := h.bs.ListProjects(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if projects == nil {
		projects = []board.ProjectInfo{}
	}
	writeJSON(w, http.StatusOK, projects)
}

// Subscribe subscribes a session to a board.
// POST /api/board/{project}/subscribe
func (h *BoardHandler) Subscribe(w http.ResponseWriter, r *http.Request) {
	project := chi.URLParam(r, "project")
	var body struct {
		SessionID   string  `json:"session_id"`
		JobTitle    string  `json:"job_title"`
		WebhookURL  *string `json:"webhook_url"`
		ReceiveMode string  `json:"receive_mode"`
	}
	if err := decodeJSON(r, &body); err != nil || body.SessionID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "session_id required"})
		return
	}
	if body.JobTitle == "" {
		body.JobTitle = "Agent"
	}
	sub, err := h.bs.Subscribe(r.Context(), project, body.SessionID, body.JobTitle, body.WebhookURL, nil, body.ReceiveMode)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, sub)
}

// Unsubscribe removes a session from a board.
// DELETE /api/board/{project}/subscribe
func (h *BoardHandler) Unsubscribe(w http.ResponseWriter, r *http.Request) {
	project := chi.URLParam(r, "project")
	var body struct {
		SessionID string `json:"session_id"`
	}
	if err := decodeJSON(r, &body); err != nil || body.SessionID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "session_id required"})
		return
	}
	found, err := h.bs.Unsubscribe(r.Context(), project, body.SessionID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if !found {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "subscriber not found"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// PostMessage posts a message to a board.
// POST /api/board/{project}/messages
func (h *BoardHandler) PostMessage(w http.ResponseWriter, r *http.Request) {
	project := chi.URLParam(r, "project")
	var body struct {
		SessionID     string  `json:"session_id"`
		Content       string  `json:"content"`
		TargetGroupID *string `json:"target_group_id,omitempty"`
	}
	if err := decodeJSON(r, &body); err != nil || body.Content == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "content required"})
		return
	}
	msg, err := h.bs.PostMessage(r.Context(), project, body.SessionID, body.Content, body.TargetGroupID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	// Fire-and-forget webhook dispatch
	go h.dispatchWebhooks(project, body.SessionID, msg)

	writeJSON(w, http.StatusOK, msg)
}

// dispatchWebhooks sends webhook callbacks to all subscribers with webhook_url set.
func (h *BoardHandler) dispatchWebhooks(project, senderSessionID string, msg *board.Message) {
	ctx := context.Background()
	targets, err := h.bs.GetWebhookTargets(ctx, project, senderSessionID)
	if err != nil || len(targets) == 0 {
		return
	}

	// Look up sender's job_title
	senderTitle := "Unknown"
	subs, err := h.bs.ListSubscribers(ctx, project)
	if err == nil {
		for _, s := range subs {
			if s.SessionID == senderSessionID {
				senderTitle = s.JobTitle
				break
			}
		}
	}

	payload := map[string]any{
		"project": project,
		"message": map[string]any{
			"id":         msg.ID,
			"session_id": msg.SessionID,
			"job_title":  senderTitle,
			"content":    msg.Content,
			"created_at": msg.CreatedAt,
		},
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return
	}

	client := &http.Client{Timeout: 5 * time.Second}
	for _, target := range targets {
		if target.WebhookURL == nil || *target.WebhookURL == "" {
			continue
		}
		go func(url string) {
			req, err := http.NewRequest("POST", url, bytes.NewReader(body))
			if err != nil {
				return
			}
			req.Header.Set("Content-Type", "application/json")
			resp, err := client.Do(req)
			if err != nil {
				slog.Debug("board webhook delivery failed", "url", url, "error", err)
				return
			}
			resp.Body.Close()
		}(*target.WebhookURL)
	}
}

// ReadMessages reads new messages (cursor-based).
// GET /api/board/{project}/messages
func (h *BoardHandler) ReadMessages(w http.ResponseWriter, r *http.Request) {
	project := chi.URLParam(r, "project")
	if h.isPaused(project) {
		writeJSON(w, http.StatusOK, []board.Message{})
		return
	}
	sessionID := r.URL.Query().Get("session_id")
	limit := queryInt(r, "limit", 50)
	messages, err := h.bs.ReadMessages(r.Context(), project, sessionID, limit)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if messages == nil {
		messages = []board.Message{}
	}
	writeJSON(w, http.StatusOK, messages)
}

// ListAllMessages returns all messages (no cursor advancement).
// GET /api/board/{project}/messages/all
func (h *BoardHandler) ListAllMessages(w http.ResponseWriter, r *http.Request) {
	project := chi.URLParam(r, "project")
	limit := queryInt(r, "limit", 200)
	if limit > 500 {
		limit = 500
	}
	offset := queryInt(r, "offset", 0)
	beforeID := int64(queryInt(r, "before_id", 0))
	format := r.URL.Query().Get("format")
	messages, err := h.bs.ListMessages(r.Context(), project, limit, offset, beforeID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if format == "dashboard" {
		total, _ := h.bs.CountMessages(r.Context(), project)
		writeJSON(w, http.StatusOK, map[string]any{
			"messages": messages,
			"total":    total,
			"limit":    limit,
			"offset":   offset,
		})
	} else {
		// Default: bare array for CLI consumers
		if messages == nil {
			messages = []board.Message{}
		}
		writeJSON(w, http.StatusOK, messages)
	}
}

// CheckUnread returns the unread message count.
// GET /api/board/{project}/messages/check
func (h *BoardHandler) CheckUnread(w http.ResponseWriter, r *http.Request) {
	project := chi.URLParam(r, "project")
	if h.isPaused(project) {
		writeJSON(w, http.StatusOK, map[string]any{"unread": 0})
		return
	}
	sessionID := r.URL.Query().Get("session_id")
	count, err := h.bs.CheckUnread(r.Context(), project, sessionID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"unread": count})
}

// DeleteMessage deletes a single message.
// DELETE /api/board/{project}/messages/{messageID}
func (h *BoardHandler) DeleteMessage(w http.ResponseWriter, r *http.Request) {
	msgID, _ := strconv.ParseInt(chi.URLParam(r, "messageID"), 10, 64)
	found, err := h.bs.DeleteMessage(r.Context(), msgID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if !found {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "message not found"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// ListSubscribers returns subscribers for a board.
// GET /api/board/{project}/subscribers
func (h *BoardHandler) ListSubscribers(w http.ResponseWriter, r *http.Request) {
	project := chi.URLParam(r, "project")
	subs, err := h.bs.ListSubscribers(r.Context(), project)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if subs == nil {
		subs = []board.Subscriber{}
	}
	writeJSON(w, http.StatusOK, subs)
}

// PauseBoard pauses reads for a board.
// POST /api/board/{project}/pause
func (h *BoardHandler) PauseBoard(w http.ResponseWriter, r *http.Request) {
	project := chi.URLParam(r, "project")
	h.mu.Lock()
	h.paused[project] = true
	h.mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "paused": true})
}

// ResumeBoard resumes reads for a board.
// POST /api/board/{project}/resume
func (h *BoardHandler) ResumeBoard(w http.ResponseWriter, r *http.Request) {
	project := chi.URLParam(r, "project")
	h.mu.Lock()
	delete(h.paused, project)
	h.mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "paused": false})
}

// GetPaused returns whether a board is paused.
// GET /api/board/{project}/paused
func (h *BoardHandler) GetPaused(w http.ResponseWriter, r *http.Request) {
	project := chi.URLParam(r, "project")
	writeJSON(w, http.StatusOK, map[string]any{"paused": h.isPaused(project)})
}

// DeleteBoard deletes a board and all its messages.
// DELETE /api/board/{project}
func (h *BoardHandler) DeleteBoard(w http.ResponseWriter, r *http.Request) {
	project := chi.URLParam(r, "project")
	h.mu.Lock()
	delete(h.paused, project)
	h.mu.Unlock()
	h.bs.DeleteProject(r.Context(), project)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// ── Group endpoints ──────────────────────────────────────────────────

// ListGroups returns all groups for a project with member counts.
// GET /api/board/{project}/groups
func (h *BoardHandler) ListGroups(w http.ResponseWriter, r *http.Request) {
	project := chi.URLParam(r, "project")
	groups, err := h.bs.ListGroups(r.Context(), project)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if groups == nil {
		groups = []board.GroupInfo{}
	}
	writeJSON(w, http.StatusOK, groups)
}

// ListGroupMembers returns session IDs in a group.
// GET /api/board/{project}/groups/{groupID}/members
func (h *BoardHandler) ListGroupMembers(w http.ResponseWriter, r *http.Request) {
	project := chi.URLParam(r, "project")
	groupID := chi.URLParam(r, "groupID")
	members, err := h.bs.ListGroupMembers(r.Context(), project, groupID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if members == nil {
		members = []string{}
	}
	writeJSON(w, http.StatusOK, members)
}

// AddGroupMember adds a session to a group.
// POST /api/board/{project}/groups/{groupID}/members
func (h *BoardHandler) AddGroupMember(w http.ResponseWriter, r *http.Request) {
	project := chi.URLParam(r, "project")
	groupID := chi.URLParam(r, "groupID")
	var body struct {
		SessionID string `json:"session_id"`
	}
	if err := decodeJSON(r, &body); err != nil || body.SessionID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "session_id required"})
		return
	}
	if err := h.bs.AddToGroup(r.Context(), project, groupID, body.SessionID); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// RemoveGroupMember removes a session from a group.
// DELETE /api/board/{project}/groups/{groupID}/members/{sessionID}
func (h *BoardHandler) RemoveGroupMember(w http.ResponseWriter, r *http.Request) {
	project := chi.URLParam(r, "project")
	groupID := chi.URLParam(r, "groupID")
	sessionID := chi.URLParam(r, "sessionID")
	removed, err := h.bs.RemoveFromGroup(r.Context(), project, groupID, sessionID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if !removed {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "member not found"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}
