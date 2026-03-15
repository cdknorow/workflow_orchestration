# Inter-Agent Message Board

## Context

Agents working on the same project across different machines need a shared communication channel. A backend debugger might find request logs that a frontend agent needs, but there's no way to share them. This feature adds a project-scoped message board that feels like a group chat to agents — subscribe, post, read — while the board handles cursor tracking, message routing, and push notifications.

Built as a **self-contained module** inside the Coral repo — own database, own store, own FastAPI sub-app — with zero dependencies on Coral internals. Coral mounts it as a sub-application. Extraction to a standalone service later requires only lifting the directory out.

## Design Principles

1. **Dead simple agent API** — subscribe, post, read. That's it.
2. **Board manages complexity** — per-subscriber read cursors, own-message filtering, webhook dispatch
3. **Project-scoped** — agents join by project name, assigned by the developer
4. **Calling card** — each subscriber identified by `session_id` + `job_title`
5. **Push + poll** — subscribers can register a webhook URL for push notifications; polling always works too

## Agent Experience

```bash
# 1. Developer assigns agent to project "auth-refactor"
#    Agent subscribes on startup:
POST /api/board/auth-refactor/subscribe
  {"session_id": "abc123", "job_title": "Backend Debugger", "webhook_url": "http://remote-coral:8420/api/board/notify"}

# 2. Agent posts a message:
POST /api/board/auth-refactor/messages
  {"session_id": "abc123", "content": "Found 401 errors — auth header missing Bearer prefix"}

# 3. Agent reads new messages (board tracks cursor, excludes own messages):
GET /api/board/auth-refactor/messages?session_id=abc123
  → returns only messages from OTHER subscribers, posted since last read

# 4. Agent unsubscribes when done:
DELETE /api/board/auth-refactor/subscribe
  {"session_id": "abc123"}
```

## Database Schema

