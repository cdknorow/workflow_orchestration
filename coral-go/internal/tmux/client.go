// Package tmux provides a client for interacting with tmux sessions.
package tmux

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Pane represents a tmux pane with its metadata.
type Pane struct {
	PaneTitle   string `json:"pane_title"`
	SessionName string `json:"session_name"`
	Target      string `json:"target"`
	CurrentPath string `json:"current_path"`
	SocketPath  string `json:"-"` // which tmux socket this pane was found on (empty = default)
}

// Client wraps tmux command execution.
type Client struct {
	// TmuxBin is the path to the tmux binary. Defaults to "tmux".
	TmuxBin string
	// SocketPath is an explicit tmux socket path (-S flag).
	// If set, all commands use this socket for consistent session visibility
	// across different launch contexts (terminal vs app bundle).
	SocketPath string
	// FallbackToDefault enables checking the default tmux socket (no -S flag)
	// when no sessions are found on the primary socket. This provides backward
	// compatibility with sessions created before the fixed socket was introduced.
	FallbackToDefault bool
	// sessionSockets caches which socket each session was found on.
	// Populated by ListPanes, used by runForTarget to route commands.
	sessionSockets map[string]string
}

// NewClient creates a new tmux Client.
// Uses ~/.coral/tmux.sock as the fixed socket with fallback to the default socket.
func NewClient() *Client {
	c := &Client{TmuxBin: "tmux", FallbackToDefault: true, sessionSockets: make(map[string]string)}

	// Find tmux binary if not on PATH (native app may not have /opt/homebrew/bin)
	if _, err := exec.LookPath(c.TmuxBin); err != nil {
		for _, p := range []string{"/opt/homebrew/bin/tmux", "/usr/local/bin/tmux", "/usr/bin/tmux"} {
			if _, err := os.Stat(p); err == nil {
				c.TmuxBin = p
				break
			}
		}
	}

	if sp := os.Getenv("CORAL_TMUX_SOCKET"); sp != "" {
		c.SocketPath = sp
	} else {
		home, _ := os.UserHomeDir()
		if home != "" {
			c.SocketPath = filepath.Join(home, ".coral", "tmux.sock")
		}
	}
	return c
}

// ListPanes returns all tmux panes with their titles, session names, and targets.
// If FallbackToDefault is enabled and the primary socket has no sessions,
// also checks the default tmux socket for backward compatibility.
func (c *Client) ListPanes(ctx context.Context) ([]Pane, error) {
	panes := c.listPanesOnSocket(ctx, c.SocketPath)

	// Always merge sessions from the default socket for backward compatibility
	if c.FallbackToDefault && c.SocketPath != "" {
		fallbackPanes := c.listPanesOnSocket(ctx, "")
		// Deduplicate by session name (prefer primary socket)
		seen := make(map[string]bool)
		for _, p := range panes {
			seen[p.SessionName] = true
		}
		for _, p := range fallbackPanes {
			if !seen[p.SessionName] {
				panes = append(panes, p)
			}
		}
	}

	// Cache which socket each session lives on
	for _, p := range panes {
		c.sessionSockets[p.SessionName] = p.SocketPath
	}

	return panes, nil
}

func (c *Client) listPanesOnSocket(ctx context.Context, socketPath string) []Pane {
	args := []string{"list-panes", "-a", "-F", "#{pane_title}|#{session_name}|#S:#I.#P|#{pane_current_path}"}
	if socketPath != "" {
		args = append([]string{"-S", socketPath}, args...)
	}
	cmd := exec.CommandContext(ctx, c.TmuxBin, args...)
	out, err := cmd.Output()
	if err != nil {
		return nil
	}

	var panes []Pane
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "|", 4)
		if len(parts) != 4 {
			continue
		}
		panes = append(panes, Pane{
			PaneTitle:   parts[0],
			SessionName: parts[1],
			Target:      parts[2],
			CurrentPath: parts[3],
			SocketPath:  socketPath,
		})
	}
	return panes
}

