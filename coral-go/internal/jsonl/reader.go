// Package jsonl provides incremental JSONL reading for live agent session transcripts.
package jsonl

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
)

// SessionReader incrementally reads JSONL session files for live chat display.
type SessionReader struct {
	mu    sync.Mutex
	cache map[string]*sessionCache
}

type sessionCache struct {
	path         string
	offset       int64
	messages     []map[string]any
	toolUseNames map[string]string // tool_use_id → tool_name
}

// NewSessionReader creates a new JSONL session reader.
func NewSessionReader() *SessionReader {
	return &SessionReader{cache: make(map[string]*sessionCache)}
}

// ReadNewMessages reads new messages since the last call for the given session.
// Returns (new_messages, total_count).
func (r *SessionReader) ReadNewMessages(sessionID, workingDirectory, agentType string) ([]map[string]any, int) {
	r.mu.Lock()
	defer r.mu.Unlock()

	c := r.cache[sessionID]
	if c == nil {
		c = &sessionCache{toolUseNames: make(map[string]string)}
		r.cache[sessionID] = c
	}

	// Resolve path on first call
	if c.path == "" {
		c.path = resolveTranscriptPath(sessionID, workingDirectory, agentType)
		if c.path == "" {
			return nil, 0
		}
	}

	// Read new data from file
	f, err := os.Open(c.path)
	if err != nil {
		return nil, len(c.messages)
	}
	defer f.Close()

	if _, err := f.Seek(c.offset, io.SeekStart); err != nil {
		return nil, len(c.messages)
	}
	newData, err := io.ReadAll(f)
	if err != nil {
		return nil, len(c.messages)
	}
	c.offset += int64(len(newData))

	if len(newData) == 0 {
		return nil, len(c.messages)
	}

	// Parse new lines
	var newMessages []map[string]any
	for _, line := range strings.Split(string(newData), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var entry map[string]any
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			continue
		}
		parsed := parseTranscriptEntry(entry, c.toolUseNames, agentType)
		newMessages = append(newMessages, parsed...)
	}

	c.messages = append(c.messages, newMessages...)
	return newMessages, len(c.messages)
}

// ClearSession removes cached state for a session.
func (r *SessionReader) ClearSession(sessionID string) {
	r.mu.Lock()
	delete(r.cache, sessionID)
	r.mu.Unlock()
}

// resolveTranscriptPath finds the transcript file for a session.
func resolveTranscriptPath(sessionID, workingDirectory, agentType string) string {
	switch agentType {
	case "gemini":
		return resolveGeminiTranscript(sessionID)
	default:
		return resolveClaudeTranscript(sessionID, workingDirectory)
	}
}

