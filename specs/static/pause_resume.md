# Pause/Resume Behavior

The terminal capture pane auto-scrolls to the bottom as new content arrives. When the user scrolls up or selects text, updates pause so the viewport stays put. Scrolling back to the bottom (or clicking the pause badge) resumes.

## State

```
state.autoScroll   bool   default: true    — controls whether new content scrolls to bottom
state.isSelecting  bool   default: false   — true while user has text selected in the capture pane
```

Xterm renderer has additional internal state:

```
_userScrolledUp    bool   default: false   — user has scrolled up in the xterm viewport
_xtermSelecting    bool   default: false   — user has text selected in xterm
_pendingContent    string|null             — buffered update held while paused
_scrollUpCount     int    default: 0       — debounce counter for scroll-up detection
```

## Pause Triggers

### Scroll up
- **Xterm:** 2+ consecutive upward wheel ticks (`deltaY < 0`) sets `_userScrolledUp = true`, `state.autoScroll = false`, shows pause badge.
- **Semantic:** Scroll handler continuously checks position. `autoScroll` becomes `false` when user is >50px from the bottom.

### Text selection
- **Xterm:** `terminal.onSelectionChange` sets `_xtermSelecting = true`, `state.isSelecting = true`, shows pause badge.
- **Semantic:** DOM `selectionchange` event sets `state.isSelecting = true` if selection is inside the capture pane. Shows pause badge.

## Resume Triggers

### Scroll to bottom
- **Xterm:** Downward scroll (`deltaY > 0`) only resumes when `viewport.baseY <= viewport.viewportY` (user reached the end of the buffer). Calls `resumeScroll()`.
- **Semantic:** Scroll handler sets `autoScroll = true` when within 50px of the bottom.

### Selection cleared
- **Xterm:** `onSelectionChange` fires with `hasSelection = false` → clears `_xtermSelecting`, flushes pending content.
- **Semantic:** `selectionchange` fires with empty selection → clears `state.isSelecting`.

### Badge click
Clicking the pause badge calls `resumeScroll()`.

### Session switch
`connectTerminalWs()` calls `resumeScroll()` to reset all pause state.

## Behavior During Pause

### Xterm renderer
New WebSocket updates are buffered in `_pendingContent` (latest wins — earlier updates overwritten). On resume, `_flushPending()` writes the buffered content with ANSI clear-screen + cursor-home, then `scrollToBottom()`.

### Semantic renderer
`refreshCapture()` saves `scrollTop` before replacing innerHTML, then restores it after. If `state.isSelecting` is true, the DOM update is skipped entirely.

## resumeScroll()

```
_userScrolledUp = false
state.autoScroll = true
hide pause badge
flush pending content (xterm only)
```

## Edge Cases

- **Selection + scroll-up:** Both flags can be true simultaneously. Content stays buffered until both clear.
- **Rapid updates while paused:** Only the latest content is kept in `_pendingContent`.
- **Session switch:** Forces resume regardless of current pause state.
- **Semantic renderer has no scroll-up badge** — only selection shows the badge. Scroll pause is implicit (no visual indicator).
