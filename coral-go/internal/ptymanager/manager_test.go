package ptymanager

import (
	"os/exec"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/creack/pty"
)

func canFork(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh not available")
	}
	// Test if PTY fork/exec with process group is permitted
	cmd := exec.Command("true")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Pgid: 0}
	ptmx, err := pty.StartWithSize(cmd, &pty.Winsize{Rows: 24, Cols: 80})
	if err != nil {
		t.Skipf("pty.StartWithSize not permitted: %v", err)
	}
	ptmx.Close()
	cmd.Wait()
}

func TestPTYBackendSpawnAndKill(t *testing.T) {
	canFork(t)
	backend := NewPTYBackend()
	defer backend.Close()

	err := backend.Spawn("test-agent", "test", t.TempDir(), "sid-123", "sh -c 'echo hello; sleep 60'", 80, 24)
	if err != nil {
		t.Fatalf("Spawn failed: %v", err)
	}

	if !backend.IsRunning("test-agent") {
		t.Error("expected session to be running")
	}

	sessions := backend.ListSessions()
	if len(sessions) != 1 {
		t.Fatalf("expected 1 session, got %d", len(sessions))
	}
	if sessions[0].AgentName != "test-agent" {
		t.Errorf("expected agent name 'test-agent', got %q", sessions[0].AgentName)
	}

	err = backend.Kill("test-agent")
	if err != nil {
		t.Fatalf("Kill failed: %v", err)
	}

	if backend.IsRunning("test-agent") {
		t.Error("expected session to not be running after kill")
	}
}

func TestPTYBackendSendInputAndRead(t *testing.T) {
	canFork(t)
	backend := NewPTYBackend()
	defer backend.Close()

	// Use cat which echoes input
	err := backend.Spawn("echo-test", "test", t.TempDir(), "sid-echo", "cat", 80, 24)
	if err != nil {
		t.Fatalf("Spawn failed: %v", err)
	}

	ch, err := backend.Subscribe("echo-test", "ws-1")
	if err != nil {
		t.Fatalf("Subscribe failed: %v", err)
	}

	// Send input
	err = backend.SendInput("echo-test", []byte("hello\n"))
	if err != nil {
		t.Fatalf("SendInput failed: %v", err)
	}

	// Read output (should see "hello" echoed back)
	var output strings.Builder
	timeout := time.After(3 * time.Second)
	for {
		select {
		case data, ok := <-ch:
			if !ok {
				goto done
			}
			output.Write(data)
			if strings.Contains(output.String(), "hello") {
				goto done
			}
		case <-timeout:
			t.Fatalf("timeout waiting for output, got: %q", output.String())
		}
	}
done:

	if !strings.Contains(output.String(), "hello") {
		t.Errorf("expected output to contain 'hello', got %q", output.String())
	}

	backend.Unsubscribe("echo-test", "ws-1")
	backend.Kill("echo-test")
}

func TestPTYBackendResize(t *testing.T) {
	canFork(t)
	backend := NewPTYBackend()
	defer backend.Close()

	err := backend.Spawn("resize-test", "test", t.TempDir(), "sid-resize", "sh", 80, 24)
	if err != nil {
		t.Fatalf("Spawn failed: %v", err)
	}

	// Resize should not error
	err = backend.Resize("resize-test", 120, 40)
	if err != nil {
		t.Errorf("Resize failed: %v", err)
	}

	backend.Kill("resize-test")
}

func TestPTYBackendCaptureContent(t *testing.T) {
	canFork(t)
	backend := NewPTYBackend()
	defer backend.Close()

	err := backend.Spawn("capture-test", "test", t.TempDir(), "sid-cap", "sh -c 'echo MARKER_123'", 80, 24)
	if err != nil {
		t.Fatalf("Spawn failed: %v", err)
	}

	// Wait for output
	time.Sleep(500 * time.Millisecond)

	content, err := backend.CaptureContent("capture-test")
	if err != nil {
		t.Fatalf("CaptureContent failed: %v", err)
	}

	if !strings.Contains(content, "MARKER_123") {
		t.Errorf("expected capture to contain 'MARKER_123', got %q", content)
	}

	backend.Kill("capture-test")
}

func TestPTYBackendLogPath(t *testing.T) {
	canFork(t)
	backend := NewPTYBackend()
	defer backend.Close()

	err := backend.Spawn("log-test", "claude", t.TempDir(), "sid-log", "echo test", 80, 24)
	if err != nil {
		t.Fatalf("Spawn failed: %v", err)
	}

	logPath := backend.LogPath("log-test")
	if logPath == "" {
		t.Error("expected non-empty log path")
	}
	if !strings.Contains(logPath, "claude_coral_sid-log.log") {
		t.Errorf("unexpected log path: %q", logPath)
	}

	backend.Kill("log-test")
}

func TestPTYBackendDuplicateSpawn(t *testing.T) {
	canFork(t)
	backend := NewPTYBackend()
	defer backend.Close()

	err := backend.Spawn("dup-test", "test", t.TempDir(), "sid-dup", "sh", 80, 24)
	if err != nil {
		t.Fatalf("first Spawn failed: %v", err)
	}

	err = backend.Spawn("dup-test", "test", t.TempDir(), "sid-dup2", "sh", 80, 24)
	if err == nil {
		t.Error("expected error on duplicate spawn")
	}

	backend.Kill("dup-test")
}

func TestPTYBackendNotFound(t *testing.T) {
	backend := NewPTYBackend()

	err := backend.Kill("nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent session")
	}

	err = backend.SendInput("nonexistent", []byte("test"))
	if err == nil {
		t.Error("expected error for nonexistent session")
	}

	_, err = backend.CaptureContent("nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent session")
	}

	if backend.IsRunning("nonexistent") {
		t.Error("expected false for nonexistent session")
	}
}

func TestParseCommand(t *testing.T) {
	tests := []struct {
		input string
		want  []string
	}{
		{"echo hello", []string{"echo", "hello"}},
		{"sh -c 'echo test'", []string{"sh", "-c", "'echo", "test'"}}, // no metachar, fields split
		{"ls | grep foo", []string{"sh", "-c", "ls | grep foo"}},
		{"claude --session-id abc", []string{"claude", "--session-id", "abc"}},
		{"", nil},
	}

	for _, tt := range tests {
		got := parseCommand(tt.input)
		if tt.want == nil && got != nil {
			t.Errorf("parseCommand(%q) = %v, want nil", tt.input, got)
			continue
		}
		if len(got) != len(tt.want) {
			t.Errorf("parseCommand(%q) = %v, want %v", tt.input, got, tt.want)
			continue
		}
	}
}
