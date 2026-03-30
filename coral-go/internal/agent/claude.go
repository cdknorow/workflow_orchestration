package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// ClaudeAgent implements the Agent interface for Claude Code.
type ClaudeAgent struct{}

func (a *ClaudeAgent) AgentType() string    { return "claude" }
func (a *ClaudeAgent) SupportsResume() bool { return true }

func (a *ClaudeAgent) HistoryBasePath() string {
	if v := os.Getenv("CLAUDE_PROJECTS_DIR"); v != "" {
		return v
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".claude", "projects")
}

func (a *ClaudeAgent) HistoryGlobPattern() string { return "*.jsonl" }

func (a *ClaudeAgent) BuildLaunchCommand(params LaunchParams) string {
	bin := resolveBinary(params.CLIPath, "claude")
	parts := []string{bin}

	effectiveID := params.SessionID
	if params.ResumeSessionID != "" {
		effectiveID = params.ResumeSessionID
		parts = append(parts, "--resume", params.ResumeSessionID)
	} else {
		parts = append(parts, "--session-id", params.SessionID)
	}

	// Build merged settings with hooks and system prompt
	merged := buildMergedSettings(params.WorkingDir)

	// Combine protocol + board system prompt into systemPrompt
	var sysParts []string
	if proto := readProtocolFile(params.ProtocolPath); proto != "" {
		sysParts = append(sysParts, proto)
	}
	boardSysPrompt := BuildBoardSystemPrompt(params.BoardName, params.Role, params.Prompt, params.PromptOverrides, params.BoardType)
	if boardSysPrompt != "" {
		sysParts = append(sysParts, boardSysPrompt)
	}
	if len(sysParts) > 0 {
		merged["systemPrompt"] = strings.Join(sysParts, "\n\n")
	}

	// Inject permissions from capabilities
	if perms := TranslateToClaudePermissions(params.Capabilities); perms != nil {
		permMap := map[string]any{}
		if len(perms.Allow) > 0 {
			permMap["allow"] = perms.Allow
		}
		if len(perms.Deny) > 0 {
			permMap["deny"] = perms.Deny
		}
		merged["permissions"] = permMap
	}

	// Set CORAL_SESSION_NAME and CORAL_SUBSCRIBER_ID in env so coral-board and hooks
	// can identify this agent. CORAL_SUBSCRIBER_ID is the stable board identity (role name).
	{
		envMap, _ := merged["env"].(map[string]interface{})
		if envMap == nil {
			envMap = make(map[string]interface{})
		}
		if params.SessionName != "" {
			envMap["CORAL_SESSION_NAME"] = params.SessionName
		}
		if params.Role != "" {
			envMap["CORAL_SUBSCRIBER_ID"] = params.Role
		}
		merged["env"] = envMap
	}

	// Add Coral tools dir to PATH in env settings so that coral-board,
	// coral hooks, etc. can be found by Claude CLI and its subprocesses.
	if macosDir := CoralToolsDir(); macosDir != "" {
		envMap, _ := merged["env"].(map[string]interface{})
		if envMap == nil {
			envMap = make(map[string]interface{})
		}
		if existingPath, ok := envMap["PATH"].(string); ok {
			// Prepend if not already present
			if !strings.Contains(existingPath, macosDir) {
				envMap["PATH"] = macosDir + ":" + existingPath
			}
		} else {
			// Resolve $PATH from the actual environment — JSON settings files
			// are not shell-expanded, so literal "$PATH" would be passed as-is.
			currentPath := os.Getenv("PATH")
			envMap["PATH"] = macosDir + ":" + currentPath
		}
		merged["env"] = envMap
	}

	// Write settings to temp file using writeTempFile for safe creation
	data, _ := json.MarshalIndent(merged, "", "  ")
	settingsFile := writeTempFile("settings", effectiveID, "json", append(data, '\n'))
	parts = append(parts, "--settings", settingsFile)

	if len(params.Flags) > 0 {
		parts = append(parts, params.Flags...)
	}

	// Pass the prompt as a CLI positional argument so the agent starts immediately
	// without relying on fragile tmux send-keys delivery.
	cliPrompt := BuildBoardActionPrompt(params.BoardName, params.Role, params.Prompt, params.PromptOverrides, params.BoardType)
	if cliPrompt != "" {
		promptFile := writeTempFile("prompt", effectiveID, "txt", []byte(cliPrompt))
		parts = append(parts, FormatPromptFileArg(promptFile))
	}

	return strings.Join(parts, " ")
}

