# Developer Documentation

Welcome to the development guide for Corral! This document covers the project structure, API endpoints, and database schema to help you understand how the system works and how to contribute.

## Project Structure

```
src/corral/
├── launch_agents.sh      # Shell script to discover worktrees, launch tmux sessions,
│                         #   and start the web server
├── launch.py             # Launcher entry point (launch-corral CLI)
├── web_server.py         # FastAPI server (REST + WebSocket endpoints)
├── PROTOCOL.md           # Agent status/summary reporting protocol
├── agents/               # Agent implementations
│   ├── base.py           # Base agent class
│   ├── claude.py         # Claude agent
│   └── gemini.py         # Gemini agent
├── api/                  # REST API route modules
│   ├── live_sessions.py  # Live session endpoints (capture, send, kill, tasks, notes, events)
│   ├── history.py        # Historical session endpoints (messages, git, tags, notes)
│   └── system.py         # System endpoints (settings, filesystem, tags)
├── store/                # SQLite storage layer
│   ├── connection.py     # Database connection and schema initialization
│   ├── sessions.py       # Session CRUD and FTS queries
│   ├── git.py            # Git snapshot storage
│   └── tasks.py          # Agent tasks, notes, events, and live state storage
├── tools/                # Core utilities
│   ├── session_manager.py  # Tmux discovery, session launch/kill
│   ├── tmux_manager.py   # Tmux pane management and capture
│   ├── log_streamer.py   # Async log file tailing + snapshot for streaming
│   ├── pulse_detector.py # PULSE protocol event parsing
│   ├── jsonl_reader.py   # JSONL session file reader
│   └── utils.py          # Shared utility functions
├── background_tasks/     # Background services
│   ├── session_indexer.py  # Background indexer + batch summarizer
│   ├── auto_summarizer.py  # AI-powered session summarization via Claude CLI
│   └── git_poller.py     # Background git branch/commit polling for live agents
├── hooks/                # Claude Code integration hooks
│   ├── task_state.py     # Task state sync hook (corral-hook-task-sync)
│   ├── agentic_state.py  # Agentic state hook (corral-hook-agentic-state)
│   └── utils.py          # Hook utility functions
├── templates/
│   ├── index.html        # Dashboard HTML
│   └── includes/         # Jinja2 partials (modals.html, sidebar.html, views/)
└── static/
    ├── style.css         # Dark theme CSS
    ├── app.js            # Entry point
    ├── state.js          # Client state management
    ├── api.js            # REST API fetch functions
    ├── render.js         # DOM rendering (session lists, chat, pagination)
    ├── renderers.js      # Content renderers
    ├── sessions.js       # Session selection and management
    ├── controls.js       # Quick actions, mode toggling, session controls
    ├── capture.js        # Real-time pane text rendering
    ├── commits.js        # Git commit history display
    ├── tags.js           # Tag CRUD and UI
    ├── notes.js          # Notes editing and markdown rendering
    ├── agent_notes.js    # Agent-authored notes display
    ├── tasks.js          # Task management UI
    ├── live_chat.js      # Live chat interface
    ├── history_tabs.js   # History session tab navigation
    ├── agentic_state.js  # Agentic state display
    ├── modals.js         # Launch and info modal dialogs
    ├── browser.js        # Directory browser for launch dialog
    ├── sidebar.js        # Sidebar and command pane resizing
    ├── websocket.js      # Corral WebSocket subscription
    ├── syntax.js         # Syntax highlighting for code blocks
    └── utils.js          # Escape functions, toast notifications
```

## API Endpoints

The dashboard is powered by a FastAPI backend:

### Live Sessions

| Method | Path | Description |
|---|---|---|
| `GET` | `/` | Dashboard |
| `GET` | `/api/sessions/live` | List active corral agents with status and git branch |
| `GET` | `/api/sessions/live/{name}` | Detailed info for a live session (`?agent_type=`) |
| `GET` | `/api/sessions/live/{name}/capture` | Capture tmux pane content |
| `GET` | `/api/sessions/live/{name}/chat` | Get live chat messages |
| `GET` | `/api/sessions/live/{name}/info` | Enriched session metadata (git branch, commit info) |
| `GET` | `/api/sessions/live/{name}/git` | Git commit snapshots for a live agent (`?limit=`) |
| `POST` | `/api/sessions/live/{name}/send` | Send a command to an agent |
| `POST` | `/api/sessions/live/{name}/keys` | Send raw tmux keys (Escape, BTab, etc.) |
| `POST` | `/api/sessions/live/{name}/kill` | Kill a tmux session |
| `POST` | `/api/sessions/live/{name}/restart` | Restart the agent in the same pane |
| `POST` | `/api/sessions/live/{name}/resume` | Resume a persistent session |
| `POST` | `/api/sessions/live/{name}/attach` | Open a terminal attached to the session |
| `PUT` | `/api/sessions/live/{name}/display-name` | Set a display name for a live session |
| `POST` | `/api/sessions/launch` | Launch a new agent session |
| `GET` | `/api/sessions/live/{name}/tasks` | Get tasks for a live session |
| `POST` | `/api/sessions/live/{name}/tasks` | Create a task for a live session |
| `DELETE` | `/api/sessions/live/{name}/tasks/{task_id}` | Delete a task |
| `POST` | `/api/sessions/live/{name}/tasks/reorder` | Reorder tasks |
| `GET` | `/api/sessions/live/{name}/notes` | Get agent notes for a live session |
| `POST` | `/api/sessions/live/{name}/notes` | Create an agent note |
| `DELETE` | `/api/sessions/live/{name}/notes/{note_id}` | Delete an agent note |
| `GET` | `/api/sessions/live/{name}/events` | Get events for a live session |
| `POST` | `/api/sessions/live/{name}/events` | Create an event |
| `GET` | `/api/sessions/live/{name}/events/counts` | Get event counts |
| `DELETE` | `/api/sessions/live/{name}/events` | Clear events |

