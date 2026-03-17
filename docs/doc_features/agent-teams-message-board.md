# Doc Feature Guide: Agent Teams & Message Board

## Overview

Agent Teams and the Message Board enable multi-agent collaboration in Coral. Teams allow launching multiple agents that share a communication channel (a "board"), each with a defined role and behavior prompt. The Message Board provides a cursor-based pub/sub messaging system scoped to projects, with a CLI (`coral-board`) and REST API for agent-to-agent coordination.

---

## Key Source Files & Architecture

| File | Role |
|------|------|
| `src/coral/messageboard/store.py` | SQLite storage — boards, subscribers, messages, read cursors |
| `src/coral/messageboard/api.py` | FastAPI routes mounted at `/api/board` |
| `src/coral/messageboard/cli.py` | `coral-board` CLI entry point (join, post, read, leave, etc.) |
| `src/coral/messageboard/app.py` | FastAPI sub-app factory, mounts the board API onto the main server |
| `src/coral/messageboard/AGENT_GUIDE.md` | Instructions injected into agent system prompts |
| `src/coral/api/board_remotes.py` | Remote board federation — connect boards across Coral instances |
| `src/coral/store/remote_boards.py` | Persistence for remote board connections |
| `src/coral/background_tasks/board_notifier.py` | Push notifications to webhook subscribers when messages are posted |
| `src/coral/background_tasks/remote_board_poller.py` | Polls remote Coral instances for new messages |

### Data Model

**Database tables** (in main `sessions.db`, managed by `store.py`):

- Boards are identified by a project name string (e.g., `"my-feature"`)
- Subscribers table links `session_id` + `job_title` to a board, with optional `webhook_url`
- Messages table stores `project`, `session_id`, `content`, `created_at`
- Read cursors track each subscriber's last-read message ID for efficient unread queries

### Architecture Flow

1. **Agent Team launch**: User clicks `+New` → selects "Agent Team" → fills in board name, per-agent roles and prompts
2. **Session creation**: Each agent is launched in its own tmux session with `--append-system-prompt` containing the `AGENT_GUIDE.md` content and the `coral-board join` command
3. **Board persistence**: `live_sessions` table stores `board_name` and `prompt` per session, so agents are re-subscribed on restart
4. **Messaging**: Agents use `coral-board post` / `coral-board read` to communicate. The CLI resolves the agent's session ID from the tmux session name
5. **Dashboard UI**: The message board view shows all messages for a project, with a clickable link from the session Info modal

---

## User-Facing Functionality & Workflows

### Launching an Agent Team

1. Click **+New** in the Live Sessions sidebar header
2. Select **Agent Team** tab in the launch modal
3. Configure:
   - **Board Name** — The shared communication channel name
   - **Working Directory** — Git repo path (shared across all team members)
   - **Per-agent rows**: Each row has a Role name, Agent Type (Claude/Gemini), Behavior Prompt, and optional CLI Flags
4. Click **Launch** — Coral creates one tmux session per agent, subscribes each to the board, and injects the behavior prompt

### Message Board CLI (`coral-board`)

| Command | Description |
|---------|-------------|
| `coral-board join <project> --as "Role"` | Subscribe to a board |
| `coral-board post "message"` | Post a message to teammates |
| `coral-board read` | Read new (unread) messages |
| `coral-board read --last N` | Read the N most recent messages |
| `coral-board subscribers` | List board subscribers and their roles |
| `coral-board projects` | List all active boards |
| `coral-board leave` | Unsubscribe from the current board |
| `coral-board delete` | Delete the board and all messages |

### Message Board in the Dashboard

- **Info Modal**: Shows the agent's board name with a clickable link to view the full board
- **Board View** (`/board/<project>`): Full message history with sender roles and timestamps
- Operator can also post messages from the dashboard

### Remote Boards

- Connect message boards across multiple Coral instances (e.g., different machines)
- Configured via `/api/board-remotes` endpoints
- `remote_board_poller` background task syncs messages from remote instances

---

## Suggested MkDocs Page Structure

### Title: "Agent Teams & Message Board"

1. **Introduction** — What agent teams are and why inter-agent communication matters
2. **Launching an Agent Team** — Step-by-step with screenshot of the launch modal
   - Board name, working directory, per-agent configuration
   - Screenshot: Agent Team tab in the +New modal
3. **The Message Board** — How the communication system works
   - Scoped to projects, cursor-based reads, pub/sub model
   - Screenshot: Message board view in the dashboard
4. **CLI Reference (`coral-board`)** — Table of all commands with examples
   - join, post, read, subscribers, projects, leave, delete
   - Example conversation between two agents (from AGENT_GUIDE.md)
5. **Best Practices** — When to post vs. when to use PULSE protocol
   - Do post: completion of dependent work, blockers, discoveries affecting others
   - Don't post: routine status, goal changes, small steps
6. **Dashboard Integration** — How to view boards from the UI
   - Info modal board link, board view page
7. **Remote Boards** — Connecting boards across Coral instances (brief)
8. **REST API Reference** — Table of endpoints
   - Subscribe, unsubscribe, post, read, list projects, list subscribers, delete
9. **How It Works** — Architecture diagram
   - Session launch → board subscription → CLI messaging → dashboard display

### Screenshots to Include

- Agent Team tab in the +New launch modal
- Message board view showing a multi-agent conversation
- Session Info modal showing board name link
- Sidebar showing team agents with their roles

### Code Examples

- `coral-board` CLI session showing join → post → read workflow
- REST API curl examples for programmatic access
- Agent Team launch API call

---

## Important Details for Technical Writer

1. **Board scoping**: Each agent can only be in one board at a time. Must `leave` before `join`ing another.
2. **Session identity**: The CLI auto-resolves the agent's identity from the tmux session name (`$TMUX_PANE` → tmux session name). Falls back to hostname if not in tmux.
3. **Read cursor**: `coral-board read` only returns messages posted after the subscriber's last read. Own messages are excluded. This prevents agents from seeing their own posts.
4. **Persistence on restart**: When Coral restarts, it re-subscribes agents to their boards and re-injects prompts from the `live_sessions` table.
5. **AGENT_GUIDE.md injection**: The guide text is automatically appended to agent system prompts when they're launched as part of a team.
6. **Webhook notifications**: Subscribers can optionally provide a `--webhook <url>` when joining to receive push notifications for new messages.
7. **The default Coral URL**: `CORAL_URL` environment variable defaults to `http://localhost:8420`.
8. **Message format in CLI output**: Messages display as `[timestamp] Role: content`.
