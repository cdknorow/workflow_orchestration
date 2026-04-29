# Themes & Customization

Coral's dashboard is fully themeable. You can switch between bundled themes, create custom themes with a visual editor, import and export themes as JSON files, and even generate themes using AI. The theme system is powered by CSS custom properties, making every color in the dashboard — including the terminal emulator, diff viewer, and syntax highlighting — configurable.

---

## Switching themes

1. Open the **Theme Configurator** from the settings (gear icon).
2. Select a theme from the **Load Saved Theme** dropdown.
3. The theme applies immediately — no page reload needed.

Coral ships with the **GhostV3** theme as the default: a dark indigo theme with soft pastels and cool blue accents.

---

## Creating a custom theme

The theme editor lets you customize every color in the dashboard using color pickers organized by category.

1. Open the Theme Configurator.
2. Choose a **base** scheme: dark or light.
3. Adjust colors by category — each variable has a color picker and hex input:

| Category | What it controls |
|----------|-----------------|
| **Surface / Background** | Dashboard backgrounds, topbar, elevated surfaces |
| **Borders** | Border colors throughout the UI |
| **Text** | Primary, secondary, and muted text |
| **Accent / Brand** | Links, highlights, active elements |
| **Semantic Status** | Success (green), warning (yellow), error (red) indicators |
| **Agent Badges** | Claude and Gemini badge colors |
| **Syntax Highlighting** | Keywords, strings, comments, numbers in code blocks |
| **Diff** | Addition and deletion colors in inline diffs |
| **Tool / Event Colors** | Activity timeline colors for Read, Write, Edit, Bash, Grep, etc. |
| **Chat** | Human message background and text in the chat transcript |
| **Terminal (xterm)** | Full 16-color ANSI palette plus background, foreground, cursor, and selection |
| **Diff Viewer (diff2html)** | Side-by-side diff viewer colors |

4. Click **Preview** to see changes live without saving.
5. Enter a theme name and click **Save & Apply**.

!!! info
    Themes are stored as JSON files in `~/.coral/themes/`, not in the database. Each theme is a standalone file that can be shared independently.

---

## AI theme generation

Coral can generate a complete theme from a text description using Claude.

1. In the Theme Configurator, enter a description in the **AI Generate** field (e.g., "Cyberpunk neon with pink and teal accents" or "Warm earth tones inspired by a desert sunset").
2. Select the base scheme (dark or light).
3. Click **Generate**.
4. Coral calls Claude Haiku to produce values for all 60+ CSS variables.
5. The generated theme populates the editor for review and adjustment.
6. Customize any colors you want to change, then save.

!!! note
    AI theme generation requires the Claude CLI to be installed. If it's not available, the feature shows an error message.

---

## Import & export

### Exporting a theme

Click **Export** in the Theme Configurator to download the current theme as a JSON file. Share it with teammates or back it up.

### Importing a theme

Click **Import** and select a `.json` theme file. The theme is saved to `~/.coral/themes/` and becomes available in the theme dropdown.

### Theme JSON format

```json
{
  "description": "A brief description of the theme",
  "base": "dark",
  "variables": {
    "--bg-primary": "#0a0e27",
    "--bg-secondary": "#1a1f3a",
    "--text-primary": "#d0d4e8",
    "--accent": "#7dd3fc",
    "--xterm-background": "#0a0e27",
    "--xterm-foreground": "#d0d4e8"
  }
}
```

The `variables` object maps CSS custom property names to color values (hex, rgb, or named colors).

---

## CSS variable reference

Coral themes use 60+ CSS custom properties across 12 categories. Here are the key variables:

### Surface & background

| Variable | Description |
|----------|-------------|
| `--bg-primary` | Main dashboard background |
| `--bg-secondary` | Sidebar and panel backgrounds |
| `--bg-tertiary` | Nested element backgrounds |
| `--bg-hover` | Hover state background |
| `--bg-elevated` | Elevated surface (modals, cards) |
| `--topbar-bg` | Top navigation bar background |
| `--topbar-border` | Top bar bottom border |

### Text & borders

| Variable | Description |
|----------|-------------|
| `--text-primary` | Main text color |
| `--text-secondary` | Supporting text |
| `--text-muted` | De-emphasized text |
| `--border` | Standard border color |
| `--border-light` | Subtle border color |

### Accent & status

| Variable | Description |
|----------|-------------|
| `--accent` | Primary accent color (links, active states) |
| `--accent-dim` | Dimmed accent |
| `--success` | Success indicators |
| `--warning` | Warning indicators |
| `--error` | Error indicators |
| `--badge-claude` | Claude agent badge color |
| `--badge-gemini` | Gemini agent badge color |

### Terminal (xterm)

| Variable | Description |
|----------|-------------|
| `--xterm-background` | Terminal background |
| `--xterm-foreground` | Terminal text |
| `--xterm-cursor` | Cursor color |
| `--xterm-selection-background` | Text selection |
| `--xterm-black` through `--xterm-white` | Standard 8 ANSI colors |
| `--xterm-bright-black` through `--xterm-bright-white` | Bright 8 ANSI colors |

For the complete list of all variables, use the API: `GET /api/themes/variables`.

---

## API reference

| Method | Endpoint | Description |
|--------|----------|-------------|
| `GET` | `/api/themes` | List all saved themes |
| `GET` | `/api/themes/{name}` | Get a specific theme |
| `PUT` | `/api/themes/{name}` | Save or update a theme |
| `DELETE` | `/api/themes/{name}` | Delete a theme |
| `POST` | `/api/themes/import` | Import a theme from an uploaded JSON file |
| `POST` | `/api/themes/generate` | AI-generate theme from a text description |
| `GET` | `/api/themes/variables` | Get all CSS variable definitions grouped by category |

---

## See also

- [Live Sessions](live-sessions.md) — The dashboard where themes are applied
- [Session History & Search](session-history-search.md) — Themes affect the history view too