// FindPane finds the tmux pane for a given agent, matching by session_id or agent_name.
func (c *Client) FindPane(ctx context.Context, agentName string, agentType string, sessionID string) (*Pane, error) {
	panes, err := c.ListPanes(ctx)
	if err != nil {
		return nil, err
	}

	// Fast path: match by session_id in tmux session name
	if sessionID != "" {
		sidLow := strings.ToLower(sessionID)
		for i := range panes {
			if strings.Contains(strings.ToLower(panes[i].SessionName), sidLow) {
				return &panes[i], nil
			}
		}
	}

	// Fallback: fuzzy match by agent_name
	agentLow := strings.ToLower(agentName)
	normName := strings.ToLower(strings.ReplaceAll(agentName, "_", "-"))
	typeLow := strings.ToLower(agentType)

	var fallback *Pane
	for i := range panes {
		titleLow := strings.ToLower(panes[i].PaneTitle)
		sessLow := strings.ToLower(panes[i].SessionName)
		pathBase := strings.ToLower(filepath.Base(strings.TrimRight(panes[i].CurrentPath, "/")))

		nameMatch := strings.Contains(titleLow, agentLow) ||
			strings.Contains(titleLow, normName) ||
			strings.Contains(sessLow, agentLow) ||
			strings.Contains(sessLow, normName) ||
			agentLow == pathBase ||
			normName == pathBase

		if !nameMatch {
			continue
		}

		if typeLow != "" {
			if strings.Contains(titleLow, typeLow) || strings.Contains(sessLow, typeLow) {
				return &panes[i], nil
			}
			if fallback == nil {
				p := panes[i]
				fallback = &p
			}
		} else {
			return &panes[i], nil
		}
	}

	return fallback, nil
}

// FindPaneTarget returns the tmux target address for a given agent.
func (c *Client) FindPaneTarget(ctx context.Context, agentName, agentType, sessionID string) (string, error) {
	pane, err := c.FindPane(ctx, agentName, agentType, sessionID)
	if err != nil {
		return "", err
	}
	if pane == nil {
		return "", nil
	}
	return pane.Target, nil
}

// SendKeys sends a command to a tmux pane. Multi-line text is wrapped in bracket paste.
func (c *Client) SendKeys(ctx context.Context, agentName, command, agentType, sessionID string) error {
	target, err := c.FindPaneTarget(ctx, agentName, agentType, sessionID)
	if err != nil {
		return err
	}
	if target == "" {
		return fmt.Errorf("pane %q not found in any tmux session", agentName)
	}

	return c.SendKeysToTarget(ctx, target, command)
}

// SendKeysToTarget sends a command to a specific tmux target.
func (c *Client) SendKeysToTarget(ctx context.Context, target, command string) error {
	// Disable bracketed paste before each send — the shell may have re-enabled it
	// since session creation. Without this, tmux send-keys -l wraps text in
	// \e[200~ ... \e[201~ sequences, causing '00~' to leak into the command.
	c.disableBracketedPaste(ctx, target)

	if strings.Contains(command, "\n") {
		// Multi-line: wrap in bracket paste
		if err := c.sendBracketPasted(ctx, target, command); err != nil {
			return err
		}
	} else {
		// Single-line: send as literal text
		if _, err := c.run(ctx, "send-keys", "-t", target, "-l", command); err != nil {
			return fmt.Errorf("send-keys failed: %w", err)
		}
	}

	// Brief pause for tmux to deliver keystrokes
	time.Sleep(300 * time.Millisecond)

	// Send Enter
	if _, err := c.run(ctx, "send-keys", "-t", target, "Enter"); err != nil {
		return fmt.Errorf("send Enter failed: %w", err)
	}
	return nil
}

// SendRawKeys sends raw tmux key names (e.g. "BTab", "Escape") to a pane.
func (c *Client) SendRawKeys(ctx context.Context, agentName string, keys []string, agentType, sessionID string) error {
	target, err := c.FindPaneTarget(ctx, agentName, agentType, sessionID)
	if err != nil {
		return err
	}
	if target == "" {
		return fmt.Errorf("pane %q not found in any tmux session", agentName)
	}

	for _, key := range keys {
		if _, err := c.run(ctx, "send-keys", "-t", target, key); err != nil {
			return fmt.Errorf("send-keys %q failed: %w", key, err)
		}
		time.Sleep(100 * time.Millisecond)
	}
	return nil
}

