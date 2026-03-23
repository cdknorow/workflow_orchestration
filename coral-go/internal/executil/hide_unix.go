//go:build !windows

package executil

import (
	"context"
	"os/exec"
)

// HideWindow sets platform-specific flags to prevent console windows from flashing.
// On Unix, this is a no-op.
func HideWindow(cmd *exec.Cmd) {
	// No-op on Unix — no console window to hide.
}

// Command is like exec.CommandContext but hides console windows on Windows.
func Command(ctx context.Context, name string, args ...string) *exec.Cmd {
	return exec.CommandContext(ctx, name, args...)
}
