// Package routes implements HTTP handlers for the Coral API.
package routes

import (
	"context"
	crand "crypto/rand"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/cdknorow/coral/internal/agent"
	"github.com/cdknorow/coral/internal/board"
	"github.com/cdknorow/coral/internal/config"
	"github.com/cdknorow/coral/internal/jsonl"
	"github.com/cdknorow/coral/internal/pulse"
	"github.com/cdknorow/coral/internal/ptymanager"
	"github.com/cdknorow/coral/internal/store"
	"github.com/cdknorow/coral/internal/tmux"
)

// SessionsHandler handles all live session API endpoints.
type SessionsHandler struct {
	db      *store.DB
	ss      *store.SessionStore
	ts      *store.TaskStore
	gs      *store.GitStore
	bs      *board.Store
	cfg     *config.Config
	tmux    *tmux.Client
	jsonl   *jsonl.SessionReader
	backend ptymanager.TerminalBackend // nil = use tmux directly

	// Deduplication state for status/summary events (mirrors Python _last_known)
	lastKnownMu sync.RWMutex
	lastKnown   map[string]lastKnownState
}

type lastKnownState struct {
	Status  string
	Summary string
}

// NewSessionsHandler creates a SessionsHandler with the given dependencies.
func NewSessionsHandler(db *store.DB, cfg *config.Config, backend ptymanager.TerminalBackend, bs *board.Store) *SessionsHandler {
	return &SessionsHandler{
		db:        db,
		ss:        store.NewSessionStore(db),
		ts:        store.NewTaskStore(db),
		gs:        store.NewGitStore(db),
		bs:        bs,
		cfg:       cfg,
		tmux:      tmux.NewClient(),
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
	panes, err := h.tmux.ListPanes(ctx.Context())
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

		logPath := filepath.Join(h.cfg.LogDir, fmt.Sprintf("%s_coral_%s.log", agentType, sessionID))

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
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
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
	latestGoals, _ := h.ts.GetLatestGoals(ctx, sessionIDs)
	if latestGoals == nil {
		latestGoals = map[string]string{}
	}

	// Fetch board subscriptions keyed by tmux session name
	var boardSubs map[string]*board.Subscriber
	if h.bs != nil {
		boardSubs, _ = h.bs.GetAllSubscriptions(ctx)
	}
	if boardSubs == nil {
		boardSubs = map[string]*board.Subscriber{}
	}

	// Fetch board unread counts
	var allUnread map[string]int
	if h.bs != nil {
		allUnread, _ = h.bs.GetAllUnreadCounts(ctx)
	}
	if allUnread == nil {
		allUnread = map[string]int{}
	}

	// Fallback: board_name from live_sessions DB for agents not yet subscribed
	liveBoardNames := make(map[string][2]string) // session_id -> [board_name, display_name]
	{
		var rows []struct {
			SessionID   string  `db:"session_id"`
			BoardName   *string `db:"board_name"`
			DisplayName *string `db:"display_name"`
		}
		if err := h.db.SelectContext(ctx, &rows, "SELECT session_id, board_name, display_name FROM live_sessions WHERE board_name IS NOT NULL"); err == nil {
			for _, r := range rows {
				bn, dn := "", ""
				if r.BoardName != nil { bn = *r.BoardName }
				if r.DisplayName != nil { dn = *r.DisplayName }
				liveBoardNames[r.SessionID] = [2]string{bn, dn}
			}
		}
	}

	var sessions []map[string]any
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
			"branch":             branchVal,
			"waiting_for_input":  waiting,
			"done":               done,
			"waiting_reason":     nilIf(!waiting, latestEv),
			"waiting_summary":    nilIf(!waiting, evSummary),
			"working":            working,
			"changed_file_count": fc,
			"commands":           map[string]string{"compress": "/compact", "clear": "/clear"},
			"board_project":      boardProject(boardSubs, liveBoardNames, tmuxName, sid),
			"board_job_title":    boardJobTitle(boardSubs, liveBoardNames, tmuxName, sid),
			"board_unread":       boardUnread,
			"log_path":           agent.LogPath,
		}

		// Track status/summary for event deduplication
		h.trackStatusSummary(ctx, agent.AgentName, status, summary, sid)

		sessions = append(sessions, entry)
	}

	if sessions == nil {
		sessions = []map[string]any{}
	}

	writeJSON(w, http.StatusOK, sessions)
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

	paneText, _ := h.tmux.CapturePane(r.Context(), name, 200, agentType, sessionID)

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

	text, err := h.tmux.CapturePane(r.Context(), name, 200, agentType, sessionID)
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
	if text, err := h.tmux.CapturePane(ctx, name, 200, agentType, sessionID); err == nil && text != "" {
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
		agentType = "claude"
	}

	// Use session_id if provided, otherwise use name as session_id
	id := sessionID
	if id == "" {
		id = name
	}

	newMessages, total := h.jsonl.ReadNewMessages(id, workingDir, agentType)
	if newMessages == nil {
		newMessages = []map[string]any{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"messages": newMessages, "total": total})
}

