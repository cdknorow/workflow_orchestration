package routes

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"nhooyr.io/websocket"
	"nhooyr.io/websocket/wsjson"

	"github.com/cdknorow/coral/internal/board"
)

// ── /ws/coral — Diff-based session list streaming ────────────────────

// WSCoral streams the coral-wide session list via WebSocket.
//
// First message is a full "coral_update" with all sessions.
// Subsequent messages are "coral_diff" with only changed/removed sessions
// to reduce bandwidth. Full session objects are sent per changed agent
// (no field-level diffs).
func (h *SessionsHandler) WSCoral(w http.ResponseWriter, r *http.Request) {
	if debugEnabled() {
		slog.Info("[debug] ws/coral connection from", "remote", r.RemoteAddr, "origin", r.Header.Get("Origin"))
	}
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		// Allow localhost origins (matches CORS config)
		InsecureSkipVerify: true,
	})
	if err != nil {
		slog.Debug("ws/coral accept failed", "error", err)
		return
	}
	defer func() {
		if debugEnabled() {
			slog.Info("[debug] ws/coral disconnected", "remote", r.RemoteAddr)
		}
		conn.CloseNow()
	}()

	ctx := conn.CloseRead(r.Context())

	// Per-connection state for diff calculation
	prevSessions := make(map[string]string) // session key -> json string
	prevRunsJSON := "[]"
	firstMessage := true

	pollInterval := time.Duration(h.cfg.WSPollIntervalS) * time.Second
	if pollInterval == 0 {
		pollInterval = 5 * time.Second
	}

	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			conn.Close(websocket.StatusNormalClosure, "")
			return
		case <-ticker.C:
		}

		sessions, err := h.buildSessionListForWS(r)
		if err != nil {
			slog.Warn("ws/coral build session list failed", "error", err)
			continue
		}

		// Fetch active job runs
		activeRuns := h.getActiveRuns(r.Context())

		// Build per-session state map for diff
		currSessions := make(map[string]string, len(sessions))
		sessionByKey := make(map[string]map[string]any, len(sessions))
		for _, s := range sessions {
			key := ""
			if sid, ok := s["session_id"].(string); ok && sid != "" {
				key = sid
			} else if name, ok := s["name"].(string); ok {
				key = name
			}
			if key == "" {
				continue
			}
			serialized, _ := json.Marshal(s)
			currSessions[key] = string(serialized)
			sessionByKey[key] = s
		}

		currRunsJSON, _ := json.Marshal(activeRuns)
		currRunsStr := string(currRunsJSON)

		if firstMessage {
			msg := map[string]any{
				"type":        "coral_update",
				"sessions":    sessions,
				"active_runs": activeRuns,
			}
			if err := wsjson.Write(ctx, conn, msg); err != nil {
				return
			}
			prevSessions = currSessions
			prevRunsJSON = currRunsStr
			firstMessage = false
			continue
		}

		// Calculate diff
		var changed []map[string]any
		for key, sJSON := range currSessions {
			if prevSessions[key] != sJSON {
				changed = append(changed, sessionByKey[key])
			}
		}

		var removed []string
		for key := range prevSessions {
			if _, exists := currSessions[key]; !exists {
				removed = append(removed, key)
			}
		}

		runsChanged := currRunsStr != prevRunsJSON

		if len(changed) > 0 || len(removed) > 0 || runsChanged {
			payload := map[string]any{"type": "coral_diff"}
			if len(changed) > 0 {
				payload["changed"] = changed
			}
			if len(removed) > 0 {
				payload["removed"] = removed
			}
			if runsChanged {
				payload["active_runs"] = activeRuns
			}
			if err := wsjson.Write(ctx, conn, payload); err != nil {
				return
			}
			prevSessions = currSessions
			prevRunsJSON = currRunsStr
		}
	}
}

