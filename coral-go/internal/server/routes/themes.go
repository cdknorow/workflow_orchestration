package routes

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/cdknorow/coral/internal/config"
	"github.com/cdknorow/coral/internal/executil"
)

// ThemesHandler handles theme CRUD endpoints.
type ThemesHandler struct {
	cfg       *config.Config
	themesDir string
}

func NewThemesHandler(cfg *config.Config) *ThemesHandler {
	return &ThemesHandler{
		cfg:       cfg,
		themesDir: filepath.Join(cfg.CoralDir(), "themes"),
	}
}

var safeNameRE = regexp.MustCompile(`[^a-zA-Z0-9\-_ ]`)

func (h *ThemesHandler) ensureDir() {
	os.MkdirAll(h.themesDir, 0755)
}

// bundledThemes maps theme name → JSON content for themes shipped with Coral.
var bundledThemes = map[string]string{
	"GhostV3": `{
  "description": "Dark indigo theme with soft pastels and cool blue accents",
  "base": "dark",
  "variables": {
    "--bg-primary": "#0a0e27",
    "--bg-secondary": "#1a1f3a",
    "--bg-tertiary": "#2a2f4a",
    "--bg-hover": "#35394f",
    "--bg-elevated": "#3a3f5a",
    "--topbar-bg": "#1a1f3a",
    "--topbar-border": "#4a4f6a",
    "--border": "#4a4f6a",
    "--border-light": "#5a5f7a",
    "--text-primary": "#d0d4e8",
    "--text-secondary": "#8a8fa0",
    "--text-muted": "#5a5f75",
    "--accent": "#7dd3fc",
    "--accent-dim": "#38bdf8",
    "--success": "#86efac",
    "--warning": "#fbbf24",
    "--error": "#f87171",
    "--badge-claude": "#a78bfa",
    "--badge-gemini": "#f472b6",
    "--sh-keyword": "#a78bfa",
    "--sh-string": "#86efac",
    "--sh-comment": "#6b7280",
    "--sh-number": "#fbbf24",
    "--sh-builtin": "#7dd3fc",
    "--sh-decorator": "#f472b6",
    "--diff-add-bg": "#1d3a1a",
    "--diff-add-color": "#86efac",
    "--diff-del-bg": "#3a1d1d",
    "--diff-del-color": "#f87171",
    "--color-tool-read": "#60a5fa",
    "--color-tool-write": "#34d399",
    "--color-tool-edit": "#fbbf24",
    "--color-tool-bash": "#f87171",
    "--color-tool-grep": "#a78bfa",
    "--color-tool-web": "#7dd3fc",
    "--color-tool-status": "#60a5fa",
    "--color-tool-goal": "#a78bfa",
    "--color-tool-stop": "#f87171",
    "--chat-human-bg": "#1a1f3a",
    "--chat-human-color": "#d0d4e8",
    "--xterm-background": "#0a0e27",
    "--xterm-foreground": "#d0d4e8",
    "--xterm-cursor": "#7dd3fc",
    "--xterm-selection-background": "#35394f",
    "--xterm-black": "#1a1f3a",
    "--xterm-red": "#f87171",
    "--xterm-green": "#86efac",
    "--xterm-yellow": "#fbbf24",
    "--xterm-blue": "#60a5fa",
    "--xterm-magenta": "#a78bfa",
    "--xterm-cyan": "#7dd3fc",
    "--xterm-white": "#d0d4e8",
    "--xterm-bright-black": "#4a4f6a",
    "--xterm-bright-red": "#fb7185",
    "--xterm-bright-green": "#6ee7b7",
    "--xterm-bright-yellow": "#fcd34d",
    "--xterm-bright-blue": "#93c5fd",
    "--xterm-bright-magenta": "#d8b4fe",
    "--xterm-bright-cyan": "#a5f3fc",
    "--xterm-bright-white": "#f1f5f9",
    "--mb-bg": "#1a1f3a",
    "--mb-text": "#e0e0e0",
    "--mb-text-bright": "#f0f0f0",
    "--mb-heading": "#88c0d0",
    "--mb-code-bg": "#0a0e27"
  }
}`,
}

// SeedBundledThemes copies bundled themes to the user themes directory if they
// don't already exist. Matches Python's seed_bundled_themes().
func (h *ThemesHandler) SeedBundledThemes() {
	h.ensureDir()
	for name, content := range bundledThemes {
		dest := filepath.Join(h.themesDir, name+".json")
		if _, err := os.Stat(dest); err == nil {
			continue // already exists
		}
		if err := os.WriteFile(dest, []byte(content), 0644); err != nil {
			log.Printf("Warning: failed to seed bundled theme %s: %v", name, err)
			continue
		}
		log.Printf("Seeded bundled theme: %s", name)
	}
}

func (h *ThemesHandler) safePath(name string) string {
	safe := strings.TrimSpace(safeNameRE.ReplaceAllString(name, ""))
	if safe == "" {
		return ""
	}
	return filepath.Join(h.themesDir, safe+".json")
}