// Info returns enriched metadata for the session info modal.
// GET /api/sessions/live/{name}/info
func (h *SessionsHandler) Info(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	agentType := r.URL.Query().Get("agent_type")
	sessionID := r.URL.Query().Get("session_id")
	ctx := r.Context()

	pane, _ := h.tmux.FindPane(ctx, name, agentType, sessionID)

	result := map[string]any{
		"name":       name,
		"session_id": sessionID,
	}

	if pane != nil {
		result["tmux_session"] = pane.SessionName
		result["pane_title"] = pane.PaneTitle
		result["current_path"] = pane.CurrentPath
		result["attach_command"] = fmt.Sprintf("tmux attach -t %s", pane.SessionName)
	}

	// Look up git state by session_id first, then by name
	var git *store.GitSnapshot
	if sessionID != "" {
		git, _ = h.gs.GetLatestGitStateBySession(ctx, sessionID)
	}
	if git == nil {
		git, _ = h.gs.GetLatestGitState(ctx, name)
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
	pane, _ := h.tmux.FindPane(ctx, name, agentType, sessionID)
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

// getDiffBase returns the merge-base ref for diffing on feature branches.
func getDiffBase(ctx context.Context, workdir string) string {
	out, err := exec.CommandContext(ctx, "git", "-C", workdir, "rev-parse", "--abbrev-ref", "HEAD").Output()
	if err != nil {
		return "HEAD"
	}
	branch := strings.TrimSpace(string(out))
	if branch == "main" || branch == "master" || branch == "HEAD" || branch == "" {
		return "HEAD"
	}
	for _, baseBranch := range []string{"main", "master"} {
		out, err = exec.CommandContext(ctx, "git", "-C", workdir, "merge-base", baseBranch, "HEAD").Output()
		if err == nil && len(out) > 0 {
			return strings.TrimSpace(string(out))
		}
	}
	return "HEAD"
}

// Files returns changed files for a live agent.
// GET /api/sessions/live/{name}/files
func (h *SessionsHandler) Files(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	sessionID := r.URL.Query().Get("session_id")
	var sidPtr *string
	if sessionID != "" {
		sidPtr = &sessionID
	}
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

	workdir := h.resolveWorkdir(r.Context(), name, "", body.SessionID)
	if workdir == "" {
		writeJSON(w, http.StatusOK, map[string]any{"error": "Could not determine working directory", "files": []any{}})
		return
	}

	// Run git diff --numstat to get changed files
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	base := getDiffBase(ctx, workdir)
	out, err := exec.CommandContext(ctx, "git", "-C", workdir, "diff", base, "--numstat").Output()
	fileMap := make(map[string]store.ChangedFile)
	if err == nil {
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
	untrackedOut, err := exec.CommandContext(ctx, "git", "-C", workdir, "ls-files", "--others", "--exclude-standard").Output()
	if err == nil {
		for _, f := range strings.Split(strings.TrimSpace(string(untrackedOut)), "\n") {
			if f == "" {
				continue
			}
			if _, exists := fileMap[f]; !exists {
				fileMap[f] = store.ChangedFile{Filepath: f, Additions: 0, Deletions: 0, Status: "??"}
			}
		}
	}

	// Merge in files from agent Write/Edit events
	var sidPtr *string
	if body.SessionID != "" {
		sidPtr = &body.SessionID
	}
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
			adds := 0
			if info, err := os.Stat(fp); err == nil && !info.IsDir() {
				data, err := os.ReadFile(fp)
				if err == nil {
					adds = strings.Count(string(data), "\n") + 1
				}
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

	workdir := h.resolveWorkdir(r.Context(), name, "", sessionID)
	if workdir == "" {
		writeJSON(w, http.StatusOK, map[string]any{"error": "Could not determine working directory"})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	base := getDiffBase(ctx, workdir)
	out, err := exec.CommandContext(ctx, "git", "-C", workdir, "diff", base, "--", fp).Output()
	diffText := ""
	if err == nil {
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
func (h *SessionsHandler) SearchFiles(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	query := strings.TrimSpace(strings.ToLower(r.URL.Query().Get("q")))
	sessionID := r.URL.Query().Get("session_id")

	workdir := h.resolveWorkdir(r.Context(), name, "", sessionID)
	if workdir == "" {
		writeJSON(w, http.StatusOK, map[string]any{"files": []string{}})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

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
	for i := 0; i < len(matches); i++ {
		for j := i + 1; j < len(matches); j++ {
			if matches[j].score < matches[i].score || (matches[j].score == matches[i].score && matches[j].path < matches[i].path) {
				matches[i], matches[j] = matches[j], matches[i]
			}
		}
	}
	result := make([]string, 0, 50)
	for i, m := range matches {
		if i >= 50 {
			break
		}
		result = append(result, m.path)
	}
	writeJSON(w, http.StatusOK, map[string]any{"files": result})
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
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
		return
	}
	if body.Command == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "No command provided"})
		return
	}

	if err := h.tmux.SendKeys(r.Context(), name, body.Command, body.AgentType, body.SessionID); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
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
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "keys must be a non-empty list"})
		return
	}

	if err := h.tmux.SendRawKeys(r.Context(), name, body.Keys, body.AgentType, body.SessionID); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
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
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "columns must be >= 10"})
		return
	}

	if err := h.tmux.ResizePane(r.Context(), name, body.Columns, body.AgentType, body.SessionID); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
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

	if err := h.tmux.KillSession(r.Context(), name, body.AgentType, body.SessionID); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	// Unregister from live sessions DB
	if body.SessionID != "" {
		h.ss.UnregisterLiveSession(r.Context(), body.SessionID)
	}

	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// Restart restarts the agent session.