// buildSessionListForWS builds the enriched session list (same fields as List handler).
func (h *SessionsHandler) buildSessionListForWS(r *http.Request) ([]map[string]any, error) {
	agents, err := h.discoverAgents(r)
	if err != nil {
		return nil, err
	}

	ctx := r.Context()
	sessionIDs := make([]string, 0, len(agents))
	for _, a := range agents {
		if a.SessionID != "" {
			sessionIDs = append(sessionIDs, a.SessionID)
		}
	}
	displayNames, _ := h.ss.GetDisplayNames(ctx, sessionIDs)

	// Latest events for waiting/done/working state
	latestEvents, _ := h.ts.GetLatestEventTypes(ctx, sessionIDs)
	if latestEvents == nil {
		latestEvents = map[string][2]string{}
	}

	// Board subscriptions (keyed by tmux session name)
	var boardSubs map[string]*board.Subscriber
	if h.bs != nil {
		boardSubs, _ = h.bs.GetAllSubscriptions(ctx)
	}
	if boardSubs == nil {
		boardSubs = map[string]*board.Subscriber{}
	}

	// Fallback: board_name from live_sessions DB
	liveBoardNames := make(map[string][2]string)
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
	liveSIDs := make(map[string]bool)
	for _, agent := range agents {
		logInfo := getLogStatus(agent.LogPath)
		sid := agent.SessionID

		// Compute waiting/done/working from latest event
		ev := latestEvents[sid]
		latestEv := ev[0]
		evSummary := ev[1]
		needsInput := latestEv == "notification"
		done := latestEv == "stop"
		staleF, _ := logInfo["staleness_seconds"].(float64)
		working := latestEv == "tool_use" || latestEv == "prompt_submit"
		if working && strings.HasPrefix(evSummary, "Ran: sleep") {
			working = false
		}
		if working && staleF > 420 {
			working = false
		}

		var waitingReason, waitingSummary any
		if needsInput {
			waitingReason = latestEv
			waitingSummary = evSummary
		}

		entry := map[string]any{
			"name":               agent.AgentName,
			"agent_type":         agent.AgentType,
			"session_id":         sid,
			"tmux_session":       agent.TmuxSession,
			"status":             logInfo["status"],
			"summary":            logInfo["summary"],
			"staleness_seconds":  logInfo["staleness_seconds"],
			"display_name":       nilIfEmpty(displayNames[sid]),
			"working_directory":  agent.WorkingDir,
			"waiting_for_input":  needsInput,
			"done":               done,
			"stuck":              false,
			"waiting_reason":     waitingReason,
			"waiting_summary":    waitingSummary,
			"working":            working,
			"changed_file_count": 0,
			"commands":           map[string]string{"compress": "/compact", "clear": "/clear"},
			"board_project":      boardProject(boardSubs, liveBoardNames, agent.TmuxSession, sid),
			"board_job_title":    boardJobTitle(boardSubs, liveBoardNames, agent.TmuxSession, sid),
			"board_unread":       0,
			"log_path":           agent.LogPath,
			"sleeping":           false,
		}
		liveSIDs[sid] = true
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
	}

	return sessions, nil
}

// getActiveRuns fetches active job runs for the Jobs sidebar.
func (h *SessionsHandler) getActiveRuns(ctx context.Context) []map[string]any {
	// Stub — will be wired when ScheduleStore is injected into SessionsHandler
	return []map[string]any{}
}

// ── /ws/terminal/{name} — Bidirectional terminal WebSocket ──────────

// WSTerminal provides bidirectional terminal WebSocket.
//
// With PTY backend: true real-time streaming via goroutine fan-out.
// With tmux backend: adaptive polling with capture-pane.
func (h *SessionsHandler) WSTerminal(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	agentType := r.URL.Query().Get("agent_type")
	sessionID := r.URL.Query().Get("session_id")

	if debugEnabled() {
		slog.Info("[debug] ws/terminal connect", "name", name, "agent_type", agentType, "session_id", sessionID, "remote", r.RemoteAddr)
	}

	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		InsecureSkipVerify: true,
	})
	if err != nil {
		slog.Debug("ws/terminal accept failed", "error", err)
		return
	}
	defer func() {
		if debugEnabled() {
			slog.Info("[debug] ws/terminal disconnected", "name", name, "session_id", sessionID)
		}
		conn.CloseNow()
	}()

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	// Try PTY streaming mode first
	if h.backend != nil {
		ch, err := h.backend.Subscribe(name, fmt.Sprintf("ws-%d", time.Now().UnixNano()))
		if err == nil && ch != nil {
			if debugEnabled() {
				slog.Info("[debug] ws/terminal using PTY streaming", "name", name)
			}
			h.wsTerminalStreaming(ctx, conn, name, ch)
			return
		}
	}

	if debugEnabled() {
		slog.Info("[debug] ws/terminal using tmux polling", "name", name)
	}
	// Fallback to tmux polling mode
	h.wsTerminalPolling(ctx, conn, r, name)
}

