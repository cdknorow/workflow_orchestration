# Doc Feature Guide: Live Session Management

## Overview

Live Session Management is Coral's core real-time monitoring and control interface. It allows users to launch, observe, interact with, and manage AI coding agents from a single browser tab. Each session represents one agent running in a tmux session, with live terminal output, activity tracking, task management, notes, and conversation history.

---

## Key Source Files & Architecture

| File | Role |
|------|------|
| `src/coral/api/live_sessions.py` | FastAPI routes — launch, kill, restart, rename, list sessions, send commands |
| `src/coral/tools/session_manager.py` | Session lifecycle — launch agent, restart, resume, register/unregister sessions |
| `src/coral/tools/tmux_manager.py` | tmux operations — create sessions, send keys, pipe-pane, capture output, kill |
| `src/coral/tools/log_streamer.py` | Tails agent log files, strips ANSI codes, extracts PULSE tags, detects input-waiting state |
| `src/coral/tools/pulse_detector.py` | Incrementally scans logs for PULSE events, records them as agent_events |
| `src/coral/agents/base.py` | Base agent class — defines the interface for all agent types |
| `src/coral/agents/claude.py` | Claude agent — launch command, flags, history file discovery, resume support |
| `src/coral/agents/gemini.py` | Gemini agent — launch command, protocol injection via env var |
| `src/coral/web_server.py` | WebSocket endpoints (`/ws/coral`, `/ws/terminal/{name}`) for real-time updates |
| `src/coral/templates/index.html` | Main dashboard template |
| `src/coral/static/` | JavaScript, CSS for the dashboard UI |

### Database Tables

| Table | Purpose |
|-------|---------|
| `live_sessions` | Registered sessions — session_id, agent_type, agent_name, working_dir, display_name, resume_from_id, flags, prompt, board_name |
| `agent_events` | Activity events — tool use, PULSE events, stops |
| `agent_tasks` | Per-session task checklists |
| `agent_notes` | Per-session markdown notes |
| `agent_live_state` | Current session mapping for each agent |

### Architecture Flow

1. **Launch**: User clicks +New or uses API → `session_manager` creates tmux session with UUID name, sets up `pipe-pane` logging, launches agent command
2. **Monitoring**: WebSocket `/ws/coral` polls every 3s → queries tmux for all sessions → reads logs for status/summary → broadcasts to all browsers
3. **Terminal streaming**: WebSocket `/ws/terminal/{name}` captures tmux pane content every 0.5s
4. **Interaction**: User sends commands via input bar → API sends keystrokes to tmux pane
5. **Persistence**: Sessions are stored in `live_sessions` table and re-launched on Coral restart

---

## User-Facing Functionality & Workflows

### Launching Sessions

Three launch types from the +New modal:
1. **AI Agent** — Single agent with optional prompt and board subscription
2. **Agent Team** — Multiple agents on a shared message board (see Agent Teams guide)
3. **Terminal** — Plain shell session

Launch fields: Agent Name, Working Directory (with Browse), Agent Type, Flags, Prompt

### Session View

- **Header**: Status dot (green/yellow/gray), agent type badge, name, branch (with copy), Goal, Status, action buttons (Info/Attach/Restart/Kill)
- **Terminal**: Live output via xterm.js (Claude default) or semantic blocks (Gemini default)
- **Command Pane**: Resizable toolbar with mode toggles (Plan/Accept/Bash), macros (/compact, /clear, custom), navigation keys, text input

### Side Panel (4 tabs)

1. **Activity** — Real-time event timeline (Read, Write, Edit, Bash, Grep, Web, Tasks, Status, Confidence). Filterable. Activity chart at bottom.
2. **Tasks** — Drag-reorderable checklist. Syncs with Claude Code via hooks.
3. **Notes** — Markdown editor per session.
4. **History** — Live JSONL conversation transcript with tool-use cards.

### Session Management

- **Rename**: Right-click in sidebar → set display name
- **Restart**: Click Restart → optional new flags → agent restarts in same working dir
- **Kill**: Terminate the tmux session
- **Attach**: Open native terminal attached to tmux session
- **Resume**: Continue a historical session on a live agent (Claude only)
- **Info modal**: Full metadata — tmux name, attach command, working dir, log path, branch, commit, prompt, board link

### Sidebar

- Sessions grouped by working directory
- Status dots, agent type badges, branch tags, NEEDS INPUT badges
- Collapsible groups
- Kebab menu for session actions

---

## Suggested MkDocs Page Structure

### Title: "Live Sessions"

1. **Introduction** — What live sessions are, single-tab monitoring
2. **Getting Started** — Launch the dashboard, launch first session
3. **Launching Sessions** — The +New modal, three types, all fields
   - Screenshot: Launch modal
4. **Session View** — Header, terminal, command pane
   - Session header elements (status dot, badges, goal, status)
   - Terminal rendering modes (xterm.js vs semantic blocks)
   - Command pane toolbar and input
5. **Side Panel** — Activity, Tasks, Notes, History tabs
   - Screenshot: Each tab
6. **Session Management** — Rename, restart, kill, attach, resume
   - Input waiting banner
7. **Sidebar** — Layout, grouping, indicators
8. **Session Info Modal** — All metadata fields
9. **How It Works** — tmux integration, WebSocket architecture, log tailing
10. **Configuration** — Port, host, renderer, macros, log directory

### Screenshots to Include

- Full dashboard with session selected
- Launch modal (all three types)
- Session header with status, goal, branch
- Terminal area (xterm.js and semantic blocks)
- Command pane with toolbar
- Side panel tabs (Activity, Tasks, Notes, History)
- Sidebar with multiple sessions in different states
- Info modal

### Code Examples

- Launch session API call
- Send command API call
- tmux attach command

---

## Important Details for Technical Writer

1. **UUID naming**: tmux sessions are named `{agent_type}-{uuid}`, e.g., `claude-abc123-def456`
2. **Log files**: Output piped to `/tmp/{agent_type}_coral_{folder_name}.log` via `tmux pipe-pane`
3. **Input detection**: The log streamer detects "waiting for input" state by looking for specific patterns in agent output
4. **Text selection pauses updates**: When the user selects text in the terminal, auto-refresh pauses to prevent selection loss
5. **Per-session input preservation**: Typed text in the command input is saved per-session and restored when switching
6. **Persistent sessions**: Sessions survive Coral restarts. The `live_sessions` table tracks them. On startup, `resume_persistent_sessions()` re-launches all registered sessions.
7. **WebSocket intervals**: Session list polls at 3s, terminal content at 0.5s
8. **Renderers**: xterm.js for full terminal emulation (colors, formatting), semantic blocks for parsed output. Configurable per agent type in Settings.
