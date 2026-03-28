package ptymanager

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/cdknorow/coral/internal/naming"
	"github.com/cdknorow/coral/internal/pulse"
	"github.com/cdknorow/coral/internal/tmux"
)

// TmuxBackend implements TerminalBackend using tmux sessions.
// It wraps the existing tmux.Client to provide the same interface
// as PTYBackend, enabling seamless switching via config flag.
type TmuxBackend struct {
	client *tmux.Client
	logDir string

	mu       sync.RWMutex
	sessions map[string]*tmuxSession // keyed by session name
}

type tmuxSession struct {
	info    SessionInfo
	logPath string
}

// NewTmuxBackend creates a TmuxBackend wrapping the given tmux client.
func NewTmuxBackend(client *tmux.Client, logDir string) *TmuxBackend {
	return &TmuxBackend{
		client:   client,
		logDir:   logDir,
		sessions: make(map[string]*tmuxSession),
	}
}

func (b *TmuxBackend) Spawn(name, agentType, workDir, sessionID, command string, cols, rows uint16) error {
	ctx := context.Background()
	tmuxName := naming.SessionName(agentType, sessionID)
	logPath := naming.LogFile(b.logDir, agentType, sessionID)

	// Create empty log file
	os.WriteFile(logPath, []byte{}, 0644)

	// Create tmux session
	if err := b.client.NewSession(ctx, tmuxName, workDir); err != nil {
		return fmt.Errorf("tmux new-session: %w", err)
	}

	// Setup pipe-pane logging
	b.client.PipePane(ctx, tmuxName, logPath)

	// Set pane title using tmux native command (avoids shell echo issues)
	folderName := filepath.Base(strings.TrimRight(workDir, "/"))
	paneTitle := fmt.Sprintf("%s — %s", folderName, agentType)
	b.client.SetPaneTitle(ctx, tmuxName+".0", paneTitle)

	// Launch the agent command
	if command != "" {
		b.client.SendKeysToTarget(ctx, tmuxName+".0", command)
	}

	// Track session
	b.mu.Lock()
	b.sessions[name] = &tmuxSession{
		info: SessionInfo{
			AgentName:  name,
			AgentType:  agentType,
			SessionID:  sessionID,
			WorkingDir: workDir,
			Running:    true,
		},
		logPath: logPath,
	}
	b.mu.Unlock()

	return nil
}

func (b *TmuxBackend) Kill(name string) error {
	ctx := context.Background()

	b.mu.RLock()
	sess, ok := b.sessions[name]
	b.mu.RUnlock()

	if !ok {
		// Try to find by discovering tmux sessions
		return b.client.KillSession(ctx, name, "", "")
	}

	err := b.client.KillSession(ctx, name, sess.info.AgentType, sess.info.SessionID)

	b.mu.Lock()
	delete(b.sessions, name)
	b.mu.Unlock()

	return err
}

func (b *TmuxBackend) Restart(name, command string) error {
	ctx := context.Background()

	b.mu.RLock()
	sess, ok := b.sessions[name]
	b.mu.RUnlock()
	if !ok {
		return fmt.Errorf("session %q not found", name)
	}

	pane, err := b.client.FindPane(ctx, name, sess.info.AgentType, sess.info.SessionID)
	if err != nil || pane == nil {
		return fmt.Errorf("pane not found for %q", name)
	}

	// Close pipe-pane, respawn, re-establish
	b.client.ClosePipePane(ctx, pane.Target)
	if err := b.client.RespawnPane(ctx, pane.Target, sess.info.WorkingDir); err != nil {
		return err
	}

	time.Sleep(500 * time.Millisecond)
	b.client.PipePane(ctx, pane.Target, sess.logPath)

	if command != "" {
		b.client.SendKeysToTarget(ctx, pane.Target, command)
	}

	return nil
}

func (b *TmuxBackend) SendInput(name string, data []byte) error {
	ctx := context.Background()
	text := string(data)

	// Find the session info for agent type / session ID
	b.mu.RLock()
	sess, ok := b.sessions[name]
	b.mu.RUnlock()

	if ok {
		return b.client.SendKeys(ctx, name, text, sess.info.AgentType, sess.info.SessionID)
	}
	// Fallback: try without type/session hints
	return b.client.SendKeys(ctx, name, text, "", "")
}

func (b *TmuxBackend) Resize(name string, cols, rows uint16) error {
	ctx := context.Background()

	b.mu.RLock()
	sess, ok := b.sessions[name]
	b.mu.RUnlock()

	agentType, sessionID := "", ""
	if ok {
		agentType = sess.info.AgentType
		sessionID = sess.info.SessionID
	}
	return b.client.ResizePane(ctx, name, int(cols), agentType, sessionID)
}

func (b *TmuxBackend) Subscribe(name, subscriberID string) (<-chan []byte, error) {
	// Tmux backend doesn't support real-time subscription.
	// The WebSocket writer should fall back to polling capture-pane.
	// Return nil channel to signal "use polling mode".
	return nil, nil
}

func (b *TmuxBackend) Unsubscribe(name, subscriberID string) {
	// No-op for tmux backend (polling-based)
}

func (b *TmuxBackend) CaptureContent(name string) (string, error) {
	ctx := context.Background()

	b.mu.RLock()
	sess, ok := b.sessions[name]
	b.mu.RUnlock()

	agentType, sessionID := "", ""
	if ok {
		agentType = sess.info.AgentType
		sessionID = sess.info.SessionID
	}

	return b.client.CapturePane(ctx, name, 200, agentType, sessionID)
}

func (b *TmuxBackend) ListSessions() []SessionInfo {
	ctx := context.Background()
	panes, err := b.client.ListPanes(ctx)
	if err != nil {
		return nil
	}

	seen := make(map[string]bool)
	var sessions []SessionInfo

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

		sessions = append(sessions, SessionInfo{
			AgentName:  agentName,
			AgentType:  agentType,
			SessionID:  sessionID,
			WorkingDir: pane.CurrentPath,
			Running:    true,
		})
	}

	return sessions
}

func (b *TmuxBackend) IsRunning(name string) bool {
	ctx := context.Background()
	b.mu.RLock()
	sess, ok := b.sessions[name]
	b.mu.RUnlock()

	if !ok {
		return false
	}

	pane, _ := b.client.FindPane(ctx, name, sess.info.AgentType, sess.info.SessionID)
	return pane != nil
}

func (b *TmuxBackend) LogPath(name string) string {
	b.mu.RLock()
	defer b.mu.RUnlock()

	if sess, ok := b.sessions[name]; ok {
		return sess.logPath
	}
	return ""
}

func (b *TmuxBackend) Close() error {
	// Don't kill tmux sessions on close — they should persist
	return nil
}
