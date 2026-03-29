// Package routes implements HTTP handlers for the Coral API.
package routes

import (
	"context"
	crand "crypto/rand"
	"encoding/json"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/cdknorow/coral/internal/agent"
	at "github.com/cdknorow/coral/internal/agenttypes"
	"github.com/cdknorow/coral/internal/board"
	"github.com/cdknorow/coral/internal/config"
	"github.com/cdknorow/coral/internal/naming"
	"github.com/cdknorow/coral/internal/gitutil"
	"github.com/cdknorow/coral/internal/httputil"
	"github.com/cdknorow/coral/internal/jsonl"
	"github.com/cdknorow/coral/internal/pulse"
	"github.com/cdknorow/coral/internal/ptymanager"
	"github.com/cdknorow/coral/internal/store"
	"github.com/cdknorow/coral/internal/tracking"
)

// SessionsHandler handles all live session API endpoints.
type SessionsHandler struct {
	db      *store.DB
	ss      *store.SessionStore
	ts      *store.TaskStore
	gs      *store.GitStore
	bs      *board.Store
	cfg     *config.Config
	terminal ptymanager.SessionTerminal
	jsonl   *jsonl.SessionReader
	backend ptymanager.TerminalBackend // nil = use tmux directly

	boardHandler *BoardHandler // for sleep/wake board pausing

	// Deduplication state for status/summary events (mirrors Python _last_known)
	lastKnownMu sync.RWMutex
	lastKnown   map[string]lastKnownState
}

// SetBoardHandler sets the board handler reference for sleep/wake operations.
func (h *SessionsHandler) SetBoardHandler(bh *BoardHandler) {
	h.boardHandler = bh
}

type lastKnownState struct {
	Status  string
	Summary string
}

// getDiffMode reads the git_diff_mode from global user settings.
// Returns "" (default branch_point), "previous_commit", or "main_head".
func (h *SessionsHandler) getDiffMode(ctx context.Context) string {
	settings, err := h.ss.GetSettings(ctx)
	if err != nil {
		return ""
	}
	return settings["git_diff_mode"]
}

// NewSessionsHandler creates a SessionsHandler with the given dependencies.
func NewSessionsHandler(db *store.DB, cfg *config.Config, backend ptymanager.TerminalBackend, terminal ptymanager.SessionTerminal, bs *board.Store) *SessionsHandler {
	return &SessionsHandler{
		db:        db,
		ss:        store.NewSessionStore(db),
		ts:        store.NewTaskStore(db),
		gs:        store.NewGitStore(db),
		bs:        bs,
		cfg:       cfg,
		terminal:  terminal,
		jsonl:     jsonl.NewSessionReader(),
		backend:   backend,
		lastKnown: make(map[string]lastKnownState),
	}
}

// ── Agent Discovery ─────────────────────────────────────────────────────

// AgentInfo represents a discovered live agent.
type AgentInfo struct {
	AgentType    string `json:"agent_type"`
	AgentName    string `json:"agent_name"`
	SessionID    string `json:"session_id"`
	TmuxSession  string `json:"tmux_session"`
	LogPath      string `json:"log_path"`
	WorkingDir   string `json:"working_directory"`
}

func (h *SessionsHandler) discoverAgents(ctx *http.Request) ([]AgentInfo, error) {
	panes, err := h.terminal.ListSessions(ctx.Context())
	if err != nil {
		return nil, err
	}

	seen := make(map[string]bool)
	var agents []AgentInfo

	for _, pane := range panes {
		agentType, sessionID := pulse.ParseSessionName(pane.SessionName)
		if agentType == "" || sessionID == "" {
			continue
		}
		if seen[sessionID] {
			continue
		}
		seen[sessionID] = true

		agentName := filepath.Base(strings.TrimRight(pane.CurrentPath, "/"))
		if agentName == "" {
			agentName = sessionID[:8]
		}

		logPath := naming.LogFile(h.cfg.LogDir, agentType, sessionID)

		agents = append(agents, AgentInfo{
			AgentType:   agentType,
			AgentName:   agentName,
			SessionID:   sessionID,
			TmuxSession: pane.SessionName,
			LogPath:     logPath,
			WorkingDir:  pane.CurrentPath,
		})
	}

	return agents, nil
}

// getLogStatus reads a log file and extracts PULSE status/summary.
func getLogStatus(logPath string) map[string]any {
	result := map[string]any{
		"status":            nil,
		"summary":           nil,
		"staleness_seconds": nil,
		"recent_lines":      []string{},
	}

	info, err := os.Stat(logPath)
	if err != nil {
		return result
	}

	staleness := time.Since(info.ModTime()).Seconds()
	result["staleness_seconds"] = staleness

	// Read tail of the file (last ~256KB)
	const tailBytes = 256_000
	f, err := os.Open(logPath)
	if err != nil {
		return result
	}
	defer f.Close()

	fileSize := info.Size()
	start := int64(0)
	if fileSize > tailBytes {
		start = fileSize - tailBytes
	}
	f.Seek(start, 0)
	raw, err := os.ReadFile(logPath)
	if err != nil {
		return result
	}
	if start > 0 {
		raw = raw[start:]
	}

	// Split into lines, decode, strip ANSI
	rawLines := strings.Split(string(raw), "\n")
	if start > 0 && len(rawLines) > 0 {
		rawLines = rawLines[1:] // drop partial first line
	}

	cleanLines := make([]string, 0, len(rawLines))
	for _, line := range rawLines {
		cleanLines = append(cleanLines, pulse.StripANSI(line))
	}

	parsed := pulse.ParseLogLines(cleanLines)
	if parsed.Status != "" {
		result["status"] = parsed.Status
	}
	if parsed.Summary != "" {
		result["summary"] = parsed.Summary
	}
	result["recent_lines"] = parsed.RecentLines

	return result
}

// ── List / Detail ───────────────────────────────────────────────────────

// List returns all live agent sessions with enriched metadata.
// GET /api/sessions/live
func (h *SessionsHandler) List(w http.ResponseWriter, r *http.Request) {
	agents, err := h.discoverAgents(r)
	if err != nil {
		errInternalServer(w, err.Error())
		return
	}

	ctx := r.Context()

	// Batch fetch enrichment data
	sessionIDs := make([]string, 0, len(agents))
	for _, a := range agents {
		if a.SessionID != "" {
			sessionIDs = append(sessionIDs, a.SessionID)
		}
	}
	displayNames, _ := h.ss.GetDisplayNames(ctx, sessionIDs)
	icons, _ := h.ss.GetIcons(ctx, sessionIDs)
	if icons == nil {
		icons = map[string]string{}
	}

	// Batch fetch git state, file counts, events, and goals
	gitState, _ := h.gs.GetAllLatestGitState(ctx)
	if gitState == nil {
		gitState = map[string]*store.GitSnapshot{}
	}
	fileCounts, _ := h.gs.GetAllChangedFileCounts(ctx)
	if fileCounts == nil {
		fileCounts = map[string]int{}
	}
	latestEvents, _ := h.ts.GetLatestEventTypes(ctx, sessionIDs)
	if latestEvents == nil {
		latestEvents = map[string][2]string{}
	}
	var latestGoals map[string]string
	latestGoals, err = h.ts.GetLatestGoals(ctx, sessionIDs)
	if err != nil {
		slog.Warn("failed to get latest goals", "error", err)
	}
	if latestGoals == nil {
		latestGoals = map[string]string{}
	}

	// Fetch board subscriptions keyed by tmux session name
	var boardSubs map[string]*board.Subscriber
	if h.bs != nil {
		boardSubs, err = h.bs.GetAllSubscriptions(ctx)
		if err != nil {
			slog.Warn("failed to get board subscriptions", "error", err)
		}
	}
	if boardSubs == nil {
		boardSubs = map[string]*board.Subscriber{}
	}

	// Fetch board unread counts
	var allUnread map[string]int
	if h.bs != nil {
		allUnread, err = h.bs.GetAllUnreadCounts(ctx)
		if err != nil {
			slog.Warn("failed to get board unread counts", "error", err)
		}
	}
	if allUnread == nil {
		allUnread = map[string]int{}
	}

	// Fallback: board_name from live_sessions DB for agents not yet subscribed
	liveBoardNames := make(map[string][2]string) // session_id -> [board_name, display_name]
	liveSleeping := make(map[string]bool)         // session_id -> is_sleeping
	type liveExtra struct {
		Prompt       *string
		Model        *string
		Capabilities *string
	}
	liveExtras := make(map[string]liveExtra) // session_id -> extra fields
	{
		var rows []struct {
			SessionID    string  `db:"session_id"`
			BoardName    *string `db:"board_name"`
			DisplayName  *string `db:"display_name"`
			IsSleeping   int     `db:"is_sleeping"`
			Prompt       *string `db:"prompt"`
			Model        *string `db:"model"`
			Capabilities *string `db:"capabilities"`
		}
		if err := h.db.SelectContext(ctx, &rows, "SELECT session_id, board_name, display_name, is_sleeping, prompt, model, capabilities FROM live_sessions"); err == nil {
			for _, r := range rows {
				if r.BoardName != nil {
					bn := *r.BoardName
					dn := ""
					if r.DisplayName != nil { dn = *r.DisplayName }
					liveBoardNames[r.SessionID] = [2]string{bn, dn}
				}
				if r.IsSleeping == 1 {
					liveSleeping[r.SessionID] = true
				}
				liveExtras[r.SessionID] = liveExtra{
					Prompt: r.Prompt, Model: r.Model, Capabilities: r.Capabilities,
				}
			}
		}
	}

	var sessions []map[string]any
	liveSIDs := make(map[string]bool) // track session IDs already in results
	for _, agent := range agents {
		logInfo := getLogStatus(agent.LogPath)

		status, _ := logInfo["status"].(string)
		summary, _ := logInfo["summary"].(string)
		staleness := logInfo["staleness_seconds"]

		sid := agent.SessionID

		// Resolve git state: try session_id first, then agent name
		var git *store.GitSnapshot
		if sid != "" {
			git = gitState[sid]
		}
		if git == nil {
			git = gitState[agent.AgentName]
		}

		// Resolve changed file count
		fc := 0
		if sid != "" {
			if c, ok := fileCounts[sid]; ok {
				fc = c
			}
		}
		if fc == 0 {
			if c, ok := fileCounts[agent.AgentName]; ok {
				fc = c
			}
		}

		// Resolve latest event type for waiting/working detection
		var latestEv, evSummary string
		if sid != "" {
			if ev, ok := latestEvents[sid]; ok {
				latestEv, evSummary = ev[0], ev[1]
			}
		}
		waiting := latestEv == "notification"
		done := latestEv == "stop"
		staleF, _ := staleness.(float64)
		working := (latestEv == "tool_use" || latestEv == "prompt_submit") && staleF < 120
		// Sleep loop detection: agent stuck in a sleep loop is not actually working
		if working && strings.HasPrefix(evSummary, "Ran: sleep") {
			working = false
		}

		// Summary fallback to latest goal
		if summary == "" && sid != "" {
			if goal, ok := latestGoals[sid]; ok {
				summary = goal
			}
		}

		// Board unread
		tmuxName := agent.TmuxSession
		boardSub := boardSubs[tmuxName]
		boardUnread := 0
		if boardSub != nil {
			boardUnread = allUnread[tmuxName]
		}

		var branchVal any
		if git != nil {
			branchVal = git.Branch
		}

		var iconVal any
		if ic, ok := icons[sid]; ok && sid != "" {
			iconVal = ic
		}

		entry := map[string]any{
			"name":               agent.AgentName,
			"agent_type":         agent.AgentType,
			"session_id":         sid,
			"tmux_session":       agent.TmuxSession,
			"status":             nilIfEmpty(status),
			"summary":            nilIfEmpty(summary),
			"staleness_seconds":  staleness,
			"working_directory":  agent.WorkingDir,
			"display_name":       displayNames[sid],
			"icon":               iconVal,
			"branch":             branchVal,
			"waiting_for_input":  waiting,
			"done":               done,
			"waiting_reason":     nilIf(!waiting, latestEv),
			"waiting_summary":    nilIf(!waiting, evSummary),
			"working":            working,
			"stuck":              false,
			"changed_file_count": fc,
			"commands":           map[string]string{"compress": "/compact", "clear": "/clear"},
			"board_project":      boardProject(boardSubs, liveBoardNames, tmuxName, sid),
			"board_job_title":    boardJobTitle(boardSubs, liveBoardNames, tmuxName, sid),
			"board_unread":       boardUnread,
			"log_path":           agent.LogPath,
			"sleeping":           liveSleeping[sid],
		}
		// Include prompt, model, and capabilities from live_sessions DB
		if extra, ok := liveExtras[sid]; ok {
			if extra.Prompt != nil {
				entry["prompt"] = *extra.Prompt
			}
			if extra.Model != nil && *extra.Model != "" {
				entry["model"] = *extra.Model
			}
			if extra.Capabilities != nil && *extra.Capabilities != "" && json.Valid([]byte(*extra.Capabilities)) {
				entry["capabilities"] = json.RawMessage(*extra.Capabilities)
			}
		}

		// Track status/summary for event deduplication
		h.trackStatusSummary(ctx, agent.AgentName, status, summary, sid)

		if sid != "" {
			liveSIDs[sid] = true
		}
		sessions = append(sessions, entry)
	}

	// Add placeholder entries for sleeping sessions without active tmux
	allLive, _ := h.ss.GetAllLiveSessions(ctx)
	for _, ls := range allLive {
		if ls.IsSleeping != 1 || liveSIDs[ls.SessionID] {
			continue
		}
		bp, dn := "", ""
		if ls.BoardName != nil {
			bp = *ls.BoardName
		}
		if ls.DisplayName != nil {
			dn = *ls.DisplayName
		}
		sessions = append(sessions, map[string]any{
			"name":               ls.AgentName,
			"agent_type":         ls.AgentType,
			"session_id":         ls.SessionID,
			"tmux_session":       nil,
			"status":             "Sleeping",
			"summary":            nil,
			"staleness_seconds":  nil,
			"working_directory":  ls.WorkingDir,
			"display_name":       dn,
			"icon":               ls.Icon,
			"branch":             nil,
			"waiting_for_input":  false,
			"done":               false,
			"waiting_reason":     nil,
			"waiting_summary":    nil,
			"working":            false,
			"stuck":              false,
			"changed_file_count": 0,
			"commands":           map[string]string{"compress": "/compact", "clear": "/clear"},
			"board_project":      bp,
			"board_job_title":    dn,
			"board_unread":       0,
			"log_path":           "",
			"sleeping":           true,
		})
		// Include prompt, model, and capabilities for sleeping sessions too
		entry := sessions[len(sessions)-1]
		if ls.Prompt != nil {
			entry["prompt"] = *ls.Prompt
		}
		if ls.Model != nil && *ls.Model != "" {
			entry["model"] = *ls.Model
		}
		if ls.Capabilities != nil && *ls.Capabilities != "" && json.Valid([]byte(*ls.Capabilities)) {
			entry["capabilities"] = json.RawMessage(*ls.Capabilities)
		}
	}

	writeJSON(w, http.StatusOK, emptyIfNil(sessions))
}

