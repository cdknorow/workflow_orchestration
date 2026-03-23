//go:build windows

package executil

import (
	"context"
	"os/exec"
	"syscall"
)

// HideWindow sets platform-specific flags to prevent console windows from flashing.
// On Windows, this sets CREATE_NO_WINDOW to suppress the console.
func HideWindow(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.CreationFlags |= 0x08000000 // CREATE_NO_WINDOW
}

// Command is like exec.CommandContext but hides console windows on Windows.
func Command(ctx context.Context, name string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, name, args...)
	HideWindow(cmd)
	return cmd
}
