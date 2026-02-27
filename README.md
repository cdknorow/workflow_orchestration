<img width="1024" height="250" alt="image" src="https://github.com/user-attachments/assets/8b7b1467-ecba-4b63-9151-137f2d2243ec" />

---


Corral 🤠 ➰ 🤖 🤖, is an MIT licensed multi-agent orchestration application for managing AI coding agents across git worktrees running locally on your local or remote machine. The  Its application is built using tmux and FastAPI for easy extensibilty and modification.


<img width="1504" height="824" alt="image" src="https://github.com/user-attachments/assets/18fe9a2b-c00b-445e-8bb0-b6a3b2c55e60" />

## Features

- **Multi-agent support** — Launch and manage both Claude and Gemini agents side-by-side across worktrees
- **Web dashboard** — Real-time monitoring with pane capture, status tracking, and command input
- **Session history** — Browse past sessions from both Claude and Gemini
- **Full-text search** — Search across all session content 
- **Auto-summarization** — Summarization of sessions are stored for text search later
- **Session notes & activity** — Add markdown notes and see the activty that occured in each seassion live and historically
- **Remote control** — Send commands, navigate modes, and manage agents from the dashboard
- **Attach/Kill/Restart/Resume** — Open a terminal attached to any agent's tmux session, or kill it directly from the UI, or relaunch as a neew session
- **Git integration & PR Linking** Tracks, commits, and remote URL per agent & session

As Demoed by Claude 😂😂😂
<p align="center">
  <img src="corral-dashboard-tour.gif" alt="Corral Dashboard Tour" width="800" />
</p>



## Installation

Install from PyPI:

```bash
pip install agent-corral
```

Or install directly from GitHub:

```bash
pip install git+https://github.com/cdknorow/corral.git
```

## Usage


### Launch agents and web dashboard

The launcher discovers worktree subdirectories, creates a tmux session with an agent for each one, and starts the web dashboard in its own tmux session:

```bash
# Launch Claude agents and web dashboard for worktrees in the current directory
launch-corral

# Launch Gemini agents from a specific path
launch-corral <path-to-root> gemini

```

> **Note:** This system is currently mostly tested with Claude Code and to some extent Gemini CLI. However, the underlying architecture is designed to support other agents, which can be integrated with some additional work from others.


### Web dashboard (standalone)

You can launch the web server directly using `corral` or `corral-dashboard`:

```bash
# Start the web dashboard directly (default: http://localhost:8420)
corral-dashboard

# Custom host/port
corral-dashboard --host 127.0.0.1 --port 9000

```


### Managing sessions from the dashboard

<!-- TODO: Add a GIF here showing the live pane capture updating, sending commands to an agent, and toggling plan/base mode. -->
<img width="1510" height="813" alt="image" src="https://github.com/user-attachments/assets/5a2e7909-ef08-4371-b485-f6e141a5a02c" />

The web dashboard provides quick-action buttons for each live session:

| Action | Description |
|---|---|
| **Esc / Arrow / Enter** | Send navigation keys to the agent |
| **Plan Mode** | Toggle Claude Code plan mode |
| **Accept Edits** | Toggle Claude Code auto-accept mode |
| **Bash Mode** | Send `!` command to enter bash mode |
| **Base Mode** | Toggle base mode |
| **/compact / /clear** | Send compress or clear commands (adapts per agent type) |
| **Reset** | Compress then clear the session |
| **Attach** | Open a local terminal window attached to the agent's tmux session |
| **Restart** | Restart the agent in the same tmux pane |
| **Kill** | Terminate the tmux session and remove it from the dashboard |

You can also type arbitrary commands in the input bar and send them to the selected agent.

### Session history search and filtering

<!-- TODO: Add a GIF here showing full-text search across past Claude/Gemini sessions and adding notes/tags. -->
<img width="1512" height="799" alt="image" src="https://github.com/user-attachments/assets/37561737-caf9-438b-81af-8c48a6cfe30a" />


The sidebar History section includes a search bar and filters for browsing your entire AI coding session history along with activity, notes, and git commit tracking

On startup, the server launches three background services:

