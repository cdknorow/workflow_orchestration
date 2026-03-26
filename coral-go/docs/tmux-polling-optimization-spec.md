# Tmux Terminal Polling Optimization Spec

## Background

Coral uses tmux as the terminal backend for agent sessions. The WebSocket terminal
endpoint (`/ws/terminal/{name}`) streams terminal output to the browser by polling
tmux — there is no push-based mechanism. Each connected client runs a polling loop
that spawns tmux subprocesses to capture pane content.

**Why tmux (not PTY):** tmux is the chosen backend because operators can
`tmux attach` to any agent session directly from the terminal for debugging and
monitoring. The PTY backend would be more efficient but removes this capability.

### Current Polling Flow (per WebSocket client)

```
Every 100ms:
  1. os.Stat(logFile)            ~0.01ms   — check if agent produced output
  2. IF mtime changed OR input received:
     a. FindTarget()             ~0.1ms    — tmux list-panes (locate session pane)
     b. DisplayMessage()         ~0.1ms    — tmux display-message (cursor + alt screen)
     c. CaptureRawOutput()       ~1-5ms    — tmux capture-pane -e -S-200
     d. JSON serialize + WS send ~1ms
```

**Cost when idle:** ~0.01ms/100ms (just stat — negligible)
**Cost when active:** ~2-6ms per capture, up to 10 captures/sec per client

### Problem

With N active agents and M browser tabs, the server spawns up to `N * M * 30`
tmux subprocesses per second during active output. This contributed to resource
exhaustion and server crashes (see: fd leak + subprocess accumulation bug).

---

## Optimization 1: Cache tmux output (skip redundant captures)

**Status:** Approved
**Priority:** High (quick win)
**Files:** `internal/server/routes/websocket.go`

### Problem

`doCapture()` always runs all three tmux commands (FindTarget, DisplayMessage,
CaptureRawOutput) even when the output hasn't changed. The mtime gate helps, but
during active agent output the log file changes constantly — triggering full
captures every 100ms even if the visible terminal content is identical.

### Design

Cache the last captured content and cursor state. After running CaptureRawOutput,
compare against the cached values. Only serialize and send over WebSocket if
something actually changed.

Additionally, cache the FindTarget result — the pane target doesn't change unless
the session is killed and recreated.

```
doCapture():
  IF cachedTarget == "":
    cachedTarget = FindTarget()        // Only on first call or after pane-gone

  cursor = DisplayMessage(cachedTarget)
  content = CaptureRawOutput(cachedTarget)

  IF content == lastContent AND cursor == lastCursor:
    return                             // Skip WebSocket send entirely

  lastContent = content
  lastCursor = cursor
  send(content, cursor)
```

### Savings

- **FindTarget:** Called once instead of every capture. Saves ~0.1ms + 1 subprocess per capture.
- **WebSocket writes:** Eliminated when terminal is unchanged (common during
  pauses between agent actions). Reduces network overhead.
- **DisplayMessage + CaptureRawOutput:** Still called every time mtime changes,
  but the diff check prevents unnecessary serialization and network I/O.

### Notes

- `lastContent` comparison already exists at line 641 (`if content != lastContent`).
  The optimization here is primarily caching FindTarget.
- The content comparison should use a hash if content strings are large (200 lines
  at 80 cols = ~16KB). A simple `content != lastContent` string comparison is fine
  for this size.
- When pane-gone is detected, clear `cachedTarget` to force re-resolution.

---

## ~~Optimization 2: Share captures across WebSocket clients~~

**Status:** Deferred (not a priority right now)

Multiple browser tabs watching the same session each run independent polling loops.
A fan-out mechanism could run one capture and broadcast to all subscribers. Deferred
because the common case is 1 tab per session.

---

## Optimization 3: Event-driven capture via fsnotify

**Status:** Approved
**Priority:** Medium
**Files:** `internal/server/routes/websocket.go`, new dependency: `fsnotify`

### Problem

The 100ms stat ticker polls the log file mtime even when nothing is happening.
While cheap (0.01ms), this is architecturally wasteful — the OS already knows
when a file changes and can notify us.

### Design

