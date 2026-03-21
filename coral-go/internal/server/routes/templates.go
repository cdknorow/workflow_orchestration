package routes

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
)

const (
	templateRepo     = "davila7/claude-code-templates"
	githubAPIBase    = "https://api.github.com/repos/" + templateRepo + "/contents"
	templateCacheTTL = time.Hour
)

// cacheEntry stores a cached GitHub API response.
type cacheEntry struct {
	fetchedAt time.Time
	data      interface{}
}

// TemplatesHandler proxies the davila7/claude-code-templates GitHub repo with in-memory caching.
type TemplatesHandler struct {
	mu    sync.RWMutex
	cache map[string]cacheEntry
}

// NewTemplatesHandler creates a new TemplatesHandler.
func NewTemplatesHandler() *TemplatesHandler {
	return &TemplatesHandler{
		cache: make(map[string]cacheEntry),
	}
}

func (h *TemplatesHandler) cacheGet(key string) (interface{}, bool) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	entry, ok := h.cache[key]
	if !ok || time.Since(entry.fetchedAt) > templateCacheTTL {
		return nil, false
	}
	return entry.data, true
}

func (h *TemplatesHandler) cacheSet(key string, data interface{}) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.cache[key] = cacheEntry{fetchedAt: time.Now(), data: data}
}

// githubFetch fetches a path from the GitHub Contents API with caching.
func (h *TemplatesHandler) githubFetch(path string) (interface{}, error) {
	url := fmt.Sprintf("%s/%s", githubAPIBase, path)
	if cached, ok := h.cacheGet(url); ok {
		return cached, nil
	}

	client := &http.Client{Timeout: 15 * time.Second}
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github.v3+json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("GitHub API returned %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var data interface{}
	if err := json.Unmarshal(body, &data); err != nil {
		return nil, err
	}

	h.cacheSet(url, data)
	return data, nil
}

// parseFrontmatter parses YAML-like frontmatter from markdown content without a YAML library.
func parseFrontmatter(content string) (map[string]string, string) {
	parts := strings.SplitN(content, "---", 3)
	if len(parts) >= 3 {
		meta := make(map[string]string)
		for _, line := range strings.Split(strings.TrimSpace(parts[1]), "\n") {
			idx := strings.Index(line, ":")
			if idx < 0 {
				continue
			}
			key := strings.TrimSpace(line[:idx])
			val := strings.TrimSpace(line[idx+1:])
			val = strings.Trim(strings.Trim(val, "\""), "'")
			if val != "" {
				meta[key] = val
			}
		}
		return meta, strings.TrimSpace(parts[2])
	}
	return map[string]string{}, content
}

// decodeGitHubFileContent decodes base64 file content from a GitHub API response.
func decodeGitHubFileContent(data map[string]interface{}) string {
	contentB64, _ := data["content"].(string)
	if contentB64 == "" {
		return ""
	}
	// GitHub returns base64 with embedded newlines; strip them first (matches Python b64decode behavior)
	cleaned := strings.ReplaceAll(contentB64, "\n", "")
	decoded, err := base64.StdEncoding.DecodeString(cleaned)
	if err != nil {
		return ""
	}
	return string(decoded)
}

// extractDirItems extracts directory entries of a given type from a GitHub Contents API response.
func extractDirItems(data interface{}, filterType string) []map[string]string {
	items, ok := data.([]interface{})
	if !ok {
		return nil
	}
	var result []map[string]string
	for _, item := range items {
		m, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		itemType, _ := m["type"].(string)
		name, _ := m["name"].(string)
		if itemType == filterType {
			result = append(result, map[string]string{"name": name, "type": itemType})
		}
	}
	return result
}

// extractFileItems extracts .md file entries from a GitHub Contents API response.
func extractFileItems(data interface{}) []map[string]string {
	items, ok := data.([]interface{})
	if !ok {
		return nil
	}
	var result []map[string]string
	for _, item := range items {
		m, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		itemType, _ := m["type"].(string)
		name, _ := m["name"].(string)
		if itemType == "file" && strings.HasSuffix(name, ".md") {
			result = append(result, map[string]string{
				"name":     strings.TrimSuffix(name, ".md"),
				"filename": name,
			})
		}
	}
	return result
}

// ── Agent templates ─────────────────────────────────────────────────

// ListAgentCategories lists agent template categories (top-level directories).
// GET /api/templates/agents
func (h *TemplatesHandler) ListAgentCategories(w http.ResponseWriter, r *http.Request) {
	data, err := h.githubFetch("cli-tool/components/agents")
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"error":      err.Error(),
			"categories": []interface{}{},
		})
		return
	}
	categories := extractDirItems(data, "dir")
	if categories == nil {
		categories = []map[string]string{}
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"categories": categories})
}

