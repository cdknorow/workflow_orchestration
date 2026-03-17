# Doc Feature Guide: Git Integration & PR Linking

## Overview

Coral tracks git state for every live agent session — current branch, commits, remote URLs, and per-file change statistics. A background poller snapshots git state every 120 seconds, and the dashboard surfaces branch info, commit history, and changed files throughout the UI. This ties agent activity back to concrete code changes.

---

## Key Source Files & Architecture

| File | Role |
|------|------|
| `src/coral/store/git.py` | SQLite CRUD for `git_snapshots` and `git_changed_files` tables |
| `src/coral/background_tasks/git_poller.py` | Background service — polls git state every 120s for all live sessions |
| `src/coral/api/live_sessions.py` | API endpoints that expose git data (branch, commits) as part of session info |
| `src/coral/static/changed_files.js` | Dashboard JS for displaying per-file change statistics |

### Database Tables

| Table | Purpose |
|-------|---------|
| `git_snapshots` | Periodic snapshots — agent_name, working_directory, branch, commit_hash, commit_subject, commit_timestamp, session_id, remote_url. Unique on (session_id, commit_hash) |
| `git_changed_files` | Working tree diff stats — filepath, additions, deletions, status (M/A/D), per session |

### Architecture Flow

1. **Git Poller** runs every ~120 seconds for all live sessions
2. For each session, it runs git commands in the working directory:
   - `git branch --show-current` → current branch
   - `git log -1 --format=...` → latest commit hash, subject, timestamp
   - `git remote get-url origin` → remote URL
   - `git diff --numstat` → per-file change stats (additions, deletions)
3. Results are upserted into `git_snapshots` (deduplicated by session_id + commit_hash)
4. Changed files are stored in `git_changed_files`
5. Dashboard displays branch in sidebar tags, commit info in session header and Info modal
6. Historical sessions show git commits during the session's time range in the Commits tab

---

## User-Facing Functionality & Workflows

### Branch Display

- **Sidebar**: Branch tag next to each session name (e.g., `main`, `feature/auth`)
- **Session header**: Current branch with copy-to-clipboard button
- **Info modal**: Branch and latest commit hash + message

### Commit Tracking

- **Live sessions**: Latest commit shown in session header and Info modal
- **Historical sessions**: Commits tab shows all git commits during the session's time range
- Each commit displays: hash, subject, author, timestamp

### Changed Files

- Per-file diff statistics (additions/deletions) tracked per session
- `changed_files.js` renders these in the dashboard UI

### PR Linking

- Remote URL tracking enables linking to GitHub/GitLab PRs
- When a remote URL is available, the dashboard can construct PR links

### Git Worktree Integration

- Each agent runs in an isolated git worktree
- Branch isolation prevents conflicts between parallel agents
- Worktree commands: `git worktree add`, `git worktree list`, `git worktree remove`

---

## Suggested MkDocs Page Structure

### Title: "Git Integration & PR Linking"

1. **Introduction** — What git integration provides and why it matters for multi-agent workflows
2. **Branch Tracking** — Where branch info appears in the UI
   - Sidebar tags, session header, Info modal
   - Screenshot: Session header showing branch
3. **Commit Tracking** — How commits are captured and displayed
   - Git poller background task
   - Historical session Commits tab
   - Screenshot: Commits tab
4. **Changed Files** — Per-file diff statistics
   - Additions/deletions tracking
   - Dashboard display
5. **PR Linking** — Remote URL tracking and PR links
6. **Git Worktree Integration** — Why worktrees matter for multi-agent
   - Isolation, branch independence, safe merging
   - Worktree commands reference
7. **How It Works** — Git poller architecture
   - Polling interval, git commands used, deduplication
8. **Database Schema** — Tables and relationships

### Screenshots to Include

- Sidebar showing branch tags per session
- Session header with branch and copy button
- Info modal with branch and commit details
- Commits tab in historical session view
- Changed files display

### Code Examples

- Git worktree creation commands
- API response showing git data

---

## Important Details for Technical Writer

1. **Polling interval**: Every 120 seconds (2 minutes). Not configurable via settings currently.
2. **Deduplication**: `git_snapshots` uses a UNIQUE constraint on `(session_id, commit_hash)` so the same commit isn't recorded twice for a session.
3. **Remote URL**: Extracted from `git remote get-url origin`. May be HTTPS or SSH format.
4. **Changed files**: Uses `git diff --numstat` to get per-file addition/deletion counts. The `status` field is M (modified), A (added), or D (deleted).
5. **Session-linked commits**: Commits are tied to sessions by session_id. The historical Commits tab queries snapshots within the session's time range.
6. **Worktree path for scheduled jobs**: Scheduled job worktrees are created at `{repo}/.coral-jobs/{job_id}/{run_id}`.
7. **Git commands run in working dir**: All git commands execute in the session's `working_dir`, which should be a valid git repository.
