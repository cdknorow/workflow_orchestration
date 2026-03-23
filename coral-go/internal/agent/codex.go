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
	var parts []string

	if params.ResumeSessionID != "" {
		// Codex uses a subcommand for resume: codex resume --session <id>
		parts = append(parts, "codex", "resume", "--session", params.ResumeSessionID)
	} else {
		parts = append(parts, "codex")
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
	if params.ProtocolPath != "" {
		if content, err := os.ReadFile(params.ProtocolPath); err == nil {
			promptParts = append(promptParts, string(content))
		}
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
		promptFile := filepath.Join(os.TempDir(), fmt.Sprintf("coral_codex_prompt_%s.txt", params.SessionID))
		os.WriteFile(promptFile, []byte(combined), 0600)
		parts = append(parts, FormatPromptFileArg(promptFile))
	}

	return strings.Join(parts, " ")
}

func (a *CodexAgent) PrepareResume(sessionID, workingDir string) {
	// Codex handles resume natively via the 'resume' subcommand; no file preparation needed.
}
