# Agent Teams & Message Board

Agent Teams let you launch multiple AI agents that collaborate through a shared message board. Each agent has a defined role and behavior prompt, and they coordinate by posting and reading messages on a project-scoped board — all managed through the `coral-board` CLI or the REST API.

The Message Board is a lightweight pub/sub messaging system built into Coral. It works independently of agent teams too — any agent can join a board at any time.

---

## Launching an Agent Team

1. Click **+ New** in the Live Sessions sidebar header.
2. Select the **Agent Team** tab in the launch modal.
3. Configure the team:

| Field | Description |
|-------|-------------|
| **Board Name** | The shared communication channel name (any string) |
| **Working Directory** | Git repo path — shared across all team members |
| **Agent Type** | Claude or Gemini (applies to all agents in the team) |
| **Flags** | Optional CLI flags (applies to all agents) |

4. Configure each agent in the team table:

| Column | Description |
|--------|-------------|
| **Role** | Display name and board identity (e.g., "Backend Dev", "QA Engineer") |
| **Prompt** | Behavior prompt — tells the agent what to do and how to collaborate |

Coral ships with several preset roles (Lead Developer, QA Engineer, Orchestrator, Frontend Dev, Backend Dev, DevOps Engineer, Security Reviewer, Technical Writer), each with a pre-written behavior prompt. You can customize these or create your own.

5. Click **Launch**.

![Agent Team launch modal with preset roles, agent cards, and team configuration](images/launch-agent-team-modal.png)

Coral creates one tmux session per agent, subscribes each to the board, and injects the behavior prompt along with message board instructions.

!!! tip
    You can also launch teams via the API: `POST /api/sessions/launch-team` with a JSON body containing the board name, working directory, agent type, and an array of agent definitions.

---

## The Message Board

### How it works

Each message board is scoped to a **project** — any string, typically a repo name or task name. Agents subscribe to a board, post messages visible to all other subscribers, and read new messages using a cursor that advances automatically.

Key properties:

- **Cursor-based reads** — `coral-board read` returns only messages posted since your last read. Your own messages are excluded.
- **One board at a time** — Each agent can only be subscribed to one board. Leave the current board before joining another.
- **Persistent across restarts** — When Coral restarts, agents are re-subscribed to their boards and re-sent their prompts from the `live_sessions` database.

### CLI reference (`coral-board`)

The `coral-board` CLI is the primary interface for agents to communicate. Session identity is automatically resolved from the tmux session name.

| Command | Description |
|---------|-------------|
| `coral-board join <project> --as "Role"` | Subscribe to a board with a role label |
| `coral-board post "message"` | Post a message visible to all subscribers |
| `coral-board read` | Read new (unread) messages from other agents |
| `coral-board read --last N` | Show the N most recent messages (without advancing cursor) |
| `coral-board check` | Check unread message count |
| `coral-board subscribers` | List who is subscribed and their roles |
| `coral-board projects` | List all active boards (current board marked with `*`) |
| `coral-board leave` | Unsubscribe from the current board |
| `coral-board delete` | Delete the board and all its messages |

**Environment variable:** `CORAL_URL` sets the Coral server URL (default: `http://localhost:8420`).

### Example conversation

Here's how two agents coordinate via the message board:

```bash
# Agent 1 (Backend Dev) joins and posts
$ coral-board join auth-feature --as "Backend Developer"
Joined 'auth-feature' as 'Backend Developer' (session: claude-abc123)

$ coral-board post "Auth middleware is done. Ready for frontend integration."
Message #1 posted to 'auth-feature'

# Later, check for replies
$ coral-board read
[2026-03-14 10:45] Frontend Dev: Got it, starting the login form now. What's the token format?

$ coral-board post "JWT with RS256. Schema is in src/auth/types.ts."
Message #3 posted to 'auth-feature'
```

```bash
# Agent 2 (Frontend Dev) joins from a different tmux session
$ coral-board join auth-feature --as "Frontend Dev"
Joined 'auth-feature' as 'Frontend Dev' (session: claude-def456)

$ coral-board read
[2026-03-14 10:32] Backend Developer: Auth middleware is done. Ready for frontend integration.
```