func (h *SessionsHandler) trackStatusSummary(ctx interface{}, agentName, status, summary, sessionID string) {
	h.lastKnownMu.Lock()
	defer h.lastKnownMu.Unlock()

	key := sessionID
	if key == "" {
		key = agentName
	}

	prev := h.lastKnown[key]
	if status != "" && status != prev.Status {
		// TODO: store.InsertAgentEvent(agentName, "status", status, sessionID)
	}
	if summary != "" && summary != prev.Summary {
		// TODO: store.InsertAgentEvent(agentName, "goal", summary, sessionID)
	}
	h.lastKnown[key] = lastKnownState{Status: status, Summary: summary}
}

// Detail returns detailed info for a specific live session.
// GET /api/sessions/live/{name}
func (h *SessionsHandler) Detail(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	agentType := r.URL.Query().Get("agent_type")
	sessionID := r.URL.Query().Get("session_id")

	logPath := h.findLogPath(agentType, sessionID)
	logInfo := getLogStatus(logPath)

	paneText, _ := h.terminal.CaptureOutput(r.Context(), name, 200, agentType, sessionID)

	writeJSON(w, http.StatusOK, map[string]any{
		"name":              name,
		"session_id":        sessionID,
		"status":            logInfo["status"],
		"summary":           logInfo["summary"],
		"recent_lines":      logInfo["recent_lines"],
		"staleness_seconds": logInfo["staleness_seconds"],
		"pane_capture":      paneText,
	})
}

// Capture returns a tmux pane capture for a session.
// GET /api/sessions/live/{name}/capture
func (h *SessionsHandler) Capture(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	agentType := r.URL.Query().Get("agent_type")
	sessionID := r.URL.Query().Get("session_id")

	text, err := h.terminal.CaptureOutput(r.Context(), name, 200, agentType, sessionID)
	if err != nil || text == "" {
		writeJSON(w, http.StatusOK, map[string]any{"name": name, "capture": nil, "error": "Could not capture pane"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"name": name, "capture": text})
}

// Poll returns capture, tasks, and events in a single batch response.
// GET /api/sessions/live/{name}/poll
func (h *SessionsHandler) Poll(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	agentType := r.URL.Query().Get("agent_type")
	sessionID := r.URL.Query().Get("session_id")
	eventsLimit := queryInt(r, "events_limit", 50)
	if eventsLimit > 200 {
		eventsLimit = 200
	}

	ctx := r.Context()

	// Capture pane
	captureResult := map[string]any{"name": name, "capture": nil}
	if text, err := h.terminal.CaptureOutput(ctx, name, 200, agentType, sessionID); err == nil && text != "" {
		captureResult["capture"] = text
	} else {
		captureResult["error"] = fmt.Sprintf("Could not capture pane for '%s'", name)
	}

	// Tasks
	var tasks any = []any{}
	if sessionID != "" {
		if t, err := h.ts.ListAgentTasks(ctx, name, &sessionID); err == nil {
			tasks = t
		}
	}

	// Events
	var events any = []any{}
	if e, err := h.ts.ListAgentEvents(ctx, name, eventsLimit, strPtr(sessionID)); err == nil {
		events = e
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"capture": captureResult,
		"tasks":   tasks,
		"events":  events,
	})
}

// Chat returns the JSONL conversation transcript.
// GET /api/sessions/live/{name}/chat
func (h *SessionsHandler) Chat(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	agentType := r.URL.Query().Get("agent_type")
	sessionID := r.URL.Query().Get("session_id")
	workingDir := r.URL.Query().Get("working_directory")

	if agentType == "" {
		agentType = at.Claude
	}

	// Use session_id if provided, otherwise use name as session_id
	id := sessionID
	if id == "" {
		id = name
	}

	// When after=0, the client wants the full conversation history (e.g. after
	// switching to the Chat tab). Return all accumulated messages, not just new ones.
	// Supports pagination: ?after=0&limit=100&offset=0 returns the last 100 messages.
	after, _ := strconv.Atoi(r.URL.Query().Get("after"))
	var messages []map[string]any
	var total int
	if after == 0 {
		messages, total = h.jsonl.ReadAllMessages(id, workingDir, agentType)
	} else {
		messages, total = h.jsonl.ReadNewMessages(id, workingDir, agentType)
	}
	messages = emptyIfNil(messages)

	// Pagination for full history: return the most recent `limit` messages,
	// with `offset` counting backwards from the end for "Load More".
	if after == 0 && len(messages) > 0 {
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
		if limit <= 0 {
			limit = 100 // default page size
		}
		// offset=0 means the most recent messages; offset=100 means skip the last 100
		end := len(messages) - offset
		start := end - limit
		if start < 0 {
			start = 0
		}
		if end < 0 {
			end = 0
		}
		hasMore := start > 0
		messages = messages[start:end]
		writeJSON(w, http.StatusOK, map[string]any{
			"messages": messages,
			"total":    total,
			"has_more": hasMore,
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"messages": messages, "total": total})
}

// Info returns enriched metadata for the session info modal.
// GET /api/sessions/live/{name}/info
func (h *SessionsHandler) Info(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	agentType := r.URL.Query().Get("agent_type")
	sessionID := r.URL.Query().Get("session_id")
	ctx := r.Context()

	pane, _ := h.terminal.FindSession(ctx, name, agentType, sessionID)

	result := map[string]any{
		"name":       name,
		"session_id": sessionID,
		"agent_name": name,
		"agent_type": agentType,
	}

	if pane != nil {
		result["tmux_session_name"] = pane.SessionName
		result["pane_title"] = pane.PaneTitle
		result["working_directory"] = pane.CurrentPath
		result["tmux_command"] = h.terminal.AttachCommand(pane.SessionName)
	}

	// Include log path and other metadata from live session record
	if sessionID != "" {
		if ls, err := h.ss.GetLiveSession(ctx, sessionID); err == nil && ls != nil {
			if ls.DisplayName != nil && *ls.DisplayName != "" {
				result["agent_name"] = *ls.DisplayName
			}
			result["agent_type"] = ls.AgentType
			// Construct log path from agent type + session ID
			logPath := naming.LogFile(h.cfg.LogDir, ls.AgentType, sessionID)
			result["log_path"] = logPath
		}
	}

	// Look up git state by session_id first, then by name
	var git *store.GitSnapshot
	if sessionID != "" {
		var gitErr error
		git, gitErr = h.gs.GetLatestGitStateBySession(ctx, sessionID)
		if gitErr != nil {
			slog.Warn("failed to get git state by session", "session_id", sessionID, "error", gitErr)
		}
	}
	if git == nil {
		var gitErr error
		git, gitErr = h.gs.GetLatestGitState(ctx, name)
		if gitErr != nil {
			slog.Warn("failed to get git state by name", "agent_name", name, "error", gitErr)
		}
	}
	if git != nil {
		result["git_branch"] = git.Branch
		result["git_commit_hash"] = git.CommitHash
		result["git_commit_subject"] = git.CommitSubject
	}

	// Include prompt and board info from live session record
	if sessionID != "" {
		if info, err := h.ss.GetLiveSessionPromptInfo(ctx, sessionID); err == nil && info != nil {
			if info.Prompt != nil {
				result["prompt"] = *info.Prompt
			}
			if info.BoardName != nil {
				result["board_name"] = *info.BoardName
			}
		}
	}

	writeJSON(w, http.StatusOK, result)
}

// ── Files / Git ─────────────────────────────────────────────────────────

// resolveWorkdir determines the working directory for an agent session.
func (h *SessionsHandler) resolveWorkdir(ctx context.Context, name, agentType, sessionID string) string {
	pane, _ := h.terminal.FindSession(ctx, name, agentType, sessionID)
	if pane != nil {
		if pane.CurrentPath != "" {
			return pane.CurrentPath
		}
	}
	if sessionID != "" {
		snap, err := h.gs.GetLatestGitStateBySession(ctx, sessionID)
		if err == nil && snap != nil {
			return snap.WorkingDirectory
		}
	}
	snap, err := h.gs.GetLatestGitState(ctx, name)
	if err == nil && snap != nil {
		return snap.WorkingDirectory
	}
	return ""
}

// resolveGitRoot returns the git toplevel for the agent's working directory.
func (h *SessionsHandler) resolveGitRoot(ctx context.Context, name, agentType, sessionID string) string {
	workdir := h.resolveWorkdir(ctx, name, agentType, sessionID)
	if workdir == "" {
		return ""
	}
	return gitutil.ResolveGitRoot(ctx, workdir)
}

// Files returns changed files for a live agent.
// GET /api/sessions/live/{name}/files
func (h *SessionsHandler) Files(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	sidPtr := querySessionID(r)
	files, err := h.gs.GetChangedFiles(r.Context(), name, sidPtr)
	if err != nil {
		files = []store.ChangedFile{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"agent_name": name, "files": files})
}

// RefreshFiles runs fresh git queries and merges agent Write/Edit events.
// POST /api/sessions/live/{name}/files/refresh
func (h *SessionsHandler) RefreshFiles(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	var body struct {
		SessionID string `json:"session_id"`
	}
	json.NewDecoder(r.Body).Decode(&body)

	workdir := h.resolveGitRoot(r.Context(), name, "", body.SessionID)
	if workdir == "" {
		writeJSON(w, http.StatusOK, map[string]any{"error": "Could not determine working directory", "files": []any{}})
		return
	}

	// Run git diff --numstat to get changed files
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	base := gitutil.GetDiffBase(ctx, workdir, h.getDiffMode(r.Context()))
	out, err := exec.CommandContext(ctx, "git", "-C", workdir, "diff", base, "--numstat").Output()
	fileMap := make(map[string]store.ChangedFile)
	if err != nil {
		slog.Warn("git diff --numstat failed", "agent_name", name, "workdir", workdir, "error", err)
	} else {
		for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
			if line == "" {
				continue
			}
			parts := strings.SplitN(line, "\t", 3)
			if len(parts) < 3 {
				continue
			}
			adds, _ := strconv.Atoi(parts[0])
			dels, _ := strconv.Atoi(parts[1])
			fileMap[parts[2]] = store.ChangedFile{Filepath: parts[2], Additions: adds, Deletions: dels, Status: "M"}
		}
	}

	// Include untracked files
	untrackedSet := make(map[string]bool)
	untrackedOut, err := exec.CommandContext(ctx, "git", "-C", workdir, "ls-files", "--others", "--exclude-standard").Output()
	if err != nil {
		slog.Warn("git ls-files (untracked) failed", "agent_name", name, "workdir", workdir, "error", err)
	} else {
		for _, f := range strings.Split(strings.TrimSpace(string(untrackedOut)), "\n") {
			if f == "" {
				continue
			}
			untrackedSet[f] = true
			if _, exists := fileMap[f]; !exists {
				fileMap[f] = store.ChangedFile{Filepath: f, Additions: 0, Deletions: 0, Status: "??"}
			}
		}
	}

	// Build tracked file set (one batch call instead of per-file git ls-files)
	trackedSet := make(map[string]bool)
	trackedOut, err := exec.CommandContext(ctx, "git", "-C", workdir, "ls-files").Output()
	if err != nil {
		slog.Warn("git ls-files (tracked) failed", "agent_name", name, "workdir", workdir, "error", err)
	} else {
		for _, f := range strings.Split(strings.TrimSpace(string(trackedOut)), "\n") {
			if f != "" {
				trackedSet[f] = true
			}
		}
	}

	// Merge in files from agent Write/Edit events
	sidPtr := strPtr(body.SessionID)
	events, _ := h.ts.ListAgentEvents(r.Context(), name, 200, sidPtr)
	for _, ev := range events {
		if ev.ToolName == nil || (*ev.ToolName != "Write" && *ev.ToolName != "Edit") {
			continue
		}
		if ev.DetailJSON == nil {
			continue
		}
		var detail map[string]any
		if err := json.Unmarshal([]byte(*ev.DetailJSON), &detail); err != nil {
			continue
		}
		fp, ok := detail["file_path"].(string)
		if !ok || fp == "" {
			continue
		}
		rel, err := filepath.Rel(workdir, fp)
		if err != nil || strings.HasPrefix(rel, "..") {
			continue
		}
		if _, exists := fileMap[rel]; !exists {
			// Skip files that no longer exist on disk
			fullPath := filepath.Join(workdir, rel)
			info, statErr := os.Stat(fullPath)
			if statErr != nil || info.IsDir() {
				continue
			}
			// Skip tracked files not in fileMap — they have no diff (clean)
			if trackedSet[rel] && !untrackedSet[rel] {
				continue
			}
			adds := 0
			data, err := os.ReadFile(fullPath)
			if err != nil {
				slog.Warn("failed to read file for line count", "path", fullPath, "error", err)
			} else {
				adds = strings.Count(string(data), "\n") + 1
			}
			fileMap[rel] = store.ChangedFile{Filepath: rel, Additions: adds, Deletions: 0, Status: "??"}
		}
	}

	// Update DB cache
	files := make([]store.ChangedFile, 0, len(fileMap))
	for _, f := range fileMap {
		files = append(files, f)
	}
	h.gs.ReplaceChangedFiles(r.Context(), name, workdir, files, sidPtr)

	writeJSON(w, http.StatusOK, map[string]any{"agent_name": name, "files": files})
}