// ListThemes returns all saved themes.
// GET /api/themes
func (h *ThemesHandler) ListThemes(w http.ResponseWriter, r *http.Request) {
	h.ensureDir()
	entries, err := os.ReadDir(h.themesDir)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"themes": []any{}})
		return
	}

	type themeInfo struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Base        string `json:"base"`
	}

	var themes []themeInfo
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(h.themesDir, e.Name()))
		if err != nil {
			continue
		}
		var t struct {
			Description string `json:"description"`
			Base        string `json:"base"`
		}
		if json.Unmarshal(data, &t) != nil {
			continue
		}
		name := strings.TrimSuffix(e.Name(), ".json")
		themes = append(themes, themeInfo{Name: name, Description: t.Description, Base: t.Base})
	}
	sort.Slice(themes, func(i, j int) bool { return themes[i].Name < themes[j].Name })
	if themes == nil {
		themes = []themeInfo{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"themes": themes})
}

// GetTheme returns a specific theme.
// GET /api/themes/{name}
func (h *ThemesHandler) GetTheme(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	path := h.safePath(name)
	if path == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid theme name"})
		return
	}
	data, err := os.ReadFile(path)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "Theme not found"})
		return
	}
	var theme map[string]any
	if json.Unmarshal(data, &theme) != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Invalid theme file"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"name": name, "theme": theme})
}

// SaveTheme saves or updates a theme.
// PUT /api/themes/{name}
func (h *ThemesHandler) SaveTheme(w http.ResponseWriter, r *http.Request) {
	h.ensureDir()
	name := chi.URLParam(r, "name")
	path := h.safePath(name)
	if path == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid theme name"})
		return
	}
	var body map[string]any
	if err := decodeJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
		return
	}
	themeData := map[string]any{
		"description": body["description"],
		"base":        body["base"],
		"variables":   body["variables"],
	}
	data, _ := json.MarshalIndent(themeData, "", "  ")
	if err := os.WriteFile(path, data, 0644); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "name": name})
}

// DeleteTheme deletes a theme.
// DELETE /api/themes/{name}
func (h *ThemesHandler) DeleteTheme(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	path := h.safePath(name)
	if path != "" {
		os.Remove(path)
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// ImportTheme imports a theme from an uploaded JSON file.
// POST /api/themes/import
func (h *ThemesHandler) ImportTheme(w http.ResponseWriter, r *http.Request) {
	h.ensureDir()
	r.ParseMultipartForm(10 << 20) // 10 MB
	file, header, err := r.FormFile("file")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "file required"})
		return
	}
	defer file.Close()

	data, _ := io.ReadAll(file)
	var parsed map[string]any
	if json.Unmarshal(data, &parsed) != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid JSON file"})
		return
	}

	name, _ := parsed["name"].(string)
	if name == "" {
		name = strings.TrimSuffix(header.Filename, ".json")
	}
	safe := strings.TrimSpace(safeNameRE.ReplaceAllString(name, ""))
	if safe == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Could not determine theme name"})
		return
	}

	themeData := map[string]any{
		"description": parsed["description"],
		"base":        parsed["base"],
		"variables":   parsed["variables"],
	}
	out, _ := json.MarshalIndent(themeData, "", "  ")
	os.WriteFile(filepath.Join(h.themesDir, safe+".json"), out, 0644)

	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "name": safe})
}

// GetThemeVariables returns the theme variable definitions.
// GET /api/themes/variables
func (h *ThemesHandler) GetThemeVariables(w http.ResponseWriter, r *http.Request) {
	// Matches Python THEME_VARIABLES dict
	writeJSON(w, http.StatusOK, map[string]any{"groups": themeVariableGroups})
}

// GenerateTheme uses an LLM to generate theme colors from a text description.
// POST /api/themes/generate
func (h *ThemesHandler) GenerateTheme(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Description string `json:"description"`
		Base        string `json:"base"`
		AgentType   string `json:"agent_type"`
	}
	if err := decodeJSON(r, &body); err != nil || strings.TrimSpace(body.Description) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Description is required"})
		return
	}
	if body.Base == "" {
		body.Base = "dark"
	}

	// Find an available LLM CLI — try requested type first, then fall back
	cliPath, cliArgs := resolveThemeCLI(body.AgentType)
	if cliPath == "" {
		writeJSON(w, http.StatusOK, map[string]string{"error": "No LLM CLI found — install Claude Code (npm install -g @anthropic-ai/claude-code) or Gemini CLI"})
		return
	}

	// Build the variable list for the prompt
	var varList strings.Builder
	for groupName, vars := range themeVariableGroups {
		varList.WriteString("\n" + groupName + ":\n")
		for cssVar, label := range vars {
			varList.WriteString("  " + cssVar + " — " + label + "\n")
		}
	}

	prompt := generatePrompt + varList.String() + "\n" +
		"The theme should be based on a " + body.Base + " color scheme.\n" +
		"User's description: " + body.Description + "\n\n" +
		"Respond with ONLY the JSON object."

	args := append(cliArgs, prompt)
	cmd := executil.Command(r.Context(), cliPath, args...)
	output, err := cmd.Output()
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]string{"error": "Claude CLI failed: " + err.Error()})
		return
	}

	// Strip markdown fences if present
	raw := strings.TrimSpace(string(output))
	if strings.HasPrefix(raw, "```") {
		lines := strings.Split(raw, "\n")
		var cleaned []string
		for _, line := range lines {
			if !strings.HasPrefix(strings.TrimSpace(line), "```") {
				cleaned = append(cleaned, line)
			}
		}
		raw = strings.TrimSpace(strings.Join(cleaned, "\n"))
	}

	var result map[string]any
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"error": "Failed to parse LLM response as JSON", "raw": raw})
		return
	}

	variables, _ := result["variables"].(map[string]any)
	if variables == nil {
		variables, _ = result["variables"].(map[string]any)
		if variables == nil {
			// Response might be flat (variables at root level)
			variables = result
		}
	}

	name, _ := result["name"].(string)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "variables": variables, "name": name})
}