func resolveClaudeTranscript(sessionID, workingDirectory string) string {
	home, _ := os.UserHomeDir()
	basePath := os.Getenv("CLAUDE_PROJECTS_DIR")
	if basePath == "" {
		basePath = filepath.Join(home, ".claude", "projects")
	}

	// Try working directory hint first
	if workingDirectory != "" {
		encoded := strings.ReplaceAll(workingDirectory, "/", "-")
		candidate := filepath.Join(basePath, encoded, sessionID+".jsonl")
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}

	// Search all project dirs
	entries, err := os.ReadDir(basePath)
	if err != nil {
		return ""
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		candidate := filepath.Join(basePath, entry.Name(), sessionID+".jsonl")
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	return ""
}

func resolveGeminiTranscript(sessionID string) string {
	home, _ := os.UserHomeDir()
	basePath := filepath.Join(home, ".gemini", "tmp")
	entries, err := os.ReadDir(basePath)
	if err != nil {
		return ""
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		candidate := filepath.Join(basePath, entry.Name(), "chats", sessionID+".json")
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	return ""
}

// parseTranscriptEntry converts a raw JSONL entry into normalized frontend messages.
func parseTranscriptEntry(entry map[string]any, toolUseNames map[string]string, agentType string) []map[string]any {
	switch agentType {
	case "gemini":
		return parseGeminiEntry(entry)
	default:
		return parseClaudeEntry(entry, toolUseNames)
	}
}

var pulseRE = regexp.MustCompile(`\|\|PULSE:\w+[^|]*\|\|`)

func parseClaudeEntry(entry map[string]any, toolUseNames map[string]string) []map[string]any {
	etype, _ := entry["type"].(string)
	timestamp, _ := entry["timestamp"].(string)

	switch etype {
	case "user":
		return parseClaudeUserEntry(entry, timestamp, toolUseNames)
	case "assistant":
		return parseClaudeAssistantEntry(entry, timestamp, toolUseNames)
	}
	return nil
}

func parseClaudeUserEntry(entry map[string]any, timestamp string, toolUseNames map[string]string) []map[string]any {
	msg, _ := entry["message"].(map[string]any)
	if msg == nil {
		return nil
	}
	content := msg["content"]

	switch c := content.(type) {
	case string:
		if strings.TrimSpace(c) == "" {
			return nil
		}
		return []map[string]any{{"type": "user", "timestamp": timestamp, "content": c}}

	case []any:
		var results []map[string]any
		var textParts []string

		for _, block := range c {
			b, ok := block.(map[string]any)
			if !ok {
				continue
			}
			bt, _ := b["type"].(string)
			if bt == "text" {
				if text, _ := b["text"].(string); text != "" {
					textParts = append(textParts, text)
				}
			} else if bt == "tool_result" {
				toolUseID, _ := b["tool_use_id"].(string)
				isError, _ := b["is_error"].(bool)
				resultContent := extractToolResultContent(b["content"])
				if resultContent == "" {
					continue
				}
				if len(resultContent) > 10000 {
					resultContent = resultContent[:10000] + "\n... (truncated)"
				}
				toolName := ""
				if toolUseID != "" {
					toolName = toolUseNames[toolUseID]
				}
				results = append(results, map[string]any{
					"type":        "tool_result",
					"timestamp":   timestamp,
					"content":     resultContent,
					"tool_name":   toolName,
					"tool_use_id": toolUseID,
					"is_error":    isError,
				})
			}
		}

		if len(results) > 0 {
			return results
		}
		if len(textParts) == 0 {
			return nil
		}
		text := strings.Join(textParts, "\n")
		if strings.TrimSpace(text) == "" {
			return nil
		}
		return []map[string]any{{"type": "user", "timestamp": timestamp, "content": text}}
	}
	return nil
}

func extractToolResultContent(content any) string {
	switch c := content.(type) {
	case string:
		return c
	case []any:
		var parts []string
		for _, p := range c {
			if pm, ok := p.(map[string]any); ok {
				if pt, _ := pm["type"].(string); pt == "text" {
					if text, _ := pm["text"].(string); text != "" {
						parts = append(parts, text)
					}
				}
			}
		}
		return strings.Join(parts, "\n")
	}
	return ""
}

func parseClaudeAssistantEntry(entry map[string]any, timestamp string, toolUseNames map[string]string) []map[string]any {
	msg, _ := entry["message"].(map[string]any)
	if msg == nil {
		return nil
	}
	content := msg["content"]

	var text string
	var toolUses []map[string]any

	switch c := content.(type) {
	case string:
		text = c
	case []any:
		var textParts []string
		for _, block := range c {
			b, ok := block.(map[string]any)
			if !ok {
				continue
			}
			bt, _ := b["type"].(string)
			if bt == "text" {
				if t, _ := b["text"].(string); t != "" {
					textParts = append(textParts, t)
				}
			} else if bt == "tool_use" {
				toolName, _ := b["name"].(string)
				toolUseID, _ := b["id"].(string)
				toolInput, _ := b["input"].(map[string]any)
				if toolInput == nil {
					toolInput = map[string]any{}
				}

				toolEntry := map[string]any{
					"name":          toolName,
					"tool_use_id":   toolUseID,
					"input_summary": summarizeToolInput(toolName, toolInput),
				}

				// Add extra fields for specific tools
				switch toolName {
				case "Bash":
					if cmd, _ := toolInput["command"].(string); cmd != "" {
						toolEntry["command"] = cmd
					}
					if desc, _ := toolInput["description"].(string); desc != "" {
						toolEntry["description"] = desc
					}
				case "AskUserQuestion":
					if q, ok := toolInput["questions"]; ok {
						toolEntry["questions"] = q
					}
				case "Edit":
					if old, _ := toolInput["old_string"].(string); old != "" {
						toolEntry["old_string"] = old
					}
					if new_, _ := toolInput["new_string"].(string); new_ != "" {
						toolEntry["new_string"] = new_
					}
				case "Write":
					if wc, _ := toolInput["content"].(string); wc != "" {
						if len(wc) > 10000 {
							wc = wc[:10000] + "\n... (truncated)"
						}
						toolEntry["write_content"] = wc
					}
				}

				toolUses = append(toolUses, toolEntry)
				if toolUseID != "" {
					toolUseNames[toolUseID] = toolName
				}
			}
		}
		text = strings.Join(textParts, "\n")
	default:
		return nil
	}

	// Strip PULSE markers
	text = pulseRE.ReplaceAllString(text, "")
	text = strings.TrimSpace(text)

	if text == "" && len(toolUses) == 0 {
		return nil
	}

	result := map[string]any{
		"type":      "assistant",
		"timestamp": timestamp,
		"text":      text,
	}
	if toolUses != nil {
		result["tool_uses"] = toolUses
	} else {
		result["tool_uses"] = []map[string]any{}
	}
	return []map[string]any{result}
}

func summarizeToolInput(name string, input map[string]any) string {
	switch name {
	case "Read", "Edit", "Write", "NotebookEdit":
		if fp, _ := input["file_path"].(string); fp != "" {
			return fp
		}
		if np, _ := input["notebook_path"].(string); np != "" {
			return np
		}
	case "Bash":
		if cmd, _ := input["command"].(string); cmd != "" {
			if len(cmd) > 120 {
				return cmd[:120] + "..."
			}
			return cmd
		}
	case "Grep", "Glob":
		pattern, _ := input["pattern"].(string)
		path, _ := input["path"].(string)
		if path != "" {
			return pattern + " in " + path
		}
		return pattern
	case "Agent":
		if desc, _ := input["description"].(string); desc != "" {
			return truncate(desc, 120)
		}
		if prompt, _ := input["prompt"].(string); prompt != "" {
			return truncate(prompt, 120)
		}
	case "TaskCreate", "TaskUpdate":
		if subj, _ := input["subject"].(string); subj != "" {
			return subj
		}
		if tid, _ := input["taskId"].(string); tid != "" {
			return tid
		}
	case "WebSearch":
		if q, _ := input["query"].(string); q != "" {
			return q
		}
	case "WebFetch":
		if u, _ := input["url"].(string); u != "" {
			return u
		}
	}
	// Fallback: first non-empty string value
	for _, v := range input {
		if s, ok := v.(string); ok && s != "" {
			return truncate(s, 100)
		}
	}
	return ""
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n] + "..."
	}
	return s
}

// parseGeminiEntry handles Gemini JSON transcript format.
func parseGeminiEntry(entry map[string]any) []map[string]any {
	role, _ := entry["role"].(string)
	parts, _ := entry["parts"].([]any)
	timestamp, _ := entry["timestamp"].(string)

	if len(parts) == 0 {
		return nil
	}

	var textParts []string
	for _, p := range parts {
		if pm, ok := p.(map[string]any); ok {
			if text, _ := pm["text"].(string); text != "" {
				textParts = append(textParts, text)
			}
		}
	}

	text := strings.Join(textParts, "\n")
	if strings.TrimSpace(text) == "" {
		return nil
	}

	msgType := "user"
	if role == "model" {
		msgType = "assistant"
	}

	result := map[string]any{
		"type":      msgType,
		"timestamp": timestamp,
		"content":   text,
	}
	if msgType == "assistant" {
		result["text"] = text
		result["tool_uses"] = []map[string]any{}
	}
	return []map[string]any{result}
}