// Diff returns the unified diff for a single file.
// GET /api/sessions/live/{name}/diff
func (h *SessionsHandler) Diff(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	fp := r.URL.Query().Get("filepath")
	sessionID := r.URL.Query().Get("session_id")

	if fp == "" || strings.HasPrefix(fp, "-") || strings.ContainsAny(fp, "\x00") {
		writeJSON(w, http.StatusOK, map[string]any{"error": "invalid filepath"})
		return
	}

	workdir := h.resolveGitRoot(r.Context(), name, "", sessionID)
	if workdir == "" {
		writeJSON(w, http.StatusOK, map[string]any{"error": "Could not determine working directory"})
		return
	}

	// Path traversal protection
	fullPath, _ := filepath.Abs(filepath.Join(workdir, fp))
	realWorkdir, _ := filepath.EvalSymlinks(workdir)
	realPath, _ := filepath.EvalSymlinks(fullPath)
	if realPath != "" && !strings.HasPrefix(realPath, realWorkdir+string(os.PathSeparator)) {
		writeJSON(w, http.StatusOK, map[string]any{"error": "path traversal not allowed"})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	base := gitutil.GetDiffBase(ctx, workdir, h.getDiffMode(r.Context()))
	out, err := exec.CommandContext(ctx, "git", "-C", workdir, "diff", base, "--", fp).Output()
	diffText := ""
	if err != nil {
		slog.Warn("git diff failed", "agent_name", name, "filepath", fp, "error", err)
	} else {
		diffText = string(out)
	}

	// For untracked files, show the file content as a "new file" diff
	if diffText == "" {
		fullPath := filepath.Join(workdir, fp)
		realFull, err := filepath.EvalSymlinks(fullPath)
		realWork, _ := filepath.EvalSymlinks(workdir)
		if err == nil && strings.HasPrefix(realFull, realWork+string(os.PathSeparator)) {
			if info, err := os.Stat(realFull); err == nil && !info.IsDir() {
				data, err := os.ReadFile(realFull)
				if err == nil {
					lines := strings.Split(string(data), "\n")
					diffText = fmt.Sprintf("diff --git a/%s b/%s\nnew file mode 100644\n--- /dev/null\n+++ b/%s\n@@ -0,0 +1,%d @@\n",
						fp, fp, fp, len(lines))
					for _, line := range lines {
						diffText += "+" + line + "\n"
					}
				}
			}
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{"filepath": fp, "diff": diffText, "working_directory": workdir})
}

// SearchFiles searches for files in the agent's working directory.
// GET /api/sessions/live/{name}/search-files
//
// Query parameters:
//
//	q          - search query (fuzzy mode)
//	dir        - directory path to list (directory browsing mode)
//	session_id - optional session identifier
//
// When 'dir' is provided, returns entries in that directory with type info
// (directory browsing mode). When 'q' is provided, returns fuzzy matches
// (search mode). When neither is provided, returns the first 50 files.
func (h *SessionsHandler) SearchFiles(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	query := strings.TrimSpace(strings.ToLower(r.URL.Query().Get("q")))
	dir := r.URL.Query().Get("dir")
	sessionID := r.URL.Query().Get("session_id")

	workdir := h.resolveGitRoot(r.Context(), name, "", sessionID)
	if workdir == "" {
		writeJSON(w, http.StatusOK, map[string]any{"files": []string{}})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	// Directory browsing mode: list entries in a specific directory
	if dir != "" {
		h.searchFilesDir(w, ctx, workdir, dir, query)
		return
	}

	out, err := exec.CommandContext(ctx, "git", "-C", workdir, "ls-files", "--cached", "--others", "--exclude-standard").Output()
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"files": []string{}})
		return
	}

	allFiles := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(allFiles) == 1 && allFiles[0] == "" {
		allFiles = nil
	}

	if query == "" {
		if len(allFiles) > 50 {
			allFiles = allFiles[:50]
		}
		writeJSON(w, http.StatusOK, map[string]any{"files": allFiles})
		return
	}

	// Score matches: basename match > basename contains > path contains
	type scored struct {
		score int
		path  string
	}
	var matches []scored
	for _, fp := range allFiles {
		fpLower := strings.ToLower(fp)
		basename := strings.ToLower(filepath.Base(fp))
		if strings.Contains(fpLower, query) {
			s := 2
			if basename == query {
				s = 0
			} else if strings.Contains(basename, query) {
				s = 1
			}
			matches = append(matches, scored{s, fp})
		}
	}
	// Sort by score then path
	sort.Slice(matches, func(i, j int) bool {
		if matches[i].score != matches[j].score {
			return matches[i].score < matches[j].score
		}
		return matches[i].path < matches[j].path
	})
	result := make([]string, 0, 50)
	for i, m := range matches {
		if i >= 50 {
			break
		}
		result = append(result, m.path)
	}
	writeJSON(w, http.StatusOK, map[string]any{"files": result})
}

// searchFilesDir lists filesystem entries in a specific directory for
// directory browsing mode. Uses os.ReadDir for direct filesystem access,
// like ls/tab-completion. Returns entries with name, path, and type
// (dir/file). Directories are listed first, then files. An optional
// filter (from the 'q' parameter) restricts results to matching names.
func (h *SessionsHandler) searchFilesDir(w http.ResponseWriter, _ context.Context, workdir, dir, filter string) {
	// Normalize dir: strip leading/trailing slashes, use "." for root
	dir = strings.TrimPrefix(dir, "/")
	dir = strings.TrimSuffix(dir, "/")
	if dir == "" {
		dir = "."
	}

	// Prevent path traversal
	if strings.Contains(dir, "..") {
		writeJSON(w, http.StatusOK, map[string]any{"entries": []any{}})
		return
	}

	// Build the absolute path to read
	absDir := workdir
	if dir != "." {
		absDir = filepath.Join(workdir, dir)
	}

	dirEntries, err := os.ReadDir(absDir)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"entries": []any{}, "dir": dir})
		return
	}

	type entry struct {
		Name string `json:"name"`
		Path string `json:"path"`
		Type string `json:"type"` // "dir" or "file"
	}

	var dirs []entry
	var files []entry
	showHidden := strings.HasPrefix(filter, ".")

	for _, de := range dirEntries {
		name := de.Name()

		// Skip hidden files/dirs unless filter starts with .
		if !showHidden && strings.HasPrefix(name, ".") {
			continue
		}

		// Skip non-text files and files over 1MB
		if !de.IsDir() {
			if !isTextFile(name) {
				continue
			}
			if info, err := de.Info(); err == nil && info.Size() > 1<<20 {
				continue
			}
		}

		// Apply filter if provided
		if filter != "" && !strings.Contains(strings.ToLower(name), filter) {
			continue
		}

		entryPath := name
		if dir != "." {
			entryPath = dir + "/" + name
		}

		if de.IsDir() {
			dirs = append(dirs, entry{
				Name: name + "/",
				Path: entryPath,
				Type: "dir",
			})
		} else {
			files = append(files, entry{
				Name: name,
				Path: entryPath,
				Type: "file",
			})
		}
	}

	// Sort dirs and files alphabetically by name (case-insensitive)
	sort.Slice(dirs, func(i, j int) bool { return strings.ToLower(dirs[i].Name) < strings.ToLower(dirs[j].Name) })
	sort.Slice(files, func(i, j int) bool { return strings.ToLower(files[i].Name) < strings.ToLower(files[j].Name) })

	// Combine: directories first, then files
	entries := make([]entry, 0, len(dirs)+len(files))
	entries = append(entries, dirs...)
	entries = append(entries, files...)

	// Limit to 100 entries
	if len(entries) > 100 {
		entries = entries[:100]
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"entries": entries,
		"dir":     dir,
	})
}

// textExtensions is a whitelist of file extensions known to be text/source
// files safe to preview in the web UI. Files not matching this list are
// excluded from directory browsing results to prevent loading binary files.
var textExtensions = map[string]bool{
	// Go
	".go": true, ".mod": true, ".sum": true,
	// JavaScript / TypeScript
	".js": true, ".ts": true, ".tsx": true, ".jsx": true, ".mjs": true, ".cjs": true,
	// Web
	".html": true, ".htm": true, ".css": true, ".scss": true, ".less": true,
	// Data / Config
	".json": true, ".yaml": true, ".yml": true, ".toml": true, ".xml": true,
	".csv": true, ".ini": true, ".cfg": true, ".conf": true, ".properties": true,
	// Documentation
	".md": true, ".txt": true, ".rst": true, ".adoc": true,
	// Python
	".py": true, ".pyi": true, ".pyx": true,
	// Ruby
	".rb": true, ".erb": true, ".rake": true, ".gemspec": true,
	// Rust
	".rs": true,
	// C / C++
	".c": true, ".h": true, ".cpp": true, ".hpp": true, ".cc": true, ".hh": true,
	// Java / JVM
	".java": true, ".kt": true, ".kts": true, ".scala": true, ".gradle": true,
	// Shell
	".sh": true, ".bash": true, ".zsh": true, ".fish": true,
	// SQL / Query
	".sql": true, ".graphql": true, ".gql": true,
	// Protocol / Schema
	".proto": true, ".avro": true, ".thrift": true,
	// Environment / Config files
	".env": true, ".envrc": true,
	".gitignore": true, ".gitattributes": true, ".gitmodules": true,
	".dockerignore": true, ".editorconfig": true,
	".eslintrc": true, ".prettierrc": true, ".babelrc": true,
	// Swift / Objective-C
	".swift": true, ".m": true, ".mm": true,
	// Other
	".log": true, ".lock": true, ".patch": true, ".diff": true,
	".tf": true, ".hcl": true, // Terraform
	".lua": true, ".vim": true, ".el": true,
	".r": true, ".R": true, ".jl": true, // R, Julia
	".ex": true, ".exs": true, // Elixir
	".hs": true, ".cabal": true, // Haskell
	".pl": true, ".pm": true, // Perl
	".php": true, ".twig": true,
	".dart": true, ".svelte": true, ".vue": true,
	".nix": true, ".dhall": true,
	".tmpl": true, ".tpl": true, // Go templates
	".plist": true, // macOS property lists
}

// textFileNames are known text files without extensions.
var textFileNames = map[string]bool{
	"Makefile": true, "Dockerfile": true, "Containerfile": true,
	"LICENSE": true, "README": true, "CHANGELOG": true,
	"Gemfile": true, "Rakefile": true, "Procfile": true,
	"Vagrantfile": true, "Brewfile": true,
	"CLAUDE.md": true, "MEMORY.md": true,
	".gitignore": true, ".gitattributes": true, ".gitmodules": true,
	".dockerignore": true, ".editorconfig": true,
	".eslintrc": true, ".prettierrc": true,
}

// isTextFile returns true if the filename has a known text file extension
// or is a known extensionless text file.
func isTextFile(name string) bool {
	if textFileNames[name] {
		return true
	}
	ext := strings.ToLower(filepath.Ext(name))
	if ext == "" {
		return false
	}
	return textExtensions[ext]
}

// Git returns recent git snapshots for a live agent.
// GET /api/sessions/live/{name}/git
func (h *SessionsHandler) Git(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	sessionID := r.URL.Query().Get("session_id")
	limit := queryInt(r, "limit", 20)
	if limit > 100 {
		limit = 100
	}

	var snapshots []store.GitSnapshot
	var err error
	if sessionID != "" {
		snapshots, err = h.gs.GetGitSnapshotsForSession(r.Context(), sessionID, limit)
	} else {
		snapshots, err = h.gs.GetGitSnapshots(r.Context(), name, limit)
	}
	if err != nil || snapshots == nil {
		snapshots = []store.GitSnapshot{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"agent_name": name, "snapshots": snapshots})
}

// ── Commands ────────────────────────────────────────────────────────────