// resolveThemeCLI finds an available LLM CLI for theme generation.
// Tries the requested agent type first, then falls back to others.
func resolveThemeCLI(agentType string) (path string, args []string) {
	type cliOption struct {
		binary string
		args   []string
	}
	options := []cliOption{
		{"claude", []string{"--print", "--model", "haiku", "--no-session-persistence"}},
		{"gemini", []string{"--print"}},
		{"codex", []string{"--print"}},
	}

	// Try requested type first
	if agentType != "" {
		for _, opt := range options {
			if opt.binary == agentType {
				if p, err := exec.LookPath(opt.binary); err == nil {
					return p, opt.args
				}
			}
		}
	}

	// Fall back to any available CLI
	for _, opt := range options {
		if p, err := exec.LookPath(opt.binary); err == nil {
			return p, opt.args
		}
	}
	return "", nil
}

const generatePrompt = `You are a UI theme designer. Given a description of a color theme, generate a complete set of CSS color values for a web dashboard.

You MUST respond with ONLY a valid JSON object — no markdown, no explanation, no code fences. The JSON must have this exact structure:

{
  "name": "A short creative name for this theme (2-4 words)",
  "variables": {
    "--css-variable-name": "#hexcolor",
    ...
  }
}

Here are the CSS variables you must provide values for, grouped by category:
`

var themeVariableGroups = map[string]map[string]string{
	"Surface / Background": {
		"--bg-primary": "Primary background", "--bg-secondary": "Secondary background",
		"--bg-tertiary": "Tertiary background", "--bg-hover": "Hover background",
		"--bg-elevated": "Elevated surface", "--topbar-bg": "Top bar background",
		"--topbar-border": "Top bar border",
	},
	"Borders":         {"--border": "Border", "--border-light": "Light border"},
	"Text":            {"--text-primary": "Primary text", "--text-secondary": "Secondary text", "--text-muted": "Muted text"},
	"Accent / Brand":  {"--accent": "Accent", "--accent-dim": "Accent dim"},
	"Semantic Status": {"--success": "Success", "--warning": "Warning", "--error": "Error"},
	"Agent Badges":    {"--badge-claude": "Claude badge", "--badge-gemini": "Gemini badge"},
	"Syntax Highlighting": {
		"--sh-keyword": "Keyword", "--sh-string": "String", "--sh-comment": "Comment",
		"--sh-number": "Number", "--sh-builtin": "Builtin", "--sh-decorator": "Decorator",
	},
	"Diff": {
		"--diff-add-bg": "Addition background", "--diff-add-color": "Addition text",
		"--diff-del-bg": "Deletion background", "--diff-del-color": "Deletion text",
	},
	"Tool / Event Colors": {
		"--color-tool-read": "Read tool", "--color-tool-write": "Write tool",
		"--color-tool-edit": "Edit tool", "--color-tool-bash": "Bash tool",
		"--color-tool-grep": "Grep tool", "--color-tool-web": "Web tool",
		"--color-tool-status": "Status event", "--color-tool-goal": "Goal event",
		"--color-tool-stop": "Stop event",
	},
	"Chat": {
		"--chat-human-bg": "Human message background", "--chat-human-color": "Human message text",
	},
	"Terminal (xterm)": {
		"--xterm-background": "Background", "--xterm-foreground": "Foreground",
		"--xterm-cursor": "Cursor", "--xterm-selection-background": "Selection background",
		"--xterm-black": "Black", "--xterm-red": "Red", "--xterm-green": "Green",
		"--xterm-yellow": "Yellow", "--xterm-blue": "Blue", "--xterm-magenta": "Magenta",
		"--xterm-cyan": "Cyan", "--xterm-white": "White",
		"--xterm-bright-black": "Bright black", "--xterm-bright-red": "Bright red",
		"--xterm-bright-green": "Bright green", "--xterm-bright-yellow": "Bright yellow",
		"--xterm-bright-blue": "Bright blue", "--xterm-bright-magenta": "Bright magenta",
		"--xterm-bright-cyan": "Bright cyan", "--xterm-bright-white": "Bright white",
	},
	"Message Board": {
		"--mb-bg": "Message background", "--mb-text": "Body text",
		"--mb-text-bright": "Bold/emphasis text", "--mb-heading": "Heading color",
		"--mb-code-bg": "Code block background",
	},
}
