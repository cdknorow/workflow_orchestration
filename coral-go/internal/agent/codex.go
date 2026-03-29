package agent

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
)

// CodexAgent implements the Agent interface for OpenAI Codex CLI.
type CodexAgent struct{}

func (a *CodexAgent) AgentType() string    { return "codex" }
func (a *CodexAgent) SupportsResume() bool { return true }

func (a *CodexAgent) HistoryBasePath() string {
	if v := os.Getenv("CODEX_HOME"); v != "" {
		return filepath.Join(v, "sessions")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".codex", "sessions")
}

func (a *CodexAgent) HistoryGlobPattern() string { return "rollout-*.jsonl" }

func (a *CodexAgent) BuildLaunchCommand(params LaunchParams) string {
	bin := resolveBinary(params.CLIPath, "codex")
	var parts []string

	// Export env vars so child processes (coral-board, hooks) inherit them.
	// Single quotes prevent shell expansion; SanitizeShellValue strips metacharacters.
	if params.SessionName != "" {
		parts = append(parts, fmt.Sprintf(`export CORAL_SESSION_NAME='%s' &&`, SanitizeShellValue(params.SessionName)))
	}
	if params.Role != "" {
		parts = append(parts, fmt.Sprintf(`export CORAL_SUBSCRIBER_ID='%s' &&`, SanitizeShellValue(params.Role)))
	}

	// NOTE: PATH injection is handled by callers via WrapWithBundlePath()

	// Binary and resume
	if params.ResumeSessionID != "" {
		parts = append(parts, bin, "resume", params.ResumeSessionID)
	} else {
		parts = append(parts, bin)
	}

	// System prompt injection via -c developer_instructions
	// Combines protocol file + board system prompt (CLI usage, role instructions)
	// Note: The $(cat '...') pattern shell-expands the file path but not its content.
	// The temp file path is from os.TempDir() (safe). Content sources (protocol files,
	// board prompts) are trusted internal strings.
	var sysParts []string
	if proto := readProtocolFile(params.ProtocolPath); proto != "" {
		sysParts = append(sysParts, proto)
	}
	boardSysPrompt := BuildBoardSystemPrompt(params.BoardName, params.Role, "", params.PromptOverrides, params.BoardType)
	if boardSysPrompt != "" {
		sysParts = append(sysParts, boardSysPrompt)
	}

	if len(sysParts) > 0 {
		sysFile := writeTempFile("codex_instructions", params.SessionID, "md", []byte(strings.Join(sysParts, "\n\n")))
		parts = append(parts, fmt.Sprintf(`-c developer_instructions="$(cat '%s')"`, sysFile))
	}

	// Whitelist Coral env vars through Codex's sandboxed shell so child processes
	// like coral-board and hooks can access them.
	parts = append(parts, "-c", `'shell_environment_policy.inherit=["CORAL_SESSION_NAME","CORAL_SUBSCRIBER_ID","CORAL_URL","CORAL_PORT"]'`)

	// Permission flags from capabilities
	if perms := TranslateToCodexPermissions(params.Capabilities); perms != nil {
		if perms.BypassSandbox {
			parts = append(parts, "--dangerously-bypass-approvals-and-sandbox")
		} else if perms.FullAuto {
			parts = append(parts, "--full-auto")
		} else {
			if perms.SandboxMode != "" {
				parts = append(parts, "--sandbox", perms.SandboxMode)
			}
			if perms.ApprovalPolicy != "" {
				parts = append(parts, "-a", perms.ApprovalPolicy)
			}
		}
		if perms.Search {
			parts = append(parts, "--search")
		}
	}

	// User-provided flags — translate or drop Claude-specific flags
	claudeOnlyFlags := map[string]bool{
		"--settings": true, "--session-id": true, "--resume": true,
	}
	for _, flag := range params.Flags {
		if flag == "--dangerously-skip-permissions" {
			parts = append(parts, "--full-auto")
			continue
		}
		if claudeOnlyFlags[flag] {
			slog.Warn("dropping Claude-specific flag for Codex agent", "flag", flag)
			continue
		}
		parts = append(parts, flag)
	}

	// Action prompt as separate positional argument
	actionPrompt := BuildBoardActionPrompt(params.BoardName, params.Role, params.Prompt, params.PromptOverrides, params.BoardType)
	if actionPrompt == "" {
		actionPrompt = params.Prompt
	}

	if actionPrompt != "" {
		promptFile := writeTempFile("codex_prompt", params.SessionID, "txt", []byte(actionPrompt))
		parts = append(parts, FormatPromptFileArg(promptFile))
	}

	return strings.Join(parts, " ")
}
