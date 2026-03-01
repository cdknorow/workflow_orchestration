# CLAUDE.md - Project Guide

## Project Overview
**Corral** is a multi-agent orchestration system for managing AI coding agents (Claude and Gemini) running in parallel git worktrees using tmux. It features a web dashboard, real-time logging, complete session history with FTS5 search, and git state polling.

## Project Structure Highlights
- `src/corral/`: Main package directory
  - `launch_agents.sh`: Bash script to discover worktrees, launch tmux sessions, and start the web server.
  - `web_server.py`: FastAPI web dashboard (REST + WebSocket endpoints).
  - `session_manager.py`: Core logic for tmux discovery, pane targeting, history loading, session launch/kill.
  - `session_store.py`: SQLite storage (WAL mode) for notes, tags, session index, FTS, and summarizer queue.
  - `PROTOCOL.md`: Protocol for agents to follow (status/summary reporting).
- `DEVELOP.md`: Detailed developer guide containing full project structure, API endpoints, and database schema.
- `pyproject.toml`: Project configuration and dependencies.

## Key Commands

### Setup & Installation
```bash
# Install the package in editable mode
pip install -e .
```

### Launching the Corral
```bash
# Launch Claude agents and web dashboard for worktrees in the current directory
./src/corral/launch_agents.sh .

# Launch Gemini agents from a specific path
./src/corral/launch_agents.sh <path-to-root> gemini

# Override the web dashboard port (default: 8420)
CORRAL_PORT=9000 ./src/corral/launch_agents.sh .
```

### Running the Web Dashboard (standalone)
```bash
# Start the web dashboard (default: http://localhost:8420)
corral

# Custom host/port
corral --host 127.0.0.1 --port 9000
```

### Managing Agents
- **Attach to tmux (Claude):** `tmux attach -t claude-agent-1`
- **Attach to tmux (Gemini):** `tmux attach -t gemini-agent-1`
- **Attach to web server:** `tmux attach -t corral-web-server`
- **Switch window:** `Ctrl+b n` (next) / `Ctrl+b p` (previous)
- **Detach tmux:** `Ctrl+b d`

## Agent Protocol
All agent events use the `||PULSE:<EVENT_TYPE> <payload>||` format. The dashboard parses these from agent output in real time.

- `||PULSE:STATUS <Short Description>||`: Current task (emit before/after each subtask).
- `||PULSE:SUMMARY <Goal Description>||`: High-level goal (emit once at start or when goal changes).
- `||PULSE:CONFIDENCE <Low|High> <specific reason>||`: Flag uncertainty (`Low`) or non-obvious confidence (`High`) with a specific reason.

## Development Guidelines
- **Build System:** Setuptools with `pyproject.toml`.
- **Dependencies:** `fastapi`, `uvicorn`, `jinja2` (Python 3.8+).
- **Database:** SQLite (`~/.corral/sessions.db`) using WAL mode.
- **Logs:** Agents stream output to `/tmp/<agent_type>_corral_<folder_name>.log` via `tmux pipe-pane`.
