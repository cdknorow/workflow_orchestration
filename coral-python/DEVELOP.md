# Developer Documentation

Welcome to the development guide for Coral! This document covers the project structure, API endpoints, and database schema to help you understand how the system works and how to contribute.

## Project Structure

```
src/coral/
├── launch_agents.sh      # Shell script to discover worktrees, launch tmux sessions,
│                         #   and start the web server
├── launch.py             # Launcher entry point (launch-coral CLI)
├── web_server.py         # FastAPI server (REST + WebSocket endpoints)
├── tray.py               # macOS menu bar tray application
├── PROTOCOL.md           # Agent status/summary reporting protocol
├── agents/               # Agent implementations
│   ├── base.py           # Base agent class
│   ├── claude.py         # Claude agent
│   └── gemini.py         # Gemini agent
├── api/                  # REST API route modules
│   ├── live_sessions.py  # Live session endpoints (capture, send, kill, tasks, notes, events)
│   ├── history.py        # Historical session endpoints (messages, git, tags, notes)
│   ├── system.py         # System endpoints (settings, filesystem, tags, update check)
│   ├── board_remotes.py  # Remote message board proxy endpoints
│   ├── schedule.py       # Scheduled jobs CRUD and run history
│   ├── tasks.py          # Ad-hoc task run endpoints
│   ├── themes.py         # Theme CRUD, import, and generation
│   ├── uploads.py        # File upload endpoint
│   └── webhooks.py       # Webhook configuration and delivery history
├── store/                # SQLite storage layer
│   ├── connection.py     # Database connection and schema initialization
│   ├── sessions.py       # Session CRUD and FTS queries
│   ├── git.py            # Git snapshot storage
│   ├── tasks.py          # Agent tasks, notes, events, and live state storage
│   ├── schedule.py       # Scheduled job and run persistence
│   ├── webhooks.py       # Webhook config and delivery persistence
│   └── remote_boards.py  # Remote board server persistence
├── tools/                # Core utilities
│   ├── session_manager.py  # Tmux discovery, session launch/kill
│   ├── tmux_manager.py   # Tmux pane management and capture
│   ├── log_streamer.py   # Async log file tailing + snapshot for streaming
│   ├── pulse_detector.py # PULSE protocol event parsing
│   ├── jsonl_reader.py   # JSONL session file reader
│   ├── cron_parser.py    # Cron expression parsing for scheduled jobs
│   ├── run_callback.py   # Callback runner for scheduled/task completions
│   ├── update_checker.py # PyPI version check for update notifications
│   └── utils.py          # Shared utility functions
├── background_tasks/     # Background services
│   ├── session_indexer.py    # Background indexer + batch summarizer
│   ├── auto_summarizer.py    # AI-powered session summarization via Claude CLI
│   ├── git_poller.py         # Background git branch/commit polling for live agents
│   ├── idle_detector.py      # Detects idle agents for webhook notifications
│   ├── scheduler.py          # Cron job scheduler loop
│   ├── webhook_dispatcher.py # Delivers webhook HTTP notifications
│   ├── board_notifier.py     # Notifies agents of new message board messages
│   └── remote_board_poller.py # Polls remote board servers for new messages
├── messageboard/         # Inter-agent message board
│   ├── store.py          # SQLite storage for boards, messages, subscriptions
│   ├── api.py            # FastAPI routes for the board REST API
│   ├── app.py            # Board FastAPI app factory
│   ├── cli.py            # coral-board CLI entry point
│   └── AGENT_GUIDE.md    # Guide for agents using the message board
├── bundled_themes/       # Built-in theme JSON files
│   └── GhostV3.json      # Default bundled theme
├── templates/
│   ├── index.html        # Dashboard HTML
│   ├── diff.html         # File diff viewer
│   └── includes/         # Jinja2 partials
│       ├── modals.html   # Launch, info, and team modals
│       ├── sidebar.html  # Sidebar navigation
│       └── views/        # Main content views
│           ├── live_session.html     # Live session view
│           ├── history_session.html  # History session view
│           └── message_board.html    # Message board view
└── static/
    ├── css/              # Modular CSS files
    │   ├── variables.css       # CSS custom properties (theme variables)
    │   ├── base.css            # Base element styles
    │   ├── layout.css          # Page layout and grid
    │   ├── components.css      # Reusable UI components
    │   ├── session.css         # Session card and list styles
    │   ├── chat.css            # Chat and message styles
    │   ├── command-pane.css    # Command input pane
    │   ├── history.css         # History view styles
    │   ├── output.css          # Terminal output styles
    │   ├── agentic.css         # Agentic state display
    │   ├── scheduler.css       # Scheduler UI styles
    │   ├── theme-configurator.css # Theme editor styles
    │   └── animations.css      # Keyframe animations
    ├── style.css         # Legacy/additional styles
    ├── app.js            # Entry point
    ├── state.js          # Client state management
    ├── api.js            # REST API fetch functions
    ├── render.js         # DOM rendering (session lists, chat, pagination)
    ├── renderers.js      # Content renderers
    ├── sessions.js       # Session selection and management
    ├── controls.js       # Quick actions, mode toggling, session controls
    ├── capture.js        # Real-time pane text rendering
    ├── commits.js        # Git commit history display
    ├── changed_files.js  # Working tree changed files display
    ├── tags.js           # Tag CRUD and UI
    ├── notes.js          # Notes editing and markdown rendering
    ├── agent_notes.js    # Agent-authored notes display
    ├── tasks.js          # Task management UI
    ├── live_chat.js      # Live chat interface
    ├── live_jobs.js      # Live scheduled job monitoring
    ├── history_tabs.js   # History session tab navigation
    ├── agentic_state.js  # Agentic state display
    ├── modals.js         # Launch and info modal dialogs
    ├── browser.js        # Directory browser for launch dialog
    ├── sidebar.js        # Sidebar and command pane resizing
    ├── websocket.js      # Coral WebSocket subscription
    ├── syntax.js         # Syntax highlighting for code blocks
    ├── message_board.js  # Message board UI
    ├── scheduler.js      # Scheduled jobs UI
    ├── search_filters.js # History search and filter controls
    ├── theme_config.js   # Theme configurator UI
    ├── update_check.js   # Version update notification
    ├── webhooks.js       # Webhook configuration UI
    ├── file_mention.js   # File mention/linking in messages
    ├── xterm_renderer.js # Terminal emulation renderer
    └── utils.js          # Escape functions, toast notifications
```

