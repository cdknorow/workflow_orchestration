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
	bin := resolveBinary(params.CLIPath, "gemini")
	var parts []string

	// Inject system prompt: combine protocol file + board system prompt
	// Gemini uses GEMINI_SYSTEM_MD env var pointing to a markdown file
	var sysParts []string
	if proto := readProtocolFile(params.ProtocolPath); proto != "" {
		sysParts = append(sysParts, proto)
	}
	boardSysPrompt := BuildBoardSystemPrompt(params.BoardName, params.Role, params.Prompt, params.PromptOverrides, params.BoardType)
	if boardSysPrompt != "" {
		sysParts = append(sysParts, boardSysPrompt)
	}

	if len(sysParts) > 0 {
		sysFile := writeTempFile("gemini_sys", params.SessionID, "md", []byte(strings.Join(sysParts, "\n\n")))
		parts = append(parts, fmt.Sprintf(`GEMINI_SYSTEM_MD="%s"`, sysFile))
	}

	if params.SessionName != "" {
		parts = append(parts, fmt.Sprintf(`CORAL_SESSION_NAME="%s"`, params.SessionName))
	}
	if params.Role != "" {
		parts = append(parts, fmt.Sprintf(`CORAL_SUBSCRIBER_ID="%s"`, params.Role))
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