// wsTerminalStreaming handles the WebSocket using PTY streaming (zero-polling).
func (h *SessionsHandler) wsTerminalStreaming(ctx context.Context, conn *websocket.Conn, name string, dataCh <-chan []byte) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	subscriberID := fmt.Sprintf("ws-%d", time.Now().UnixNano())
	defer func() {
		if h.backend != nil {
			h.backend.Unsubscribe(name, subscriberID)
		}
	}()

	// Send initial snapshot
	if h.backend != nil {
		if content, err := h.backend.CaptureContent(name); err == nil && content != "" {
			msg := map[string]any{
				"type": "terminal_stream",
				"data": content,
			}
			wsjson.Write(ctx, conn, msg)
		}
	}

	// Reader goroutine: receives terminal input from the client
	go func() {
		defer cancel()
		for {
			_, raw, err := conn.Read(ctx)
			if err != nil {
				return
			}
			var msg struct {
				Type string `json:"type"`
				Data string `json:"data"`
				Cols int    `json:"cols"`
				Rows int    `json:"rows"`
			}
			if err := json.Unmarshal(raw, &msg); err != nil {
				continue
			}
			switch msg.Type {
			case "terminal_input":
				if msg.Data != "" && h.backend != nil {
					h.backend.SendInput(name, []byte(msg.Data))
				}
			case "terminal_resize":
				if msg.Cols >= 10 && h.backend != nil {
					rows := uint16(msg.Rows)
					if rows == 0 {
						rows = 50
					}
					h.backend.Resize(name, uint16(msg.Cols), rows)
				}
			}
		}
	}()

	// Writer loop: forward PTY output to WebSocket
	for {
		select {
		case <-ctx.Done():
			conn.Close(websocket.StatusNormalClosure, "")
			return
		case data, ok := <-dataCh:
			if !ok {
				// Channel closed — session ended
				wsjson.Write(ctx, conn, map[string]any{"type": "terminal_closed"})
				conn.Close(websocket.StatusNormalClosure, "session ended")
				return
			}
			msg := map[string]any{
				"type": "terminal_stream",
				"data": string(data),
			}
			if err := wsjson.Write(ctx, conn, msg); err != nil {
				return
			}
		}
	}
}

