// Package agent provides agent implementations for Claude and Gemini.
package agent

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
	case "gemini":
		return &GeminiAgent{}
	default:
		return &ClaudeAgent{}
	}
}

// GetAllAgents returns all registered agent implementations.
func GetAllAgents() []Agent {
	return []Agent{&ClaudeAgent{}, &GeminiAgent{}}
}

// CLIInfo holds the CLI binary name and install instructions for an agent type.
type CLIInfo struct {
	Binary         string `json:"binary"`
	InstallCommand string `json:"install_command"`
}

// AgentCLIs maps agent types to their required CLI tools and install instructions.
var AgentCLIs = map[string]CLIInfo{
	"claude": {Binary: "claude", InstallCommand: "npm install -g @anthropic-ai/claude-code"},
	"gemini": {Binary: "gemini", InstallCommand: "pip install google-gemini-cli"},
	"codex":  {Binary: "codex", InstallCommand: "npm install -g @openai/codex"},
}

// GetCLIInfo returns CLI info for an agent type, or nil if unknown.
func GetCLIInfo(agentType string) *CLIInfo {
	if info, ok := AgentCLIs[agentType]; ok {
		return &info
	}
	return nil
}