// POST /api/sessions/live/{name}/restart
func (h *SessionsHandler) Restart(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	var body struct {
		AgentType  string `json:"agent_type"`
		ExtraFlags string `json:"extra_flags"`
		SessionID  string `json:"session_id"`
	}
	json.NewDecoder(r.Body).Decode(&body)

	ctx := r.Context()
	agentType := body.AgentType
	if agentType == "" {
		agentType = "claude"
	}

	pane, err := h.tmux.FindPane(ctx, name, agentType, body.SessionID)
	if err != nil || pane == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "Pane not found"})
		return
	}

	newSessionID := generateUUID()
	newSessionName := fmt.Sprintf("%s-%s", agentType, newSessionID)
	newLogPath := filepath.Join(h.cfg.LogDir, fmt.Sprintf("%s_coral_%s.log", agentType, newSessionID))

	// Close old pipe-pane, respawn, rename
	h.tmux.ClosePipePane(ctx, pane.Target)
	if err := h.tmux.RespawnPane(ctx, pane.Target, pane.CurrentPath); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if err := h.tmux.RenameSession(ctx, pane.SessionName, newSessionName); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	target := fmt.Sprintf("%s:0.0", newSessionName)
	time.Sleep(500 * time.Millisecond)

	// Clear scrollback, create log, setup pipe-pane
	h.tmux.ClearHistory(ctx, target)
	os.WriteFile(newLogPath, []byte{}, 0644)
	h.tmux.PipePane(ctx, target, newLogPath)

	// Set pane title
	folderName := filepath.Base(strings.TrimRight(pane.CurrentPath, "/"))
	titleCmd := fmt.Sprintf(`printf '\033]2;%s — %s\033\\'`, folderName, agentType)
	h.tmux.SendKeysToTarget(ctx, target, titleCmd)
	time.Sleep(300 * time.Millisecond)

	// Build and send launch command
	agentImpl := agent.GetAgent(agentType)
	protocolPath := h.protocolPath()
	var flags []string
	if body.ExtraFlags != "" {
		flags = strings.Fields(body.ExtraFlags)
	}
	cmd := agentImpl.BuildLaunchCommand(newSessionID, protocolPath, "", flags, pane.CurrentPath)
	h.tmux.SendKeysToTarget(ctx, target, cmd)

	// Replace live session in DB
	h.ss.ReplaceLiveSession(ctx, body.SessionID, &store.LiveSession{
		SessionID:    newSessionID,
		AgentType:    agentType,
		AgentName:    folderName,
		WorkingDir:   pane.CurrentPath,
		ResumeFromID: strPtr(body.SessionID),
	})
	h.ss.MigrateDisplayName(ctx, body.SessionID, newSessionID)

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
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "session_id is required"})
		return
	}

	agentType := body.AgentType
	if agentType == "" {
		agentType = "claude"
	}
	agentImpl := agent.GetAgent(agentType)
	if !agentImpl.SupportsResume() {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("Resume not supported for %s", agentType)})
		return
	}

	ctx := r.Context()
	pane, _ := h.tmux.FindPane(ctx, name, agentType, body.CurrentSessionID)
	if pane == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "Pane not found"})
		return
	}

	// Prepare resume files
	agentImpl.PrepareResume(body.SessionID, pane.CurrentPath)

	newSessionID := generateUUID()
	newSessionName := fmt.Sprintf("%s-%s", agentType, newSessionID)
	newLogPath := filepath.Join(h.cfg.LogDir, fmt.Sprintf("%s_coral_%s.log", agentType, newSessionID))

	h.tmux.ClosePipePane(ctx, pane.Target)
	h.tmux.RespawnPane(ctx, pane.Target, pane.CurrentPath)
	h.tmux.RenameSession(ctx, pane.SessionName, newSessionName)

	target := fmt.Sprintf("%s:0.0", newSessionName)
	time.Sleep(500 * time.Millisecond)
	h.tmux.ClearHistory(ctx, target)
	os.WriteFile(newLogPath, []byte{}, 0644)
	h.tmux.PipePane(ctx, target, newLogPath)

	cmd := agentImpl.BuildLaunchCommand(newSessionID, h.protocolPath(), body.SessionID, nil, pane.CurrentPath)
	h.tmux.SendKeysToTarget(ctx, target, cmd)

	h.ss.ReplaceLiveSession(ctx, body.CurrentSessionID, &store.LiveSession{
		SessionID:    newSessionID,
		AgentType:    agentType,
		AgentName:    filepath.Base(strings.TrimRight(pane.CurrentPath, "/")),
		WorkingDir:   pane.CurrentPath,
		ResumeFromID: strPtr(body.SessionID),
	})

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

	pane, _ := h.tmux.FindPane(r.Context(), name, body.AgentType, body.SessionID)
	if pane == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "Pane not found"})
		return
	}

	// Open Terminal.app attached to the tmux session (macOS)
	go func() {
		cmd := fmt.Sprintf(`tell application "Terminal" to do script "tmux attach -t %s"`, pane.SessionName)
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
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
		return
	}
	if body.SessionID == "" || body.DisplayName == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "session_id and display_name required"})
		return
	}

	if err := h.ss.SetDisplayName(r.Context(), body.SessionID, body.DisplayName); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "display_name": body.DisplayName})
}