// ListAgentsInCategory lists agent templates in a category.
// GET /api/templates/agents/{category}
func (h *TemplatesHandler) ListAgentsInCategory(w http.ResponseWriter, r *http.Request) {
	category := chi.URLParam(r, "category")
	data, err := h.githubFetch(fmt.Sprintf("cli-tool/components/agents/%s", category))
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"error":  err.Error(),
			"agents": []interface{}{},
		})
		return
	}
	agents := extractFileItems(data)
	if agents == nil {
		agents = []map[string]string{}
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"agents":   agents,
		"category": category,
	})
}

// GetAgentTemplate returns a specific agent template with parsed frontmatter.
// GET /api/templates/agents/{category}/{name}
func (h *TemplatesHandler) GetAgentTemplate(w http.ResponseWriter, r *http.Request) {
	category := chi.URLParam(r, "category")
	name := chi.URLParam(r, "name")
	filename := name
	if !strings.HasSuffix(filename, ".md") {
		filename += ".md"
	}

	data, err := h.githubFetch(fmt.Sprintf("cli-tool/components/agents/%s/%s", category, filename))
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{"error": err.Error()})
		return
	}

	dataMap, ok := data.(map[string]interface{})
	if !ok {
		writeJSON(w, http.StatusOK, map[string]interface{}{"error": "unexpected response format"})
		return
	}

	content := decodeGitHubFileContent(dataMap)
	meta, body := parseFrontmatter(content)
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"name":        metaOrDefault(meta, "name", name),
		"description": metaOrDefault(meta, "description", ""),
		"tools":       metaOrDefault(meta, "tools", ""),
		"model":       metaOrDefault(meta, "model", ""),
		"body":        body,
		"category":    category,
	})
}

// ── Command templates ───────────────────────────────────────────────

// ListCommandCategories lists command template categories.
// GET /api/templates/commands
func (h *TemplatesHandler) ListCommandCategories(w http.ResponseWriter, r *http.Request) {
	data, err := h.githubFetch("cli-tool/components/commands")
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"error":      err.Error(),
			"categories": []interface{}{},
		})
		return
	}
	categories := extractDirItems(data, "dir")
	if categories == nil {
		categories = []map[string]string{}
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"categories": categories})
}

// ListCommandsInCategory lists command templates in a category.
// GET /api/templates/commands/{category}
func (h *TemplatesHandler) ListCommandsInCategory(w http.ResponseWriter, r *http.Request) {
	category := chi.URLParam(r, "category")
	data, err := h.githubFetch(fmt.Sprintf("cli-tool/components/commands/%s", category))
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"error":    err.Error(),
			"commands": []interface{}{},
		})
		return
	}
	commands := extractFileItems(data)
	if commands == nil {
		commands = []map[string]string{}
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"commands": commands,
		"category": category,
	})
}

// GetCommandTemplate returns a specific command template with parsed frontmatter.
// GET /api/templates/commands/{category}/{name}
func (h *TemplatesHandler) GetCommandTemplate(w http.ResponseWriter, r *http.Request) {
	category := chi.URLParam(r, "category")
	name := chi.URLParam(r, "name")
	filename := name
	if !strings.HasSuffix(filename, ".md") {
		filename += ".md"
	}

	data, err := h.githubFetch(fmt.Sprintf("cli-tool/components/commands/%s/%s", category, filename))
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{"error": err.Error()})
		return
	}

	dataMap, ok := data.(map[string]interface{})
	if !ok {
		writeJSON(w, http.StatusOK, map[string]interface{}{"error": "unexpected response format"})
		return
	}

	content := decodeGitHubFileContent(dataMap)
	meta, body := parseFrontmatter(content)
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"name":          metaOrDefault(meta, "name", name),
		"description":   metaOrDefault(meta, "description", ""),
		"allowed_tools": metaOrDefault(meta, "allowed-tools", ""),
		"argument_hint": metaOrDefault(meta, "argument-hint", ""),
		"body":          body,
		"category":      category,
	})
}

func metaOrDefault(meta map[string]string, key, fallback string) string {
	if v, ok := meta[key]; ok {
		return v
	}
	return fallback
}
