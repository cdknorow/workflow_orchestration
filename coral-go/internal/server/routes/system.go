package routes

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/cdknorow/coral/internal/agent"
	"github.com/cdknorow/coral/internal/config"
	"github.com/cdknorow/coral/internal/store"
)

// Indexer is the interface for triggering an index refresh.
type Indexer interface {
	RunOnce(ctx context.Context) error
}

// SystemHandler handles settings, tags, and filesystem endpoints.
type SystemHandler struct {
	db      *store.DB
	ss      *store.SessionStore
	cfg     *config.Config
	indexer Indexer
}

// NewSystemHandler creates a SystemHandler.
func NewSystemHandler(db *store.DB, cfg *config.Config) *SystemHandler {
	return &SystemHandler{db: db, ss: store.NewSessionStore(db), cfg: cfg}
}

// SetIndexer injects the session indexer for manual refresh triggers.
func (h *SystemHandler) SetIndexer(idx Indexer) {
	h.indexer = idx
}

// ── Settings ────────────────────────────────────────────────────────────

// sensitiveKeys are settings that should never be returned to the frontend.
var sensitiveKeys = map[string]bool{}

// GetSettings returns all user settings, filtering out sensitive keys.
// GET /api/settings
func (h *SystemHandler) GetSettings(w http.ResponseWriter, r *http.Request) {
	rows, err := h.db.QueryxContext(r.Context(), "SELECT key, value FROM user_settings")
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer rows.Close()

	settings := make(map[string]string)
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err == nil {
			if !sensitiveKeys[k] {
				settings[k] = v
			}
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"settings": settings})
}

// PutSettings upserts one or more settings.
// PUT /api/settings
func (h *SystemHandler) PutSettings(w http.ResponseWriter, r *http.Request) {
	var body map[string]any
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
		return
	}
	for k, v := range body {
		// Convert any value to string, matching Python's str() behavior.
		// Python's str(True)="True", str(False)="False" — capitalize bools.
		var s string
		switch val := v.(type) {
		case bool:
			if val {
				s = "True"
			} else {
				s = "False"
			}
		default:
			s = fmt.Sprintf("%v", v)
		}
		_, err := h.db.ExecContext(r.Context(),
			"INSERT INTO user_settings (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value",
			k, s)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// ── Default Prompt Constants ─────────────────────────────────────────

const DefaultOrchestratorSystemPrompt = `Post a message with coral-board post "<your introduction>" that introduces yourself, then discuss your proposed plan with the operator (the human user) before posting assignments to the team.`

const DefaultWorkerSystemPrompt = `Post a message with coral-board post "<your introduction>" that introduces yourself, then wait for instructions from the Orchestrator.`

const DefaultOrchestratorPrompt = `IMPORTANT: You were automatically joined to message board "{board_name}". Do NOT run coral-board join. Post a message with coral-board post "<your introduction>" that introduces yourself, then discuss your proposed plan with the operator (the human user) before posting assignments. Periodically check for new messages.`

const DefaultWorkerPrompt = `IMPORTANT: You were automatically joined to message board "{board_name}". Do NOT run coral-board join. Do not start any actions until you receive instructions from the Orchestrator on the message board. Post a message with coral-board post "<your introduction>" that introduces yourself, then periodically check for new messages.`

// GetDefaultPrompts returns the hardcoded default prompt templates.
// GET /api/settings/default-prompts
func (h *SystemHandler) GetDefaultPrompts(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"default_prompt_orchestrator":        DefaultOrchestratorPrompt,
		"default_prompt_worker":              DefaultWorkerPrompt,
		"default_system_prompt_orchestrator": DefaultOrchestratorSystemPrompt,
		"default_system_prompt_worker":       DefaultWorkerSystemPrompt,
		"team_reminder_orchestrator":         "Remember to coordinate with your team and check the message board for updates",
		"team_reminder_worker":               "Remember to work with your team",
	})
}

// Status returns server status.
// GET /api/system/status
func (h *SystemHandler) Status(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"startup_complete": true,
		"version":          "0.1.0-go",
	})
}