// CapturePane captures the current content of a tmux pane.
func (c *Client) CapturePane(ctx context.Context, agentName string, lines int, agentType, sessionID string) (string, error) {
	target, err := c.FindPaneTarget(ctx, agentName, agentType, sessionID)
	if err != nil {
		return "", err
	}
	if target == "" {
		return "", nil
	}

	return c.CapturePaneTarget(ctx, target, lines)
}

// CapturePaneTarget captures pane content by target address.
func (c *Client) CapturePaneTarget(ctx context.Context, target string, lines int) (string, error) {
	stdout, err := c.run(ctx, "capture-pane", "-t", target, "-p", fmt.Sprintf("-S-%d", lines))
	if err != nil {
		return "", nil
	}
	return stdout, nil
}

// CapturePaneRawTarget captures pane content with ANSI sequences preserved.
// If visibleOnly is true, captures only the visible viewport (no scrollback).
// This is needed for TUI apps (vim, nano) that use the alternate screen buffer.
func (c *Client) CapturePaneRawTarget(ctx context.Context, target string, lines int, visibleOnly ...bool) (string, error) {
	args := []string{"capture-pane", "-t", target, "-p", "-e"}
	if len(visibleOnly) == 0 || !visibleOnly[0] {
		args = append(args, fmt.Sprintf("-S-%d", lines))
	}
	stdout, err := c.run(ctx, args...)
	if err != nil {
		return "", nil
	}
	return stdout, nil
}

// DisplayMessage runs tmux display-message to query pane state variables.
// Returns the formatted output string.
func (c *Client) DisplayMessage(ctx context.Context, target, format string) (string, error) {
	return c.run(ctx, "display-message", "-t", target, "-p", format)
}

// KillSession kills the tmux session for a given agent.
func (c *Client) KillSession(ctx context.Context, agentName, agentType, sessionID string) error {
	pane, err := c.FindPane(ctx, agentName, agentType, sessionID)
	if err != nil {
		return err
	}
	if pane == nil {
		return fmt.Errorf("pane %q not found in any tmux session", agentName)
	}

	_, err = c.run(ctx, "kill-session", "-t", pane.SessionName)
	if err != nil {
		return fmt.Errorf("kill-session failed: %w", err)
	}

	// Clean up log file
	if sessionID != "" {
		logDir := os.TempDir()
		logPath := filepath.Join(logDir, fmt.Sprintf("%s_coral_%s.log", agentType, sessionID))
		os.Remove(logPath)

		// Clean up settings temp file
		settingsFile := filepath.Join(os.TempDir(), fmt.Sprintf("coral_settings_%s.json", sessionID))
		os.Remove(settingsFile)
	}

	return nil
}

// KillSessionOnly kills a tmux session without cleaning up log/settings files.
// Used by sleep to preserve state for later wake.
func (c *Client) KillSessionOnly(ctx context.Context, agentName, agentType, sessionID string) error {
	pane, err := c.FindPane(ctx, agentName, agentType, sessionID)
	if err != nil {
		return err
	}
	if pane == nil {
		return fmt.Errorf("pane %q not found in any tmux session", agentName)
	}

	_, err = c.run(ctx, "kill-session", "-t", pane.SessionName)
	if err != nil {
		return fmt.Errorf("kill-session failed: %w", err)
	}
	return nil
}

// ResizePane resizes a tmux pane width.
func (c *Client) ResizePane(ctx context.Context, agentName string, columns int, agentType, sessionID string) error {
	target, err := c.FindPaneTarget(ctx, agentName, agentType, sessionID)
	if err != nil {
		return err
	}
	if target == "" {
		return fmt.Errorf("pane %q not found in any tmux session", agentName)
	}

	_, err = c.run(ctx, "resize-window", "-t", target, "-x", fmt.Sprintf("%d", columns))
	return err
}

