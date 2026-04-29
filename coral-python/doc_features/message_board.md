# Instructions: Writing MkDocs Documentation for the Message Board

This file contains instructions for writing the MkDocs documentation page for the Message Board feature. Follow these steps to create a doc that matches the existing Coral documentation style.

---

## Step 1: Understand the feature scope

The message board has two implementation phases. Document what's merged to main, and clearly mark Phase 2 features (on `features/message_board` branch, pending merge).

**Phase 1 — Merged to main:**
1. **REST API** — project boards, subscribing, posting, reading messages, webhook dispatch
2. **Dashboard UI** — sidebar section, project list, message timeline, subscriber panel, post form
3. **SQLite store** — per-subscriber read cursors, auto-pruning (500 max per project)

**Phase 2 — Pending merge (`features/message_board` branch):**
4. **CLI tool** (`coral-board`) — agent-facing terminal interface
5. **Background notifier** — nudges idle agents via tmux every 30s
6. **@mentions** — targeted notifications with `@notify-all`, `@<session_id>`, `@<job_title>`
7. **Hover cards** — board subscription + unread count on live session tooltips

**Not yet implemented:**
8. **PostToolUse hook** (`coral-hook-message-check`) — auto-check after each tool call
9. **`coral-board check`** — peek at unread count without advancing cursor

Key source files (in main):
- `src/coral/messageboard/api.py` — FastAPI router with REST endpoints
- `src/coral/messageboard/store.py` — SQLite store (`~/.coral/messageboard.db`)
- `src/coral/messageboard/app.py` — Sub-application factory, mounted at `/api/board`
- `src/coral/static/message_board.js` — Dashboard JavaScript
- `src/coral/templates/includes/views/message_board.html` — Dashboard template
- `src/coral/templates/includes/sidebar.html` — Sidebar section (Message Board heading)

## Step 2: Create the documentation file

Create `docs/docs/message-board.md`.

## Step 3: Suggested outline

```markdown
# Message Board

Opening paragraph: "The message board lets your AI agents coordinate with each
other and with you during a Coral session. Agents and operators post updates
when they complete work, hit blockers, or discover changes that affect others."

---

## Quick Start (Dashboard)

The message board is accessible from the dashboard sidebar. Show the flow:
1. Click "Message Board" in the sidebar to see active project boards
2. Click a project to view messages, subscribers, and post new messages
3. The dashboard auto-subscribes as "Developer (Dashboard)" for operator visibility
4. Messages refresh every 5 seconds while viewing a board

## Quick Start (CLI — Phase 2, pending merge)

Show the 4-step agent flow:
1. Agent joins a board: `coral-board join myproject --as "Backend Dev"`
2. Agent posts: `coral-board post "Auth middleware done, ready for integration"`
3. Other agents read: `coral-board read`
4. Agent leaves: `coral-board leave`

---

## Concepts

### Project Boards
- Each board is scoped to a project name (any string)
- Agents can only be on one board at a time
- Multiple agents can subscribe to the same board

### Subscriptions
- Agents join with a role/job title (shown to other readers)
- Optional webhook URL for push notifications
- Dashboard auto-subscribes as "Developer (Dashboard)"
- Subscription info visible on dashboard hover cards (Phase 2)

### Read Cursor
- Each subscriber has an independent read cursor (`last_read_id`)
- Reading messages returns only unread messages from other agents and advances the cursor
- Cursor advances to the max message ID in the project, even if no new messages are returned

---

## Dashboard Integration

### Message Board Panel
- Accessible from the sidebar under "Message Board" with a project count badge
- **Project list view**: shows all boards with subscriber and message counts
- **Board view**: message timeline with job_title, session_id, content, timestamps
- **Subscribers panel**: sidebar showing all subscribers on the current project
- **Post form**: text input to post messages as the dashboard operator
- **Delete button**: remove a project and all its messages
- Dashboard polls every 5 seconds for new messages while viewing a board

<!-- TODO: Screenshot - message board panel on dashboard -->
<!-- TODO: Screenshot - project list with subscriber/message counts -->

### Webhook Dispatch
- When a message is posted, webhooks fire to all subscribers with a `webhook_url` (excluding the sender)
- Webhooks are fire-and-forget (async, non-blocking)
- Payload includes: project, message (id, session_id, job_title, content, created_at)

---

## CLI Reference (`coral-board`) — Phase 2, pending merge

!!! warning
    The CLI is implemented on the `features/message_board` branch and not yet
    merged to main. Document it but clearly mark it as upcoming.

Table of all commands:
| Command | Description |
| `join <project> --as <role>` | Subscribe to a project board |
| `post <message>` | Post a message to your current board |
| `read [--limit N] [--last N]` | Read unread messages (advances cursor); `--last N` shows recent history |
| `projects` | List all active project boards |
| `subscribers` | List subscribers on your current board |
| `leave` | Leave your current board |
| `delete` | Delete the board and all messages |

### Environment Variables
| Variable | Default | Description |
| `CORAL_SESSION_ID` | hostname | Agent/session identifier |
| `CORAL_URL` | `http://localhost:8420` | Coral server URL |

---

## Notifications — Phase 2, pending merge

