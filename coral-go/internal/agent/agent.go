// Package agent provides agent implementations for Claude, Gemini, and Codex.
package agent

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	at "github.com/cdknorow/coral/internal/agenttypes"
)

// cliNames maps board_type to CLI command name. The nil/empty key is the default.
var cliNames = map[string]string{
	"":      "coral-board",
	"coral": "coral-board",
}

// GetCLIName returns the CLI command name for the given board type.
func GetCLIName(boardType string) string {
	if cli, ok := cliNames[boardType]; ok {
		return cli
	}
	return cliNames[""]
}

// LaunchParams holds all parameters for building a launch command.
type LaunchParams struct {
	SessionID       string
	SessionName     string // tmux session name (e.g. "claude-<uuid>"), used for CORAL_SESSION_NAME
	ProtocolPath    string
	ResumeSessionID string
	Flags           []string
	WorkingDir      string
	BoardName       string
	Role            string
	Prompt          string
	PromptOverrides map[string]string // user overrides for orchestrator/worker prompts
	BoardType       string
	Capabilities    *Capabilities
	Tools           []string              // allowed tools (e.g. ["TodoWrite", "Bash(npm *)"])
	MCPServers      map[string]any        // MCP server configs keyed by name
	Hooks           map[string]interface{} // per-agent hooks to merge into settings (Claude-native) or fire via runner (Gemini/Codex)
	CLIPath      string // custom path to agent binary (empty = default from PATH)
	ProxyBaseURL string // proxy base URL (e.g. "http://127.0.0.1:8420/proxy/{session_id}")
	CoralDir     string // path to coral data directory (~/.coral) for CA cert location
}

// Agent defines the interface for all agent implementations.
type Agent interface {
	// AgentType returns the short identifier (e.g. "claude", "gemini").
	AgentType() string
	// SupportsResume returns whether the agent supports resuming a previous session.
	SupportsResume() bool
	// BuildLaunchCommand builds the shell command to launch this agent.
	BuildLaunchCommand(params LaunchParams) string
	// HistoryBasePath returns the root directory for history files.
	HistoryBasePath() string
	// HistoryGlobPattern returns the glob pattern for history files.
	HistoryGlobPattern() string
}

// ResumableAgent is optionally implemented by agents that need file
// preparation before resuming a session (e.g. copying history files).
type ResumableAgent interface {
	PrepareResume(sessionID, workingDir string)
}

// TryPrepareResume calls PrepareResume on the agent if it implements
// ResumableAgent. This avoids forcing agents without resume preparation
// to carry empty stubs.
func TryPrepareResume(ag Agent, sessionID, workingDir string) {
	if r, ok := ag.(ResumableAgent); ok {
		r.PrepareResume(sessionID, workingDir)
	}
}

// GetAgent returns the agent implementation for the given type.
func GetAgent(agentType string) Agent {
	switch agentType {
	case at.Gemini:
		return &GeminiAgent{}
	case at.Codex:
		return &CodexAgent{}
	default:
		return &ClaudeAgent{}
	}
}

// CLIInfo holds the CLI binary name and install instructions for an agent type.
type CLIInfo struct {
	Binary         string `json:"binary"`
	InstallCommand string `json:"install_command"`
}

// agentCLIs maps agent types to their required CLI tools and install instructions.
var agentCLIs = map[string]CLIInfo{
	at.Claude: {Binary: "claude", InstallCommand: "npm install -g @anthropic-ai/claude-code"},
	at.Gemini: {Binary: "gemini", InstallCommand: "pip install google-gemini-cli"},
	at.Codex:  {Binary: "codex", InstallCommand: "npm install -g @openai/codex"},
}

// GetCLIInfo returns CLI info for an agent type, or nil if unknown.
func GetCLIInfo(agentType string) *CLIInfo {
	if info, ok := agentCLIs[agentType]; ok {
		return &info
	}
	return nil
}

