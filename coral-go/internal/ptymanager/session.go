package ptymanager

import (
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/cdknorow/coral/internal/naming"
)

// session represents a single running PTY session.
type session struct {
	name       string
	agentType  string
	sessionID  string
	workingDir string
	command    string

	proc    ptyProcess
	logFile *os.File
	logPath string

	mu          sync.Mutex
	subscribers map[string]chan []byte

	// Ring buffer of recent output for snapshot on reconnect
	ringMu  sync.Mutex
	ring    []byte
	ringMax int
}

// newSession creates and starts a new PTY session.
func newSession(name, agentType, workDir, sessionID, command string, cols, rows uint16) (*session, error) {
	parts := parseCommand(command)
	if len(parts) == 0 {
		// Default to a shell if no command specified
		parts = defaultShell()
	}

	env := append(os.Environ(),
		"TERM=xterm-256color",
		"COLORTERM=truecolor",
		fmt.Sprintf("CORAL_SESSION_NAME=%s", name),
	)

	proc, err := startPTYProcess(parts[0], parts[1:], workDir, env, cols, rows)
	if err != nil {
		return nil, fmt.Errorf("startPTYProcess: %w", err)
	}

	// Set up log file (use os.TempDir for cross-platform support)
	logPath := naming.LogFile(os.TempDir(), agentType, sessionID)
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		log.Printf("Warning: could not open log file %s: %v", logPath, err)
	}

	s := &session{
		name:        name,
		agentType:   agentType,
		sessionID:   sessionID,
		workingDir:  workDir,
		command:     command,
		proc:        proc,
		logFile:     logFile,
		logPath:     logPath,
		subscribers: make(map[string]chan []byte),
		ring:        make([]byte, 0, 256*1024),
		ringMax:     256 * 1024, // 256KB ring buffer
	}

	// Start output reader goroutine
	go s.readLoop()

	return s, nil
}

// readLoop reads PTY output and fans out to log file and subscribers.
// When the PTY closes (EOF), the log file is closed here to prevent FD leaks
// if the session exits naturally without kill() being called.
func (s *session) readLoop() {
	defer func() {
		if s.logFile != nil {
			s.logFile.Close()
			s.logFile = nil
		}
	}()
	buf := make([]byte, 32*1024)
	for {
		n, err := s.proc.Read(buf)
		if n > 0 {
			data := make([]byte, n)
			copy(data, buf[:n])

			// Write to log file (for PULSE parsing, FTS indexing)
			if s.logFile != nil {
				s.logFile.Write(data)
			}

			// Store in ring buffer for snapshot
			s.ringMu.Lock()
			s.ring = append(s.ring, data...)
			if len(s.ring) > s.ringMax {
				s.ring = s.ring[len(s.ring)-s.ringMax:]
			}
			s.ringMu.Unlock()

			// Fan out to WebSocket subscribers (non-blocking)
			s.mu.Lock()
			for _, ch := range s.subscribers {
				select {
				case ch <- data:
				default:
					// Subscriber slow — drop frame
				}
			}
			s.mu.Unlock()
		}
		if err != nil {
			if err != io.EOF {
				log.Printf("PTY read error for %s: %v", s.name, err)
			}
			return
		}
	}
}

// sendInput writes raw bytes to the PTY (terminal input).
func (s *session) sendInput(data []byte) error {
	_, err := s.proc.Write(data)
	return err
}

// resize changes the PTY window size.
func (s *session) resize(cols, rows uint16) error {
	return s.proc.Resize(cols, rows)
}

// subscribe registers a subscriber for terminal output.
func (s *session) subscribe(id string) <-chan []byte {
	ch := make(chan []byte, 256)
	s.mu.Lock()
	s.subscribers[id] = ch
	s.mu.Unlock()
	return ch
}

// unsubscribe removes a subscriber.
func (s *session) unsubscribe(id string) {
	s.mu.Lock()
	if ch, ok := s.subscribers[id]; ok {
		close(ch)
		delete(s.subscribers, id)
	}
	s.mu.Unlock()
}

// captureContent returns the recent buffered output (for initial snapshot).
func (s *session) captureContent() string {
	s.ringMu.Lock()
	defer s.ringMu.Unlock()
	return string(s.ring)
}

// kill terminates the session and all child processes.
func (s *session) kill() error {
	// Graceful termination
	s.proc.Terminate()

	// Wait for graceful exit (5s timeout)
	select {
	case <-s.proc.Done():
		// Clean exit
	case <-time.After(5 * time.Second):
		// Force kill
		s.proc.ForceKill()
		<-s.proc.Done()
	}

	// Close PTY handle
	s.proc.Close()

	// Close log file (may already be nil if readLoop closed it on natural exit)
	if s.logFile != nil {
		s.logFile.Close()
		s.logFile = nil
	}

	// Close all subscriber channels
	s.mu.Lock()
	for id, ch := range s.subscribers {
		close(ch)
		delete(s.subscribers, id)
	}
	s.mu.Unlock()

	return nil
}

// isRunning returns true if the process is still alive.
func (s *session) isRunning() bool {
	select {
	case <-s.proc.Done():
		return false
	default:
		return true
	}
}

// parseCommand splits a shell command string into parts.
// Handles simple quoting but delegates complex cases to the platform shell.
func parseCommand(cmd string) []string {
	cmd = strings.TrimSpace(cmd)
	if cmd == "" {
		return nil
	}
	// If the command contains shell metacharacters, wrap via platform shell
	if strings.ContainsAny(cmd, "|&;`$(){}[]<>!~*?#") || strings.Contains(cmd, "&&") {
		return shellWrap(cmd)
	}
	return strings.Fields(cmd)
}

// defaultShell returns the platform default shell command.
// Respects CORAL_SHELL env var for user shell preference, then
// falls back to platform defaults.
func defaultShell() []string {
	// User override via CORAL_SHELL (e.g. "pwsh.exe", "/usr/bin/zsh", "bash")
	if cs := os.Getenv("CORAL_SHELL"); cs != "" {
		return []string{cs}
	}
	if runtime.GOOS == "windows" {
		// Prefer PowerShell if available (more capable for CLI tools)
		for _, ps := range []string{"pwsh.exe", "powershell.exe"} {
			if _, err := exec.LookPath(ps); err == nil {
				return []string{ps}
			}
		}
		return []string{"cmd.exe"}
	}
	if sh := os.Getenv("SHELL"); sh != "" {
		return []string{sh}
	}
	return []string{"/bin/sh"}
}
