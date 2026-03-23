package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// GeminiAgent implements the Agent interface for Gemini CLI.
type GeminiAgent struct{}

func (a *GeminiAgent) AgentType() string    { return "gemini" }
func (a *GeminiAgent) SupportsResume() bool { return false }

func (a *GeminiAgent) HistoryBasePath() string {
	if v := os.Getenv("GEMINI_TMP_DIR"); v != "" {
		return v
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".gemini", "tmp")
}

func (a *GeminiAgent) HistoryGlobPattern() string { return "session-*.json" }

func (a *GeminiAgent) BuildLaunchCommand(params LaunchParams) string {
	bin := "gemini"
	if params.CLIPath != "" {
		bin = params.CLIPath
	}
	var parts []string

	// Inject system prompt: combine protocol file + board system prompt
	// Gemini uses GEMINI_SYSTEM_MD env var pointing to a markdown file
	var sysParts []string
	if params.ProtocolPath != "" {
		if content, err := os.ReadFile(params.ProtocolPath); err == nil {
			sysParts = append(sysParts, string(content))
		}
	}
	boardSysPrompt := BuildBoardSystemPrompt(params.BoardName, params.Role, params.Prompt, params.PromptOverrides, params.BoardType)
	if boardSysPrompt != "" {
		sysParts = append(sysParts, boardSysPrompt)
	}

	if len(sysParts) > 0 {
		sysFile := filepath.Join(os.TempDir(), fmt.Sprintf("coral_gemini_sys_%s.md", params.SessionID))
		os.WriteFile(sysFile, []byte(strings.Join(sysParts, "\n\n")), 0600)
		parts = append(parts, fmt.Sprintf(`GEMINI_SYSTEM_MD="%s"`, sysFile))
	}

	parts = append(parts, bin)

	if len(params.Flags) > 0 {
		parts = append(parts, params.Flags...)
	}

	// Build action prompt using shared helper
	cliPrompt := BuildBoardActionPrompt(params.BoardName, params.Role, params.Prompt, params.PromptOverrides, params.BoardType)
	if cliPrompt != "" {
		parts = append(parts, fmt.Sprintf(`"%s"`, strings.ReplaceAll(cliPrompt, `"`, `\"`)))
	}

	return strings.Join(parts, " ")
}

func (a *GeminiAgent) PrepareResume(sessionID, workingDir string) {
	// Gemini does not support resume
}
