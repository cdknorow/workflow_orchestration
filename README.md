# Claude Agent Worktree Orchestration
<img width="1483" height="855" alt="image" src="https://github.com/user-attachments/assets/c6bb192f-12f4-4acc-8f39-d356149c0b2a" />

This is a workflow designed for people using worktrees and multiple agents. 

## Installation

You can install this tool as a Python package:

```bash
# Clone the repository
git clone <repo-url>
cd workflow_orchestrator

# Install in editable mode
pip install -e .
```

## Usage

Run the launcher and it will create an agent for each worktree inside a tmux session. The status of each of your models will be displayed inside the Dashboard.

```bash
# Launch agents and dashboard from current directory
agent-fleet

# Launch agents and dashboard from a specific path
agent-fleet <path-to-root> --model gemini

# Or use the script directly
./src/agent_fleet/launch_agents.sh <path-to-root> claude

# Or launch just the dashboard if agents are already running
agent-fleet --no-launch
```