// Launch creates a new agent session.
// POST /api/sessions/launch
func (h *SessionsHandler) Launch(w http.ResponseWriter, r *http.Request) {
	var body struct {
		WorkingDir  string   `json:"working_dir"`
		AgentType   string   `json:"agent_type"`
		DisplayName string   `json:"display_name"`
		Flags       []string `json:"flags"`
		Prompt      string   `json:"prompt"`
		BoardName   string   `json:"board_name"`
		BoardServer string   `json:"board_server"`
		Backend     string   `json:"backend"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
		return
	}
	if body.WorkingDir == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "working_dir is required"})
		return
	}

	result, err := h.launchSession(r.Context(), body.WorkingDir, body.AgentType, body.DisplayName,
		"", body.Flags, body.Prompt, body.BoardName, body.BoardServer, body.Backend)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	// Setup board and prompt in background
	if body.BoardName != "" || body.Prompt != "" {
		go h.setupBoardAndPrompt(result["session_id"].(string), result["session_name"].(string),
			body.AgentType, body.Prompt, body.BoardName, body.DisplayName, result["backend"].(string))
	}

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
		Agents      []struct {
			Name   string `json:"name"`
			Prompt string `json:"prompt"`
		} `json:"agents"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
		return
	}
	if body.BoardName == "" || body.WorkingDir == "" || len(body.Agents) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "board_name, working_dir, and agents required"})
		return
	}

	ctx := r.Context()
	var launched []map[string]any

	for _, agentDef := range body.Agents {
		if agentDef.Name == "" {
			continue
		}
		result, err := h.launchSession(ctx, body.WorkingDir, body.AgentType, agentDef.Name,
			"", body.Flags, agentDef.Prompt, body.BoardName, body.BoardServer, body.Backend)
		if err != nil {
			launched = append(launched, map[string]any{"name": agentDef.Name, "error": err.Error()})
			continue
		}

		go h.setupBoardAndPrompt(result["session_id"].(string), result["session_name"].(string),
			body.AgentType, agentDef.Prompt, body.BoardName, agentDef.Name, result["backend"].(string))

		launched = append(launched, map[string]any{
			"name": agentDef.Name, "session_id": result["session_id"], "session_name": result["session_name"],
		})
	}

	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "board": body.BoardName, "agents": launched})
}

