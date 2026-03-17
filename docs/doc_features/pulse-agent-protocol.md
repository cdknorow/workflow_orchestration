# Doc Feature Guide: The PULSE Agent Protocol

## Overview

The PULSE protocol is a lightweight, text-based protocol that agents emit to stdout for Coral to parse in real time. It surfaces three pieces of context on the dashboard: what the agent is doing (Status), what it's trying to accomplish (Goal/Summary), and when it's uncertain (Confidence). The protocol is agent-agnostic — any process that writes `||PULSE:...||` tags to stdout works with Coral.

---

## Key Source Files & Architecture

| File | Role |
|------|------|
| `src/coral/PROTOCOL.md` | Full protocol specification — injected into agent system prompts |
| `src/coral/tools/log_streamer.py` | Reads log files backwards, strips ANSI codes, rejoins split lines, extracts latest STATUS and SUMMARY via regex |
| `src/coral/tools/pulse_detector.py` | Incrementally scans logs for CONFIDENCE events, records them as activity entries in `agent_events` |
| `src/coral/agents/claude.py` | Injects PROTOCOL.md via `--append-system-prompt` on launch |
| `src/coral/agents/gemini.py` | Injects protocol via `GEMINI_SYSTEM_MD` environment variable |

### Protocol Events

| Event | Format | Purpose | Frequency |
|-------|--------|---------|-----------|
| STATUS | `\|\|PULSE:STATUS <text>\|\|` | Current task | Frequent — before/after each subtask |
| SUMMARY | `\|\|PULSE:SUMMARY <text>\|\|` | High-level goal | Infrequent — at start, when goal changes |
| CONFIDENCE | `\|\|PULSE:CONFIDENCE <Low\|High> <reason>\|\|` | Flag uncertainty | As needed — only when certainty is useful context |

### Architecture Flow

1. **Agent emits PULSE tag** to stdout (e.g., `||PULSE:STATUS Reading tests||`)
2. **tmux `pipe-pane`** captures all terminal output to `/tmp/{type}_coral_{name}.log`
3. **Log streamer** reads the log file backwards, strips ANSI escape codes, rejoins lines split by terminal wrapping (`_rejoin_pulse_lines()`), and extracts the latest STATUS and SUMMARY values via regex
4. **Pulse detector** incrementally scans the log for CONFIDENCE events and records them as activity entries in the `agent_events` table
5. **Live sessions API** calls both the log streamer and pulse detector on every poll cycle (every 3 seconds via WebSocket)
6. **Status/Summary dedup** — the store only inserts a new row when the value actually changes
7. **WebSocket** pushes updated session state to all connected browsers every 3 seconds
8. **Dashboard renders**: Status → "Status:" line in header, Summary → "Goal:" line in header, Confidence → Activity timeline

### Line Rejoining

Long PULSE tags can be split across multiple terminal lines by wrapping. The log streamer handles this automatically via `_rejoin_pulse_lines()`, which reassembles fragments. Lines split across more than 5 continuation lines are dropped to avoid false matches.

---

## User-Facing Functionality & Workflows

### Dashboard Display

- **Status line**: Session header shows "Status: Reading codebase structure" — updates every 3 seconds
- **Goal line**: Session header shows "Goal: Implementing user authentication end-to-end"
- **Confidence events**: Appear in the Activity timeline with dedicated icons
  - Low confidence events may also trigger webhook notifications if configured
- **Status dot color**: Green (active), yellow (stale), amber (waiting for input)

### Agent-Type Injection

| Agent | How Protocol is Injected |
|-------|-------------------------|
| Claude | `--append-system-prompt` flag with PROTOCOL.md content |
| Gemini | `GEMINI_SYSTEM_MD` environment variable |
| Custom | Paste protocol text into system prompt, or emit tags from a wrapper script |

### Activity Timeline Integration

All three event types (Status, Goal, Confidence) appear in the Activity tab and are filterable using the Filter dropdown. They're stored in `agent_events` with:
- `event_type`: "status", "goal", "confidence"
- `summary`: The payload text
- `detail_json`: Optional additional data

---

## Suggested MkDocs Page Structure

### Title: "Agent Protocol (PULSE)"

1. **Introduction** — What PULSE is, why it exists, agent-agnostic design
2. **Protocol Events Reference** — Detailed spec for each event type
   - STATUS: format, guidelines, examples
   - SUMMARY: format, guidelines, examples
   - CONFIDENCE: format, levels (Low/High), guidelines, examples
3. **How It Works** — End-to-end pipeline diagram
   - Agent stdout → tmux pipe-pane → log file → log streamer → WebSocket → browser
   - Line rejoining for terminal-wrapped tags
4. **Dashboard Display** — Where each event appears
   - Status and Goal in session header
   - Confidence in Activity timeline
   - Screenshot: Session header with populated Status and Goal
   - Screenshot: Activity timeline with confidence events
5. **Custom Agents** — How to make any process work with PULSE
   - Just emit the tags to stdout
   - Wrapper script approach for external tools (Aider, Cursor, etc.)
6. **Per-Agent Injection** — How Coral injects the protocol for Claude, Gemini, custom
7. **Troubleshooting** — Common issues table
   - Dashboard shows "Idle", Goal line empty, events not appearing, split lines

### Screenshots to Include

- Session header showing Status and Goal lines populated by PULSE
- Activity timeline showing CONFIDENCE events with icons
- Activity filter dropdown with Status, Goal, Confidence toggles
- Full dashboard view showing the pipeline in action

### Code Examples

- All three PULSE tag formats with examples
- Wrapper script for custom agents
- PROTOCOL.md full content reference

---

## Important Details for Technical Writer

1. **Not a numeric scale**: CONFIDENCE is `Low` or `High`, not 1-5 (the existing index.md doc page incorrectly says 1-5, but the actual protocol uses Low/High).
2. **Line length limits**: STATUS should be under 60 characters, SUMMARY under 120 characters, to stay within terminal wrapping limits.
3. **Deduplication**: Repeated identical STATUS or SUMMARY values are collapsed — only changes trigger new database entries.
4. **Split line handling**: `_rejoin_pulse_lines()` handles up to 5 continuation lines. Extremely long payloads are dropped.
5. **Protocol is auto-injected**: For Claude and Gemini agents launched by Coral, the protocol is automatically included — no manual configuration needed.
6. **Stale detection**: If no STATUS is emitted for a configurable period, the status dot turns yellow (stale).
7. **Activity event storage**: All PULSE events are stored in `agent_events` table, enabling historical activity timeline reconstruction.
8. **Regex extraction**: The log streamer uses regex to find `||PULSE:STATUS ...||` and `||PULSE:SUMMARY ...||` patterns, reading the log file backwards for efficiency (most recent values first).