func (a *ClaudeAgent) PrepareResume(sessionID, workingDir string) {
	basePath := a.HistoryBasePath()
	encoded := strings.ReplaceAll(strings.ReplaceAll(workingDir, "/", "-"), "_", "-")
	targetProject := filepath.Join(basePath, encoded)
	targetJSONL := filepath.Join(targetProject, sessionID+".jsonl")

	if _, err := os.Stat(targetJSONL); err == nil {
		return // Already exists
	}

	// Search for the session file in other project dirs
	entries, err := os.ReadDir(basePath)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		source := filepath.Join(basePath, entry.Name(), sessionID+".jsonl")
		if _, err := os.Stat(source); err == nil {
			os.MkdirAll(targetProject, 0755)
			data, err := os.ReadFile(source)
			if err == nil {
				os.WriteFile(targetJSONL, data, 0644)
			}
			// Copy session directory if it exists
			sourceDir := filepath.Join(basePath, entry.Name(), sessionID)
			targetDir := filepath.Join(targetProject, sessionID)
			if info, err := os.Stat(sourceDir); err == nil && info.IsDir() {
				copyDir(sourceDir, targetDir)
			}
			return
		}
	}
}

// Coral hooks to inject into every Claude session.
var coralHooks = map[string][]map[string]interface{}{
	"PostToolUse": {
		{"matcher": "TaskCreate|TaskUpdate", "hooks": []map[string]interface{}{
			{"type": "command", "command": "coral-hook-task-sync"},
		}},
		{"hooks": []map[string]interface{}{
			{"type": "command", "command": "coral-hook-agentic-state"},
		}},
		{"hooks": []map[string]interface{}{
			{"type": "command", "command": "coral-hook-message-check"},
		}},
	},
	"Stop": {
		{"hooks": []map[string]interface{}{
			{"type": "command", "command": "coral-hook-agentic-state"},
		}},
	},
	"Notification": {
		{"hooks": []map[string]interface{}{
			{"type": "command", "command": "coral-hook-agentic-state"},
		}},
	},
}

func buildMergedSettings(workingDir string) map[string]interface{} {
	home, _ := os.UserHomeDir()
	homeClaude := filepath.Join(home, ".claude")

	global := readSettingsFile(filepath.Join(homeClaude, "settings.json"))
	var project, local map[string]interface{}
	if workingDir != "" {
		project = readSettingsFile(filepath.Join(workingDir, ".claude", "settings.json"))
		local = readSettingsFile(filepath.Join(workingDir, ".claude", "settings.local.json"))
	}

	// Shallow merge: local > project > global
	merged := make(map[string]interface{})
	for k, v := range global {
		merged[k] = v
	}
	for k, v := range project {
		merged[k] = v
	}
	for k, v := range local {
		merged[k] = v
	}

	// Deep-merge hooks
	mergedHooks := make(map[string][]interface{})
	for _, source := range []map[string]interface{}{global, project, local} {
		if hooks, ok := source["hooks"].(map[string]interface{}); ok {
			for event, groups := range hooks {
				if groupList, ok := groups.([]interface{}); ok {
					mergedHooks[event] = append(mergedHooks[event], groupList...)
				}
			}
		}
	}

	// Append Coral hooks
	for event, groups := range coralHooks {
		existing := mergedHooks[event]
		for _, group := range groups {
			command := ""
			if hooks, ok := group["hooks"].([]map[string]interface{}); ok && len(hooks) > 0 {
				if cmd, ok := hooks[0]["command"].(string); ok {
					command = cmd
				}
			}
			if !hookEntryExists(existing, command) {
				mergedHooks[event] = append(mergedHooks[event], group)
			}
		}
	}

	merged["hooks"] = mergedHooks
	return merged
}

func readSettingsFile(path string) map[string]interface{} {
	data, err := os.ReadFile(path)
	if err != nil {
		return map[string]interface{}{}
	}
	var result map[string]interface{}
	if err := json.Unmarshal(data, &result); err != nil {
		return map[string]interface{}{}
	}
	return result
}

func hookEntryExists(groups []interface{}, command string) bool {
	if command == "" {
		return false
	}
	for _, g := range groups {
		if gMap, ok := g.(map[string]interface{}); ok {
			if hooks, ok := gMap["hooks"].([]interface{}); ok {
				for _, h := range hooks {
					if hMap, ok := h.(map[string]interface{}); ok {
						if hMap["command"] == command {
							return true
						}
					}
				}
			}
			// Also handle typed version
			if hooks, ok := gMap["hooks"].([]map[string]interface{}); ok {
				for _, h := range hooks {
					if h["command"] == command {
						return true
					}
				}
			}
		}
	}
	return false
}


func copyDir(src, dst string) {
	entries, err := os.ReadDir(src)
	if err != nil {
		return
	}
	os.MkdirAll(dst, 0755)
	for _, entry := range entries {
		srcPath := filepath.Join(src, entry.Name())
		dstPath := filepath.Join(dst, entry.Name())
		if entry.IsDir() {
			copyDir(srcPath, dstPath)
		} else {
			data, err := os.ReadFile(srcPath)
			if err == nil {
				os.WriteFile(dstPath, data, 0644)
			}
		}
	}
}