// wsTerminalPolling handles the WebSocket using tmux capture-pane polling.
func (h *SessionsHandler) wsTerminalPolling(ctx context.Context, conn *websocket.Conn, r *http.Request, name string) {
	agentType := r.URL.Query().Get("agent_type")
	sessionID := r.URL.Query().Get("session_id")

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Resolve pane target
	target, err := h.terminal.FindTarget(ctx, name, agentType, sessionID)
	if err != nil || target == "" {
		conn.Close(websocket.StatusInternalError, "pane not found")
		return
	}

	var (
		lastContent string
		closed      bool
		closedMu    sync.Mutex
		inputEvent  = make(chan struct{}, 1)
		targetMu    sync.Mutex
	)

	isClosed := func() bool {
		closedMu.Lock()
		defer closedMu.Unlock()
		return closed
	}
	setClosed := func() {
		closedMu.Lock()
		closed = true
		closedMu.Unlock()
		cancel()
	}

	// Reader goroutine
	go func() {
		defer setClosed()
		for {
			_, raw, err := conn.Read(ctx)
			if err != nil {
				return
			}
			var msg struct {
				Type string `json:"type"`
				Data string `json:"data"`
				Cols int    `json:"cols"`
			}
			if err := json.Unmarshal(raw, &msg); err != nil {
				continue
			}
			targetMu.Lock()
			currentTarget := target
			targetMu.Unlock()
			switch msg.Type {
			case "terminal_input":
				if msg.Data != "" && currentTarget != "" {
					h.terminal.SendTerminalInput(ctx, currentTarget, msg.Data)
					select {
					case inputEvent <- struct{}{}:
					default:
					}
				}
			case "terminal_resize":
				if msg.Cols >= 10 && currentTarget != "" {
					h.terminal.ResizeTarget(ctx, currentTarget, msg.Cols)
				}
			}
		}
	}()

	// Writer loop: file-triggered polling with cursor/TUI support.
	//
	// Instead of fixed-interval polling, watches the agent's log file
	// (written by tmux pipe-pane) for mtime changes. This gives near-real-time
	// latency with zero capture cost when idle.
	//
	// Three triggers cause a capture:
	// - Log file mtime changed (new output from agent)
	// - User input event (keystroke echo)
	// - Heartbeat every 2s (detect pane disappearance)

	logPath := h.findLogPath(agentType, sessionID)
	var lastMtime time.Time
	if logPath != "" {
		if info, err := os.Stat(logPath); err == nil {
			lastMtime = info.ModTime()
		}
	}

	const minCaptureInterval = 15 * time.Millisecond
	lastCaptureTime := time.Time{}
	lastCursorX, lastCursorY := -1, -1
	paneGoneNotified := false

	doCapture := func() {
		now := time.Now()
		if now.Sub(lastCaptureTime) < minCaptureInterval {
			return
		}
		lastCaptureTime = now

		targetMu.Lock()
		currentTarget := target
		targetMu.Unlock()

		if currentTarget == "" {
			newTarget, _ := h.terminal.FindTarget(ctx, name, agentType, sessionID)
			if newTarget != "" {
				targetMu.Lock()
				target = newTarget
				currentTarget = newTarget
				targetMu.Unlock()
			}
		}

		if currentTarget == "" {
			if !paneGoneNotified {
				wsjson.Write(ctx, conn, map[string]any{"type": "terminal_closed"})
				paneGoneNotified = true
			}
			return
		}

		// Query cursor position and alternate screen mode in one call
		cursorX, cursorY := -1, -1
		altScreen := false
		if infoOut, err := h.terminal.DisplayMessage(ctx, currentTarget, "#{cursor_x},#{cursor_y},#{alternate_on}"); err == nil {
			parts := strings.SplitN(strings.TrimSpace(infoOut), ",", 3)
			if len(parts) >= 3 {
				cursorX, _ = strconv.Atoi(parts[0])
				cursorY, _ = strconv.Atoi(parts[1])
				altScreen = parts[2] == "1"
			}
		}

		// Use visible-only capture when a TUI app is using the alternate screen buffer
		content, _ := h.terminal.CaptureRawOutput(ctx, currentTarget, 200, altScreen)
		if content != "" {
			paneGoneNotified = false
			if content != lastContent || cursorX != lastCursorX || cursorY != lastCursorY {
				msg := map[string]any{
					"type":    "terminal_update",
					"content": content,
				}
				if cursorX >= 0 {
					msg["cursor_x"] = cursorX
					msg["cursor_y"] = cursorY
				}
				if err := wsjson.Write(ctx, conn, msg); err != nil {
					return
				}
				lastContent = content
				lastCursorX = cursorX
				lastCursorY = cursorY
			}
		} else if !paneGoneNotified {
			wsjson.Write(ctx, conn, map[string]any{"type": "terminal_closed"})
			paneGoneNotified = true
		}
	}

	// Initial snapshot
	doCapture()

	for !isClosed() {
		if paneGoneNotified {
			// Pane is gone — slow heartbeat to detect if it comes back
			select {
			case <-ctx.Done():
				conn.Close(websocket.StatusNormalClosure, "")
				return
			case <-inputEvent:
			case <-time.After(3 * time.Second):
			}
			targetMu.Lock()
			target = ""
			targetMu.Unlock()
			doCapture()
			continue
		}

		// Check if log file changed (cheap stat syscall ~0.01ms)
		fileChanged := false
		if logPath != "" {
			if info, err := os.Stat(logPath); err == nil {
				if info.ModTime() != lastMtime {
					lastMtime = info.ModTime()
					fileChanged = true
				}
			}
		}

		if fileChanged {
			doCapture()
		}

		// Wait for input event or next stat check (10ms)
		select {
		case <-ctx.Done():
			conn.Close(websocket.StatusNormalClosure, "")
			return
		case <-inputEvent:
			doCapture()
		case <-time.After(10 * time.Millisecond):
			// Periodic stat check — also serves as heartbeat
		}
	}

	conn.Close(websocket.StatusNormalClosure, "")
}
