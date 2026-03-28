// Package naming provides canonical constructors for agent identity strings.
// These are the single source of truth — use them everywhere instead of
// ad-hoc fmt.Sprintf calls.
package naming

import (
	"fmt"
	"path/filepath"
)

// SessionName returns the canonical tmux/pty session name for an agent.
// Format: "{agentType}-{sessionID}", e.g. "claude-dc6d10f4-2f20-...".
func SessionName(agentType, sessionID string) string {
	return fmt.Sprintf("%s-%s", agentType, sessionID)
}

// SubscriberID returns the stable board identity for an agent.
// Uses the display name (role) if set, otherwise falls back to the agent type.
// Examples: "Orchestrator", "Backend Dev", "claude".
func SubscriberID(displayName, agentType string) string {
	if displayName != "" {
		return displayName
	}
	return agentType
}

// LogFile returns the canonical log file path for an agent session.
// Format: "{logDir}/{agentType}_coral_{sessionID}.log".
func LogFile(logDir, agentType, sessionID string) string {
	return filepath.Join(logDir, fmt.Sprintf("%s_coral_%s.log", agentType, sessionID))
}