Stored in its own database: `~/.coral/messageboard.db` (separate from Coral's `sessions.db`).

```sql
CREATE TABLE IF NOT EXISTS board_subscribers (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    project       TEXT NOT NULL,
    session_id    TEXT NOT NULL,
    job_title     TEXT NOT NULL,
    webhook_url   TEXT,
    last_read_id  INTEGER NOT NULL DEFAULT 0,
    subscribed_at TEXT NOT NULL,
    UNIQUE(project, session_id)
);

CREATE TABLE IF NOT EXISTS board_messages (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    project     TEXT NOT NULL,
    session_id  TEXT NOT NULL,
    content     TEXT NOT NULL,
    created_at  TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_board_messages_project
    ON board_messages(project, id);
```

**Key design decisions:**
- `last_read_id` on the subscriber row tracks their cursor — the board knows what each subscriber has already seen
- `UNIQUE(project, session_id)` prevents duplicate subscriptions
- `webhook_url` is optional — only needed if the subscribing Coral wants push notifications
- Messages indexed by `(project, id)` for efficient cursor-based reads
- No FK between messages and subscribers — messages persist even if the poster unsubscribes
- Auto-prune: keep latest 500 messages per project

## API Endpoints — `src/coral/messageboard/api.py`

Mounted at `/api/board` via `app.mount()`. Paths below are relative to that mount:

| Method | Path (relative) | Full path | Body / Params | Purpose |
|--------|-----------------|-----------|---------------|---------|
| `POST` | `/{project}/subscribe` | `/api/board/{project}/subscribe` | `{session_id, job_title, webhook_url?}` | Subscribe |
| `DELETE` | `/{project}/subscribe` | `/api/board/{project}/subscribe` | `{session_id}` | Unsubscribe |
| `GET` | `/{project}/subscribers` | `/api/board/{project}/subscribers` | — | List subscribers |
| `POST` | `/{project}/messages` | `/api/board/{project}/messages` | `{session_id, content}` | Post message |
| `GET` | `/{project}/messages` | `/api/board/{project}/messages?session_id=X` | `?session_id=X&limit=50` | Read new messages |
| `GET` | `/projects` | `/api/board/projects` | — | List active projects |

### Read mechanics (the key simplification)

When `GET /messages?session_id=abc` is called:
1. Look up subscriber row → get `last_read_id`
2. Query: `SELECT * FROM board_messages WHERE project = ? AND id > ? AND session_id != ? ORDER BY id ASC LIMIT ?`
3. Update subscriber's `last_read_id` to the max ID returned (or current max if no messages)
4. Return messages with sender's `job_title` joined from subscribers table

The agent never has to track timestamps or cursors — just call read and get new messages.

### Push notification mechanics

When `POST /messages` is called:
1. Insert the message
2. Query all subscribers for this project WHERE `session_id != poster` AND `webhook_url IS NOT NULL`
3. For each, fire an async HTTP POST to their `webhook_url` with:
```json
{
  "project": "auth-refactor",
  "message": {
    "id": 42,
    "session_id": "sender-abc",
    "job_title": "Backend Debugger",
    "content": "Found the bug",
    "created_at": "2026-03-14T10:30:00"
  }
}
```
4. Webhook dispatch is best-effort (fire and forget with timeout). Failed deliveries don't block the post.

The receiving Coral's `/api/board/notify` endpoint handles local delivery to the agent (tmux, hooks, etc) — that's a separate concern built later.

## Store — `src/coral/messageboard/store.py`

Standalone `MessageBoardStore` managing its own SQLite connection (`~/.coral/messageboard.db`). No inheritance from Coral's `DatabaseManager` — owns its own `_get_conn()` with WAL mode. Methods:

| Method | Purpose |
|--------|---------|
| `subscribe(project, session_id, job_title, webhook_url=None)` | Upsert subscriber |
| `unsubscribe(project, session_id)` | Remove subscriber |
| `list_subscribers(project)` | List all subscribers for a project |
| `post_message(project, session_id, content)` | Insert message, auto-prune to 500/project |
| `read_messages(project, session_id, limit=50)` | Get new messages (id > last_read_id, exclude own), update cursor |
| `get_webhook_targets(project, exclude_session_id)` | Get webhook URLs for notification dispatch |
| `list_projects()` | List active projects with counts |
| `delete_project(project)` | Delete all messages + subscribers for a project |

## Module Structure — `src/coral/messageboard/`

Self-contained package, no imports from `coral.store` or `coral.api`:

```
src/coral/messageboard/
├── __init__.py          # Exports create_app() for mounting
├── store.py             # MessageBoardStore — own DB, own connection
├── api.py               # FastAPI router with all endpoints
└── app.py               # create_app() → FastAPI sub-application (store + routes)
```

## Admin Dashboard View

Follows Coral's existing pattern: Jinja2 static shell + vanilla JS + REST API.

### Template: `src/coral/templates/includes/views/message_board.html`
- Project list (left panel) — click a project to see its board
- Board view (right panel) — messages in chronological order with calling cards
- Subscriber list — who's subscribed, their job titles, online status
- Post form — developer can post messages to the board directly
- Admin controls — delete projects, clear messages

### JavaScript: `src/coral/static/js/message_board.js`
- `loadProjects()` → fetch `/api/board/projects`, render project list
- `selectProject(name)` → fetch messages + subscribers, render board view
- `postMessage(project)` → POST to board as "Developer" role
- `deleteProject(project)` → DELETE with confirmation
- Auto-refresh via polling or WebSocket (can reuse existing WS connection)

### Sidebar integration: `src/coral/templates/includes/sidebar.html`
- Add "Message Board" section with project count badge
- Clicking opens the message board view

### CSS: reuse existing `.session-view`, `.btn`, `.session-list` classes; minimal new CSS if needed

## Integration Point — `src/coral/web_server.py`

Single line to mount:
```python
from coral.messageboard.app import create_app as create_board_app
app.mount("/api/board", create_board_app())
```

No changes to `connection.py`, `store/__init__.py`, or any other Coral internals.

## Future Layers (not in this PR)

- **Notification receiver** — `/api/board/notify` endpoint on remote Coral that delivers messages to local agents (tmux send-keys, hook injection)
- **Auth/OAuth** — verify agent identity instead of trusting self-reported session_id
- **Message types** — `type` field (log, question, answer, status) for filtering
- **Threading** — `reply_to` field for associating responses
- **Standalone extraction** — separate FastAPI app with own DB for hosted enterprise offering

## Implementation Steps (each agentically verifiable)

Each step must be independently testable — write the code, write a test, run it, confirm green before moving on. No human input needed between steps.

### Step 1: Store layer (`src/coral/messageboard/store.py`)
- Implement `MessageBoardStore` with own SQLite connection and schema init
- **Verify:** Write `tests/test_messageboard_store.py` with tests for:
  - `subscribe()` creates a subscriber; re-subscribe (upsert) updates job_title but preserves cursor
  - `unsubscribe()` removes subscriber
  - `list_subscribers()` returns all subscribers for a project
  - `post_message()` inserts and returns the message
  - `read_messages()` returns only messages with id > last_read_id, excludes own messages, advances cursor
  - Calling `read_messages()` twice with no new posts returns empty on second call
  - `list_projects()` returns correct counts
  - Auto-prune: post 510 messages, verify only 500 remain
- **Pass criterion:** `pytest tests/test_messageboard_store.py -v` all green

### Step 2: API layer (`src/coral/messageboard/api.py` + `app.py`)
- Implement FastAPI router and `create_app()` factory
- **Verify:** Write `tests/test_messageboard_api.py` using `httpx.AsyncClient` + `ASGITransport`:
  - POST subscribe → 200, returns subscriber
  - POST message → 200, returns message with id
  - GET messages with session_id → returns only unread messages from others
  - GET messages again → empty (cursor advanced)
  - GET subscribers → lists all subscribers with calling cards
  - GET projects → lists projects with counts
  - DELETE subscribe → removes subscriber
- **Pass criterion:** `pytest tests/test_messageboard_api.py -v` all green

### Step 3: Webhook dispatch
- Add async webhook dispatch to `post_message` API handler
- **Verify:** Add test using a mock HTTP server (or `respx`/`httpx.MockTransport`):
  - Subscribe with webhook_url, post from another subscriber → webhook fires with correct payload
  - Subscribe without webhook_url → no webhook attempt
  - Webhook failure doesn't block the post response
- **Pass criterion:** `pytest tests/test_messageboard_api.py -v` all green (including new webhook tests)

### Step 4: Mount into Coral (`src/coral/web_server.py`)
- Add single `app.mount()` line
- **Verify:**
  - `pytest tests/ -v` — all existing Coral tests still pass
  - Start the server, curl the endpoints end-to-end:
    ```bash
    curl -X POST localhost:8420/api/board/test-project/subscribe \
      -H 'Content-Type: application/json' \
      -d '{"session_id":"agent1","job_title":"Tester"}'
    curl -X POST localhost:8420/api/board/test-project/messages \
      -H 'Content-Type: application/json' \
      -d '{"session_id":"agent1","content":"hello"}'
    curl localhost:8420/api/board/projects
    ```
- **Pass criterion:** All curls return 200 with expected JSON; full test suite green

### Step 5: Admin dashboard view
- Add message board view template, JS module, sidebar section
- **Verify:**
  - Start server, subscribe agents and post messages via curl (from Step 4)
  - Open browser to `http://localhost:8420`
  - Click "Message Board" in sidebar → see project list with counts
  - Click a project → see messages with calling cards (session_id + job_title)
  - See subscriber list with active subscribers
  - Post a message as "Developer" from the admin UI
  - Delete a project → confirm it's removed
- **Pass criterion:** All interactions work in browser; full test suite green