### History Sessions

| Method | Path | Description |
|---|---|---|
| `GET` | `/api/sessions/history` | Paginated history (`?page=`, `?page_size=`, `?q=`, `?tag_id=`, `?source_type=`) |
| `GET` | `/api/sessions/history/{id}` | Get messages for a historical session |
| `GET` | `/api/sessions/history/{id}/git` | Git commits during a session's time range |
| `GET` | `/api/sessions/history/{id}/tasks` | Tasks for a historical session |
| `GET` | `/api/sessions/history/{id}/agent-notes` | Agent notes for a historical session |
| `GET` | `/api/sessions/history/{id}/events` | Events for a historical session |
| `GET` | `/api/sessions/history/{id}/notes` | Get notes and auto-summary |
| `PUT` | `/api/sessions/history/{id}/notes` | Save notes |
| `POST` | `/api/sessions/history/{id}/resummarize` | Force re-summarization |
| `GET` | `/api/sessions/history/{id}/tags` | Get tags for a session |
| `POST` | `/api/sessions/history/{id}/tags` | Add a tag to a session |
| `DELETE` | `/api/sessions/history/{id}/tags/{tag_id}` | Remove a tag from a session |

### System

| Method | Path | Description |
|---|---|---|
| `GET` | `/api/settings` | Get user settings |
| `PUT` | `/api/settings` | Update user settings |
| `GET` | `/api/tags` | List all tags |
| `POST` | `/api/tags` | Create a new tag |
| `DELETE` | `/api/tags/{tag_id}` | Delete a tag |
| `POST` | `/api/indexer/refresh` | Trigger immediate re-index |
| `GET` | `/api/filesystem/list` | List directories for the launch browser |

### WebSocket

| Type | Path | Description |
|---|---|---|
| `WS` | `/ws/corral` | Real-time corral status updates (polls every 3s) |

## Testing the Dashboard

### Setup

Install the package in editable mode and start the web server:

```bash
pip install -e .
corral
```

The dashboard runs at `http://localhost:8420/` by default.

### Reinstalling After Code Changes

The web server serves static files and templates from the installed package in site-packages, not from the source tree. After making changes, you need to reinstall:

```bash
# Option A: Send commands to the tmux session non-interactively
tmux send-keys -t corral-web-server C-c
sleep 1
tmux send-keys -t corral-web-server 'cd <current-worktree> && python -m pip install . && cd ../ && corral' Enter

# Option B: Attach to tmux and run manually
tmux attach -t corral-web-server
# Ctrl+C to stop, then:
cd <current-worktree> && python -m pip install . && cd ../ && corral
```

### Browser Testing with Claude in Chrome

You can use the Claude in Chrome MCP extension to visually inspect and interact with the dashboard:

1. Call `tabs_context_mcp` to get available browser tabs
2. Navigate to `http://localhost:8420/`
3. Use `screenshot` and `zoom` to inspect UI elements
4. Use `read_network_requests` to check for 404s or failed requests
5. Use `read_console_messages` with a pattern filter to check for JS errors
6. Hard refresh with `cmd+shift+r` to bypass cached assets

### Common Gotchas

- **Static files returning 404**: New file types in `src/corral/static/` must have their glob pattern added to `pyproject.toml` under `[tool.setuptools.package-data]` (e.g. `static/*.png`, `static/*.ico`).
- **Changes not taking effect**: Source edits alone won't appear until you reinstall with `pip install .` since files are copied to site-packages.
- **Favicon not updating**: Browsers cache favicons aggressively. Hard refresh or open the favicon URL directly to verify.
- **Finding the installed package**: `python -c "import corral; import os; print(os.path.dirname(corral.__file__))"`

## Database

All persistent state is stored in a SQLite database at `~/.corral/sessions.db` (using WAL mode for concurrent access):

| Table | Purpose |
|---|---|
| `session_index` | Session metadata, source type, file paths, timestamps, message counts |
| `session_fts` | FTS5 virtual table for full-text search (porter stemming, unicode61) |
| `session_meta` | Notes, auto-summaries, edit timestamps |
| `tags` | Tag definitions with colors |
| `session_tags` | Many-to-many tag-to-session assignments |
| `summarizer_queue` | Pending and completed auto-summarization jobs |
| `git_snapshots` | Git branch, commit hash, subject, timestamp, and remote URL per agent |
| `agent_tasks` | Task items assigned to agents (title, status, position) |
| `agent_notes` | Agent-authored notes (content, timestamps) |
| `agent_events` | Agent events (type, data, timestamps) |
| `live_sessions` | Persistent live session state |
| `user_settings` | User preferences (key-value store) |
| `agent_live_state` | Real-time agent state (agentic state tracking) |
