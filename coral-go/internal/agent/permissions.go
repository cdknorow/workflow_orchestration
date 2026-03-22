package agent

import "strings"

// Capability represents a Coral-level permission capability.
// These are agent-agnostic; each agent adapter translates them
// to the native format at launch time.
type Capability = string

// Standard capabilities.
const (
	CapFileRead   Capability = "file_read"
	CapFileWrite  Capability = "file_write"
	CapShell      Capability = "shell"
	CapWebAccess  Capability = "web_access"
	CapGitWrite   Capability = "git_write"
	CapAgentSpawn Capability = "agent_spawn"
	CapNotebook   Capability = "notebook"
)

// Capabilities defines an agent's permission scope.
type Capabilities struct {
	Allow []string `json:"allow,omitempty"`
	Deny  []string `json:"deny,omitempty"`
}

// IsEmpty returns true if no capabilities are defined.
func (c *Capabilities) IsEmpty() bool {
	return c == nil || (len(c.Allow) == 0 && len(c.Deny) == 0)
}

// ClaudePermissions is the native Claude Code settings.json permissions format.
type ClaudePermissions struct {
	Allow []string `json:"allow,omitempty"`
	Deny  []string `json:"deny,omitempty"`
}

// TranslateToClaudePermissions converts Coral capabilities to Claude Code's
// native permissions format for injection into settings.json.
func TranslateToClaudePermissions(caps *Capabilities) *ClaudePermissions {
	if caps.IsEmpty() {
		return nil
	}

	var allow, deny []string

	for _, cap := range caps.Allow {
		allow = append(allow, mapCapToClaudeTools(cap)...)
	}
	for _, cap := range caps.Deny {
		deny = append(deny, mapCapToClaudeTools(cap)...)
	}

	// Always allow coral-board CLI for message board communication
	allow = append(allow, "Bash(coral-board *)")

	if len(allow) == 0 && len(deny) == 0 {
		return nil
	}
	return &ClaudePermissions{Allow: allow, Deny: deny}
}

// mapCapToClaudeTools maps a single Coral capability to Claude Code tool patterns.
func mapCapToClaudeTools(cap string) []string {
	// Handle shell:<pattern> syntax
	if strings.HasPrefix(cap, "shell:") {
		pattern := strings.TrimPrefix(cap, "shell:")
		return []string{"Bash(" + pattern + ")"}
	}

	switch cap {
	case CapFileRead:
		return []string{"Read", "Glob", "Grep"}
	case CapFileWrite:
		return []string{"Write", "Edit"}
	case CapShell:
		return []string{"Bash"}
	case CapWebAccess:
		return []string{"WebFetch", "WebSearch"}
	case CapGitWrite:
		return []string{"Bash(git push *)", "Bash(git commit *)", "Bash(git branch *)", "Bash(git merge *)", "Bash(git rebase *)"}
	case CapAgentSpawn:
		return []string{"Agent"}
	case CapNotebook:
		return []string{"NotebookEdit"}
	default:
		// Pass through unknown capabilities as-is (future-proof)
		return []string{cap}
	}
}

// Preset permission profiles for built-in agent roles.
var Presets = map[string]*Capabilities{
	"lead_dev": {
		Allow: []string{CapFileRead, CapFileWrite, CapShell, CapGitWrite, CapAgentSpawn},
	},
	"qa": {
		Allow: []string{CapFileRead},
		Deny:  []string{CapFileWrite, CapShell},
	},
	"frontend_dev": {
		Allow: []string{CapFileRead, CapFileWrite, "shell:npm *", "shell:npx *", CapWebAccess},
	},
	"orchestrator": {
		Allow: []string{CapFileRead, CapAgentSpawn, CapWebAccess},
	},
	"devops": {
		Allow: []string{CapFileRead, CapFileWrite, CapShell, CapGitWrite},
	},
	"read_only": {
		Allow: []string{CapFileRead, CapWebAccess},
	},
	"full_access": {
		Allow: []string{CapFileRead, CapFileWrite, CapShell, CapGitWrite, CapAgentSpawn, CapWebAccess, CapNotebook},
	},
}
