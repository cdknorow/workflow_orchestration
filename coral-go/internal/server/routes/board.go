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
	bs       *board.Store
	mu       sync.RWMutex
	paused   map[string]bool // in-memory set of paused project names
	notifyFn func()          // triggers immediate board notification pass
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

// SetNotifyFn sets a callback that triggers an immediate board notification pass.
func (h *BoardHandler) SetNotifyFn(fn func()) {
	h.notifyFn = fn
}

// ListProjects returns all boards with subscriber and message counts.
// GET /api/board/projects
func (h *BoardHandler) ListProjects(w http.ResponseWriter, r *http.Request) {
	projects, err := h.bs.ListProjects(r.Context())
	if err != nil {
		errInternalServer(w, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, emptyIfNil(projects))
}

// Subscribe subscribes to a board.
// POST /api/board/{project}/subscribe
// Accepts subscriber_id (stable identity) with optional session_name.
// Falls back to session_id for backwards compatibility.
func (h *BoardHandler) Subscribe(w http.ResponseWriter, r *http.Request) {
	project := chi.URLParam(r, "project")
	var body struct {
		SubscriberID string  `json:"subscriber_id"`
		SessionID    string  `json:"session_id"` // legacy compat
		SessionName  string  `json:"session_name"`
		JobTitle     string  `json:"job_title"`
		WebhookURL   *string `json:"webhook_url"`
		ReceiveMode  string  `json:"receive_mode"`
	}
	if err := decodeJSON(r, &body); err != nil {
		errBadRequest(w, "invalid JSON")
		return
	}
	subscriberID := body.SubscriberID
	if subscriberID == "" {
		subscriberID = body.SessionID // legacy fallback
	}
	if subscriberID == "" {
		errBadRequest(w, "subscriber_id required")
		return
	}
	if body.JobTitle == "" {
		body.JobTitle = "Agent"
	}
	sub, err := h.bs.Subscribe(r.Context(), project, subscriberID, body.JobTitle, body.SessionName, body.WebhookURL, nil, body.ReceiveMode)
	if err != nil {
		errInternalServer(w, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, sub)
}

// Unsubscribe removes a subscriber from a board.
// DELETE /api/board/{project}/subscribe
func (h *BoardHandler) Unsubscribe(w http.ResponseWriter, r *http.Request) {
	project := chi.URLParam(r, "project")
	var body struct {
		SubscriberID string `json:"subscriber_id"`
		SessionID    string `json:"session_id"` // legacy compat
	}
	if err := decodeJSON(r, &body); err != nil {
		errBadRequest(w, "invalid JSON")
		return
	}
	subscriberID := body.SubscriberID
	if subscriberID == "" {
		subscriberID = body.SessionID
	}
	if subscriberID == "" {
		errBadRequest(w, "subscriber_id required")
		return
	}
	found, err := h.bs.Unsubscribe(r.Context(), project, subscriberID)
	if err != nil {
		errInternalServer(w, err.Error())
		return
	}
	if !found {
		errNotFound(w, "subscriber not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// PostMessage posts a message to a board.
// POST /api/board/{project}/messages
func (h *BoardHandler) PostMessage(w http.ResponseWriter, r *http.Request) {
	project := chi.URLParam(r, "project")
	var body struct {
		SubscriberID  string  `json:"subscriber_id"`
		SessionID     string  `json:"session_id"` // legacy compat
		Content       string  `json:"content"`
		TargetGroupID *string `json:"target_group_id,omitempty"`
		As            string  `json:"as,omitempty"` // display name for auto-subscribe (e.g. "Operator")
	}
	if err := decodeJSON(r, &body); err != nil || body.Content == "" {
		errBadRequest(w, "content required")
		return
	}

	subscriberID := body.SubscriberID
	if subscriberID == "" {
		subscriberID = body.SessionID
	}

	// Auto-subscribe the poster if 'as' is provided and they aren't subscribed yet
	if body.As != "" && subscriberID != "" {
		sub, _ := h.bs.GetSubscription(r.Context(), subscriberID)
		if sub == nil {
			h.bs.Subscribe(r.Context(), project, subscriberID, body.As, "", nil, nil, "all")
		}
	}

	msg, err := h.bs.PostMessage(r.Context(), project, subscriberID, body.Content, body.TargetGroupID)
	if err != nil {
		errInternalServer(w, err.Error())
		return
	}

	// Fire-and-forget webhook dispatch
	go h.dispatchWebhooks(project, subscriberID, msg)

	// Trigger immediate board notification so subscribers get nudged right away
	if h.notifyFn != nil {
		h.notifyFn()
	}

	writeJSON(w, http.StatusOK, msg)
}

// dispatchWebhooks sends webhook callbacks to all subscribers with webhook_url set.
func (h *BoardHandler) dispatchWebhooks(project, senderSubscriberID string, msg *board.Message) {
	ctx := context.Background()
	targets, err := h.bs.GetWebhookTargets(ctx, project, senderSubscriberID)
	if err != nil || len(targets) == 0 {
		return
	}

	// Look up sender's job_title
	senderTitle := "Unknown"
	subs, err := h.bs.ListSubscribers(ctx, project)
	if err == nil {
		for _, s := range subs {
			if s.SubscriberID == senderSubscriberID {
				senderTitle = s.JobTitle
				break
			}
		}
	}

	payload := map[string]any{
		"project": project,
		"message": map[string]any{
			"id":            msg.ID,
			"subscriber_id": msg.SubscriberID,
			"job_title":     senderTitle,
			"content":       msg.Content,
			"created_at":    msg.CreatedAt,
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
	subscriberID := r.URL.Query().Get("subscriber_id")
	if subscriberID == "" {
		subscriberID = r.URL.Query().Get("session_id") // legacy compat
	}
	limit := queryInt(r, "limit", 50)
	messages, err := h.bs.ReadMessages(r.Context(), project, subscriberID, limit)
	if err != nil {
		errInternalServer(w, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, emptyIfNil(messages))
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
		errInternalServer(w, err.Error())
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
		writeJSON(w, http.StatusOK, emptyIfNil(messages))
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
	subscriberID := r.URL.Query().Get("subscriber_id")
	if subscriberID == "" {
		subscriberID = r.URL.Query().Get("session_id") // legacy compat
	}
	count, err := h.bs.CheckUnread(r.Context(), project, subscriberID)
	if err != nil {
		errInternalServer(w, err.Error())
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
		errInternalServer(w, err.Error())
		return
	}
	if !found {
		errNotFound(w, "message not found")
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
		errInternalServer(w, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, emptyIfNil(subs))
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
		errInternalServer(w, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, emptyIfNil(groups))
}

// ListGroupMembers returns subscriber IDs in a group.
// GET /api/board/{project}/groups/{groupID}/members
func (h *BoardHandler) ListGroupMembers(w http.ResponseWriter, r *http.Request) {
	project := chi.URLParam(r, "project")
	groupID := chi.URLParam(r, "groupID")
	members, err := h.bs.ListGroupMembers(r.Context(), project, groupID)
	if err != nil {
		errInternalServer(w, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, emptyIfNil(members))
}

// AddGroupMember adds a subscriber to a group.
// POST /api/board/{project}/groups/{groupID}/members
func (h *BoardHandler) AddGroupMember(w http.ResponseWriter, r *http.Request) {
	project := chi.URLParam(r, "project")
	groupID := chi.URLParam(r, "groupID")
	var body struct {
		SubscriberID string `json:"subscriber_id"`
		SessionID    string `json:"session_id"` // legacy compat
	}
	if err := decodeJSON(r, &body); err != nil {
		errBadRequest(w, "invalid JSON")
		return
	}
	subscriberID := body.SubscriberID
	if subscriberID == "" {
		subscriberID = body.SessionID
	}
	if subscriberID == "" {
		errBadRequest(w, "subscriber_id required")
		return
	}
	if err := h.bs.AddToGroup(r.Context(), project, groupID, subscriberID); err != nil {
		errInternalServer(w, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// RemoveGroupMember removes a subscriber from a group.
// DELETE /api/board/{project}/groups/{groupID}/members/{subscriberID}
func (h *BoardHandler) RemoveGroupMember(w http.ResponseWriter, r *http.Request) {
	project := chi.URLParam(r, "project")
	groupID := chi.URLParam(r, "groupID")
	subscriberID := chi.URLParam(r, "sessionID") // URL param name kept for route compat
	removed, err := h.bs.RemoveFromGroup(r.Context(), project, groupID, subscriberID)
	if err != nil {
		errInternalServer(w, err.Error())
		return
	}
	if !removed {
		errNotFound(w, "member not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}