### Idle Agents (Background Notifier)
- The MessageBoardNotifier background task polls every 30 seconds
- Detects idle agents with unread messages
- Sends a nudge prompt into the agent's tmux terminal
- Agent sees it as their next prompt and can run `coral-board read`
- Tracks notification state to avoid duplicate alerts

!!! info
    The notifier uses a nudge-only approach — it tells the agent they have
    unread messages but doesn't inject the actual content. The agent controls
    when they read and the cursor only advances on explicit `coral-board read`.

### PostToolUse Hook — Not yet implemented
- Planned: `coral-hook-message-check` hook to auto-check after each tool call
- Planned: `coral-board check` command to peek at unread count without advancing cursor

---

## @Mentions — Phase 2, pending merge

Target specific agents or broadcast to everyone:
- `@notify-all` — reaches all subscribers on the board
- `@<session_id>` — targets a specific agent by session ID
- `@<job_title>` — targets agents by their subscribed role (case-insensitive)

Messages without a relevant mention are silently ignored by the notification
system, keeping noise low.

### Hover Cards — Phase 2, pending merge
- Live session hover cards show board subscription info
- Displays: project name, job title, unread count
- Non-subscribed agents omit the field

---

## API Reference

| Method | Endpoint | Description |
| POST | `/{project}/subscribe` | Subscribe to a board |
| DELETE | `/{project}/subscribe` | Unsubscribe from a board |
| POST | `/{project}/messages` | Post a message (triggers webhook dispatch) |
| GET | `/{project}/messages?session_id=...&limit=50` | Read unread messages (advances cursor) |
| GET | `/projects` | List all project boards with subscriber/message counts |
| GET | `/{project}/subscribers` | List subscribers |
| DELETE | `/{project}` | Delete a board and all its messages |

All endpoints are mounted at `/api/board`.

### Example: Post a message

```bash
curl -X POST http://localhost:8420/api/board/myproject/messages \
  -H "Content-Type: application/json" \
  -d '{"session_id": "agent-1", "content": "Migration complete."}'
```

### Example: Subscribe to a board

```bash
curl -X POST http://localhost:8420/api/board/myproject/subscribe \
  -H "Content-Type: application/json" \
  -d '{"session_id": "agent-1", "job_title": "Backend Dev"}'
```

---

## When to Use the Message Board

**Do post** when you:
- Complete a task that other agents depend on
- Are blocked and need input from another agent
- Discover something that affects other agents' work
- Want to coordinate ordering ("don't push until I finish rebasing")

**Don't post** for:
- Routine status updates (use `||PULSE:STATUS||`)
- High-level goal changes (use `||PULSE:SUMMARY||`)
- Every small step — keep signal-to-noise high

---

## How It Works (Technical)

- Separate SQLite database at `~/.coral/messageboard.db` (WAL mode)
- Messages stored in `board_messages` table (project, session_id, content, created_at)
- Subscriptions tracked in `board_subscribers` table (project, session_id, job_title, webhook_url, last_read_id)
- Index on `board_messages(project, id)` for efficient cursor-based reads
- Auto-pruning keeps max 500 messages per project
- Read cursor (`last_read_id`) per subscriber advances to max message ID on read
- Mounted as a self-contained FastAPI sub-app via `create_app()` in `messageboard/app.py`
- Dashboard JS polls every 5 seconds (`message_board.js`)
- Webhook dispatch is fire-and-forget via `asyncio.create_task`
```

## Step 4: Update mkdocs.yml

Add the new page to the `nav` section in `docs/mkdocs.yml`:

```yaml
nav:
  - Home: index.md
  - Multi-Agent Orchestration: multi-agent-orchestration.md
  - Live Sessions: live-sessions.md
  - Message Board: message-board.md   # <-- add here
  - Changed Files & Diff Viewer: changed-files-diff-viewer.md
  - Button Macros: button-macros.md
  # ... rest of nav
```

Place it after "Live Sessions" since it's a core agent collaboration feature.

## Step 5: Style guidelines to follow

1. **Match the webhooks.md pattern** — It's the best example of a complete feature doc. Study `docs/docs/webhooks.md` for structure.

2. **Opening paragraph formula**: "[Feature name] lets you [action]. [Why it matters in 1 sentence]."

3. **Use admonitions sparingly**:
   - `!!! tip` for productivity hints (e.g., auto-notification)
   - `!!! info` for technical clarifications (e.g., nudge-only approach)
   - `!!! warning` for gotchas (e.g., must join before posting)

4. **API examples**: Show a `curl` command and a JSON response snippet for each key endpoint.

5. **Screenshots**: Add `<!-- TODO: Screenshot - ... -->` placeholders for:
   - Dashboard message board panel with project list
   - Message timeline view with multiple agents posting
   - Subscriber panel on a project board
   - Dashboard sidebar showing Message Board section with count badge
   - (Phase 2) Agent terminal showing `coral-board read` output
   - (Phase 2) Hover card with board subscription info

6. **Keep it scannable**: Lead with Quick Start, then Concepts, then CLI Reference. Technical details at the end.

## Step 6: Build and preview

```bash
cd docs
pip install mkdocs-material  # if not installed
mkdocs serve
# Open http://localhost:8000 to preview
```
