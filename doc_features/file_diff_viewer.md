# Instructions: Writing MkDocs Documentation for the File Diff Viewer

This file contains instructions for writing the MkDocs documentation page for the Changed Files and Diff Viewer feature. Follow these steps to create a doc that matches the existing Coral documentation style.

---

## Step 1: Understand the documentation structure

The docs live in `docs/docs/` (the inner `docs/` is the MkDocs content directory). The config file is `docs/mkdocs.yml`.

Key conventions observed in existing pages:

- **Opening paragraph**: 1-2 sentences explaining what the feature does and why it matters. Written in second person ("you") addressing the user directly.
- **Sections**: Use `---` horizontal rules between major sections.
- **Tables**: Use markdown tables for configuration, fields, and API reference.
- **Admonitions**: Use `!!! tip`, `!!! info`, `!!! warning` for callouts (Material theme feature).
- **Code blocks**: Use fenced code blocks with language identifiers.
- **Screenshots**: Reference with `<!-- TODO: Screenshot - description -->` placeholders.
- **Tone**: Practical, concise, developer-focused. No fluff.

## Step 2: Create the documentation file

Create `docs/docs/changed-files-diff-viewer.md`.

## Step 3: Suggested outline

```markdown
# Changed Files & Diff Viewer

Opening paragraph: Explain that Coral tracks git working tree changes for each
agent and provides a real-time Files panel and standalone diff viewer window.

---

## Changed Files panel

### How it works
- Explain the Files tab in the agentic state side panel
- What data is shown per file (status, filename, path, +/- badges)
- How data is collected (GitPoller, git commands, 30s interval)
- Real-time file count updates via WebSocket

### File statuses
Table of status icons and what they mean:
| Icon | Status | Description |
| ~ | Modified | File has unstaged or staged changes |
| + | Added | New file added to the index |
| - | Deleted | File removed |
| ? | Untracked | File not tracked by git |

---

## Diff Viewer

### Opening a diff
- Click any file in the Files tab
- Opens in a new browser window

### View modes
- Unified view (default)
- Split / side-by-side view
- Toggle with toolbar buttons

### Auto-refresh
- Diff auto-refreshes every 10 seconds
- Manual refresh button available
- Stays current as the agent edits files

### What diffs are shown
- Unstaged changes (working tree vs index)
- Staged changes (index vs HEAD)
- Untracked files shown as "new file" diffs

---

## API Reference

Table of endpoints:
| Method | Endpoint | Description |
| GET | /api/sessions/live/{name}/files | List changed files with stats |
| GET | /api/sessions/live/{name}/diff?filepath=... | Get unified diff for one file |

Include query parameters and example responses.

---

## How it works (technical)

- GitPoller runs `git diff --numstat`, `git diff --cached --numstat`,
  `git status --porcelain` every 30 seconds
- Results stored in `git_changed_files` SQLite table
- File counts included in WebSocket coral_update payload
- Diff viewer uses diff2html library (loaded from CDN)

---

## Configuration

| Setting | Default | Description |
| Git poll interval | 30s | How often file changes are checked |
```

## Step 4: Update mkdocs.yml

Add the new page to the `nav` section in `docs/mkdocs.yml`:

```yaml
nav:
  - Home: index.md
  - Multi-Agent Orchestration: multi-agent-orchestration.md
  - Live Sessions: live-sessions.md
  - Changed Files & Diff Viewer: changed-files-diff-viewer.md   # <-- add here
  - Button Macros: button-macros.md
  # ... rest of nav
```

Place it after "Live Sessions" since it's a sub-feature of live sessions.

## Step 5: Style guidelines to follow

1. **Match the webhooks.md pattern** — It's the best example of a complete feature doc. Study `docs/docs/webhooks.md` for structure.

2. **Opening paragraph formula**: "[Feature name] lets you [action]. [Why it matters in 1 sentence]."
   - Example: "The Changed Files panel shows you exactly which files each agent has modified, added, or deleted — with a click-to-open diff viewer for inspecting the actual changes."

3. **Use admonitions sparingly**:
   - `!!! tip` for productivity hints
   - `!!! info` for technical clarifications
   - `!!! warning` for gotchas

4. **API examples**: Show a `curl` command and a JSON response snippet for each endpoint.

5. **Screenshots**: Add `<!-- TODO: Screenshot - ... -->` placeholders. These can be filled in later with actual screenshots from the running dashboard.

6. **Keep it scannable**: Users skim docs. Use headers, tables, and short paragraphs. Lead with the "what" and "how to use it", put "how it works" at the end.

## Step 6: Build and preview

```bash
cd docs
pip install mkdocs-material  # if not installed
mkdocs serve
# Open http://localhost:8000 to preview
```

## Step 7: Deploy

```bash
cd docs
mkdocs gh-deploy
```

This publishes to the `gh-pages` branch at https://cdknorow.github.io/coral/.
