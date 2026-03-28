package agent

import (
	"fmt"
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

	if params.SessionName != "" {
		parts = append(parts, fmt.Sprintf(`CORAL_SESSION_NAME="%s"`, params.SessionName))
	}
	if params.Role != "" {
		parts = append(parts, fmt.Sprintf(`CORAL_SUBSCRIBER_ID="%s"`, params.Role))
	}

	if params.ResumeSessionID != "" {
		// codex resume takes session ID as a positional argument, not --session flag
		parts = append(parts, bin, "resume", params.ResumeSessionID)
	} else {
		parts = append(parts, bin)
	}

	// Inject permissions from capabilities
	if perms := TranslateToCodexPermissions(params.Capabilities); perms != nil && perms.FullAuto {
		parts = append(parts, "--full-auto")
	}

	for _, flag := range params.Flags {
		// Translate Claude-specific flags to Codex equivalents
		if flag == "--dangerously-skip-permissions" {
			flag = "--full-auto"
		}
		parts = append(parts, flag)
	}

	// Build combined prompt: protocol + board system prompt + action prompt
	// Codex doesn't have a --system-prompt flag, so we prepend instructions
	// to the positional PROMPT argument
	var promptParts []string

	// Add protocol content
	if proto := readProtocolFile(params.ProtocolPath); proto != "" {
		promptParts = append(promptParts, proto)
	}

	// Add board system prompt (CLI usage instructions)
	boardSysPrompt := BuildBoardSystemPrompt(params.BoardName, params.Role, "", params.PromptOverrides, params.BoardType)
	if boardSysPrompt != "" {
		promptParts = append(promptParts, boardSysPrompt)
	}

	// Add action prompt (what to do first)
	actionPrompt := BuildBoardActionPrompt(params.BoardName, params.Role, params.Prompt, params.PromptOverrides, params.BoardType)
	if actionPrompt != "" {
		promptParts = append(promptParts, actionPrompt)
	} else if params.Prompt != "" {
		promptParts = append(promptParts, params.Prompt)
	}

	if len(promptParts) > 0 {
		combined := strings.Join(promptParts, "\n\n")
		promptFile := writeTempFile("codex_prompt", params.SessionID, "txt", []byte(combined))
		parts = append(parts, FormatPromptFileArg(promptFile))
	}

	return strings.Join(parts, " ")
}

