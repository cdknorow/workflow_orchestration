// Package ptymanager provides terminal session management via native PTY
// or tmux backends. It replaces direct tmux dependency for Mac App Store compatibility.
package ptymanager

// SessionInfo holds metadata about a running terminal session.
type SessionInfo struct {
	AgentName  string `json:"agent_name"`
	AgentType  string `json:"agent_type"`
	SessionID  string `json:"session_id"`
	WorkingDir string `json:"working_dir"`
	Running    bool   `json:"running"`
}

// TerminalBackend abstracts terminal session management.
// Both PTY and tmux backends implement this interface.
type TerminalBackend interface {
	// Spawn starts a new terminal session running the given command.
	Spawn(name, agentType, workDir, sessionID, command string, cols, rows uint16) error

	// Kill terminates a session and its child processes.
	Kill(name string) error

	// Restart kills and re-spawns a session with a new command.
	Restart(name, command string) error

	// SendInput writes raw bytes to the session's terminal input.
	SendInput(name string, data []byte) error

	// Resize changes the terminal dimensions.
	Resize(name string, cols, rows uint16) error

	// Subscribe registers a WebSocket subscriber for terminal output.
	// Returns a channel that receives raw PTY output bytes.
	Subscribe(name, subscriberID string) (<-chan []byte, error)

	// Unsubscribe removes a WebSocket subscriber.
	Unsubscribe(name, subscriberID string)

	// CaptureContent returns the current visible terminal content (for initial snapshot).
	// PTY backend returns recent buffered output; tmux backend calls capture-pane.
	CaptureContent(name string) (string, error)

	// ListSessions returns info about all active sessions.
	ListSessions() []SessionInfo

	// IsRunning returns true if the session's process is still running.
	IsRunning(name string) bool

	// LogPath returns the log file path for a session.
	LogPath(name string) string

	// Close shuts down all sessions and cleans up resources.
	Close() error
}