## API Endpoints

The dashboard is powered by a FastAPI backend:

### Live Sessions

| Method | Path | Description |
|---|---|---|
| `GET` | `/` | Dashboard |
| `GET` | `/diff` | File diff viewer |
| `GET` | `/api/sessions/live` | List active coral agents with status and git branch |
| `GET` | `/api/sessions/live/{name}` | Detailed info for a live session (`?agent_type=`) |
| `GET` | `/api/sessions/live/{name}/capture` | Capture tmux pane content |
| `GET` | `/api/sessions/live/{name}/chat` | Get live chat messages |
| `GET` | `/api/sessions/live/{name}/info` | Enriched session metadata (git branch, commit info) |
| `GET` | `/api/sessions/live/{name}/files` | List changed files in working tree |
| `POST` | `/api/sessions/live/{name}/files/refresh` | Refresh changed files list |
| `GET` | `/api/sessions/live/{name}/diff` | Get file diff for a session |
| `GET` | `/api/sessions/live/{name}/search-files` | Search files in the working directory |
| `GET` | `/api/sessions/live/{name}/git` | Git commit snapshots for a live agent (`?limit=`) |
| `POST` | `/api/sessions/live/{name}/send` | Send a command to an agent |
| `POST` | `/api/sessions/live/{name}/keys` | Send raw tmux keys (Escape, BTab, etc.) |
| `POST` | `/api/sessions/live/{name}/resize` | Resize the tmux pane |
| `POST` | `/api/sessions/live/{name}/kill` | Kill a tmux session |
| `POST` | `/api/sessions/live/{name}/restart` | Restart the agent in the same pane |
| `POST` | `/api/sessions/live/{name}/resume` | Resume a persistent session |
| `POST` | `/api/sessions/live/{name}/attach` | Open a terminal attached to the session |
| `PUT` | `/api/sessions/live/{name}/display-name` | Set a display name for a live session |
| `POST` | `/api/sessions/launch` | Launch a new agent session |
| `POST` | `/api/sessions/launch-team` | Launch an agent team on a shared board |
| `GET` | `/api/sessions/live/{name}/tasks` | Get tasks for a live session |
| `POST` | `/api/sessions/live/{name}/tasks` | Create a task for a live session |
| `PATCH` | `/api/sessions/live/{name}/tasks/{task_id}` | Update a task |
| `DELETE` | `/api/sessions/live/{name}/tasks/{task_id}` | Delete a task |
| `POST` | `/api/sessions/live/{name}/tasks/reorder` | Reorder tasks |
| `GET` | `/api/sessions/live/{name}/notes` | Get agent notes for a live session |
| `POST` | `/api/sessions/live/{name}/notes` | Create an agent note |
| `PATCH` | `/api/sessions/live/{name}/notes/{note_id}` | Update an agent note |
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
| `GET` | `/api/system/update-check` | Check for new Coral versions on PyPI |
| `GET` | `/api/settings` | Get user settings |
| `PUT` | `/api/settings` | Update user settings |
| `GET` | `/api/tags` | List all tags |
| `POST` | `/api/tags` | Create a new tag |
| `DELETE` | `/api/tags/{tag_id}` | Delete a tag |
| `POST` | `/api/indexer/refresh` | Trigger immediate re-index |
| `GET` | `/api/filesystem/list` | List directories for the launch browser |
| `POST` | `/api/upload` | Upload a file |