// CLIPathSettingKey returns the settings key for the agent's custom CLI path.
func CLIPathSettingKey(agentType string) string {
	return "cli_path_" + agentType
}

// resolveBinary returns cliPath if non-empty, otherwise the default binary name.
func resolveBinary(cliPath, defaultBin string) string {
	if cliPath != "" {
		return cliPath
	}
	return defaultBin
}

// readProtocolFile reads the protocol file and returns its content, or "" on error.
func readProtocolFile(path string) string {
	if path == "" {
		return ""
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(content)
}

// writeTempFile creates a temp file with an unpredictable name using os.CreateTemp
// (O_CREATE|O_EXCL) to prevent symlink attacks. The file name includes the session
// ID for later cleanup via CleanupTempFiles. Returns the file path.
func writeTempFile(prefix, id, ext string, content []byte) string {
	pattern := fmt.Sprintf("coral_%s_%s_*.%s", prefix, id, ext)
	f, err := os.CreateTemp("", pattern)
	if err != nil {
		slog.Warn("failed to create temp file", "prefix", prefix, "id", id, "error", err)
		// Fall back to predictable path so launch can still attempt to proceed
		path := filepath.Join(os.TempDir(), fmt.Sprintf("coral_%s_%s.%s", prefix, id, ext))
		if writeErr := os.WriteFile(path, content, 0600); writeErr != nil {
			slog.Warn("failed to write temp file", "path", path, "error", writeErr)
		}
		return path
	}
	defer f.Close()
	if _, err := f.Write(content); err != nil {
		slog.Warn("failed to write temp file content", "path", f.Name(), "error", err)
	}
	return f.Name()
}

// CleanupTempFiles removes all Coral temp files associated with the given session ID.
// Called during session teardown to prevent sensitive content from persisting in /tmp.
func CleanupTempFiles(sessionID string) {
	if sessionID == "" {
		return
	}
	pattern := filepath.Join(os.TempDir(), fmt.Sprintf("coral_*_%s*", sessionID))
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return
	}
	for _, path := range matches {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			slog.Debug("failed to remove temp file", "path", path, "error", err)
		}
	}
}

// Default board system-prompt fragments (used by all agents).
const DefaultOrchestratorSystemPrompt = "Post a message with coral-board post \"<your introduction>\" that introduces yourself, " +
	"then discuss your proposed plan with the operator (the human user) before posting assignments to the team.\n\n" +
	"When posting messages to specific agents, you MUST @mention them by name (e.g. @Lead Developer) so they receive a notification. " +
	"You can also use the --to flag: coral-board post --to \"Agent1,Agent2\" \"message\" which auto-prepends @mentions. " +
	"Messages without @mentions will NOT notify agents.\n\n" +
	"You can peek at any agent's terminal to check their progress without waiting for them to post:\n" +
	"  coral-board peek \"Agent Name\"           — see the last 30 lines of their terminal\n" +
	"  coral-board peek \"Agent Name\" --lines 50 — see more lines\n" +
	"Use this to monitor progress, diagnose stuck agents, or verify work before asking for a status update."

const DefaultWorkerSystemPrompt = "Post a message with coral-board post \"<your introduction>\" that introduces yourself, " +
	"then STOP and wait. Do NOT poll the message board in a loop. Coral will notify you when there are new messages."

// Default action prompts (appended to user prompt as CLI positional arg).
const DefaultOrchestratorActionPrompt = `IMPORTANT: You were automatically joined to message board "{board_name}". Do NOT run coral-board join. Post a message with coral-board post "<your introduction>" that introduces yourself, then discuss your proposed plan with the operator (the human user) before posting assignments.

CRITICAL: Do NOT poll or loop on 'coral-board read'. After posting your introduction or any message, STOP. Coral will send you a notification (as a user message) when new messages arrive. Only run 'coral-board read' after receiving such a notification.

When posting messages to specific agents, you MUST @mention them by name (e.g. @Lead Developer) so they receive a notification. You can also use the --to flag: coral-board post --to "Agent1,Agent2" "message" which auto-prepends @mentions. Messages without @mentions will NOT notify agents.

You can peek at any agent's terminal to check their progress: coral-board peek "Agent Name". Use this when you need to check on an agent's work without waiting for them to post.`