// Send sends a command to a live tmux session.
// POST /api/sessions/live/{name}/send
func (h *SessionsHandler) Send(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	var body struct {
		Command   string `json:"command"`
		AgentType string `json:"agent_type"`
		SessionID string `json:"session_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		errBadRequest(w, "invalid JSON")
		return
	}
	if body.Command == "" {
		errBadRequest(w, "No command provided")
		return
	}

	if err := h.terminal.SendInput(r.Context(), name, body.Command, body.AgentType, body.SessionID); err != nil {
		errInternalServer(w, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "command": body.Command})
}

// Keys sends raw tmux key names to a session.
// POST /api/sessions/live/{name}/keys
func (h *SessionsHandler) Keys(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	var body struct {
		Keys      []string `json:"keys"`
		AgentType string   `json:"agent_type"`
		SessionID string   `json:"session_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || len(body.Keys) == 0 {
		errBadRequest(w, "keys must be a non-empty list")
		return
	}

	if err := h.terminal.SendRawInput(r.Context(), name, body.Keys, body.AgentType, body.SessionID); err != nil {
		errInternalServer(w, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "keys": body.Keys})
}

// Resize resizes the tmux pane width.
// POST /api/sessions/live/{name}/resize
func (h *SessionsHandler) Resize(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	var body struct {
		Columns   int    `json:"columns"`
		AgentType string `json:"agent_type"`
		SessionID string `json:"session_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Columns < 10 {
		errBadRequest(w, "columns must be >= 10")
		return
	}

	if err := h.terminal.ResizeSession(r.Context(), name, body.Columns, body.AgentType, body.SessionID); err != nil {
		errInternalServer(w, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "columns": body.Columns})
}

// ── Lifecycle ───────────────────────────────────────────────────────────

// Kill terminates a tmux session.
// POST /api/sessions/live/{name}/kill
func (h *SessionsHandler) Kill(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	var body struct {
		AgentType string `json:"agent_type"`
		SessionID string `json:"session_id"`
	}
	json.NewDecoder(r.Body).Decode(&body)

	slog.Info("killing agent", "agent_name", name, "session_id", body.SessionID, "agent_type", body.AgentType)

	// Use a background context for DB operations so they complete even if
	// the HTTP request context is cancelled (e.g., during tmux command failures).
	bgCtx := context.Background()

	// Look up the live session before deleting so we can clean up board state
	var boardName string
	var wasSleeping bool
	if body.SessionID != "" {
		if ls, err := h.ss.GetLiveSession(bgCtx, body.SessionID); err == nil && ls != nil {
			if ls.BoardName != nil {
				boardName = *ls.BoardName
			}
			wasSleeping = ls.IsSleeping == 1
		}
	}

	// Unregister from DB BEFORE killing the tmux/pty session.
	// This ordering is critical: KillSession may hang or take a long time
	// (e.g., if the tmux socket is in a bad state), and we must ensure the
	// DB row is removed so sleeping sessions don't reappear on server restart.
	if body.SessionID != "" {
		if err := h.ss.UnregisterLiveSession(bgCtx, body.SessionID); err != nil {
			log.Printf("[kill] failed to unregister session %s: %v", body.SessionID, err)
		}
	}

	// If this was the last session on a board, clear board pause state.
	// This is especially important for sleeping teams: the board is paused
	// during sleep, and killing sleeping sessions must unpause it.
	if boardName != "" && h.boardHandler != nil {
		remaining, _ := h.ss.CountBoardSessions(bgCtx, boardName)
		if remaining == 0 {
			h.boardHandler.SetPaused(boardName, false)
			if wasSleeping {
				log.Printf("[kill] cleared board pause state for sleeping board %q (no remaining sessions)", boardName)
			}
		}
	}

	// Kill tmux/pty session (may fail if sleeping — that's expected)
	h.terminal.KillSession(r.Context(), name, body.AgentType, body.SessionID)

	// Clean up board state file so it doesn't accumulate over time
	removeBoardStateFile(name, h.cfg)

	// Clean up temp files (system prompts, settings, action prompts)
	if body.SessionID != "" {
		agent.CleanupTempFiles(body.SessionID)
	}

	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// Restart restarts the agent session.
// POST /api/sessions/live/{name}/restart
func (h *SessionsHandler) Restart(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	var body struct {
		AgentType    string              `json:"agent_type"`
		ExtraFlags   string              `json:"extra_flags"`
		SessionID    string              `json:"session_id"`
		Prompt       string              `json:"prompt"`
		Model        string              `json:"model"`
		Capabilities *agent.Capabilities `json:"capabilities"`
	}
	json.NewDecoder(r.Body).Decode(&body)

	ctx := r.Context()
	agentType := body.AgentType
	if agentType == "" {
		agentType = at.Claude
	}

	// Skip sleeping sessions — they should be woken via the wake endpoint, not restarted
	if body.SessionID != "" {
		if ls, err := h.ss.GetLiveSession(ctx, body.SessionID); err == nil && ls != nil && ls.IsSleeping == 1 {
			errBadRequest(w, "Session is sleeping. Use wake endpoint to resume.")
			return
		}
	}

	pane, err := h.terminal.FindSession(ctx, name, agentType, body.SessionID)
	if err != nil || pane == nil {
		errNotFound(w, "Pane not found")
		return
	}

	newSessionID := generateUUID()
	newSessionName := naming.SessionName(agentType, newSessionID)
	newLogPath := naming.LogFile(h.cfg.LogDir, agentType, newSessionID)

	// Close old pipe-pane, respawn, rename
	h.terminal.StopLogging(ctx, pane.Target)
	if err := h.terminal.RestartPane(ctx, pane.Target, pane.CurrentPath); err != nil {
		errInternalServer(w, err.Error())
		return
	}
	if err := h.terminal.RenameSession(ctx, pane.SessionName, newSessionName); err != nil {
		errInternalServer(w, err.Error())
		return
	}

	target := fmt.Sprintf("%s:0.0", newSessionName)
	time.Sleep(500 * time.Millisecond)

	// Clear scrollback, create log, setup pipe-pane
	h.terminal.ClearHistory(ctx, target)
	os.WriteFile(newLogPath, []byte{}, 0644)
	h.terminal.StartLogging(ctx, target, newLogPath)

	// Set pane title using native tmux command (avoids shell echo)
	folderName := filepath.Base(strings.TrimRight(pane.CurrentPath, "/"))
	h.terminal.SetPaneTitle(ctx, target, fmt.Sprintf("%s — %s", folderName, agentType))

	// Load stored config from the DB
	agentImpl := agent.GetAgent(agentType)
	var storedFlags []string
	var storedPrompt, storedBoard, storedBoardServer, storedDisplayName, storedBoardType, storedModel string
	var storedCaps *agent.Capabilities
	var storedCapsJSON *string
	if body.SessionID != "" {
		if ls, err := h.ss.GetLiveSession(ctx, body.SessionID); err == nil && ls != nil {
			storedFlags = store.UnmarshalFlags(ls.Flags)
			storedPrompt = derefStrPtr(ls.Prompt)
			storedBoard = derefStrPtr(ls.BoardName)
			storedBoardServer = derefStrPtr(ls.BoardServer)
			storedDisplayName = derefStrPtr(ls.DisplayName)
			storedBoardType = derefStrPtr(ls.BoardType)
			storedModel = derefStrPtr(ls.Model)
			storedCapsJSON = ls.Capabilities
			if ls.Capabilities != nil && *ls.Capabilities != "" {
				storedCaps = &agent.Capabilities{}
				json.Unmarshal([]byte(*ls.Capabilities), storedCaps)
			}
		}
	}

	// Request body overrides stored values (user edited in modal)
	if body.Prompt != "" {
		storedPrompt = body.Prompt
	}
	if body.Model != "" {
		storedModel = body.Model
	}
	if body.Capabilities != nil {
		storedCaps = body.Capabilities
		storedCapsJSON = store.MarshalCapabilities(body.Capabilities)
	}

	// When capabilities are available, strip agent-specific permission flags
	// from the stored flags — capabilities are the source of truth and
	// BuildLaunchCommand will generate the correct flags for the target agent type.
	// This prevents flag mismatches when changing agent type (e.g. Codex --full-auto → Claude).
	agentPermFlags := map[string]bool{
		"--full-auto": true, "--dangerously-skip-permissions": true,
		"--dangerously-bypass-approvals-and-sandbox": true,
		"--sandbox": true, "--search": true,
		"--approval-mode": true, "--yolo": true,
	}
	var cleanFlags []string
	skipNext := false
	for _, f := range storedFlags {
		if skipNext {
			skipNext = false
			continue
		}
		if agentPermFlags[f] {
			// Some flags take a value argument (e.g. --sandbox workspace-write, -a untrusted)
			if f == "--sandbox" || f == "--approval-mode" || f == "-a" {
				skipNext = true
			}
			continue
		}
		if f == "-a" {
			skipNext = true
			continue
		}
		cleanFlags = append(cleanFlags, f)
	}
	// Also strip --model from old flags (we'll re-add from storedModel)
	var finalFlags []string
	for i := 0; i < len(cleanFlags); i++ {
		if cleanFlags[i] == "--model" || cleanFlags[i] == "-m" {
			i++ // skip the value
			continue
		}
		finalFlags = append(finalFlags, cleanFlags[i])
	}
	if storedModel != "" {
		finalFlags = append(finalFlags, "--model", storedModel)
	}
	allFlags := append(finalFlags, strings.Fields(body.ExtraFlags)...)

	role := naming.SubscriberID(storedDisplayName, agentType)

	userSettings, _ := h.ss.GetSettings(ctx)

	cmd := agent.WrapWithBundlePath(agentImpl.BuildLaunchCommand(agent.LaunchParams{
		SessionID:       newSessionID,
		SessionName:     newSessionName,
		ProtocolPath:    h.protocolPath(),
		Flags:           allFlags,
		WorkingDir:      pane.CurrentPath,
		BoardName:       storedBoard,
		Role:            role,
		Prompt:          storedPrompt,
		PromptOverrides: promptOverrides(userSettings),
		BoardType:       storedBoardType,
		Capabilities:    storedCaps,
	}))
	log.Printf("[launch] restart session=%s cmd=%s", target, cmd)
	h.terminal.SendToTarget(ctx, target, cmd)

	// Replace live session in DB (carry forward stored fields)
	h.ss.ReplaceLiveSession(ctx, body.SessionID, &store.LiveSession{
		SessionID:    newSessionID,
		AgentType:    agentType,
		AgentName:    folderName,
		WorkingDir:   pane.CurrentPath,
		ResumeFromID: strPtr(body.SessionID),
		Flags:        store.MarshalFlags(allFlags),
		Prompt:       strPtr(storedPrompt),
		BoardName:    strPtr(storedBoard),
		BoardServer:  strPtr(storedBoardServer),
		BoardType:    strPtr(storedBoardType),
		Capabilities: storedCapsJSON,
		Model:        strPtr(storedModel),
	})
	h.ss.MigrateDisplayName(ctx, body.SessionID, newSessionID)

	// Re-subscribe to board if needed
	if storedBoard != "" {
		h.setupBoardAndPrompt(newSessionID, newSessionName, agentType, storedBoard, storedDisplayName)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"ok": true, "session_id": newSessionID, "session_name": newSessionName,
	})
}

// Resume restarts with --resume to continue a historical session.
// POST /api/sessions/live/{name}/resume
func (h *SessionsHandler) Resume(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	var body struct {
		SessionID        string `json:"session_id"`
		AgentType        string `json:"agent_type"`
		CurrentSessionID string `json:"current_session_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.SessionID == "" {
		errBadRequest(w, "session_id is required")
		return
	}

	agentType := body.AgentType
	if agentType == "" {
		agentType = at.Claude
	}
	agentImpl := agent.GetAgent(agentType)
	if !agentImpl.SupportsResume() {
		errBadRequest(w, fmt.Sprintf("Resume not supported for %s", agentType))
		return
	}

	ctx := r.Context()

	// Edition limits: check max live agents before resuming
	if h.cfg.MaxLiveAgents > 0 {
		count, err := h.ss.CountLiveSessions(ctx)
		if err == nil && count >= h.cfg.MaxLiveAgents {
			writeJSON(w, http.StatusForbidden, map[string]string{
				"error": fmt.Sprintf("Demo limit reached: maximum %d concurrent agents allowed", h.cfg.MaxLiveAgents),
			})
			return
		}
	}

	pane, _ := h.terminal.FindSession(ctx, name, agentType, body.CurrentSessionID)
	if pane == nil {
		errNotFound(w, "Pane not found")
		return
	}

	slog.Info("resuming agent", "agent_name", name, "session_id", body.SessionID, "agent_type", agentType)

	// Prepare resume files
	agent.TryPrepareResume(agentImpl, body.SessionID, pane.CurrentPath)

	newSessionID := generateUUID()
	newSessionName := naming.SessionName(agentType, newSessionID)
	newLogPath := naming.LogFile(h.cfg.LogDir, agentType, newSessionID)

	// Load stored fields from current session
	var storedPrompt, storedBoard, storedBoardServer, storedDisplayName, storedBoardType string
	if body.CurrentSessionID != "" {
		if ls, err := h.ss.GetLiveSession(ctx, body.CurrentSessionID); err == nil && ls != nil {
			storedPrompt = derefStrPtr(ls.Prompt)
			storedBoard = derefStrPtr(ls.BoardName)
			storedBoardServer = derefStrPtr(ls.BoardServer)
			storedDisplayName = derefStrPtr(ls.DisplayName)
			storedBoardType = derefStrPtr(ls.BoardType)
		}
	}

	role := naming.SubscriberID(storedDisplayName, agentType)

	userSettings, _ := h.ss.GetSettings(ctx)

	h.terminal.StopLogging(ctx, pane.Target)
	h.terminal.RestartPane(ctx, pane.Target, pane.CurrentPath)
	h.terminal.RenameSession(ctx, pane.SessionName, newSessionName)

	target := fmt.Sprintf("%s:0.0", newSessionName)
	time.Sleep(500 * time.Millisecond)
	h.terminal.ClearHistory(ctx, target)
	os.WriteFile(newLogPath, []byte{}, 0644)
	h.terminal.StartLogging(ctx, target, newLogPath)

	cmd := agent.WrapWithBundlePath(agentImpl.BuildLaunchCommand(agent.LaunchParams{
		SessionID:       newSessionID,
		SessionName:     newSessionName,
		ProtocolPath:    h.protocolPath(),
		ResumeSessionID: body.SessionID,
		WorkingDir:      pane.CurrentPath,
		BoardName:       storedBoard,
		Role:            role,
		Prompt:          storedPrompt,
		PromptOverrides: promptOverrides(userSettings),
		BoardType:       storedBoardType,
	}))
	h.terminal.SendToTarget(ctx, target, cmd)

	h.ss.ReplaceLiveSession(ctx, body.CurrentSessionID, &store.LiveSession{
		SessionID:    newSessionID,
		AgentType:    agentType,
		AgentName:    filepath.Base(strings.TrimRight(pane.CurrentPath, "/")),
		WorkingDir:   pane.CurrentPath,
		ResumeFromID: strPtr(body.SessionID),
		Prompt:       strPtr(storedPrompt),
		BoardName:    strPtr(storedBoard),
		BoardServer:  strPtr(storedBoardServer),
		BoardType:    strPtr(storedBoardType),
	})

	// Re-subscribe to board — subscriber_id (role) is stable, so the cursor persists automatically.
	if storedBoard != "" {
		h.setupBoardAndPrompt(newSessionID, newSessionName, agentType, storedBoard, storedDisplayName)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"ok": true, "session_id": newSessionID, "session_name": newSessionName,
	})
}