// UpdateCheck returns update availability info.
// GET /api/system/update-check
func (h *SystemHandler) UpdateCheck(w http.ResponseWriter, r *http.Request) {
	// Go binary doesn't have a PyPI update mechanism yet.
	// Return the current version with no update available.
	writeJSON(w, http.StatusOK, map[string]any{
		"available": false,
		"current":   "0.1.0-go",
	})
}

// CLICheck verifies whether a CLI tool is installed and returns its version.
// GET /api/system/cli-check?type=claude  (check by agent type)
// GET /api/system/cli-check?binary=/path/to/codex  (check specific path)
func (h *SystemHandler) CLICheck(w http.ResponseWriter, r *http.Request) {
	binaryPath := r.URL.Query().Get("binary")
	agentType := r.URL.Query().Get("type")

	if binaryPath == "" && agentType == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "type or binary parameter required"})
		return
	}

	var installCmd string
	if binaryPath == "" {
		info := agent.GetCLIInfo(agentType)
		if info == nil {
			writeJSON(w, http.StatusOK, map[string]any{"found": true, "agent_type": agentType})
			return
		}
		binaryPath = info.Binary
		installCmd = info.InstallCommand
	}

	// Try LookPath first, then common install locations
	resolvedPath, err := exec.LookPath(binaryPath)
	if err != nil {
		if found := agent.FindCLIInCommonPaths(binaryPath); found != "" {
			resolvedPath = found
		} else {
			result := map[string]any{
				"found":      false,
				"binary":     binaryPath,
				"agent_type": agentType,
			}
			if installCmd != "" {
				result["install_command"] = installCmd
			}
			writeJSON(w, http.StatusOK, result)
			return
		}
	}

	// Try to get version
	version := ""
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	if out, err := exec.CommandContext(ctx, resolvedPath, "--version").Output(); err == nil {
		version = strings.TrimSpace(string(out))
		// Take first line only
		if idx := strings.IndexByte(version, '\n'); idx > 0 {
			version = version[:idx]
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"found":      true,
		"path":       resolvedPath,
		"version":    version,
		"agent_type": agentType,
	})
}

// RefreshIndexer triggers a manual re-index.
// POST /api/indexer/refresh
func (h *SystemHandler) RefreshIndexer(w http.ResponseWriter, r *http.Request) {
	if h.indexer == nil {
		writeJSON(w, http.StatusOK, map[string]any{"error": "Indexer not available"})
		return
	}
	if err := h.indexer.RunOnce(r.Context()); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// ListFilesystem lists directories for the directory browser.
// GET /api/filesystem/list?path=~
func (h *SystemHandler) ListFilesystem(w http.ResponseWriter, r *http.Request) {
	reqPath := r.URL.Query().Get("path")
	if reqPath == "" {
		reqPath = "~"
	}

	// Expand ~ to home directory
	if strings.HasPrefix(reqPath, "~") {
		home, _ := os.UserHomeDir()
		reqPath = filepath.Join(home, reqPath[1:])
	}

	expanded, err := filepath.Abs(reqPath)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid path"})
		return
	}

	// Security: restrict to home directory
	home, _ := os.UserHomeDir()
	if !strings.HasPrefix(expanded, home) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "access denied"})
		return
	}

	entries, err := os.ReadDir(expanded)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"path":    expanded,
			"entries": []any{},
			"error":   err.Error(),
		})
		return
	}

	// Python returns flat string array of directory names (not objects)
	dirs := make([]string, 0)
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".") {
			continue // skip hidden
		}
		if e.IsDir() {
			dirs = append(dirs, e.Name())
		}
	}
	sort.Strings(dirs)

	writeJSON(w, http.StatusOK, map[string]any{
		"path":    expanded,
		"entries": dirs,
	})
}

// ── Tags ────────────────────────────────────────────────────────────────

// ListTags returns all tags.
// GET /api/tags
func (h *SystemHandler) ListTags(w http.ResponseWriter, r *http.Request) {
	type tag struct {
		ID    int    `json:"id" db:"id"`
		Name  string `json:"name" db:"name"`
		Color string `json:"color" db:"color"`
	}
	var tags []tag
	if err := h.db.SelectContext(r.Context(), &tags, "SELECT id, name, color FROM tags ORDER BY name"); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if tags == nil {
		tags = []tag{}
	}
	writeJSON(w, http.StatusOK, tags)
}