// HasSession checks if a tmux session with the given name exists.
func (c *Client) HasSession(ctx context.Context, name string) bool {
	_, err := c.run(ctx, "has-session", "-t", name)
	return err == nil
}

// NewSession creates a new detached tmux session.
// It automatically disables bracketed paste mode to prevent '00~' characters
// from being prepended to commands sent via send-keys.
func (c *Client) NewSession(ctx context.Context, name, workDir string) error {
	_, err := c.run(ctx, "new-session", "-d", "-s", name, "-c", workDir)
	if err != nil {
		return err
	}
	// Disable bracketed paste mode in the new session.
	// Shells with bracketed paste enabled wrap pasted text with \e[200~ ... \e[201~
	// escape sequences, which causes '00~' to leak into commands sent via tmux send-keys.
	c.disableBracketedPaste(ctx, name+".0")
	return nil
}

// SetEnvironment sets an environment variable for a tmux session.
func (c *Client) SetEnvironment(ctx context.Context, session, key, value string) error {
	_, err := c.run(ctx, "set-environment", "-t", session, key, value)
	return err
}

// PipePane sets up pipe-pane logging for a tmux session.
func (c *Client) PipePane(ctx context.Context, target, logFile string) error {
	_, err := c.run(ctx, "pipe-pane", "-t", target, "-o", fmt.Sprintf("cat >> '%s'", logFile))
	return err
}

// ClosePipePane closes the existing pipe-pane for a target (kills the cat process).
func (c *Client) ClosePipePane(ctx context.Context, target string) error {
	_, err := c.run(ctx, "pipe-pane", "-t", target)
	return err
}

// RenameSession renames a tmux session.
func (c *Client) RenameSession(ctx context.Context, oldName, newName string) error {
	_, err := c.run(ctx, "rename-session", "-t", oldName, newName)
	return err
}

// RespawnPane kills the running process in a pane and spawns a fresh shell.
// Automatically disables bracketed paste mode in the new shell.
func (c *Client) RespawnPane(ctx context.Context, target, workDir string) error {
	args := []string{"respawn-pane", "-k", "-t", target}
	if workDir != "" {
		args = append(args, "-c", workDir)
	}
	_, err := c.run(ctx, args...)
	if err != nil {
		return err
	}
	// New shell will have bracketed paste enabled — disable it
	time.Sleep(300 * time.Millisecond)
	c.disableBracketedPaste(ctx, target)
	return nil
}

// ClearHistory clears the tmux pane scrollback history.
func (c *Client) ClearHistory(ctx context.Context, target string) error {
	_, err := c.run(ctx, "clear-history", "-t", target)
	return err
}

// Bracket paste mode escape sequences
var (
	bracketPasteStart = []string{"-H", "1b", "-H", "5b", "-H", "32", "-H", "30", "-H", "30", "-H", "7e"}
	bracketPasteEnd   = []string{"-H", "1b", "-H", "5b", "-H", "32", "-H", "30", "-H", "31", "-H", "7e"}
)

// disableBracketedPaste sends an escape sequence to disable bracketed paste
// mode in a tmux pane. This prevents '00~' characters from appearing before
// commands sent via send-keys.
func (c *Client) disableBracketedPaste(ctx context.Context, target string) {
	// Send \e[?2004l as raw hex bytes to disable bracketed paste mode.
	// Using -H sends the escape sequence directly to the terminal, avoiding
	// reliance on the shell being ready or the printf command echoing artifacts.
	// \e[?2004l = ESC [ ? 2 0 0 4 l = 1b 5b 3f 32 30 30 34 6c
	c.run(ctx, "send-keys", "-t", target,
		"-H", "1b", "-H", "5b", "-H", "3f", "-H", "32", "-H", "30", "-H", "30", "-H", "34", "-H", "6c")
	time.Sleep(100 * time.Millisecond)
}

