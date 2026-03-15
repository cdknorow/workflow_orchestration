# Roadmap: File Diff Viewer

This document tracks the current state and planned improvements for Coral's **Changed Files** panel and **Diff Viewer** feature.

---

## What exists today (v0.7.4)

### Changed Files Panel

- **Files tab** in the agentic state side panel shows all modified, added, deleted, renamed, and untracked files in the agent's working tree.
- Each file displays:
  - Status icon (color-coded: yellow for modified, green for added, red for deleted, blue for renamed)
  - Filename (bold) with parent directory path (muted)
  - Addition/deletion count badges (`+N` / `-N`)
- File count badge on the "Files" tab header.
- Data collected by the **GitPoller** every 30 seconds via `git diff --numstat`, `git diff --cached --numstat`, and `git status --porcelain`.
- File counts included in the WebSocket `coral_update` payload for real-time sidebar updates.

### Diff Viewer Window

- Clicking a file in the Files tab opens a standalone `/diff` page in a new browser window.
- Uses **diff2html** library for syntax-highlighted diff rendering.
- Supports **Unified** and **Split** (side-by-side) view modes.
- Dark theme matching the Coral dashboard.
- Auto-refreshes every 10 seconds to stay current as the agent works.
- Manual refresh button in the toolbar.
- Handles staged changes, unstaged changes, and untracked files (synthesized as "new file" diffs).

### Backend

- `git_changed_files` database table stores per-file change data per agent/session.
- `GET /api/sessions/live/{name}/files` — Returns list of changed files with stats.
- `GET /api/sessions/live/{name}/diff?filepath=...` — Returns unified diff text for a single file.
- GitPoller collects file changes alongside branch/commit data every 30 seconds.

---

## Short-term improvements

### Diff Viewer UX

- [ ] **Syntax highlighting in diff** — Use highlight.js or Prism to colorize code within diff lines (not just +/- coloring).
- [ ] **Keyboard navigation** — `j`/`k` to jump between hunks, `n`/`p` for next/previous file (when viewing all files).
- [ ] **Copy button** — Copy the diff to clipboard as a patch.
- [ ] **Line comments / annotations** — Click a line number to add a note visible in the dashboard.
- [ ] **Collapse/expand hunks** — Large diffs should be collapsible by hunk.
- [ ] **Word-level diff highlighting** — Highlight the specific characters that changed within a line, not just the whole line.

### Changed Files Panel

- [ ] **File tree view** — Group files by directory in a collapsible tree instead of a flat list.
- [ ] **Sort options** — Sort by name, status, additions, or most recently modified.
- [ ] **Filter by status** — Toggle visibility of modified/added/deleted/untracked files.
- [ ] **Inline mini-diff preview** — Show a 2-3 line preview of the diff on hover without opening the full viewer.
- [ ] **Total stats summary** — Show aggregate `+N / -N` totals at the top of the file list.
- [ ] **Binary file indicators** — Mark binary files (images, compiled assets) distinctly from text files.

### Polling & Performance

- [ ] **Incremental updates** — Only re-query changed files when the working tree actually changes (watch `git status` hash or use filesystem events).
- [ ] **Configurable poll interval** — Allow users to set the git poll interval from the Settings modal.
- [ ] **Debounce rapid changes** — When an agent is actively writing files, batch updates to avoid thrashing the database.

---

## Medium-term features

### Multi-file Diff View

- [ ] **All-files diff page** — View diffs for all changed files in a single scrollable page with a file navigation sidebar (like GitHub PR review).
- [ ] **File-to-file navigation** — Next/previous file buttons in the diff viewer toolbar.
- [ ] **Diff between commits** — Compare any two commits, not just working tree vs HEAD.

### Integration with Agent Activity

- [ ] **Highlight recently touched files** — In the Files tab, visually mark files the agent edited in the last N minutes (correlate with Edit/Write events).
- [ ] **Link activity events to diffs** — Click a "Write" or "Edit" event in the Activity tab to jump directly to the diff for that file.
- [ ] **Blame integration** — Show which lines were changed by the agent vs. pre-existing in the working tree.

### History View Support

- [ ] **Changed files in historical sessions** — Show the list of files changed during a completed session (derived from git snapshots captured during the session).
- [ ] **Session diff summary** — Total lines added/removed across the entire session, visible in session notes or the history sidebar.

---

## Long-term vision

### Collaborative Review

- [ ] **PR-style review interface** — Full pull request review experience within Coral: file list, diff viewer, inline comments, approval/request-changes workflow.
- [ ] **Cross-agent diff comparison** — Compare the changes made by two agents working on related tasks.
- [ ] **Conflict detection** — Warn when two agents are modifying the same file in different worktrees.

### Advanced Diff Capabilities

- [ ] **Semantic diff** — Language-aware diffing that understands function/class boundaries (e.g., "function `foo` was moved" instead of showing a delete + add).
- [ ] **Diff annotations from agent** — Let the agent annotate its own changes with reasoning (via PULSE protocol extension).
- [ ] **Patch application** — Apply a diff from one agent's worktree to another directly from the UI.

---

## Technical notes

### Key files

| Component | File |
|-----------|------|
| Git Poller (file collection) | `src/coral/background_tasks/git_poller.py` |
| Database schema | `src/coral/store/connection.py` (`git_changed_files` table) |
| Store methods | `src/coral/store/git.py` |
| Files API endpoint | `src/coral/api/live_sessions.py` (`/files`, `/diff`) |
| Diff viewer page | `src/coral/templates/diff.html` |
| Changed files JS module | `src/coral/static/changed_files.js` |
| Changed files CSS | `src/coral/static/style.css` (`.file-*` and `.changed-files-*` classes) |
| Files tab HTML | `src/coral/templates/includes/views/live_session.html` |

### Dependencies

- **diff2html** (CDN) — Diff rendering in the standalone diff viewer page.
- **git** — All file change data comes from git commands (`diff --numstat`, `status --porcelain`, `diff -- <file>`).