// CreateTag creates a new tag.
// POST /api/tags
func (h *SystemHandler) CreateTag(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name  string `json:"name"`
		Color string `json:"color"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Tag name is required"})
		return
	}
	if body.Color == "" {
		body.Color = "#58a6ff"
	}
	result, err := h.db.ExecContext(r.Context(),
		"INSERT INTO tags (name, color) VALUES (?, ?)", body.Name, body.Color)
	if err != nil {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "tag already exists"})
		return
	}
	id, _ := result.LastInsertId()
	writeJSON(w, http.StatusOK, map[string]any{"id": id, "name": body.Name, "color": body.Color})
}

// DeleteTag removes a tag.
// DELETE /api/tags/{tagID}
func (h *SystemHandler) DeleteTag(w http.ResponseWriter, r *http.Request) {
	tagID, _ := strconv.Atoi(chi.URLParam(r, "tagID"))
	h.db.ExecContext(r.Context(), "DELETE FROM tags WHERE id = ?", tagID)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// AddSessionTag adds a tag to a session.
// POST /api/sessions/history/{sessionID}/tags
func (h *SystemHandler) AddSessionTag(w http.ResponseWriter, r *http.Request) {
	sessionID := chi.URLParam(r, "sessionID")
	var body struct {
		TagID int `json:"tag_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.TagID == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "tag_id is required"})
		return
	}
	h.db.ExecContext(r.Context(),
		"INSERT OR IGNORE INTO session_tags (session_id, tag_id) VALUES (?, ?)",
		sessionID, body.TagID)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// RemoveSessionTag removes a tag from a session.
// DELETE /api/sessions/history/{sessionID}/tags/{tagID}
func (h *SystemHandler) RemoveSessionTag(w http.ResponseWriter, r *http.Request) {
	sessionID := chi.URLParam(r, "sessionID")
	tagID, _ := strconv.Atoi(chi.URLParam(r, "tagID"))
	h.db.ExecContext(r.Context(),
		"DELETE FROM session_tags WHERE session_id = ? AND tag_id = ?",
		sessionID, tagID)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// ── Folder Tags ─────────────────────────────────────────────────────────

// GetAllFolderTags returns all folder tags grouped by folder name.
// GET /api/folder-tags
func (h *SystemHandler) GetAllFolderTags(w http.ResponseWriter, r *http.Request) {
	result, err := h.ss.GetAllFolderTags(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if result == nil {
		result = map[string][]store.Tag{}
	}
	writeJSON(w, http.StatusOK, result)
}

// GetFolderTags returns tags for a specific folder.
// GET /api/folder-tags/{folderName}
func (h *SystemHandler) GetFolderTags(w http.ResponseWriter, r *http.Request) {
	folderName := chi.URLParam(r, "folderName")
	tags, err := h.ss.GetFolderTags(r.Context(), folderName)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if tags == nil {
		tags = []store.Tag{}
	}
	writeJSON(w, http.StatusOK, tags)
}

// AddFolderTag adds a tag to a folder.
// POST /api/folder-tags/{folderName}
func (h *SystemHandler) AddFolderTag(w http.ResponseWriter, r *http.Request) {
	folderName := chi.URLParam(r, "folderName")
	var body struct {
		TagID int64 `json:"tag_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.TagID == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "tag_id is required"})
		return
	}
	if err := h.ss.AddFolderTag(r.Context(), folderName, body.TagID); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// RemoveFolderTag removes a tag from a folder.
// DELETE /api/folder-tags/{folderName}/{tagID}
func (h *SystemHandler) RemoveFolderTag(w http.ResponseWriter, r *http.Request) {
	folderName := chi.URLParam(r, "folderName")
	tagID, _ := strconv.ParseInt(chi.URLParam(r, "tagID"), 10, 64)
	if err := h.ss.RemoveFolderTag(r.Context(), folderName, tagID); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}