### Scheduled Jobs

| Method | Path | Description |
|---|---|---|
| `GET` | `/api/scheduled/jobs` | List all scheduled jobs |
| `GET` | `/api/scheduled/jobs/{job_id}` | Get a specific job |
| `POST` | `/api/scheduled/jobs` | Create a new scheduled job |
| `PUT` | `/api/scheduled/jobs/{job_id}` | Update a scheduled job |
| `DELETE` | `/api/scheduled/jobs/{job_id}` | Delete a scheduled job |
| `POST` | `/api/scheduled/jobs/{job_id}/toggle` | Enable/disable a job |
| `GET` | `/api/scheduled/jobs/{job_id}/runs` | Get run history for a job |
| `GET` | `/api/scheduled/runs/recent` | Get recent runs across all jobs |
| `POST` | `/api/scheduled/validate-cron` | Validate a cron expression |

### Tasks (Ad-hoc Runs)

| Method | Path | Description |
|---|---|---|
| `POST` | `/api/tasks/run` | Launch an ad-hoc task run |
| `GET` | `/api/tasks/runs/{run_id}` | Get status of a task run |
| `POST` | `/api/tasks/runs/{run_id}/kill` | Kill a running task |
| `GET` | `/api/tasks/runs` | List all task runs |
| `GET` | `/api/tasks/active` | List active task runs |

### Themes

| Method | Path | Description |
|---|---|---|
| `GET` | `/api/themes/variables` | Get available CSS theme variables |
| `GET` | `/api/themes` | List all themes (custom + bundled) |
| `GET` | `/api/themes/{name}` | Get a specific theme |
| `PUT` | `/api/themes/{name}` | Save/update a theme |
| `DELETE` | `/api/themes/{name}` | Delete a custom theme |
| `POST` | `/api/themes/import` | Import a theme from JSON |
| `POST` | `/api/themes/generate` | Generate a theme using AI |

### Webhooks

| Method | Path | Description |
|---|---|---|
| `GET` | `/api/webhooks` | List webhook configurations |
| `POST` | `/api/webhooks` | Create a webhook |
| `PATCH` | `/api/webhooks/{webhook_id}` | Update a webhook |
| `DELETE` | `/api/webhooks/{webhook_id}` | Delete a webhook |
| `POST` | `/api/webhooks/{webhook_id}/test` | Send a test delivery |
| `GET` | `/api/webhooks/{webhook_id}/deliveries` | Get delivery history |

### Message Board

Mounted at `/api/board`:

| Method | Path | Description |
|---|---|---|
| `GET` | `/api/board/projects` | List all boards |
| `GET` | `/api/board/{project}/subscribers` | List board subscribers |
| `POST` | `/api/board/{project}/subscribe` | Subscribe a session to a board |
| `DELETE` | `/api/board/{project}/subscribe` | Unsubscribe from a board |
| `POST` | `/api/board/{project}/messages` | Post a message |
| `GET` | `/api/board/{project}/messages` | Read new messages (cursor-based) |
| `GET` | `/api/board/{project}/messages/check` | Check for new messages |
| `GET` | `/api/board/{project}/messages/all` | List all messages (`?limit=`) |
| `DELETE` | `/api/board/{project}/messages/{message_id}` | Delete a message |
| `POST` | `/api/board/{project}/pause` | Pause message reads for a session |
| `POST` | `/api/board/{project}/resume` | Resume message reads |
| `GET` | `/api/board/{project}/paused` | Check pause status |
| `DELETE` | `/api/board/{project}` | Delete a board |