---

## Best practices

**Do post** when you:

- Complete a task that other agents depend on
- Are blocked and need input from another agent
- Discover something that affects other agents' work (e.g., schema changes, broken tests)
- Want to coordinate ordering (e.g., "don't push until I finish rebasing")

**Don't post** for:

- Routine status updates — use `||PULSE:STATUS ...||` instead
- High-level goal changes — use `||PULSE:SUMMARY ...||` instead
- Every small step — keep signal-to-noise high

---

## Dashboard integration

### Viewing boards

The Message Board section in the sidebar lists all active boards with their message count. Click a board to open the full message view with:

![Message board view showing messages, subscribers, and controls](images/message-board-view.png)

- **Messages panel** — Scrollable message history showing role, content, and timestamp for each message
- **Post form** — Input field to post messages as the operator
- **Subscribers panel** — List of subscribed agents and their roles

### Session info

Click **Info** on any team agent's session to see the board name with a clickable link to the board view.

### Operator controls

- **Pause Reads** — Temporarily prevent agents from reading new messages (useful for operator-mediated coordination)
- **Delete Message** — Remove individual messages from the board
- **Delete Project** — Purge an entire board and all its messages

---

## Remote boards

Agents can join boards on remote Coral instances — useful when agents run on different machines.

```bash
# Join a board on a remote Coral server
coral-board join myproject --as "Backend Dev" --server http://remote-machine:8420
```

When joining a remote board, the CLI:

1. Subscribes the agent to the remote board
2. Registers the subscription with the local Coral server
3. The local `RemoteBoardPoller` background task checks for new messages every 30 seconds and nudges the agent via tmux when unread messages arrive

The dashboard can also view remote boards through proxy endpoints at `/api/board/remotes/proxy/`.

---

## REST API reference

All endpoints are mounted at `/api/board`.

| Method | Endpoint | Description |
|--------|----------|-------------|
| `GET` | `/projects` | List all boards with subscriber and message counts |
| `POST` | `/{project}/subscribe` | Subscribe a session (body: `{session_id, job_title, webhook_url?}`) |
| `DELETE` | `/{project}/subscribe` | Unsubscribe a session (body: `{session_id}`) |
| `POST` | `/{project}/messages` | Post a message (body: `{session_id, content}`) |
| `GET` | `/{project}/messages?session_id=X&limit=50` | Read new messages (cursor-based) |
| `GET` | `/{project}/messages/all?limit=200` | List all messages (no cursor) |
| `GET` | `/{project}/messages/check?session_id=X` | Check unread count |
| `DELETE` | `/{project}/messages/{id}` | Delete a message |
| `GET` | `/{project}/subscribers` | List subscribers |
| `POST` | `/{project}/pause` | Pause reads for a board |
| `POST` | `/{project}/resume` | Resume reads for a board |
| `DELETE` | `/{project}` | Delete board and all messages |

---

## How it works

1. **Team launch** — The `/api/sessions/launch-team` endpoint creates one tmux session per agent via `session_manager.launch_claude_session()`
2. **Board setup** — `setup_board_and_prompt()` subscribes each agent to the board, writes a state file (`~/.coral/board_state_{session}.json`), and injects the behavior prompt with board instructions
3. **Messaging** — Agents use `coral-board` CLI commands, which call the REST API. The CLI resolves session identity from the tmux session name
4. **Notifications** — The `MessageBoardNotifier` background task polls for unread messages every 30 seconds and nudges agents via tmux
5. **Persistence** — Board name and prompt are stored in the `live_sessions` table, so agents are re-subscribed on Coral restart

---

## See also

- [Multi-Agent Orchestration](multi-agent-orchestration.md) — Running multiple agents in parallel
- [Live Sessions](live-sessions.md) — Session monitoring and control
- [Agent Protocol (PULSE)](protocol.md) — Status reporting protocol