// Attach opens a native terminal attached to the tmux session.
// POST /api/sessions/live/{name}/attach
func (h *SessionsHandler) Attach(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	var body struct {
		AgentType string `json:"agent_type"`
		SessionID string `json:"session_id"`
	}
	json.NewDecoder(r.Body).Decode(&body)

	pane, _ := h.terminal.FindSession(r.Context(), name, body.AgentType, body.SessionID)
	if pane == nil {
		errNotFound(w, "Pane not found")
		return
	}

	// Open Terminal.app attached to the tmux session (macOS)
	attachCmd := h.terminal.AttachCommand(pane.SessionName)
	go func() {
		cmd := fmt.Sprintf(`tell application "Terminal" to do script "%s"`, attachCmd)
		exec.Command("osascript", "-e", cmd).Run()
	}()

	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// SetDisplayName sets the display name for a live session.
// PUT /api/sessions/live/{name}/display-name
func (h *SessionsHandler) SetDisplayName(w http.ResponseWriter, r *http.Request) {
	var body struct {
		DisplayName string `json:"display_name"`
		SessionID   string `json:"session_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		errBadRequest(w, "invalid JSON")
		return
	}
	if body.SessionID == "" || body.DisplayName == "" {
		errBadRequest(w, "session_id and display_name required")
		return
	}

	if err := h.ss.SetDisplayName(r.Context(), body.SessionID, body.DisplayName); err != nil {
		errInternalServer(w, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "display_name": body.DisplayName})
}

// Launch creates a new agent session.
// POST /api/sessions/launch
func (h *SessionsHandler) Launch(w http.ResponseWriter, r *http.Request) {
	var body struct {
		WorkingDir   string             `json:"working_dir"`
		AgentType    string             `json:"agent_type"`
		DisplayName  string             `json:"display_name"`
		Flags        []string           `json:"flags"`
		Prompt       string             `json:"prompt"`
		BoardName    string             `json:"board_name"`
		BoardServer  string             `json:"board_server"`
		Backend      string             `json:"backend"`
		BoardType    string             `json:"board_type"`
		Model        string             `json:"model"`
		Capabilities *agent.Capabilities `json:"capabilities"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		errBadRequest(w, "invalid JSON")
		return
	}
	if body.WorkingDir == "" {
		errBadRequest(w, "working_dir is required")
		return
	}

	// Edition limits: check max live agents
	if h.cfg.MaxLiveAgents > 0 {
		count, err := h.ss.CountLiveSessions(r.Context())
		if err == nil && count >= h.cfg.MaxLiveAgents {
			writeJSON(w, http.StatusForbidden, map[string]string{
				"error": fmt.Sprintf("Demo limit reached: maximum %d concurrent agents allowed", h.cfg.MaxLiveAgents),
			})
			return
		}
	}

	// Add model flag if specified
	launchFlags := body.Flags
	if body.Model != "" {
		launchFlags = append(append([]string{}, body.Flags...), "--model", body.Model)
	}
	result, err := h.launchSession(r.Context(), body.WorkingDir, body.AgentType, body.DisplayName,
		"", launchFlags, body.Prompt, body.BoardName, body.BoardServer, body.Backend, body.BoardType, body.Model, body.Capabilities)
	if err != nil {
		errInternalServer(w, err.Error())
		return
	}

	// Setup board subscription in background (prompt is passed as CLI arg, not via tmux send-keys)
	if body.BoardName != "" {
		h.setupBoardAndPrompt(result["session_id"].(string), result["session_name"].(string),
			body.AgentType, body.BoardName, body.DisplayName)
	}

	tracking.TrackEvent("session_launched", nil)
	writeJSON(w, http.StatusOK, result)
}

// LaunchTeam launches multiple agents on a shared message board.
// POST /api/sessions/launch-team
func (h *SessionsHandler) LaunchTeam(w http.ResponseWriter, r *http.Request) {
	var body struct {
		BoardName   string   `json:"board_name"`
		WorkingDir  string   `json:"working_dir"`
		AgentType   string   `json:"agent_type"`
		Flags       []string `json:"flags"`
		BoardServer string   `json:"board_server"`
		Backend     string   `json:"backend"`
		BoardType   string   `json:"board_type"`
		Agents []struct {
			Name         string              `json:"name"`
			Prompt       string              `json:"prompt"`
			Capabilities *agent.Capabilities `json:"capabilities"`
			AgentType    string              `json:"agent_type"`
			Model        string              `json:"model"`
		} `json:"agents"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		errBadRequest(w, "invalid JSON")
		return
	}
	if body.BoardName == "" || body.WorkingDir == "" || len(body.Agents) == 0 {
		errBadRequest(w, "board_name, working_dir, and agents required")
		return
	}

	ctx := r.Context()

	// Edition limits: check max live teams
	if h.cfg.MaxLiveTeams > 0 {
		teamCount, err := h.ss.CountLiveTeams(ctx)
		if err == nil && teamCount >= h.cfg.MaxLiveTeams {
			writeJSON(w, http.StatusForbidden, map[string]string{
				"error": fmt.Sprintf("Demo limit reached: maximum %d team allowed", h.cfg.MaxLiveTeams),
			})
			return
		}
	}

	// Edition limits: check max live agents
	if h.cfg.MaxLiveAgents > 0 {
		agentCount, err := h.ss.CountLiveSessions(ctx)
		if err == nil && agentCount+len(body.Agents) > h.cfg.MaxLiveAgents {
			writeJSON(w, http.StatusForbidden, map[string]string{
				"error": fmt.Sprintf("Demo limit reached: maximum %d concurrent agents allowed", h.cfg.MaxLiveAgents),
			})
			return
		}
	}

	var launched []map[string]any

	for _, agentDef := range body.Agents {
		if agentDef.Name == "" {
			continue
		}

		// Per-agent type/model override team-level defaults
		agentType := body.AgentType
		if agentDef.AgentType != "" {
			agentType = agentDef.AgentType
		}

		// Build per-agent flags: start with team-level, add model if specified
		agentFlags := make([]string, len(body.Flags))
		copy(agentFlags, body.Flags)
		if agentDef.Model != "" {
			agentFlags = append(agentFlags, "--model", agentDef.Model)
		}

		result, err := h.launchSession(ctx, body.WorkingDir, agentType, agentDef.Name,
			"", agentFlags, agentDef.Prompt, body.BoardName, body.BoardServer, body.Backend, body.BoardType, agentDef.Model, agentDef.Capabilities)
		if err != nil {
			log.Printf("[launch-team] failed to launch agent %s: %v", agentDef.Name, err)
			launched = append(launched, map[string]any{"name": agentDef.Name, "error": err.Error()})
			continue
		}

		// Board subscription handled by setupBoardAndPrompt (prompt passed as CLI arg)
		if body.BoardName != "" {
			h.setupBoardAndPrompt(result["session_id"].(string), result["session_name"].(string),
				agentType, body.BoardName, agentDef.Name)
		}

		launched = append(launched, map[string]any{
			"name": agentDef.Name, "session_id": result["session_id"], "session_name": result["session_name"],
		})
	}

	tracking.TrackEvent("team_launched", map[string]string{"agent_count": fmt.Sprintf("%d", len(launched))})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "board": body.BoardName, "agents": launched})
}

// ResetTeam kills all agents on a board and re-launches them with their
// original prompts and configuration. Each agent gets a fresh context.
// POST /api/sessions/live/team/{boardName}/reset
func (h *SessionsHandler) ResetTeam(w http.ResponseWriter, r *http.Request) {
	boardName := chi.URLParam(r, "boardName")
	if boardName == "" {
		errBadRequest(w, "boardName is required")
		return
	}

	ctx := r.Context()
	bgCtx := context.Background()

	// Get all sessions on this board
	sessions, err := h.ss.GetBoardSessions(ctx, boardName)
	if err != nil {
		errInternalServer(w, "failed to get board sessions: "+err.Error())
		return
	}
	if len(sessions) == 0 {
		errBadRequest(w, "no agents found on board: "+boardName)
		return
	}

	// Save each agent's config before killing
	type agentConfig struct {
		DisplayName  *string
		WorkingDir   string
		AgentType    string
		Flags        *string
		Prompt       *string
		BoardServer  *string
		BoardType    *string
		Icon         *string
		Capabilities *string
		Model        *string
	}
	configs := make([]agentConfig, 0, len(sessions))
	for _, s := range sessions {
		dn := s.DisplayName
		// Fallback: if display_name is nil in live_sessions, check session_meta
		if dn == nil {
			if metaName, err := h.ss.GetDisplayName(ctx, s.SessionID); err == nil && metaName != nil {
				dn = metaName
			}
		}
		configs = append(configs, agentConfig{
			DisplayName:  dn,
			WorkingDir:   s.WorkingDir,
			AgentType:    s.AgentType,
			Flags:        s.Flags,
			Prompt:       s.Prompt,
			BoardServer:  s.BoardServer,
			BoardType:    s.BoardType,
			Icon:         s.Icon,
			Capabilities: s.Capabilities,
			Model:        s.Model,
		})
	}

	// Kill all agents and unsubscribe from board
	for _, s := range sessions {
		h.ss.UnregisterLiveSession(bgCtx, s.SessionID)
		h.terminal.KillSession(bgCtx, s.AgentName, s.AgentType, s.SessionID)
		removeBoardStateFile(s.AgentName, h.cfg)
		// Mark subscriber as inactive during reset; re-launch will reactivate.
		if h.bs != nil && s.DisplayName != nil && *s.DisplayName != "" {
			h.bs.Unsubscribe(bgCtx, boardName, *s.DisplayName)
		}
	}

	// Clear board pause state
	if h.boardHandler != nil {
		h.boardHandler.SetPaused(boardName, false)
	}

	// Brief pause to let tmux sessions clean up
	time.Sleep(500 * time.Millisecond)

	// Re-launch each agent with original config
	var launched []map[string]any
	for _, cfg := range configs {
		var flags []string
		if cfg.Flags != nil && *cfg.Flags != "" {
			json.Unmarshal([]byte(*cfg.Flags), &flags)
		}
		prompt := ""
		if cfg.Prompt != nil {
			prompt = *cfg.Prompt
		}
		boardServer := ""
		if cfg.BoardServer != nil {
			boardServer = *cfg.BoardServer
		}
		boardType := ""
		if cfg.BoardType != nil {
			boardType = *cfg.BoardType
		}
		displayName := ""
		if cfg.DisplayName != nil {
			displayName = *cfg.DisplayName
		}

		// Restore model and capabilities from saved config
		modelStr := ""
		if cfg.Model != nil {
			modelStr = *cfg.Model
		}
		var caps *agent.Capabilities
		if cfg.Capabilities != nil && *cfg.Capabilities != "" {
			caps = &agent.Capabilities{}
			json.Unmarshal([]byte(*cfg.Capabilities), caps)
		}
		// Add model flag if stored
		if modelStr != "" {
			flags = append(flags, "--model", modelStr)
		}
		result, err := h.launchSession(bgCtx, cfg.WorkingDir, cfg.AgentType, displayName,
			"", flags, prompt, boardName, boardServer, "", boardType, modelStr, caps)
		if err != nil {
			log.Printf("[reset-team] failed to re-launch %s: %v", displayName, err)
			launched = append(launched, map[string]any{"name": displayName, "error": err.Error()})
			continue
		}

		// Re-setup board subscription
		h.setupBoardAndPrompt(result["session_id"].(string), result["session_name"].(string),
			cfg.AgentType, boardName, displayName)

		launched = append(launched, map[string]any{
			"name":         displayName,
			"session_id":   result["session_id"],
			"session_name": result["session_name"],
		})
	}

	log.Printf("[reset-team] reset board %q: %d agents", boardName, len(launched))
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "board": boardName, "agents": launched})
}

// ── Tasks ───────────────────────────────────────────────────────────────

func (h *SessionsHandler) ListTasks(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	sidPtr := querySessionID(r)
	tasks, err := h.ts.ListAgentTasks(r.Context(), name, sidPtr)
	if err != nil || tasks == nil {
		tasks = []store.AgentTask{}
	}
	writeJSON(w, http.StatusOK, tasks)
}

func (h *SessionsHandler) CreateTask(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Title     string `json:"title"`
		SessionID string `json:"session_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Title == "" {
		errBadRequest(w, "title is required")
		return
	}
	name := chi.URLParam(r, "name")
	task, err := h.ts.CreateAgentTask(r.Context(), name, body.Title, strPtr(body.SessionID))
	if err != nil {
		errInternalServer(w, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, task)
}

func (h *SessionsHandler) UpdateTask(w http.ResponseWriter, r *http.Request) {
	taskID, _ := strconv.ParseInt(chi.URLParam(r, "taskID"), 10, 64)
	var body struct {
		Title     *string `json:"title"`
		Completed *int    `json:"completed"`
		SortOrder *int    `json:"sort_order"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		errBadRequest(w, "invalid JSON")
		return
	}
	if err := h.ts.UpdateAgentTask(r.Context(), taskID, body.Title, body.Completed, body.SortOrder); err != nil {
		errInternalServer(w, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (h *SessionsHandler) DeleteTask(w http.ResponseWriter, r *http.Request) {
	taskID, _ := strconv.ParseInt(chi.URLParam(r, "taskID"), 10, 64)
	if err := h.ts.DeleteAgentTask(r.Context(), taskID); err != nil {
		errInternalServer(w, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (h *SessionsHandler) ReorderTasks(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	var body struct {
		TaskIDs []int64 `json:"task_ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || len(body.TaskIDs) == 0 {
		errBadRequest(w, "task_ids required")
		return
	}
	if err := h.ts.ReorderAgentTasks(r.Context(), name, body.TaskIDs); err != nil {
		errInternalServer(w, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// ── Notes ───────────────────────────────────────────────────────────────

func (h *SessionsHandler) ListNotes(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	sidPtr := querySessionID(r)
	notes, err := h.ts.ListAgentNotes(r.Context(), name, sidPtr)
	if err != nil || notes == nil {
		notes = []store.AgentNote{}
	}
	writeJSON(w, http.StatusOK, notes)
}

func (h *SessionsHandler) CreateNote(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	var body struct {
		Content   string `json:"content"`
		SessionID string `json:"session_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Content == "" {
		errBadRequest(w, "content is required")
		return
	}
	note, err := h.ts.CreateAgentNote(r.Context(), name, body.Content, strPtr(body.SessionID))
	if err != nil {
		errInternalServer(w, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, note)
}

func (h *SessionsHandler) UpdateNote(w http.ResponseWriter, r *http.Request) {
	noteID, _ := strconv.ParseInt(chi.URLParam(r, "noteID"), 10, 64)
	var body struct {
		Content string `json:"content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Content == "" {
		errBadRequest(w, "content is required")
		return
	}
	if err := h.ts.UpdateAgentNote(r.Context(), noteID, body.Content); err != nil {
		errInternalServer(w, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (h *SessionsHandler) DeleteNote(w http.ResponseWriter, r *http.Request) {
	noteID, _ := strconv.ParseInt(chi.URLParam(r, "noteID"), 10, 64)
	if err := h.ts.DeleteAgentNote(r.Context(), noteID); err != nil {
		errInternalServer(w, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// ── Events ──────────────────────────────────────────────────────────────

func (h *SessionsHandler) ListEvents(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	sidPtr := querySessionID(r)
	limit := queryInt(r, "limit", 50)
	if limit > 200 {
		limit = 200
	}
	events, err := h.ts.ListAgentEvents(r.Context(), name, limit, sidPtr)
	if err != nil || events == nil {
		events = []store.AgentEvent{}
	}
	writeJSON(w, http.StatusOK, events)
}

func (h *SessionsHandler) CreateEvent(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	var body struct {
		EventType  string `json:"event_type"`
		Summary    string `json:"summary"`
		ToolName   string `json:"tool_name"`
		SessionID  string `json:"session_id"`
		DetailJSON any    `json:"detail_json"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		errBadRequest(w, "invalid JSON")
		return
	}
	if body.EventType == "" || body.Summary == "" {
		errBadRequest(w, "event_type and summary required")
		return
	}

	// If session_id is provided, look up the actual agent name from the live
	// sessions DB. This is more reliable than the URL path name, which comes
	// from the hook's cwd and can be wrong when multiple agents share a directory.
	agentName := name
	if body.SessionID != "" {
		if ls, err := h.ss.GetLiveSession(r.Context(), body.SessionID); err == nil && ls != nil {
			agentName = ls.AgentName
		}
	}

	event := &store.AgentEvent{
		AgentName: agentName,
		EventType: body.EventType,
		Summary:   body.Summary,
	}
	if body.SessionID != "" {
		event.SessionID = &body.SessionID
	}
	if body.ToolName != "" {
		event.ToolName = &body.ToolName
	}
	if body.DetailJSON != nil {
		djBytes, err := json.Marshal(body.DetailJSON)
		if err != nil {
			slog.Warn("failed to marshal detail_json", "error", err)
		} else {
			s := string(djBytes)
			event.DetailJSON = &s
		}
	}

	created, err := h.ts.InsertAgentEvent(r.Context(), event)
	if err != nil {
		errInternalServer(w, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, created)
}

func (h *SessionsHandler) EventCounts(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	sidPtr := querySessionID(r)
	counts, err := h.ts.GetAgentEventCounts(r.Context(), name, sidPtr)
	if err != nil || counts == nil {
		counts = []store.ToolCount{}
	}
	writeJSON(w, http.StatusOK, counts)
}

func (h *SessionsHandler) ClearEvents(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	sidPtr := querySessionID(r)
	if err := h.ts.ClearAgentEvents(r.Context(), name, sidPtr); err != nil {
		errInternalServer(w, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// WebSocket handlers are in websocket.go

// ── Helpers ─────────────────────────────────────────────────────────────

func (h *SessionsHandler) findLogPath(agentType, sessionID string) string {
	if sessionID == "" {
		return ""
	}
	if agentType == "" {
		agentType = at.Claude
	}
	return naming.LogFile(h.cfg.LogDir, agentType, sessionID)
}

// writeJSON is a package-local alias for httputil.WriteJSON.
// Kept as a short name since it's called 220+ times across route handlers.
var writeJSON = httputil.WriteJSON


func generateUUID() string {
	b := make([]byte, 16)
	crand.Read(b)
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// launchSession creates a new agent session using the specified backend (tmux or pty).
func (h *SessionsHandler) launchSession(ctx context.Context, workDir, agentType, displayName, resumeSessionID string,
	flags []string, prompt, boardName, boardServer, backend, boardType, model string, capabilities *agent.Capabilities) (map[string]any, error) {

	absDir, err := filepath.Abs(workDir)
	if err != nil || !isDir(absDir) {
		return nil, fmt.Errorf("directory not found: %s", workDir)
	}

	if agentType == "" {
		agentType = at.Claude
	}

	// Resolve custom CLI path from settings
	userSettings, _ := h.ss.GetSettings(ctx)
	cliPath := userSettings[agent.CLIPathSettingKey(agentType)]

	// Check CLI availability before launching (skip for terminal type)
	if agentType != at.Terminal {
		checkBin := cliPath
		if checkBin == "" {
			if info := agent.GetCLIInfo(agentType); info != nil {
				checkBin = info.Binary
			}
		}
		if checkBin != "" {
			if resolved, err := exec.LookPath(checkBin); err != nil {
				// LookPath failed — try common install locations
				if found := agent.FindCLIInCommonPaths(checkBin); found != "" {
					cliPath = found
					log.Printf("[launch] %s not on PATH, found at %s", checkBin, found)
				} else {
					info := agent.GetCLIInfo(agentType)
					installCmd := ""
					if info != nil {
						installCmd = info.InstallCommand
					}
					return nil, fmt.Errorf("%s CLI not found. Install it: %s", checkBin, installCmd)
				}
			} else if cliPath == "" {
				cliPath = resolved
			}
		}
	}

	if backend == "" {
		if h.backend != nil {
			backend = "pty"
		} else {
			backend = "tmux"
		}
	}
	folderName := filepath.Base(absDir)

	sessionID := generateUUID()
	sessionName := naming.SessionName(agentType, sessionID)
	logFile := naming.LogFile(h.cfg.LogDir, agentType, sessionID)

	isTerminal := agentType == at.Terminal
	agentImpl := agent.GetAgent(agentType)
	if resumeSessionID != "" && !isTerminal {
		agent.TryPrepareResume(agentImpl, resumeSessionID, absDir)
	}

	role := naming.SubscriberID(displayName, agentType)

	launchParams := agent.LaunchParams{
		SessionID:       sessionID,
		SessionName:     sessionName,
		ProtocolPath:    h.protocolPath(),
		ResumeSessionID: resumeSessionID,
		Flags:           flags,
		WorkingDir:      absDir,
		BoardName:       boardName,
		Role:            role,
		Prompt:          prompt,
		PromptOverrides: promptOverrides(userSettings),
		BoardType:       boardType,
		Capabilities:    capabilities,
		CLIPath:         cliPath,
	}
	if cliPath != "" {
		log.Printf("[launch] using custom CLI path: %s", cliPath)
	}

	if backend == "pty" && h.backend != nil {
		// PTY backend: spawn the agent process directly
		var cmd string
		if !isTerminal {
			cmd = agent.WrapWithBundlePath(agentImpl.BuildLaunchCommand(launchParams))
		}
		// Spawn a shell first (empty command), then send the agent command as input.
		// This matches the tmux pattern and works cross-platform — the shell
		// interprets bash syntax like $(cat ...) correctly.
		if err := h.backend.Spawn(sessionName, agentType, absDir, sessionID, "", 200, 50); err != nil {
			return nil, fmt.Errorf("pty spawn failed: %w", err)
		}
		// PTY backend manages its own log file
		logFile = h.backend.LogPath(sessionName)

		// Wait for shell to initialize, then send the launch command
		if !isTerminal && cmd != "" {
			log.Printf("[launch] pty session=%s agent=%s cmd=%s", sessionName, agentType, cmd)
			time.Sleep(300 * time.Millisecond)
			h.backend.SendInput(sessionName, []byte(cmd+"\n"))
		}
	} else {
		// Tmux backend: create session, pipe-pane, send keys
		backend = "tmux" // normalize if pty requested but no backend available

		// Create empty log file
		os.WriteFile(logFile, []byte{}, 0644)

		if err := h.terminal.CreateSession(ctx, sessionName, absDir); err != nil {
			return nil, fmt.Errorf("tmux new-session failed: %w", err)
		}
		// Set CORAL_SESSION_NAME and CORAL_SUBSCRIBER_ID in the tmux session environment
		if tmuxTerm, ok := h.terminal.(*ptymanager.TmuxSessionTerminal); ok {
			if err := tmuxTerm.Client().SetEnvironment(ctx, sessionName, "CORAL_SESSION_NAME", sessionName); err != nil {
				log.Printf("[launch] failed to set CORAL_SESSION_NAME for %s: %v", sessionName, err)
			}
			if err := tmuxTerm.Client().SetEnvironment(ctx, sessionName, "CORAL_SUBSCRIBER_ID", role); err != nil {
				log.Printf("[launch] failed to set CORAL_SUBSCRIBER_ID for %s: %v", sessionName, err)
			}
		}

		// Setup pipe-pane logging
		h.terminal.StartLogging(ctx, sessionName, logFile)

		// Set pane title using native tmux command (avoids shell echo)
		h.terminal.SetPaneTitle(ctx, sessionName+".0", fmt.Sprintf("%s — %s", folderName, agentType))

		// Launch the agent (unless terminal)
		if !isTerminal {
			cmd := agent.WrapWithBundlePath(agentImpl.BuildLaunchCommand(launchParams))
			log.Printf("[launch] tmux session=%s agent=%s cmd=%s", sessionName, agentType, cmd)
			h.terminal.SendToTarget(ctx, sessionName+".0", cmd)
		}
	}

	// Capture shell PID for process-tree-based identity resolution
	var shellPID int
	if backend == "tmux" {
		if tmuxTerm, ok := h.terminal.(*ptymanager.TmuxSessionTerminal); ok {
			if pid, err := tmuxTerm.Client().GetPanePID(ctx, sessionName); err == nil {
				shellPID = pid
			}
		}
	}

	// Register in DB
	h.ss.RegisterLiveSession(ctx, &store.LiveSession{
		SessionID:    sessionID,
		AgentType:    agentType,
		AgentName:    folderName,
		WorkingDir:   absDir,
		DisplayName:  strPtr(displayName),
		ResumeFromID: strPtr(resumeSessionID),
		Flags:        store.MarshalFlags(flags),
		Prompt:       strPtr(prompt),
		BoardName:    strPtr(boardName),
		BoardServer:  strPtr(boardServer),
		Backend:      strPtr(backend),
		BoardType:    strPtr(boardType),
		Capabilities: store.MarshalCapabilities(capabilities),
		Model:        strPtr(model),
		PID:          shellPID,
	})

	if displayName != "" {
		h.ss.SetDisplayName(ctx, sessionID, displayName)
	}

	return map[string]any{
		"ok": true, "session_id": sessionID, "session_name": sessionName,
		"log_file": logFile, "backend": backend,
	}, nil
}

// setupBoardAndPrompt subscribes a session to a message board.
// The agent prompt is now passed directly as a CLI positional argument
// in launchSession, so no tmux-based prompt delivery is needed.
// subscriberID is the stable identity (role name), sessionName is the mutable tmux session.
func (h *SessionsHandler) setupBoardAndPrompt(sessionID, sessionName, agentType, boardName, displayName string) {
	role := naming.SubscriberID(displayName, agentType)
	subscriberID := role // stable board identity = role name
	ctx := context.Background()

	if boardName == "" {
		return
	}

	// Board subscription (immediate — no delay needed)
	if h.bs != nil {
		isOrchestrator := strings.Contains(strings.ToLower(role), "orchestrator")
		receiveMode := "mentions"
		if isOrchestrator {
			receiveMode = "all"
		}

		// Preserve existing receive_mode on re-subscribe (e.g. restart).
		// Use session-name lookup to find the subscription for THIS session,
		// not a stale one from a previous board with the same subscriber_id.
		existing, err := h.bs.GetSubscriptionBySessionName(ctx, sessionName)
		if err == nil && existing != nil && existing.ReceiveMode != "" {
			receiveMode = existing.ReceiveMode
		}

		if _, err := h.bs.Subscribe(ctx, boardName, subscriberID, role, sessionName, nil, nil, receiveMode, isOrchestrator); err != nil {
			log.Printf("Failed to subscribe session %s to board %s: %v", sessionID[:8], boardName, err)
		}
	}

	// Write local board state file so coral-board CLI can find its subscription
	writeBoardStateFile(sessionName, boardName, role, h.cfg)
}

// writeBoardStateFile writes the local board state file that coral-board CLI
// reads to determine which board a session is subscribed to.
func writeBoardStateFile(sessionName, boardName, role string, cfg *config.Config) {
	stateDir := cfg.CoralDir()
	os.MkdirAll(stateDir, 0755)

	state := map[string]string{
		"project":   boardName,
		"job_title": role,
	}
	// Include server_url if not on default port
	if cfg != nil && cfg.Port != 8420 {
		state["server_url"] = fmt.Sprintf("http://localhost:%d", cfg.Port)
	}

	data, _ := json.Marshal(state)
	statePath := filepath.Join(stateDir, fmt.Sprintf("board_state_%s.json", sessionName))
	if err := os.WriteFile(statePath, data, 0644); err != nil {
		log.Printf("Failed to write board state file for %s: %v", sessionName, err)
	}
}

// removeBoardStateFile deletes the board state file for a session.
// Called when a session is killed so state files don't accumulate.
func removeBoardStateFile(sessionName string, cfg *config.Config) {
	statePath := filepath.Join(cfg.CoralDir(), fmt.Sprintf("board_state_%s.json", sessionName))
	os.Remove(statePath) // Ignore error — file may not exist
}

func (h *SessionsHandler) protocolPath() string {
	// Look for PROTOCOL.md relative to the binary or in known locations
	candidates := []string{
		filepath.Join(h.cfg.CoralRoot, "PROTOCOL.md"),
		filepath.Join(h.cfg.CoralRoot, "src", "coral", "PROTOCOL.md"),
	}
	// Also check near the executable
	if ex, err := os.Executable(); err == nil {
		candidates = append(candidates, filepath.Join(filepath.Dir(ex), "PROTOCOL.md"))
	}
	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

func isDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// boardProject returns the board project name for a session, checking
// board subscriptions first, then falling back to live_sessions DB.
func boardProject(subs map[string]*board.Subscriber, fallback map[string][2]string, tmuxName, sessionID string) any {
	if sub, ok := subs[tmuxName]; ok {
		return sub.Project
	}
	if fb, ok := fallback[sessionID]; ok && fb[0] != "" {
		return fb[0]
	}
	return nil
}

// boardJobTitle returns the board job title for a session.
func boardJobTitle(subs map[string]*board.Subscriber, fallback map[string][2]string, tmuxName, sessionID string) any {
	if sub, ok := subs[tmuxName]; ok {
		return sub.JobTitle
	}
	if fb, ok := fallback[sessionID]; ok && fb[1] != "" {
		return fb[1]
	}
	return nil
}

// GetFileContent returns the raw content of a file in the agent's working tree.
// GET /api/sessions/live/{name}/file-content?filepath=...&session_id=...
func (h *SessionsHandler) GetFileContent(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	fp := r.URL.Query().Get("filepath")
	sessionID := r.URL.Query().Get("session_id")

	if fp == "" || strings.HasPrefix(fp, "-") || strings.ContainsAny(fp, "\x00") {
		writeJSON(w, http.StatusOK, map[string]string{"error": "filepath is required"})
		return
	}

	workdir := h.resolveGitRoot(r.Context(), name, "", sessionID)
	if workdir == "" {
		writeJSON(w, http.StatusOK, map[string]string{"error": "Could not determine working directory"})
		return
	}

	fullPath, err := filepath.Abs(filepath.Join(workdir, fp))
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]string{"error": "invalid path"})
		return
	}
	realWorkdir, _ := filepath.EvalSymlinks(workdir)
	realPath, _ := filepath.EvalSymlinks(fullPath)
	if realPath != "" && !strings.HasPrefix(realPath, realWorkdir+string(os.PathSeparator)) {
		writeJSON(w, http.StatusOK, map[string]string{"error": "Path traversal not allowed"})
		return
	}

	info, err := os.Stat(fullPath)
	if err != nil || info.IsDir() {
		writeJSON(w, http.StatusOK, map[string]string{"error": "File not found"})
		return
	}

	content, err := os.ReadFile(fullPath)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"filepath":          fp,
		"content":           string(content),
		"working_directory": workdir,
	})
}

// GetFileOriginal returns the original (git base) content of a file.
// GET /api/sessions/live/{name}/file-original?filepath=...&session_id=...
func (h *SessionsHandler) GetFileOriginal(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	fp := r.URL.Query().Get("filepath")
	sessionID := r.URL.Query().Get("session_id")

	if fp == "" || strings.HasPrefix(fp, "-") || strings.ContainsAny(fp, "\x00:") {
		writeJSON(w, http.StatusOK, map[string]string{"error": "filepath is required"})
		return
	}

	workdir := h.resolveGitRoot(r.Context(), name, "", sessionID)
	if workdir == "" {
		writeJSON(w, http.StatusOK, map[string]string{"error": "Could not determine working directory"})
		return
	}

	// Path traversal protection
	fullPath, err := filepath.Abs(filepath.Join(workdir, fp))
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]string{"error": "invalid path"})
		return
	}
	realWorkdir, _ := filepath.EvalSymlinks(workdir)
	realPath, _ := filepath.EvalSymlinks(fullPath)
	if realPath != "" && !strings.HasPrefix(realPath, realWorkdir+string(os.PathSeparator)) {
		writeJSON(w, http.StatusOK, map[string]string{"error": "path traversal not allowed"})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	base := gitutil.GetDiffBase(ctx, workdir, h.getDiffMode(r.Context()))

	// git show <ref>:<path> needs paths relative to the repo root, not the workdir.
	prefix := gitutil.ShowPrefix(ctx, workdir)

	out, err := exec.CommandContext(ctx, "git", "-C", workdir, "show", base+":"+prefix+fp).Output()
	if err != nil {
		// File doesn't exist in the base commit (new file)
		writeJSON(w, http.StatusOK, map[string]any{
			"filepath":          fp,
			"content":           "",
			"working_directory": workdir,
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"filepath":          fp,
		"content":           string(out),
		"working_directory": workdir,
	})
}

// SaveFileContent writes content to a file in the agent's working tree.
// PUT /api/sessions/live/{name}/file-content?filepath=...&session_id=...
func (h *SessionsHandler) SaveFileContent(w http.ResponseWriter, r *http.Request) {
	// Limit body size to 50MB
	r.Body = http.MaxBytesReader(w, r.Body, 50<<20)

	name := chi.URLParam(r, "name")
	fp := r.URL.Query().Get("filepath")
	sessionID := r.URL.Query().Get("session_id")

	if fp == "" {
		writeJSON(w, http.StatusOK, map[string]string{"error": "filepath is required"})
		return
	}

	var body struct {
		Content *string `json:"content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		errBadRequest(w, "invalid JSON")
		return
	}
	if body.Content == nil {
		writeJSON(w, http.StatusOK, map[string]string{"error": "content is required"})
		return
	}

	workdir := h.resolveGitRoot(r.Context(), name, "", sessionID)
	if workdir == "" {
		writeJSON(w, http.StatusOK, map[string]string{"error": "Could not determine working directory"})
		return
	}

	fullPath, err := filepath.Abs(filepath.Join(workdir, fp))
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]string{"error": "invalid path"})
		return
	}
	realWorkdir, _ := filepath.EvalSymlinks(workdir)

	// For new files, EvalSymlinks fails because the path doesn't exist yet.
	// Check the parent directory instead, which must already exist or will be created.
	realPath, evalErr := filepath.EvalSymlinks(fullPath)
	if evalErr != nil {
		// File doesn't exist — resolve via parent dir + filename
		parentDir := filepath.Dir(fullPath)
		realParent, parentErr := filepath.EvalSymlinks(parentDir)
		if parentErr != nil {
			// Parent doesn't exist either — resolve via workdir prefix check on the absolute path
			// This is safe because filepath.Abs already resolved ".." components
			if !strings.HasPrefix(fullPath, realWorkdir+string(os.PathSeparator)) {
				writeJSON(w, http.StatusOK, map[string]string{"error": "Path traversal not allowed"})
				return
			}
		} else {
			// Append separator to both sides to prevent prefix collisions
			// (e.g. /home/user/project vs /home/user/project-evil)
			if !strings.HasPrefix(realParent+string(os.PathSeparator), realWorkdir+string(os.PathSeparator)) {
				writeJSON(w, http.StatusOK, map[string]string{"error": "Path traversal not allowed"})
				return
			}
		}
	} else if !strings.HasPrefix(realPath, realWorkdir+string(os.PathSeparator)) {
		writeJSON(w, http.StatusOK, map[string]string{"error": "Path traversal not allowed"})
		return
	}

	// Create parent directories for new files
	if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
		writeJSON(w, http.StatusOK, map[string]string{"error": err.Error()})
		return
	}

	if err := os.WriteFile(fullPath, []byte(*body.Content), 0644); err != nil {
		writeJSON(w, http.StatusOK, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "filepath": fp})
}

// SetIcon sets or clears the emoji icon for a live session.
// PUT /api/sessions/live/{name}/icon
func (h *SessionsHandler) SetIcon(w http.ResponseWriter, r *http.Request) {
	var body struct {
		SessionID string `json:"session_id"`
		Icon      string `json:"icon"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		errBadRequest(w, "invalid JSON")
		return
	}
	if body.SessionID == "" {
		writeJSON(w, http.StatusOK, map[string]string{"error": "session_id is required"})
		return
	}

	var icon *string
	trimmed := strings.TrimSpace(body.Icon)
	if trimmed != "" {
		icon = &trimmed
	}

	if err := h.ss.SetIcon(r.Context(), body.SessionID, icon); err != nil {
		errInternalServer(w, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "icon": icon})
}

// ── Team Sleep/Wake ──────────────────────────────────────────────────────

// SleepStatus returns whether a team board is sleeping.
// GET /api/sessions/live/team/{boardName}/sleep-status
func (h *SessionsHandler) SleepStatus(w http.ResponseWriter, r *http.Request) {
	boardName := chi.URLParam(r, "boardName")
	boards, err := h.ss.GetSleepingBoardNames(r.Context())
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"sleeping": false})
		return
	}
	sleeping := false
	for _, b := range boards {
		if b == boardName {
			sleeping = true
			break
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"sleeping": sleeping})
}

// Sleep puts a team to sleep: sets is_sleeping, kills tmux sessions, pauses board.
// POST /api/sessions/live/team/{boardName}/sleep
func (h *SessionsHandler) Sleep(w http.ResponseWriter, r *http.Request) {
	boardName := chi.URLParam(r, "boardName")
	ctx := r.Context()

	// Check if any sessions exist on this board
	allLive, _ := h.ss.GetAllLiveSessions(ctx)
	var boardSessions []store.LiveSession
	for _, ls := range allLive {
		if ls.BoardName != nil && *ls.BoardName == boardName {
			boardSessions = append(boardSessions, ls)
		}
	}
	if len(boardSessions) == 0 {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": "No sessions found on that board"})
		return
	}

	// Set all board sessions to sleeping
	affected, err := h.ss.SetBoardSleeping(ctx, boardName, true)
	if err != nil {
		errInternalServer(w, err.Error())
		return
	}

	// Pause the message board
	boardPaused := false
	if h.boardHandler != nil {
		h.boardHandler.SetPaused(boardName, true)
		boardPaused = true
	}

	// Kill tmux sessions for agents on this board
	killed := 0
	for _, ls := range boardSessions {
		err := h.terminal.KillSessionOnly(ctx, ls.AgentName, ls.AgentType, ls.SessionID)
		if err == nil {
			killed++
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"ok":                true,
		"sleeping":          true,
		"sessions_affected": affected,
		"sessions_killed":   killed,
		"board_paused":      boardPaused,
	})
}

// Wake wakes a sleeping team: relaunches sessions, clears sleeping, unpauses board.
// POST /api/sessions/live/team/{boardName}/wake
func (h *SessionsHandler) Wake(w http.ResponseWriter, r *http.Request) {
	boardName := chi.URLParam(r, "boardName")
	ctx := r.Context()

	// Find sleeping sessions on this board and relaunch
	allLive, _ := h.ss.GetAllLiveSessions(ctx)

	// Count currently active (non-sleeping) agents for limit enforcement
	activeCount := 0
	for _, ls := range allLive {
		if ls.IsSleeping == 0 {
			activeCount++
		}
	}

	relaunched := 0
	for _, ls := range allLive {
		if ls.IsSleeping != 1 || ls.BoardName == nil || *ls.BoardName != boardName {
			continue
		}

		// Edition limits: stop waking if we'd exceed max live agents
		if h.cfg.MaxLiveAgents > 0 && activeCount+relaunched >= h.cfg.MaxLiveAgents {
			log.Printf("[wake] limit reached (%d/%d) — skipping remaining agents on board %s",
				activeCount+relaunched, h.cfg.MaxLiveAgents, boardName)
			break
		}

		// Relaunch the session
		flags := store.ParseFlags(ls.Flags)
		prompt := derefStrPtr(ls.Prompt)
		bn := derefStrPtr(ls.BoardName)
		bs := derefStrPtr(ls.BoardServer)
		bk := "tmux"
		if ls.Backend != nil {
			bk = *ls.Backend
		}
		dn := derefStrPtr(ls.DisplayName)
		bt := derefStrPtr(ls.BoardType)
		if err := h.wakeExistingSession(ctx, &ls, flags, prompt, bn, bs, bk, bt, dn); err != nil {
			log.Printf("Failed to wake session %s — keeping asleep: %v", ls.SessionID[:8], err)
			continue
		}
		relaunched++
	}

	// Clean up orphaned sleeping rows for this board (from old wake code that created duplicates)
	cleaned := 0
	refreshed, _ := h.ss.GetAllLiveSessions(ctx)
	for _, ls := range refreshed {
		if ls.IsSleeping == 1 && ls.BoardName != nil && *ls.BoardName == boardName {
			h.ss.UnregisterLiveSession(ctx, ls.SessionID)
			cleaned++
		}
	}
	if cleaned > 0 {
		log.Printf("[wake] cleaned %d orphaned sleeping rows for board %s", cleaned, boardName)
	}

	// Unpause board if at least one agent was woken
	boardPaused := true
	if relaunched > 0 && h.boardHandler != nil {
		h.boardHandler.SetPaused(boardName, false)
		boardPaused = false
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"ok":                  true,
		"sleeping":            false,
		"sessions_relaunched": relaunched,
		"board_paused":        boardPaused,
	})
}

// wakeExistingSession recreates the tmux/pty session for a sleeping agent
// using the EXISTING session ID. Preserves display name, board subscriptions, and history.
func (h *SessionsHandler) wakeExistingSession(ctx context.Context, ls *store.LiveSession,
	flags []string, prompt, boardName, boardServer, backend, boardType, displayName string) error {

	sessionName := naming.SessionName(ls.AgentType, ls.SessionID)
	logFile := naming.LogFile(h.cfg.LogDir, ls.AgentType, ls.SessionID)

	agentImpl := agent.GetAgent(ls.AgentType)
	agent.TryPrepareResume(agentImpl, ls.SessionID, ls.WorkingDir)

	userSettings, _ := h.ss.GetSettings(ctx)
	cliPath := userSettings[agent.CLIPathSettingKey(ls.AgentType)]

	role := displayName
	if role == "" {
		role = ls.AgentType
	}

	launchParams := agent.LaunchParams{
		SessionID:       ls.SessionID,
		SessionName:     sessionName,
		ProtocolPath:    h.protocolPath(),
		ResumeSessionID: ls.SessionID,
		Flags:           flags,
		WorkingDir:      ls.WorkingDir,
		BoardName:       boardName,
		Role:            role,
		Prompt:          prompt,
		PromptOverrides: promptOverrides(userSettings),
		BoardType:       boardType,
		Capabilities:    nil,
		CLIPath:         cliPath,
	}

	if ls.AgentType != at.Terminal {
		cmd := agent.WrapWithBundlePath(agentImpl.BuildLaunchCommand(launchParams))
		log.Printf("[wake] session=%s agent=%s backend=%s cmd=%s", sessionName, ls.AgentType, backend, cmd)

		if backend == "pty" && h.backend != nil {
			// PTY backend: spawn shell, then send command
			if err := h.backend.Spawn(sessionName, ls.AgentType, ls.WorkingDir, ls.SessionID, "", 200, 50); err != nil {
				return fmt.Errorf("pty spawn failed: %w", err)
			}
			if cmd != "" {
				time.Sleep(300 * time.Millisecond)
				h.backend.SendInput(sessionName, []byte(cmd+"\n"))
			}
		} else {
			// tmux backend: create session with the EXISTING session name
			os.WriteFile(logFile, []byte{}, 0644)
			if err := h.terminal.CreateSession(ctx, sessionName, ls.WorkingDir); err != nil {
				return fmt.Errorf("create session: %w", err)
			}
			h.terminal.StartLogging(ctx, sessionName, logFile)

			folderName := filepath.Base(ls.WorkingDir)
			h.terminal.SetPaneTitle(ctx, sessionName+".0", fmt.Sprintf("%s — %s", folderName, ls.AgentType))

			h.terminal.SendToTarget(ctx, sessionName+".0", cmd)
		}
	}

	// Clear sleeping flag on existing row (no new DB row created)
	h.ss.SetSessionSleeping(ctx, ls.SessionID, false)

	// Re-subscribe to board with existing session name
	if boardName != "" {
		h.setupBoardAndPrompt(ls.SessionID, sessionName, ls.AgentType, boardName, displayName)
	}

	return nil
}

// ── Individual Session Sleep/Wake ────────────────────────────────────────

// SleepSession puts a single agent to sleep.
// POST /api/sessions/live/{sessionID}/sleep
func (h *SessionsHandler) SleepSession(w http.ResponseWriter, r *http.Request) {
	sessionID := chi.URLParam(r, "sessionID")
	ctx := r.Context()

	allLive, _ := h.ss.GetAllLiveSessions(ctx)
	var sess *store.LiveSession
	for i := range allLive {
		if allLive[i].SessionID == sessionID {
			sess = &allLive[i]
			break
		}
	}
	if sess == nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": "Session not found"})
		return
	}

	h.ss.SetSessionSleeping(ctx, sessionID, true)

	// Kill tmux session to free resources
	err := h.terminal.KillSessionOnly(ctx, sess.AgentName, sess.AgentType, sessionID)
	if err != nil {
		log.Printf("Failed to kill tmux for session %s during sleep: %v", sessionID[:8], err)
	}

	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "sleeping": true})
}

// WakeSession wakes a single sleeping agent.
// POST /api/sessions/live/{sessionID}/wake
func (h *SessionsHandler) WakeSession(w http.ResponseWriter, r *http.Request) {
	sessionID := chi.URLParam(r, "sessionID")
	ctx := r.Context()

	allLive, _ := h.ss.GetAllLiveSessions(ctx)
	var sess *store.LiveSession
	for i := range allLive {
		if allLive[i].SessionID == sessionID {
			sess = &allLive[i]
			break
		}
	}
	if sess == nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": "Session not found"})
		return
	}
	if sess.IsSleeping != 1 {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": "Session is not sleeping"})
		return
	}

	// Edition limits: check max live agents before waking
	if h.cfg.MaxLiveAgents > 0 {
		activeCount := 0
		for _, ls := range allLive {
			if ls.IsSleeping == 0 {
				activeCount++
			}
		}
		if activeCount >= h.cfg.MaxLiveAgents {
			writeJSON(w, http.StatusForbidden, map[string]string{
				"error": fmt.Sprintf("Demo limit reached: maximum %d concurrent agents allowed", h.cfg.MaxLiveAgents),
			})
			return
		}
	}

	flags := store.ParseFlags(sess.Flags)
	prompt := derefStrPtr(sess.Prompt)
	bn := derefStrPtr(sess.BoardName)
	bs := derefStrPtr(sess.BoardServer)
	bk := "tmux"
	if sess.Backend != nil {
		bk = *sess.Backend
	}
	dn := derefStrPtr(sess.DisplayName)
	bt := derefStrPtr(sess.BoardType)

	if err := h.wakeExistingSession(ctx, sess, flags, prompt, bn, bs, bk, bt, dn); err != nil {
		log.Printf("Failed to wake session %s: %v", sessionID[:8], err)
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": "Failed to relaunch session"})
		return
	}

	// Unpause the board if this agent was on one
	if bn != "" && h.boardHandler != nil {
		h.boardHandler.SetPaused(bn, false)
	}

	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "sleeping": false})
}

// ── Bulk Sleep/Wake All ─────────────────────────────────────────────────

// SleepAll puts all active agents to sleep.
// POST /api/sessions/live/sleep-all
func (h *SessionsHandler) SleepAll(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	allLive, _ := h.ss.GetAllLiveSessions(ctx)

	var active []store.LiveSession
	for _, ls := range allLive {
		if ls.IsSleeping != 1 {
			active = append(active, ls)
		}
	}
	if len(active) == 0 {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "sessions_affected": 0})
		return
	}

	boards := map[string]bool{}
	killed := 0
	for _, ls := range active {
		h.ss.SetSessionSleeping(ctx, ls.SessionID, true)
		if ls.BoardName != nil && *ls.BoardName != "" {
			boards[*ls.BoardName] = true
		}
		err := h.terminal.KillSessionOnly(ctx, ls.AgentName, ls.AgentType, ls.SessionID)
		if err == nil {
			killed++
		} else {
			log.Printf("Failed to kill tmux for session %s during sleep-all: %v", ls.SessionID[:8], err)
		}
	}

	// Pause all affected boards
	if h.boardHandler != nil {
		for b := range boards {
			h.boardHandler.SetPaused(b, true)
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"ok":                true,
		"sessions_affected": len(active),
		"sessions_killed":   killed,
	})
}

// WakeAll wakes all sleeping agents.
// POST /api/sessions/live/wake-all
func (h *SessionsHandler) WakeAll(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	allLive, _ := h.ss.GetAllLiveSessions(ctx)

	var sleeping []store.LiveSession
	for _, ls := range allLive {
		if ls.IsSleeping == 1 {
			sleeping = append(sleeping, ls)
		}
	}
	if len(sleeping) == 0 {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "sessions_relaunched": 0})
		return
	}

	boards := map[string]bool{}
	relaunched := 0
	for _, ls := range sleeping {
		flags := store.ParseFlags(ls.Flags)
		prompt := derefStrPtr(ls.Prompt)
		bn := derefStrPtr(ls.BoardName)
		bs := derefStrPtr(ls.BoardServer)
		bk := "tmux"
		if ls.Backend != nil {
			bk = *ls.Backend
		}
		dn := derefStrPtr(ls.DisplayName)
		bt := derefStrPtr(ls.BoardType)

		if err := h.wakeExistingSession(ctx, &ls, flags, prompt, bn, bs, bk, bt, dn); err != nil {
			log.Printf("Failed to wake session %s — keeping asleep: %v", ls.SessionID[:8], err)
			continue
		}
		relaunched++
		if bn != "" {
			boards[bn] = true
		}
	}

	// Unpause affected boards
	if h.boardHandler != nil {
		for b := range boards {
			h.boardHandler.SetPaused(b, false)
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "sessions_relaunched": relaunched})
}

// ResolveByPIDs looks up the agent identity for a set of process IDs.
// Used by coral-board to identify itself when env vars are stripped (e.g. Codex sandbox).
// GET /api/sessions/resolve?pids=12345,12340,12335
func (h *SessionsHandler) ResolveByPIDs(w http.ResponseWriter, r *http.Request) {
	pidStr := r.URL.Query().Get("pids")
	if pidStr == "" {
		errBadRequest(w, "pids parameter required")
		return
	}
	var pids []int
	for _, s := range strings.Split(pidStr, ",") {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		pid, err := strconv.Atoi(s)
		if err != nil {
			continue
		}
		pids = append(pids, pid)
	}
	if len(pids) == 0 {
		errBadRequest(w, "no valid PIDs")
		return
	}
	ls, err := h.ss.ResolveByPIDs(r.Context(), pids)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "no matching session"})
		return
	}
	role := ""
	if ls.DisplayName != nil {
		role = *ls.DisplayName
	}
	boardName := ""
	if ls.BoardName != nil {
		boardName = *ls.BoardName
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"subscriber_id": naming.SubscriberID(role, ls.AgentType),
		"project":       boardName,
		"session_name":  ls.SessionID,
	})
}