func (c *Client) sendBracketPasted(ctx context.Context, target, text string) error {
	args := append([]string{"send-keys", "-t", target}, bracketPasteStart...)
	if _, err := c.run(ctx, args...); err != nil {
		return fmt.Errorf("bracket paste start failed: %w", err)
	}

	if _, err := c.run(ctx, "send-keys", "-t", target, "-l", text); err != nil {
		return fmt.Errorf("send-keys failed: %w", err)
	}

	args = append([]string{"send-keys", "-t", target}, bracketPasteEnd...)
	if _, err := c.run(ctx, args...); err != nil {
		return fmt.Errorf("bracket paste end failed: %w", err)
	}

	return nil
}

// SendTerminalInputToTarget sends raw terminal input data to a resolved tmux target.
// Handles control characters, escape sequences, and multi-line text.
func (c *Client) SendTerminalInputToTarget(ctx context.Context, target, data string) error {
	// Control character map
	ctrlMap := map[string]string{
		"\r":   "Enter",
		"\x7f": "BSpace",
		"\x1b": "Escape",
		"\t":   "Tab",
	}

	// Single control character
	if len(data) == 1 {
		if key, ok := ctrlMap[data]; ok {
			_, err := c.run(ctx, "send-keys", "-t", target, key)
			return err
		}
		// Ctrl+<letter> (0x01-0x1a)
		b := data[0]
		if b >= 1 && b <= 26 {
			key := fmt.Sprintf("C-%c", b+96)
			_, err := c.run(ctx, "send-keys", "-t", target, key)
			return err
		}
	}

	// Escape sequences (arrow keys, function keys, etc.)
	if len(data) > 0 && data[0] == '\x1b' {
		hexArgs := []string{"send-keys", "-t", target}
		for _, b := range []byte(data) {
			hexArgs = append(hexArgs, "-H", fmt.Sprintf("%02x", b))
		}
		_, err := c.run(ctx, hexArgs...)
		return err
	}

	// Multi-line text: wrap in bracket paste
	if strings.ContainsAny(data, "\n\r") {
		return c.sendBracketPasted(ctx, target, data)
	}

	// Single-line literal text
	_, err := c.run(ctx, "send-keys", "-t", target, "-l", data)
	return err
}

// ResizePaneTarget resizes a tmux pane width by target address.
func (c *Client) ResizePaneTarget(ctx context.Context, target string, columns int) error {
	_, err := c.run(ctx, "resize-window", "-t", target, "-x", fmt.Sprintf("%d", columns))
	return err
}

// run executes a tmux command. If the args target a session (-t flag),
// it routes to the correct socket using the session cache.
func (c *Client) run(ctx context.Context, args ...string) (string, error) {
	// Check if this command targets a session — route via cache
	if c.FallbackToDefault && c.SocketPath != "" {
		for i, arg := range args {
			if arg == "-t" && i+1 < len(args) {
				target := args[i+1]
				session := sessionFromTarget(target)
				if socket, ok := c.sessionSockets[session]; ok && socket != c.SocketPath {
					return c.runOnSocket(ctx, socket, args...)
				}
				break
			}
		}
	}
	// Default: use primary socket, fallback on error
	out, err := c.runOnSocket(ctx, c.SocketPath, args...)
	if err != nil && c.FallbackToDefault && c.SocketPath != "" {
		if fallbackOut, fallbackErr := c.runOnSocket(ctx, "", args...); fallbackErr == nil {
			return fallbackOut, nil
		}
	}
	return out, err
}

// runOnSocket runs a tmux command on a specific socket. Empty socketPath uses the default.
func (c *Client) runOnSocket(ctx context.Context, socketPath string, args ...string) (string, error) {
	if socketPath != "" {
		args = append([]string{"-S", socketPath}, args...)
	}
	cmd := exec.CommandContext(ctx, c.TmuxBin, args...)
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// sessionFromTarget extracts the session name from a tmux target (e.g. "name.0" → "name").
func sessionFromTarget(target string) string {
	if i := strings.IndexAny(target, ".:"); i > 0 {
		return target[:i]
	}
	return target
}