// ── Tasks ───────────────────────────────────────────────────────────────

func (h *SessionsHandler) ListTasks(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	sessionID := r.URL.Query().Get("session_id")
	var sidPtr *string
	if sessionID != "" {
		sidPtr = &sessionID
	}
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
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "title is required"})
		return
	}
	name := chi.URLParam(r, "name")
	var sidPtr *string
	if body.SessionID != "" {
		sidPtr = &body.SessionID
	}
	task, err := h.ts.CreateAgentTask(r.Context(), name, body.Title, sidPtr)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
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
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
		return
	}
	if err := h.ts.UpdateAgentTask(r.Context(), taskID, body.Title, body.Completed, body.SortOrder); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (h *SessionsHandler) DeleteTask(w http.ResponseWriter, r *http.Request) {
	taskID, _ := strconv.ParseInt(chi.URLParam(r, "taskID"), 10, 64)
	if err := h.ts.DeleteAgentTask(r.Context(), taskID); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
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
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "task_ids required"})
		return
	}
	if err := h.ts.ReorderAgentTasks(r.Context(), name, body.TaskIDs); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// ── Notes ───────────────────────────────────────────────────────────────

func (h *SessionsHandler) ListNotes(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	sessionID := r.URL.Query().Get("session_id")
	var sidPtr *string
	if sessionID != "" {
		sidPtr = &sessionID
	}
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
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "content is required"})
		return
	}
	var sidPtr *string
	if body.SessionID != "" {
		sidPtr = &body.SessionID
	}
	note, err := h.ts.CreateAgentNote(r.Context(), name, body.Content, sidPtr)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
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
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "content is required"})
		return
	}
	if err := h.ts.UpdateAgentNote(r.Context(), noteID, body.Content); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (h *SessionsHandler) DeleteNote(w http.ResponseWriter, r *http.Request) {
	noteID, _ := strconv.ParseInt(chi.URLParam(r, "noteID"), 10, 64)
	if err := h.ts.DeleteAgentNote(r.Context(), noteID); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// ── Events ──────────────────────────────────────────────────────────────

func (h *SessionsHandler) ListEvents(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	sessionID := r.URL.Query().Get("session_id")
	limit := queryInt(r, "limit", 50)
	if limit > 200 {
		limit = 200
	}
	var sidPtr *string
	if sessionID != "" {
		sidPtr = &sessionID
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
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
		return
	}
	if body.EventType == "" || body.Summary == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "event_type and summary required"})
		return
	}

	event := &store.AgentEvent{
		AgentName: name,
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
		if err == nil {
			s := string(djBytes)
			event.DetailJSON = &s
		}
	}

	created, err := h.ts.InsertAgentEvent(r.Context(), event)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, created)
}

func (h *SessionsHandler) EventCounts(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	sessionID := r.URL.Query().Get("session_id")
	var sidPtr *string
	if sessionID != "" {
		sidPtr = &sessionID
	}
	counts, err := h.ts.GetAgentEventCounts(r.Context(), name, sidPtr)
	if err != nil || counts == nil {
		counts = []store.ToolCount{}
	}
	writeJSON(w, http.StatusOK, counts)
}

