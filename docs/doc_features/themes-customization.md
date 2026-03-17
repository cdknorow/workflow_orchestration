# Doc Feature Guide: Themes & Customization

## Overview

Coral supports fully customizable color themes for the dashboard. Users can switch between bundled themes, create custom themes with a visual editor, import/export themes as JSON files, and even generate themes using AI (Claude CLI). The theme system uses CSS custom properties, making every color in the dashboard configurable.

---

## Key Source Files & Architecture

| File | Role |
|------|------|
| `src/coral/api/themes.py` | FastAPI routes — list, get, save, delete, import, generate themes. Defines all CSS variable groups. |
| `src/coral/bundled_themes/` | Ships with default theme(s) (e.g., `GhostV3.json`) |
| `src/coral/static/theme_config.js` | Dashboard JS — theme picker UI, visual editor, live preview, import/export |

### Theme Data Model

Themes are stored as JSON files in `~/.coral/themes/`. Each theme has:

```json
{
  "description": "Theme description",
  "base": "dark",
  "variables": {
    "--bg-primary": "#0a0e27",
    "--text-primary": "#d0d4e8",
    "--accent": "#7dd3fc",
    ...
  }
}
```

### CSS Variable Categories

The theme system covers **10 categories** with 60+ CSS variables:

| Category | Examples |
|----------|----------|
| Surface / Background | `--bg-primary`, `--bg-secondary`, `--bg-elevated`, `--topbar-bg` |
| Borders | `--border`, `--border-light` |
| Text | `--text-primary`, `--text-secondary`, `--text-muted` |
| Accent / Brand | `--accent`, `--accent-dim` |
| Semantic Status | `--success`, `--warning`, `--error` |
| Agent Badges | `--badge-claude`, `--badge-gemini` |
| Syntax Highlighting | `--sh-keyword`, `--sh-string`, `--sh-comment`, etc. |
| Diff | `--diff-add-bg`, `--diff-del-bg`, etc. |
| Tool / Event Colors | `--color-tool-read`, `--color-tool-bash`, etc. |
| Chat | `--chat-human-bg`, `--chat-human-color` |
| Terminal (xterm) | Full 16-color palette + background/foreground/cursor/selection |
| Diff Viewer (diff2html) | `--d2h-code-bg`, `--d2h-ins-bg`, `--d2h-del-bg`, etc. |

### Architecture Flow

1. **Startup**: `seed_bundled_themes()` copies bundled themes to `~/.coral/themes/` if they don't exist
2. **Theme application**: Dashboard JS reads active theme from user settings, fetches its JSON, and applies CSS custom properties to `:root`
3. **Visual editor**: The theme editor UI groups variables by category, shows color pickers, and previews changes live
4. **AI generation**: User provides a text description → Coral calls `claude --print --model haiku` with a structured prompt → parses JSON response → populates the editor

---

## User-Facing Functionality & Workflows

### Switching Themes

1. Open the **Theme** settings (gear icon or theme button)
2. Select a theme from the dropdown list
3. Theme applies immediately with live preview

### Creating a Custom Theme

1. Open the theme editor
2. Choose a base (dark/light)
3. Customize CSS variables by category using color pickers
4. Each change previews live on the dashboard
5. Save the theme with a name

### AI Theme Generation

1. In the theme editor, click **Generate**
2. Enter a text description (e.g., "Ocean sunset with warm orange accents")
3. Select base scheme (dark/light)
4. Coral calls Claude Haiku to generate all color values
5. Generated theme populates the editor for review and adjustment
6. Save when satisfied

### Import/Export

- **Export**: Download theme as a JSON file to share or back up
- **Import**: Upload a JSON file to install a theme from another user or instance

### API Endpoints

| Method | Endpoint | Description |
|--------|----------|-------------|
| `GET` | `/api/themes` | List all themes |
| `GET` | `/api/themes/{name}` | Get a specific theme |
| `PUT` | `/api/themes/{name}` | Save/update a theme |
| `DELETE` | `/api/themes/{name}` | Delete a theme |
| `POST` | `/api/themes/import` | Import theme from uploaded JSON file |
| `POST` | `/api/themes/generate` | AI-generate theme from text description |
| `GET` | `/api/themes/variables` | Get theme variable definitions grouped by category |

---

## Suggested MkDocs Page Structure

### Title: "Themes & Customization"

1. **Introduction** — Dashboard is fully themeable, ships with a default theme
2. **Switching Themes** — How to select a theme
   - Screenshot: Theme picker dropdown
3. **Creating a Custom Theme** — Visual editor walkthrough
   - Category-by-category guide
   - Screenshot: Theme editor with color pickers
4. **AI Theme Generation** — Using Claude to generate themes from descriptions
   - How it works, example descriptions
   - Screenshot: Generate dialog
5. **Import & Export** — Sharing themes as JSON
   - Theme JSON format reference
6. **CSS Variable Reference** — Complete table of all variables by category
   - What each variable controls
7. **Bundled Themes** — What ships with Coral
8. **API Reference** — Endpoints for programmatic theme management

### Screenshots to Include

- Theme picker/selector
- Visual theme editor with color pickers
- AI theme generation dialog
- Before/after with different themes applied
- Import dialog

### Code Examples

- Theme JSON format
- AI generation API call
- Theme variable reference table

---

## Important Details for Technical Writer

1. **Storage location**: Themes are stored as JSON files in `~/.coral/themes/`, not in the database.
2. **Bundled themes**: Copied from `bundled_themes/` to `~/.coral/themes/` on first startup. Currently ships with `GhostV3` as the default.
3. **Name sanitization**: Theme names are sanitized to alphanumeric + hyphens/underscores/spaces. Path traversal is prevented.
4. **AI generation requires Claude CLI**: The generate feature uses `claude --print --model haiku`. If Claude CLI is not installed, the feature returns an error.
5. **Live preview**: Theme changes apply immediately via CSS custom properties on `:root`, no page reload needed.
6. **xterm terminal colors**: The theme includes a full 16-color ANSI palette for the terminal emulator, plus background, foreground, cursor, and selection colors.
7. **diff2html colors**: Theme variables also control the side-by-side diff viewer colors.
8. **Base scheme**: Each theme declares a "base" of "dark" or "light" which may influence fallback behavior.
