// Package ptymanager provides terminal backend abstractions.
//
// SessionTerminal abstracts all terminal operations used by the HTTP sessions
// handler. It has two implementations: TmuxSessionTerminal (wrapping
// tmux.Client) and PTYSessionTerminal (wrapping PTYBackend).
package ptymanager

import "context"

// PaneInfo describes a running agent session (backend-agnostic).
type PaneInfo struct {
	PaneTitle   string `json:"pane_title"`
	SessionName string `json:"session_name"`
	Target      string `json:"target"`      // tmux target or PTY process ID
	CurrentPath string `json:"current_path"`
}

// SessionTerminal abstracts all terminal operations for the sessions handler.
// This allows the HTTP layer to work identically with tmux or PTY backends.
type SessionTerminal interface {
	// Discovery
	ListSessions(ctx context.Context) ([]PaneInfo, error)
	FindSession(ctx context.Context, name, agentType, sessionID string) (*PaneInfo, error)

	// Output capture
	CaptureOutput(ctx context.Context, name string, lines int, agentType, sessionID string) (string, error)

	// Input
	SendInput(ctx context.Context, name, command, agentType, sessionID string) error
	SendRawInput(ctx context.Context, name string, keys []string, agentType, sessionID string) error
	SendToTarget(ctx context.Context, target, command string) error
	SendTerminalInput(ctx context.Context, target, data string) error

	// Lifecycle
	CreateSession(ctx context.Context, name, workDir string) error
	KillSession(ctx context.Context, name, agentType, sessionID string) error
	KillSessionOnly(ctx context.Context, name, agentType, sessionID string) error
	RestartPane(ctx context.Context, target, workDir string) error
	RenameSession(ctx context.Context, oldName, newName string) error
	ResizeSession(ctx context.Context, name string, columns int, agentType, sessionID string) error
	ResizeTarget(ctx context.Context, target string, columns int) error

	// Logging
	StartLogging(ctx context.Context, target, logPath string) error
	StopLogging(ctx context.Context, target string) error
	ClearHistory(ctx context.Context, target string) error

	// Pane title (native tmux command, avoids shell echo)
	SetPaneTitle(ctx context.Context, target, title string)

	// Query
	HasSession(ctx context.Context, name string) bool
	DisplayMessage(ctx context.Context, target, format string) (string, error)

	// Target-level operations (used by WebSocket terminal)
	FindTarget(ctx context.Context, name, agentType, sessionID string) (string, error)
	CaptureRawOutput(ctx context.Context, target string, lines int, visibleOnly bool) (string, error)

	// AttachCommand returns the shell command to attach to a session (includes -S socket if needed).
	AttachCommand(sessionName string) string
}
