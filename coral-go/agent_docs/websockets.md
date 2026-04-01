# WebSocket API

Coral provides two WebSocket endpoints for real-time streaming.

---

## `/ws/coral` — Session List Streaming

Streams the live session list with diff-based updates to minimize bandwidth.

### Connection

```
ws://localhost:8420/ws/coral
```

No query parameters required.

### Message Flow

**First message** — full snapshot of all sessions:

```json
{
  "type": "coral_update",
  "sessions": [
    {
      "name": "my-project",
      "agent_type": "claude",
      "session_id": "abc123-uuid",
      "tmux_session": "coral_claude_abc123",
      "status": "Working",
      "summary": "Implementing feature X",
      "staleness_seconds": 5,
      "display_name": "Custom Name",
      "icon": "🤖",
      "working_directory": "/home/user/project",
      "waiting_for_input": false,
      "done": false,
      "waiting_reason": null,
      "waiting_summary": null,
      "working": true,
      "stuck": false,
      "changed_file_count": 3,
      "commands": {"compress": "/compact", "clear": "/clear"},
      "board_project": "my-board",
      "board_job_title": "Task Title",
      "board_unread": 0,
      "log_path": "/path/to/log",
      "sleeping": false
    }
  ],
  "active_runs": []
}
```

**Subsequent messages** — only changes since last update:

```json
{
  "type": "coral_diff",
  "changed": [
    { /* full session object for each changed session */ }
  ],
  "removed": ["session-id-1", "session-id-2"],
  "active_runs": [/* only included if runs changed */]
}
```

- `changed`: Full session objects for sessions whose state changed (no field-level diffs).
- `removed`: Session IDs that are no longer live.
- `active_runs`: Only sent when the active runs list changes.

If nothing changed since the last poll, no message is sent.

### Poll Interval

Configurable via `WSPollIntervalS` in server config. Default: **5 seconds**.

### Session Status Fields

| Field | Type | Description |
|-------|------|-------------|
| `status` | string | Human-readable status from log parsing |
| `summary` | string | Current activity summary |
| `staleness_seconds` | float | Seconds since last log activity |
| `waiting_for_input` | bool | Agent is waiting for user input (notification event) |
| `done` | bool | Agent has stopped (stop event) |
| `working` | bool | Agent is actively running tools |
| `stuck` | bool | Reserved (currently always false) |
| `sleeping` | bool | Session is in sleep state |
| `changed_file_count` | int | Number of files changed by agent |
| `board_project` | string | Associated message board name |
| `board_unread` | int | Unread board messages |

---

## `/ws/terminal/{name}` — Terminal Streaming

Bidirectional WebSocket for real-time terminal interaction. Supports two backends:

- **PTY streaming**: Zero-polling, real-time output via goroutine fan-out
- **tmux polling**: Adaptive polling with file-change watching

The server automatically selects the backend based on how the session was launched.

### Connection

```
ws://localhost:8420/ws/terminal/{name}
```

**Query Parameters:**

| Parameter | Type | Description |
|-----------|------|-------------|
| `agent_type` | string | Agent type (e.g., `claude`) |
| `session_id` | string | Session UUID |

### Client → Server Messages

**Send terminal input:**
```json
{
  "type": "terminal_input",
  "data": "ls -la\n"
}
```

**Resize terminal:**
```json
{
  "type": "terminal_resize",
  "cols": 120,
  "rows": 30
}
```

### Server → Client Messages

**Terminal output (PTY streaming mode):**

Incremental output chunks as they arrive:
```json
{
  "type": "terminal_stream",
  "data": "output text..."
}
```

**Terminal update (tmux polling mode):**

Full terminal buffer snapshot with cursor position:
```json
{
  "type": "terminal_update",
  "content": "full terminal content...",
  "cursor_x": 42,
  "cursor_y": 10,
  "alt_screen": true
}
```

- `cursor_x`, `cursor_y`: Cursor position in the terminal grid.
- `alt_screen`: `true` when a TUI app (vim, htop, etc.) is using the alternate screen buffer. The client can use this to switch rendering modes.

**Initial snapshot:**

Sent immediately on connection — the current terminal content:
```json
{
  "type": "terminal_stream",
  "data": "initial pane content..."
}
```

**Terminal closed:**

Sent when the terminal pane disappears (session ended or killed):
```json
{
  "type": "terminal_closed"
}
```

### Backend Behavior

#### PTY Streaming

- Uses goroutine fan-out for zero-latency output delivery.
- Multiple WebSocket clients can subscribe to the same session simultaneously.
- Initial snapshot is sent on connect via `CaptureContent`.
- Channel closure signals session termination.

#### tmux Polling

- Uses adaptive capture with three triggers:
  1. **Log file change** — fsnotify watches the agent's log file for writes (near-real-time).
  2. **User input** — captures immediately after the client sends `terminal_input`.
  3. **Heartbeat** — periodic capture to detect pane disappearance.
- **fsnotify mode**: Event-driven with 5-second keepalive heartbeat.
- **Stat polling fallback**: 100ms polling interval when fsnotify is unavailable.
- Minimum capture interval: 15ms (prevents excessive captures during bursts).
- If the pane disappears, the server sends `terminal_closed` and enters slow heartbeat mode (3s) to detect if the pane comes back.

### Reconnection

The tmux polling backend supports automatic pane re-resolution. If a pane disappears and reappears (e.g., after a session restart), the WebSocket connection will automatically reconnect to the new pane without requiring a client reconnect.

For freshly launched sessions, the server retries pane resolution up to 15 times (200ms apart) to handle the startup delay.

---

## Origin Validation

Both WebSocket endpoints validate the `Origin` header:

- **Localhost** connections are always allowed (`localhost`, `127.0.0.1`, `[::1]`).
- **Same-origin** requests (where Origin host matches the request Host) are allowed for remote access.
- The request's `Host` header is added as an allowed origin pattern for remote access scenarios.

---

## Authentication

WebSocket connections go through the same auth middleware as REST endpoints:

- **Localhost**: Auth bypassed.
- **Remote**: Requires API key (passed as a cookie or query parameter by the frontend).