func (h *SessionsHandler) ClearEvents(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	sessionID := r.URL.Query().Get("session_id")
	var sidPtr *string
	if sessionID != "" {
		sidPtr = &sessionID
	}
	if err := h.ts.ClearAgentEvents(r.Context(), name, sidPtr); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
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
		agentType = "claude"
	}
	return filepath.Join(h.cfg.LogDir, fmt.Sprintf("%s_coral_%s.log", agentType, sessionID))
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("JSON encode error: %v", err)
	}
}

func queryInt(r *http.Request, key string, fallback int) int {
	v := r.URL.Query().Get(key)
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return fallback
	}
	return n
}

func nilIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func nilIf(cond bool, s string) any {
	if cond || s == "" {
		return nil
	}
	return s
}

func strPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// intPtrOr returns the value of p if non-nil, otherwise the default.
func intPtrOr(p *int, def int) int {
	if p != nil {
		return *p
	}
	return def
}

func generateUUID() string {
	b := make([]byte, 16)
	crand.Read(b)
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// launchSession creates a new agent session using the specified backend (tmux or pty).
func (h *SessionsHandler) launchSession(ctx context.Context, workDir, agentType, displayName, resumeSessionID string,
	flags []string, prompt, boardName, boardServer, backend string) (map[string]any, error) {

	absDir, err := filepath.Abs(workDir)
	if err != nil || !isDir(absDir) {
		return nil, fmt.Errorf("directory not found: %s", workDir)
	}

	if agentType == "" {
		agentType = "claude"
	}
	if backend == "" {
		backend = "tmux"
	}
	folderName := filepath.Base(absDir)

	sessionID := generateUUID()
	sessionName := fmt.Sprintf("%s-%s", agentType, sessionID)
	logFile := filepath.Join(h.cfg.LogDir, fmt.Sprintf("%s_coral_%s.log", agentType, sessionID))

	isTerminal := agentType == "terminal"
	agentImpl := agent.GetAgent(agentType)
	if resumeSessionID != "" && !isTerminal {
		agentImpl.PrepareResume(resumeSessionID, absDir)
	}

	if backend == "pty" && h.backend != nil {
		// PTY backend: spawn the agent process directly
		var cmd string
		if !isTerminal {
			cmd = agentImpl.BuildLaunchCommand(sessionID, h.protocolPath(), resumeSessionID, flags, absDir)
		}
		if err := h.backend.Spawn(sessionName, agentType, absDir, sessionID, cmd, 200, 50); err != nil {
			return nil, fmt.Errorf("pty spawn failed: %w", err)
		}
		// PTY backend manages its own log file
		logFile = h.backend.LogPath(sessionName)
	} else {
		// Tmux backend: create session, pipe-pane, send keys
		backend = "tmux" // normalize if pty requested but no backend available

		// Create empty log file
		os.WriteFile(logFile, []byte{}, 0644)

		if err := h.tmux.NewSession(ctx, sessionName, absDir); err != nil {
			return nil, fmt.Errorf("tmux new-session failed: %w", err)
		}

		// Setup pipe-pane logging
		h.tmux.PipePane(ctx, sessionName, logFile)

		// Set pane title
		titleCmd := fmt.Sprintf(`printf '\033]2;%s — %s\033\\'`, folderName, agentType)
		h.tmux.SendKeysToTarget(ctx, sessionName+".0", titleCmd)
		time.Sleep(300 * time.Millisecond)

		// Launch the agent (unless terminal)
		if !isTerminal {
			cmd := agentImpl.BuildLaunchCommand(sessionID, h.protocolPath(), resumeSessionID, flags, absDir)
			h.tmux.SendKeysToTarget(ctx, sessionName+".0", cmd)
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
	})

	if displayName != "" {
		h.ss.SetDisplayName(ctx, sessionID, displayName)
	}

	return map[string]any{
		"ok": true, "session_id": sessionID, "session_name": sessionName,
		"log_file": logFile, "backend": backend,
	}, nil
}

// setupBoardAndPrompt subscribes to a board and sends the initial prompt.
// Includes auto-accept for trust prompts and retry with verification.
func (h *SessionsHandler) setupBoardAndPrompt(sessionID, sessionName, agentType, prompt, boardName, displayName, backend string) {
	role := displayName
	if role == "" {
		role = agentType
	}
	ctx := context.Background()

	// Append board-specific prompt template with user overrides
	if prompt != "" && boardName != "" {
		isOrchestrator := strings.Contains(strings.ToLower(role), "orchestrator")

		// Read user prompt overrides from settings
		userSettings, _ := h.ss.GetSettings(ctx)
		var template string
		if isOrchestrator {
			template = userSettings["default_prompt_orchestrator"]
			if template == "" {
				template = DefaultOrchestratorPrompt
			}
		} else {
			template = userSettings["default_prompt_worker"]
			if template == "" {
				template = DefaultWorkerPrompt
			}
		}
		prompt += "\n\n" + strings.ReplaceAll(template, "{board_name}", boardName)
	}

	if prompt == "" {
		return
	}

	// Auto-accept trust/permission prompts before sending the real prompt
	acceptancePhrases := []string{
		"do you trust", "trust the files", "yes, proceed",
		"allow access", "(y)", "y/n", "press enter to", "to trust",
	}

	for i := 0; i < 5; i++ {
		time.Sleep(1 * time.Second)

		if backend == "pty" && h.backend != nil {
			// PTY: check captured content
			content, err := h.backend.CaptureContent(sessionName)
			if err != nil || content == "" {
				continue
			}
			lower := strings.ToLower(content)
			needsAccept := false
			for _, phrase := range acceptancePhrases {
				if strings.Contains(lower, phrase) {
					needsAccept = true
					break
				}
			}
			if needsAccept {
				log.Printf("Detected acceptance prompt in session %s, sending 'y'", sessionID[:8])
				h.backend.SendInput(sessionName, []byte("y\n"))
				time.Sleep(1 * time.Second)
				continue
			}
			if strings.Contains(content, ">") || strings.Contains(content, "❯") || strings.Contains(content, "...") {
				break
			}
		} else {
			// Tmux: capture pane
			paneText, _ := h.tmux.CapturePane(ctx, agentType, 50, agentType, sessionID)
			if paneText == "" {
				continue
			}
			lower := strings.ToLower(paneText)
			needsAccept := false
			for _, phrase := range acceptancePhrases {
				if strings.Contains(lower, phrase) {
					needsAccept = true
					break
				}
			}
			if needsAccept {
				log.Printf("Detected acceptance prompt in session %s, sending 'y'", sessionID[:8])
				h.tmux.SendKeysToTarget(ctx, sessionName+".0", "y")
				time.Sleep(1 * time.Second)
				continue
			}
			if strings.Contains(paneText, ">") || strings.Contains(paneText, "❯") || strings.Contains(paneText, "...") {
				break
			}
		}
	}

	// Send prompt with retry and verification
	maxAttempts := 3
	for attempt := 0; attempt < maxAttempts; attempt++ {
		time.Sleep(3 * time.Second)

		if backend == "pty" && h.backend != nil {
			h.backend.SendInput(sessionName, []byte(prompt+"\n"))
		} else {
			err := h.tmux.SendKeys(ctx, agentType, prompt, agentType, sessionID)
			if err != nil {
				log.Printf("Failed to send prompt to %s (attempt %d/%d): %v",
					sessionID[:8], attempt+1, maxAttempts, err)
				continue
			}
		}

		// Verify prompt was received
		time.Sleep(2 * time.Second)
		var paneText string
		if backend == "pty" && h.backend != nil {
			paneText, _ = h.backend.CaptureContent(sessionName)
		} else {
			paneText, _ = h.tmux.CapturePane(ctx, agentType, 100, agentType, sessionID)
		}

		if paneText != "" && len(prompt) > 40 && strings.Contains(paneText, prompt[:40]) {
			log.Printf("Prompt verified in session %s (attempt %d)", sessionID[:8], attempt+1)
			return
		}
		log.Printf("Prompt not found in pane for session %s (attempt %d/%d)",
			sessionID[:8], attempt+1, maxAttempts)
	}
	log.Printf("Failed to deliver prompt to session %s after %d attempts", sessionID[:8], maxAttempts)
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
