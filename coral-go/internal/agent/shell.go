package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// ShellType identifies the type of shell being used for agent sessions.
type ShellType string

const (
	ShellBash       ShellType = "bash"
	ShellZsh        ShellType = "zsh"
	ShellPowerShell ShellType = "powershell"
	ShellCmd        ShellType = "cmd"
)

// DetectShell determines the shell type from CORAL_SHELL env var,
// falling back to platform defaults.
func DetectShell() ShellType {
	if env := os.Getenv("CORAL_SHELL"); env != "" {
		return classifyShell(env)
	}

	if runtime.GOOS == "windows" {
		return ShellPowerShell
	}

	// Unix: check $SHELL
	if sh := os.Getenv("SHELL"); sh != "" {
		return classifyShell(sh)
	}
	return ShellBash
}

// classifyShell maps a shell path or name to a ShellType.
func classifyShell(shell string) ShellType {
	base := strings.ToLower(filepath.Base(shell))
	// Strip .exe suffix for Windows
	base = strings.TrimSuffix(base, ".exe")

	switch {
	case base == "pwsh" || base == "powershell":
		return ShellPowerShell
	case base == "cmd":
		return ShellCmd
	case base == "zsh":
		return ShellZsh
	default:
		// bash, sh, git-bash, wsl, etc.
		return ShellBash
	}
}

// AppBundleBinDir returns the directory containing hook binaries if the
// current executable is running from a macOS .app bundle or a similar
// packaged install. Returns empty string if not in a bundle.
func AppBundleBinDir() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	exe, err = filepath.EvalSymlinks(exe)
	if err != nil {
		return ""
	}
	dir := filepath.Dir(exe)

	// macOS .app bundle: .../Coral.app/Contents/MacOS/coral
	if strings.Contains(dir, ".app"+string(os.PathSeparator)+"Contents"+string(os.PathSeparator)+"MacOS") ||
		strings.HasSuffix(dir, ".app/Contents/MacOS") {
		return dir
	}

	// Windows/Linux installed: check if hook binaries exist alongside the executable
	hookPath := filepath.Join(dir, "coral-hook-agentic-state")
	if runtime.GOOS == "windows" {
		hookPath += ".exe"
	}
	if _, err := os.Stat(hookPath); err == nil {
		return dir
	}

	return ""
}

// PrefixWithPathEnv returns a shell command prefix that prepends the given
// directory to PATH. Returns empty string if binDir is empty.
func PrefixWithPathEnv(binDir string) string {
	if binDir == "" {
		return ""
	}
	shell := DetectShell()
	switch shell {
	case ShellPowerShell:
		return fmt.Sprintf(`$env:PATH="%s;$env:PATH"; `, binDir)
	case ShellCmd:
		return fmt.Sprintf(`set "PATH=%s;%%PATH%%" && `, binDir)
	default:
		return fmt.Sprintf(`export PATH="%s:$PATH" && `, binDir)
	}
}

// WrapWithBundlePath prepends the app bundle bin directory to PATH in the
// given command string, if running from a packaged install. No-op otherwise.
func WrapWithBundlePath(cmd string) string {
	if prefix := PrefixWithPathEnv(AppBundleBinDir()); prefix != "" {
		return prefix + cmd
	}
	return cmd
}

// FormatPromptFileArg returns shell-appropriate syntax for reading a prompt
// file and passing its content as a CLI argument.
func FormatPromptFileArg(promptFile string) string {
	shell := DetectShell()
	switch shell {
	case ShellPowerShell:
		return fmt.Sprintf("$(Get-Content -Raw '%s')", promptFile)
	case ShellCmd:
		// cmd.exe doesn't support inline file content substitution.
		// Use a workaround: pipe file content. But since this is a positional
		// arg, we use PowerShell-style (cmd users should use PowerShell for agents).
		return fmt.Sprintf("$(Get-Content -Raw '%s')", promptFile)
	default:
		// bash, zsh, sh
		return fmt.Sprintf("\"$(cat '%s')\"", promptFile)
	}
}
