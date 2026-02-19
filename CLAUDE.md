# CLAUDE.md - Project Guide

## Project Overview
**Claude Fleet** is a multi-agent orchestration system for managing AI coding agents running in parallel git worktrees using tmux.

## Project Structure
- `src/agent_fleet/`: Main package directory
  - `launch_agents.sh`: Bash script to discover worktrees and launch tmux sessions.
  - `dashboard.py`: Textual-based Python TUI for monitoring agent status.
  - `PROTOCOL.md`: Protocol for agents to follow (status/summary reporting).
- `pyproject.toml`: Project configuration and dependencies.
- `.gitignore`: Ignoring `src/agent_fleet.egg-info/`.

## Key Commands

### Setup & Installation
```bash
# Install the package in editable mode
pip install -e .
```

### Launching the Fleet
```bash
# From the project root, launch agents in a target worktree directory
./src/agent_fleet/launch_agents.sh /path/to/worktrees
```

### Running the Dashboard
```bash
# Start the TUI dashboard
agent-fleet
# OR
python -m agent_fleet.dashboard
```

### Managing Agents
- **Attach to tmux:** `tmux attach -t claude-fleet`
- **Switch window:** `Ctrl+b n` (next) / `Ctrl+b p` (previous)
- **Detach tmux:** `Ctrl+b d`
- **Stop dashboard:** `Ctrl+C`

## Agent Protocol
Agents must emit status and summary lines for the dashboard to track:
- `||SUMMARY: <Goal Description>||`: High-level goal (emit once at start or when goal changes).
- `||STATUS: <Task Description>||`: Current task (emit before/after subtasks).

## Development Guidelines
- **Build System:** Setuptools with `pyproject.toml`.
- **Dependencies:** `textual` (Python 3.8+).
- **Logs:** Agents stream output to `/tmp/claude_fleet_[folder_name].log` via `tmux pipe-pane`.
- **Persistence:** Dashboard saves task lists to `fleet_tasks.json`.