1. **Session indexer** (every 2 min) — Indexes all Claude sessions from `~/.claude/projects/**/*.jsonl` and Gemini sessions from `~/.gemini/tmp/*/chats/session-*.json`, builds a full-text search index (FTS5), and queues new sessions for auto-summarization
2. **Batch summarizer** — Continuously processes the summarization queue using Claude CLI
3. **Git poller** (every 2 min) — Polls git branch, commit, and remote URL for each live agent and stores snapshots in SQLite

Features:

- **Search** — Type in the search bar to find sessions by content (uses SQLite FTS5 with porter stemming)
- **Filter by tag** — Select a tag from the dropdown to narrow results
- **Filter by source** — Show only Claude or Gemini sessions
- **Pagination** — Browse through all sessions with prev/next controls
- **URL bookmarking** — Session URLs use hash routing (`#session/<id>`) so you can bookmark or share links
- **Notes & tags** — Add markdown notes and color-coded tags to any session, stored in `~/.corral/sessions.db`

### Claude Code Hooks (settings.json)

To fully integrate Claude Code's agentic state and task management into the Corral dashboard, configure the provided `corral-hook` scripts in your Claude Code `settings.json` (usually located at `~/.claude.json` or `~/.claude/settings.json`).

If you are already using other configuration options like a custom `statusLine` or other hooks, simply merge these hook definitions into your existing JSON:

```json
"hooks": {
    "PostToolUse": [
      {
        "matcher": "TaskCreate|TaskUpdate",
        "hooks": [
          {
            "type": "command",
            "command": "corral-hook-task-sync"
          }
        ]
      },
      {
        "matcher": "",
        "hooks": [
          {
            "type": "command",
            "command": "corral-hook-agentic-state"
          }
        ]
      }
    ],
    "Stop": [
      {
        "matcher": "",
        "hooks": [
          {
            "type": "command",
            "command": "corral-hook-agentic-state"
          }
        ]
      }
    ],
    "Notification": [
      {
        "matcher": "",
        "hooks": [
          {
            "type": "command",
            "command": "corral-hook-agentic-state"
          }
        ]
      }
    ]

```

### Remote server development (SSH port forwarding)

If you're running Corral on a remote server, forward the dashboard port over SSH to access it in your local browser:

```bash
# Forward remote port 8420 to localhost:8420
ssh -L 8420:localhost:8420 user@remote-host

# If using a custom port
ssh -L 9000:localhost:9000 user@remote-host
```

Then open `http://localhost:8420` (or your custom port) in your local browser. You can add this to your `~/.ssh/config` to make it persistent:

```
Host my-dev-server
    HostName remote-host
    User user
    LocalForward 8420 localhost:8420
```

### Manual tmux management

```bash
# Attach to a specific agent session
tmux attach -t claude-agent-1

# Switch between windows
Ctrl+b n  # next
Ctrl+b p  # previous

# Detach from tmux
Ctrl+b d
```

## Agent Protocol

Agents emit structured markers using the `||PULSE:<EVENT_TYPE> <payload>||` format. The dashboard parses these from agent output in real time:

```
||PULSE:STATUS <Short description of current task>||
||PULSE:SUMMARY <One-sentence high-level goal>||
||PULSE:CONFIDENCE <1-5> <short reason>||
```

The protocol is automatically injected via `PROTOCOL.md` when launching agents. See [`src/corral/PROTOCOL.md`](src/corral/PROTOCOL.md) for the full specification.


## Advanced Information

For information on project structure, API endpoints, and the database schema, please see [DEVELOP.md](DEVELOP.md).

## Dependencies

- Python 3.8+
- [FastAPI](https://fastapi.tiangolo.com/) + [Uvicorn](https://www.uvicorn.org/) — Web server
- [Jinja2](https://jinja.palletsprojects.com/) — HTML templating
- tmux — Session management
- Claude CLI (optional) — Powers auto-summarization

## Contributing

We welcome contributions! Whether it's adding support for new AI coding agents natively or improving the web dashboard, please feel free to open an issue or submit a pull request.

## License

This project is licensed under the MIT License.