### Remote Board Proxy

| Method | Path | Description |
|---|---|---|
| `GET` | `/api/board/remotes` | List configured remote board servers |
| `POST` | `/api/board/remotes` | Add a remote board server |
| `DELETE` | `/api/board/remotes` | Remove a remote board server |
| `GET` | `/api/board/remotes/proxy/{server}/projects` | Proxy: list remote boards |
| `GET` | `/api/board/remotes/proxy/{server}/{project}/messages/all` | Proxy: list remote messages |
| `GET` | `/api/board/remotes/proxy/{server}/{project}/subscribers` | Proxy: list remote subscribers |
| `GET` | `/api/board/remotes/proxy/{server}/{project}/messages/check` | Proxy: check for new messages |

### WebSocket

| Type | Path | Description |
|---|---|---|
| `WS` | `/ws/coral` | Real-time coral status updates (polls every 3s) |
| `WS` | `/ws/terminal/{name}` | Live terminal stream for a session |

## Testing the Dashboard

### Setup

Install the package in editable mode and start the web server:

```bash
pip install -e .
coral
```

The dashboard runs at `http://localhost:8420/` by default.

### Reinstalling After Code Changes

The web server serves static files and templates from the installed package in site-packages, not from the source tree. After making changes, you need to reinstall:

```bash
# Option A: Send commands to the tmux session non-interactively
tmux send-keys -t coral-web-server C-c
sleep 1
tmux send-keys -t coral-web-server 'cd <current-worktree> && python -m pip install . && cd ../ && coral' Enter

# Option B: Attach to tmux and run manually
tmux attach -t coral-web-server
# Ctrl+C to stop, then:
cd <current-worktree> && python -m pip install . && cd ../ && coral
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

- **Static files returning 404**: New file types in `src/coral/static/` must have their glob pattern added to `pyproject.toml` under `[tool.setuptools.package-data]` (e.g. `static/*.png`, `static/*.ico`).
- **Changes not taking effect**: Source edits alone won't appear until you reinstall with `pip install .` since files are copied to site-packages.
- **Favicon not updating**: Browsers cache favicons aggressively. Hard refresh or open the favicon URL directly to verify.
- **Finding the installed package**: `python -c "import coral; import os; print(os.path.dirname(coral.__file__))"`

## Database

All persistent state is stored in a SQLite database at `~/.coral/sessions.db` (using WAL mode for concurrent access):

| Table | Purpose |
|---|---|
| `session_index` | Session metadata, source type, file paths, timestamps, message counts |
| `session_fts` | FTS5 virtual table for full-text search (porter stemming, unicode61) |
| `session_meta` | Notes, auto-summaries, display names, edit timestamps |
| `tags` | Tag definitions with colors |
| `session_tags` | Many-to-many tag-to-session assignments |
| `summarizer_queue` | Pending and completed auto-summarization jobs |
| `git_snapshots` | Git branch, commit hash, subject, timestamp, and remote URL per agent |
| `git_changed_files` | Per-file working tree diff stats (additions, deletions, status) |
| `agent_tasks` | Task items assigned to agents (title, status, position, session) |
| `agent_notes` | Agent-authored notes (content, timestamps, session) |
| `agent_events` | Agent events (type, tool name, summary, detail JSON) |
| `live_sessions` | Persistent live session state (agent type, working dir, prompt, board) |
| `user_settings` | User preferences (key-value store) |
| `agent_live_state` | Real-time agent state (agentic state tracking) |
| `scheduled_jobs` | Cron job definitions (name, schedule, repo, prompt, flags) |
| `scheduled_runs` | Execution history for scheduled jobs (status, timing, errors) |
| `webhook_configs` | Webhook endpoint configurations (URL, platform, filters) |
| `webhook_deliveries` | Webhook delivery attempts and status tracking |
