package ptymanager

import (
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/creack/pty"
)

// session represents a single running PTY session.
type session struct {
	name       string
	agentType  string
	sessionID  string
	workingDir string
	command    string

	cmd     *exec.Cmd
	ptyFile *os.File
	logFile *os.File
	logPath string

	mu          sync.Mutex
	subscribers map[string]chan []byte

	// Ring buffer of recent output for snapshot on reconnect
	ringMu  sync.Mutex
	ring    []byte
	ringMax int

	done chan struct{} // closed when process exits
}

// newSession creates and starts a new PTY session.
func newSession(name, agentType, workDir, sessionID, command string, cols, rows uint16) (*session, error) {
	parts := parseCommand(command)
	if len(parts) == 0 {
		return nil, fmt.Errorf("empty command")
	}

	cmd := exec.Command(parts[0], parts[1:]...)
	cmd.Dir = workDir
	cmd.Env = append(os.Environ(),
		"TERM=xterm-256color",
		"COLORTERM=truecolor",
	)
	// Create new process group so we can kill the entire tree
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true,
		Pgid:    0,
	}

	winSize := &pty.Winsize{Rows: rows, Cols: cols}
	ptmx, err := pty.StartWithSize(cmd, winSize)
	if err != nil {
		return nil, fmt.Errorf("pty.StartWithSize: %w", err)
	}

	// Set up log file
	logPath := fmt.Sprintf("/tmp/%s_coral_%s.log", agentType, sessionID)
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
		cmd:         cmd,
		ptyFile:     ptmx,
		logFile:     logFile,
		logPath:     logPath,
		subscribers: make(map[string]chan []byte),
		ring:        make([]byte, 0, 256*1024),
		ringMax:     256 * 1024, // 256KB ring buffer
		done:        make(chan struct{}),
	}

	// Start output reader goroutine
	go s.readLoop()

	// Wait for process exit in background
	go func() {
		cmd.Wait()
		close(s.done)
	}()

	return s, nil
}

// readLoop reads PTY output and fans out to log file and subscribers.
func (s *session) readLoop() {
	buf := make([]byte, 32*1024)
	for {
		n, err := s.ptyFile.Read(buf)
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
	_, err := s.ptyFile.Write(data)
	return err
}

// resize changes the PTY window size.
func (s *session) resize(cols, rows uint16) error {
	return pty.Setsize(s.ptyFile, &pty.Winsize{Rows: rows, Cols: cols})
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
	// Send SIGTERM to process group
	if s.cmd.Process != nil {
		pgid, err := syscall.Getpgid(s.cmd.Process.Pid)
		if err == nil && pgid > 0 {
			syscall.Kill(-pgid, syscall.SIGTERM)
		} else {
			s.cmd.Process.Signal(syscall.SIGTERM)
		}
	}

	// Wait for graceful exit (5s timeout)
	select {
	case <-s.done:
		// Clean exit
	case <-time.After(5 * time.Second):
		// Force kill
		if s.cmd.Process != nil {
			pgid, err := syscall.Getpgid(s.cmd.Process.Pid)
			if err == nil && pgid > 0 {
				syscall.Kill(-pgid, syscall.SIGKILL)
			} else {
				s.cmd.Process.Kill()
			}
			<-s.done
		}
	}

	// Close PTY master
	s.ptyFile.Close()

	// Close log file
	if s.logFile != nil {
		s.logFile.Close()
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
	case <-s.done:
		return false
	default:
		return true
	}
}

// parseCommand splits a shell command string into parts.
// Handles simple quoting but delegates complex cases to sh -c.
func parseCommand(cmd string) []string {
	cmd = strings.TrimSpace(cmd)
	if cmd == "" {
		return nil
	}
	// If the command contains shell metacharacters, wrap in sh -c
	if strings.ContainsAny(cmd, "|&;`$(){}[]<>!~*?#") || strings.Contains(cmd, "&&") {
		return []string{"sh", "-c", cmd}
	}
	return strings.Fields(cmd)
}