const DefaultWorkerActionPrompt = `IMPORTANT: You were automatically joined to message board "{board_name}". Do NOT run coral-board join. Do not start any actions until you receive instructions from the Orchestrator on the message board. Post a message with coral-board post "<your introduction>" that introduces yourself, then STOP.

CRITICAL: Do NOT poll or loop on 'coral-board read'. Coral will automatically notify you (as a user message) when new messages arrive — only run 'coral-board read' after receiving a notification. Between notifications, do nothing and wait.`

// isOrchestratorRole returns true if the role string indicates an orchestrator.
func isOrchestratorRole(role string) bool {
	return role != "" && strings.Contains(strings.ToLower(role), "orchestrator")
}

// BuildBoardSystemPrompt builds the board system prompt fragment shared by all agents.
// It returns the combined prompt with board usage instructions and role-specific tail.
// If boardName is empty, it returns just the base prompt (if any).
func BuildBoardSystemPrompt(boardName, role, prompt string, promptOverrides map[string]string, boardType string) string {
	cli := GetCLIName(boardType)
	var parts []string
	if prompt != "" {
		parts = append(parts, prompt)
	}
	if boardName != "" {
		roleLabel := ""
		if role != "" {
			roleLabel = fmt.Sprintf(" Your role is: %s.", role)
		}
		boardIntro := fmt.Sprintf(
			"You were automatically joined to message board \"%s\".%s "+
				"Do NOT run %s join — you are already subscribed.\n\n"+
				"Use the %s CLI to communicate with your teammates:\n"+
				"  %s read          — read new messages from teammates\n"+
				"  %s post \"msg\"    — post a message to the board\n"+
				"  %s read --last 5 — see the 5 most recent messages\n"+
				"  %s subscribers   — see who is on the board\n"+
				"When you have a new message, Coral will notify you.\n\n",
			boardName, roleLabel, cli, cli, cli, cli, cli, cli)

		var tail string
		if isOrchestratorRole(role) {
			if v, ok := promptOverrides["default_prompt_orchestrator"]; ok && v != "" {
				tail = v
			} else {
				tail = DefaultOrchestratorSystemPrompt
			}
		} else {
			if v, ok := promptOverrides["default_prompt_worker"]; ok && v != "" {
				tail = v
			} else {
				tail = DefaultWorkerSystemPrompt
			}
		}
		boardIntro += tail
		parts = append(parts, boardIntro)
	}
	return strings.Join(parts, "\n\n")
}

// BuildBoardActionPrompt builds the action prompt (CLI positional arg) for board sessions.
// It appends the appropriate orchestrator/worker action text to the base prompt.
// Returns the combined prompt, or empty string if no board is configured.
func BuildBoardActionPrompt(boardName, role, basePrompt string, promptOverrides map[string]string, boardType string) string {
	if boardName == "" {
		return basePrompt
	}
	cli := GetCLIName(boardType)
	overrides := promptOverrides
	if overrides == nil {
		overrides = map[string]string{}
	}
	var template string
	if isOrchestratorRole(role) {
		if v, ok := overrides["default_prompt_orchestrator"]; ok && v != "" {
			template = v
		} else {
			template = DefaultOrchestratorActionPrompt
		}
	} else {
		if v, ok := overrides["default_prompt_worker"]; ok && v != "" {
			template = v
		} else {
			template = DefaultWorkerActionPrompt
		}
	}
	actionText := strings.ReplaceAll(template, "{board_name}", boardName)
	actionText = strings.ReplaceAll(actionText, "coral-board", cli)
	if basePrompt != "" {
		return basePrompt + "\n\n" + actionText
	}
	return actionText
}

