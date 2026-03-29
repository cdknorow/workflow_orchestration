package agent

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
)

// GeminiAgent implements the Agent interface for Gemini CLI.
type GeminiAgent struct{}

func (a *GeminiAgent) AgentType() string    { return "gemini" }
// SupportsResume returns false because Gemini's --resume flag only accepts
// index numbers or "latest", not session UUIDs. With multiple Gemini agents
// per project, both are unsafe — "latest" or an index could resume the wrong
// agent's session, leaking conversation context across roles.
// TODO: Re-enable when Gemini CLI adds --resume <sessionId> support. At that
// point, store the Gemini sessionId at launch and map to our Coral session ID.
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

	// Inject system prompt via GEMINI_SYSTEM_MD env var
	var sysParts []string
	if proto := readProtocolFile(params.ProtocolPath); proto != "" {
		sysParts = append(sysParts, proto)
	}
	boardSysPrompt := BuildBoardSystemPrompt(params.BoardName, params.Role, "", params.PromptOverrides, params.BoardType)
	if boardSysPrompt != "" {
		sysParts = append(sysParts, boardSysPrompt)
	}

	if len(sysParts) > 0 {
		sysFile := writeTempFile("gemini_sys", params.SessionID, "md", []byte(strings.Join(sysParts, "\n\n")))
		parts = append(parts, fmt.Sprintf(`GEMINI_SYSTEM_MD="%s"`, sysFile))
	}

	// Environment variable prefix — use single quotes to prevent shell expansion
	if params.SessionName != "" {
		parts = append(parts, fmt.Sprintf(`CORAL_SESSION_NAME='%s'`, SanitizeShellValue(params.SessionName)))
	}
	if params.Role != "" {
		parts = append(parts, fmt.Sprintf(`CORAL_SUBSCRIBER_ID='%s'`, SanitizeShellValue(params.Role)))
	}

	// NOTE: PATH injection is handled by callers via WrapWithBundlePath()

	parts = append(parts, bin)

	// Resume is disabled — see SupportsResume() comment.
	// The --resume flag code is kept commented for future reference:
	// if params.ResumeSessionID != "" {
	//     parts = append(parts, "--resume", "<gemini-session-id>")
	// }

	// Permission flags from capabilities
	if perms := TranslateToGeminiPermissions(params.Capabilities); perms != nil {
		if perms.ApprovalMode != "" {
			parts = append(parts, "--approval-mode", perms.ApprovalMode)
		}
		if perms.Sandbox {
			parts = append(parts, "--sandbox")
		}
	}

	// User-provided flags (warn about Claude-specific flags)
	claudeOnlyFlags := map[string]bool{
		"--settings": true, "--session-id": true, "--dangerously-skip-permissions": true,
	}
	for _, flag := range params.Flags {
		if claudeOnlyFlags[flag] {
			slog.Warn("Claude-specific flag passed to Gemini agent, may not work as expected", "flag", flag)
		}
		parts = append(parts, flag)
	}

	// Action prompt via temp file for robustness
	cliPrompt := BuildBoardActionPrompt(params.BoardName, params.Role, params.Prompt, params.PromptOverrides, params.BoardType)
	if cliPrompt == "" {
		cliPrompt = params.Prompt
	}

	if cliPrompt != "" {
		promptFile := writeTempFile("gemini_prompt", params.SessionID, "txt", []byte(cliPrompt))
		parts = append(parts, FormatPromptFileArg(promptFile))
	}

	return strings.Join(parts, " ")
}
