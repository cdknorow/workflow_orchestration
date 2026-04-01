package routes

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/cdknorow/coral/internal/board"
	"github.com/cdknorow/coral/internal/ptymanager"
)

// BoardHandler handles message board HTTP endpoints.
type BoardHandler struct {
	bs       *board.Store
	terminal ptymanager.SessionTerminal
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

// SetTerminal sets the terminal backend for peek functionality.
func (h *BoardHandler) SetTerminal(t ptymanager.SessionTerminal) {
	h.terminal = t
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

func (h *BoardHandler) buildAssignmentNotification(ctx context.Context, project string, task *board.Task, assignee string, reassigned bool) string {
	if assignee == "" {
		if reassigned {
			return fmt.Sprintf("@notify-all [Task #%d reassigned — now unassigned] %s — run 'coral-board task claim' to pick it up", task.ID, task.Title)
		}
		return fmt.Sprintf("@notify-all [New Task #%d (%s)] %s — run 'coral-board task claim' to pick it up", task.ID, task.Priority, task.Title)
	}

	hasActiveTask, err := h.bs.HasActiveTaskForAssignee(ctx, project, assignee, task.ID)
	if err != nil {
		slog.Warn("check active assignee task failed", "project", project, "assignee", assignee, "task_id", task.ID, "error", err)
	}
	if hasActiveTask {
		if reassigned {
			return fmt.Sprintf("[Task #%d reassigned to %s — notification deferred while they have an active task] %s", task.ID, assignee, task.Title)
		}
		return fmt.Sprintf("[Task #%d (%s) assigned to %s — notification deferred while they have an active task] %s", task.ID, task.Priority, assignee, task.Title)
	}

	if reassigned {
		return fmt.Sprintf("@%s [Task #%d reassigned to you] %s — run 'coral-board task claim' to start", assignee, task.ID, task.Title)
	}
	return fmt.Sprintf("@%s [Task #%d (%s)] %s — assigned to you, run 'coral-board task claim' to start", assignee, task.ID, task.Priority, task.Title)
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
// Pass ?id=N to fetch a single message by ID (does not advance cursor).
func (h *BoardHandler) ListAllMessages(w http.ResponseWriter, r *http.Request) {
	// Single message lookup by ID
	if idStr := r.URL.Query().Get("id"); idStr != "" {
		msgID, err := strconv.ParseInt(idStr, 10, 64)
		if err != nil {
			errBadRequest(w, "invalid message id")
			return
		}
		msg, err := h.bs.GetMessageByID(r.Context(), msgID)
		if err != nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "message not found"})
			return
		}
		writeJSON(w, http.StatusOK, []board.Message{*msg})
		return
	}

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

// PeekAgent captures terminal output of another agent on the same board.
// GET /api/board/{project}/peek?target=<name>&subscriber_id=<caller>&lines=30
func (h *BoardHandler) PeekAgent(w http.ResponseWriter, r *http.Request) {
	project := chi.URLParam(r, "project")
	callerID := r.URL.Query().Get("subscriber_id")
	target := r.URL.Query().Get("target")
	lines := queryInt(r, "lines", 30)

	if lines > 500 {
		lines = 500
	}

	if callerID == "" || target == "" {
		errBadRequest(w, "subscriber_id and target are required")
		return
	}

	// Check caller has can_peek permission — use project-scoped lookup
	// to prevent cross-board authorization bypass.
	allSubs, err := h.bs.ListSubscribers(r.Context(), project)
	if err != nil {
		errInternalServer(w, err.Error())
		return
	}

	// Find caller in this board's subscribers
	var caller *board.Subscriber
	for i, s := range allSubs {
		if s.SubscriberID == callerID {
			caller = &allSubs[i]
			break
		}
	}
	if caller == nil {
		errForbidden(w, "not subscribed to this board")
		return
	}
	if caller.CanPeek == 0 {
		errForbidden(w, "peek permission not granted for this subscriber")
		return
	}

	// Find target in same board's subscribers
	var targetSub *board.Subscriber
	for i, s := range allSubs {
		if s.SubscriberID == target || s.JobTitle == target {
			targetSub = &allSubs[i]
			break
		}
	}
	if targetSub == nil {
		errNotFound(w, "target subscriber not found on this board")
		return
	}

	if h.terminal == nil {
		errInternalServer(w, "terminal backend not available")
		return
	}

	// Capture terminal output using the target's session name
	output, err := h.terminal.CaptureOutput(r.Context(), targetSub.SessionName, lines, "", "")
	if err != nil {
		slog.Warn("peek capture failed", "target", target, "error", err)
		errInternalServer(w, "failed to capture terminal output")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"target":       target,
		"session_name": targetSub.SessionName,
		"lines":        lines,
		"output":       output,
	})
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

// ── Task endpoints ───────────────────────────────────────────────────

// CreateTask creates a new task on a board.
// POST /api/board/{project}/tasks
func (h *BoardHandler) CreateTask(w http.ResponseWriter, r *http.Request) {
	project := chi.URLParam(r, "project")
	var body struct {
		Title        string `json:"title"`
		Body         string `json:"body"`
		Priority     string `json:"priority"`
		CreatedBy    string `json:"created_by"`
		SubscriberID string `json:"subscriber_id"`
		AssignedTo   string `json:"assigned_to"`
	}
	if err := decodeJSON(r, &body); err != nil {
		errBadRequest(w, "invalid JSON")
		return
	}
	if body.Title == "" {
		errBadRequest(w, "title required")
		return
	}
	createdBy := body.CreatedBy
	if createdBy == "" {
		createdBy = body.SubscriberID
	}
	if createdBy == "" {
		errBadRequest(w, "created_by or subscriber_id required")
		return
	}
	if body.Priority == "" {
		body.Priority = "medium"
	}
	task, err := h.bs.CreateTask(r.Context(), project, body.Title, body.Body, body.Priority, createdBy, body.AssignedTo)
	if err != nil {
		errInternalServer(w, err.Error())
		return
	}
	// Notify agents about the new task — @mention assignee if pre-assigned
	go func() {
		assignee := ""
		if task.AssignedTo != nil {
			assignee = *task.AssignedTo
		}
		notification := h.buildAssignmentNotification(context.Background(), project, task, assignee, false)
		h.bs.PostMessage(context.Background(), project, "Coral Task Queue", notification, nil)
	}()
	writeJSON(w, http.StatusCreated, task)
}

// ListTasks returns all tasks for a board.
// GET /api/board/{project}/tasks
func (h *BoardHandler) ListTasks(w http.ResponseWriter, r *http.Request) {
	project := chi.URLParam(r, "project")
	tasks, err := h.bs.ListTasks(r.Context(), project)
	if err != nil {
		errInternalServer(w, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"tasks": emptyIfNil(tasks)})
}

// ClaimTask claims the next available task by priority.
// POST /api/board/{project}/tasks/claim
func (h *BoardHandler) ClaimTask(w http.ResponseWriter, r *http.Request) {
	project := chi.URLParam(r, "project")
	var body struct {
		SubscriberID string `json:"subscriber_id"`
	}
	if err := decodeJSON(r, &body); err != nil {
		errBadRequest(w, "invalid JSON")
		return
	}
	if body.SubscriberID == "" {
		errBadRequest(w, "subscriber_id required")
		return
	}
	task, err := h.bs.ClaimTask(r.Context(), project, body.SubscriberID)
	if err != nil {
		errInternalServer(w, err.Error())
		return
	}
	if task == nil {
		errNotFound(w, "no available tasks")
		return
	}
	// Post board notification asynchronously to avoid DB contention
	go func() {
		notification := fmt.Sprintf("[Task #%d claimed by %s] %s", task.ID, body.SubscriberID, task.Title)
		h.bs.PostMessage(context.Background(), project, "Coral Task Queue", notification, nil)
	}()
	writeJSON(w, http.StatusOK, task)
}

// CompleteTaskByID marks a task as completed.
// POST /api/board/{project}/tasks/{taskID}/complete
func (h *BoardHandler) CompleteTaskByID(w http.ResponseWriter, r *http.Request) {
	project := chi.URLParam(r, "project")
	taskID, err := strconv.ParseInt(chi.URLParam(r, "taskID"), 10, 64)
	if err != nil {
		errBadRequest(w, "invalid task ID")
		return
	}
	var body struct {
		SubscriberID string  `json:"subscriber_id"`
		Message      *string `json:"message"`
	}
	if err := decodeJSON(r, &body); err != nil {
		errBadRequest(w, "invalid JSON")
		return
	}
	if body.SubscriberID == "" {
		errBadRequest(w, "subscriber_id required")
		return
	}
	task, err := h.bs.CompleteTask(r.Context(), project, taskID, body.SubscriberID, body.Message)
	if err != nil {
		errInternalServer(w, err.Error())
		return
	}
	// Post board notification asynchronously to avoid DB contention
	go func() {
		msg := task.Title
		if body.Message != nil && *body.Message != "" {
			msg = *body.Message
		}
		notification := fmt.Sprintf("[Task #%d completed by %s] %s", task.ID, body.SubscriberID, msg)
		h.bs.PostMessage(context.Background(), project, "Coral Task Queue", notification, nil)
	}()
	writeJSON(w, http.StatusOK, task)
}

// CancelTaskByID marks a task as skipped/cancelled.
// POST /api/board/{project}/tasks/{taskID}/cancel
func (h *BoardHandler) CancelTaskByID(w http.ResponseWriter, r *http.Request) {
	project := chi.URLParam(r, "project")
	taskID, err := strconv.ParseInt(chi.URLParam(r, "taskID"), 10, 64)
	if err != nil {
		errBadRequest(w, "invalid task ID")
		return
	}
	var body struct {
		SubscriberID string  `json:"subscriber_id"`
		Message      *string `json:"message"`
	}
	if err := decodeJSON(r, &body); err != nil {
		errBadRequest(w, "invalid JSON")
		return
	}
	if body.SubscriberID == "" {
		errBadRequest(w, "subscriber_id required")
		return
	}
	task, err := h.bs.CancelTask(r.Context(), project, taskID, body.SubscriberID, body.Message)
	if err != nil {
		errInternalServer(w, err.Error())
		return
	}
	go func() {
		notification := fmt.Sprintf("[Task #%d cancelled by %s] %s", task.ID, body.SubscriberID, task.Title)
		h.bs.PostMessage(context.Background(), project, "Coral Task Queue", notification, nil)
	}()
	writeJSON(w, http.StatusOK, task)
}

// ReassignTask resets a task to pending with an optional new assignee.
// POST /api/board/{project}/tasks/{taskID}/reassign
func (h *BoardHandler) ReassignTask(w http.ResponseWriter, r *http.Request) {
	project := chi.URLParam(r, "project")
	taskID, err := strconv.ParseInt(chi.URLParam(r, "taskID"), 10, 64)
	if err != nil {
		errBadRequest(w, "invalid task ID")
		return
	}
	var body struct {
		SubscriberID string `json:"subscriber_id"`
		Assignee     string `json:"assignee"`
	}
	if err := decodeJSON(r, &body); err != nil {
		errBadRequest(w, "invalid JSON")
		return
	}
	task, err := h.bs.ReassignTask(r.Context(), project, taskID, body.Assignee)
	if err != nil {
		errInternalServer(w, err.Error())
		return
	}
	go func() {
		notification := h.buildAssignmentNotification(context.Background(), project, task, body.Assignee, true)
		h.bs.PostMessage(context.Background(), project, "Coral Task Queue", notification, nil)
	}()
	writeJSON(w, http.StatusOK, task)
}
