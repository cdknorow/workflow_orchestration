// Package agent provides agent implementations for Claude, Gemini, and Codex.
package agent

import (
	"fmt"
	"strings"

	at "github.com/cdknorow/coral/internal/agenttypes"
)

// CLINames maps board_type to CLI command name. The nil/empty key is the default.
var CLINames = map[string]string{
	"":      "coral-board",
	"coral": "coral-board",
}

// GetCLIName returns the CLI command name for the given board type.
func GetCLIName(boardType string) string {
	if cli, ok := CLINames[boardType]; ok {
		return cli
	}
	return CLINames[""]
}

// LaunchParams holds all parameters for building a launch command.
type LaunchParams struct {
	SessionID       string
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
	CLIPath         string // custom path to agent binary (empty = default from PATH)
}

// Agent defines the interface for all agent implementations.
type Agent interface {
	// AgentType returns the short identifier (e.g. "claude", "gemini").
	AgentType() string
	// SupportsResume returns whether the agent supports resuming a previous session.
	SupportsResume() bool
	// BuildLaunchCommand builds the shell command to launch this agent.
	BuildLaunchCommand(params LaunchParams) string
	// PrepareResume prepares for resuming a session (e.g. copy files).
	PrepareResume(sessionID, workingDir string)
	// HistoryBasePath returns the root directory for history files.
	HistoryBasePath() string
	// HistoryGlobPattern returns the glob pattern for history files.
	HistoryGlobPattern() string
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

// GetAllAgents returns all registered agent implementations.
func GetAllAgents() []Agent {
	return []Agent{&ClaudeAgent{}, &GeminiAgent{}, &CodexAgent{}}
}

// CLIInfo holds the CLI binary name and install instructions for an agent type.
type CLIInfo struct {
	Binary         string `json:"binary"`
	InstallCommand string `json:"install_command"`
}

// AgentCLIs maps agent types to their required CLI tools and install instructions.
var AgentCLIs = map[string]CLIInfo{
	at.Claude: {Binary: "claude", InstallCommand: "npm install -g @anthropic-ai/claude-code"},
	at.Gemini: {Binary: "gemini", InstallCommand: "pip install google-gemini-cli"},
	at.Codex:  {Binary: "codex", InstallCommand: "npm install -g @openai/codex"},
}

// GetCLIInfo returns CLI info for an agent type, or nil if unknown.
func GetCLIInfo(agentType string) *CLIInfo {
	if info, ok := AgentCLIs[agentType]; ok {
		return &info
	}
	return nil
}

// CLIPathSettingKey returns the settings key for the agent's custom CLI path.
func CLIPathSettingKey(agentType string) string {
	return "cli_path_" + agentType
}

// Default board system-prompt fragments (used by all agents).
const DefaultOrchestratorSystemPrompt = "Post a message with coral-board post \"<your introduction>\" that introduces yourself, " +
	"then discuss your proposed plan with the operator (the human user) before posting assignments to the team."

const DefaultWorkerSystemPrompt = "Post a message with coral-board post \"<your introduction>\" that introduces yourself, " +
	"then wait for instructions from the Orchestrator."

// Default action prompts (appended to user prompt as CLI positional arg).
const DefaultOrchestratorActionPrompt = `IMPORTANT: You were automatically joined to message board "{board_name}". Do NOT run coral-board join. Post a message with coral-board post "<your introduction>" that introduces yourself, then discuss your proposed plan with the operator (the human user) before posting assignments. Periodically check for new messages.`

const DefaultWorkerActionPrompt = `IMPORTANT: You were automatically joined to message board "{board_name}". Do NOT run coral-board join. Do not start any actions until you receive instructions from the Orchestrator on the message board. Post a message with coral-board post "<your introduction>" that introduces yourself, then periodically check for new messages.`

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
				"Check the board periodically for updates from your teammates.\n\n",
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
