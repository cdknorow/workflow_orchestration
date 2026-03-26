//go:build !windows

package main

import (
	"os"
	"syscall"
)

// redirectStderr duplicates the log file's fd onto stderr (fd 2) so that
// native/CGO crash messages (e.g. from systray or Cocoa frameworks) are
// captured in tray.log instead of being lost.
func redirectStderr(f *os.File) {
	syscall.Dup2(int(f.Fd()), int(os.Stderr.Fd()))
}

// detachProcessAttrs returns SysProcAttr for running a detached background process.
func detachProcessAttrs() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setsid: true}
}

// signalProcess sends a signal to a process. Used for --stop and PID liveness checks.
func signalProcess(pid int, sig syscall.Signal) error {
	return syscall.Kill(pid, sig)
}