Replace the `statTicker` with filesystem event notifications using kqueue (macOS)
via the `fsnotify` library. The log file is written by `tmux pipe-pane` which
appends agent output continuously — every append triggers a kqueue WRITE event.

```
Current:
  statTicker (100ms) → os.Stat(logFile) → if mtime changed → doCapture()

Proposed:
  fsnotify.Watcher on logFile → on WRITE event → doCapture()
  keepalive ticker (5s) → doCapture() if no events received (fallback heartbeat)
```

### Implementation

```go
watcher, err := fsnotify.NewWatcher()
if err != nil {
    // Fall back to stat polling (current behavior)
    return h.wsTerminalPollingLegacy(ctx, conn, r, name)
}
defer watcher.Close()

if err := watcher.Add(logPath); err != nil {
    // Fall back to stat polling
    return h.wsTerminalPollingLegacy(ctx, conn, r, name)
}

keepalive := time.NewTicker(5 * time.Second)
defer keepalive.Stop()

for {
    select {
    case <-ctx.Done():
        return
    case event := <-watcher.Events:
        if event.Op&fsnotify.Write != 0 {
            doCapture()
        }
    case <-inputEvent:
        doCapture()
    case <-keepalive.C:
        doCapture()  // Heartbeat — catches edge cases fsnotify might miss
    }
}
```

### Savings

- **Idle sessions:** Zero stat syscalls. Watcher is passive — the kernel notifies
  us only when output arrives.
- **Active sessions:** Captures fire immediately on output instead of waiting up to
  100ms for the next stat tick. **Lower latency AND lower CPU.**
- **Keepalive ticker at 5s** (vs 100ms) reduces timer overhead 50x for idle sessions.

### Trade-offs

| Aspect | Stat Polling (current) | fsnotify (proposed) |
|--------|----------------------|---------------------|
| Idle CPU | 10 stat calls/sec | 0 (passive wait) |
| Latency | 0-100ms | 0-1ms (event-driven) |
| Dependency | None | `fsnotify` package |
| Fallback | N/A | Graceful → stat polling |
| Event coalescing | N/A | Handled by minCaptureInterval gate |
| Platform support | All | macOS (kqueue), Linux (inotify) |

### Risks and Mitigations

- **Rapid event storms:** During heavy agent output, fsnotify may fire hundreds of
  events per second. The existing `minCaptureInterval` (15ms) gate already
  deduplicates these — no additional throttling needed.
- **File recreation:** If the log file is rotated or recreated, the watcher becomes
  stale. Mitigation: watch the directory, or re-add the watch on fsnotify.Remove
  events. The keepalive ticker also catches this case.
- **fsnotify unavailable:** Graceful fallback to current stat polling. No behavior
  change for the user.

### Dependency

```
go get github.com/fsnotify/fsnotify
```

Mature, well-maintained library. Used by Docker, Kubernetes, Hugo, Viper. macOS
uses kqueue backend, Linux uses inotify. No CGO required.

---

## Summary

| Optimization | Subprocess savings | Latency improvement | Complexity |
|-------------|-------------------|--------------------| -----------|
| 1. Cache FindTarget | -1 subprocess/capture | None | Low |
| 1. Skip unchanged sends | -1 WS write when idle | None | Low |
| 3. fsnotify replaces stat | -10 stat calls/sec idle | 0-100ms → 0-1ms | Medium |

### Implementation Order

1. **Cache FindTarget result** — trivial, immediate win
2. **Add fsnotify watcher** with stat polling fallback
3. **Adjust keepalive from 100ms to 5s** (only after fsnotify is in place)

### Decision Log

| Decision | Rationale |
|----------|-----------|
| Keep tmux backend as default | Operators need `tmux attach` for debugging |
| Defer multi-client fan-out (opt 2) | Common case is 1 tab per session |
| Use fsnotify over tmux wait-for | wait-for requires shell integration in agents |
| Use fsnotify over tmux control mode | Control mode adds socket management complexity |
| Graceful fallback to stat polling | fsnotify failure shouldn't break terminal streaming |
| 15ms min capture interval unchanged | Already handles event storm deduplication |
