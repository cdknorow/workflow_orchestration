# Release Notes

## v0.3.0

### New Features
- **Persistent global settings store** — New `user_settings` table with API endpoints and Settings UI (gear icon in top bar).
- **JSONL-based live chat view** — Rich chat rendering with tool-use block support in the live stream.
- **Block grouping in live terminal stream** — Related output blocks are visually grouped for readability.
- **Pluggable rendering engines** — Rendering logic abstracted into pluggable classes with per-agent defaults.
- **Right sidebar history tab** — Live history view moved to a right sidebar tab; terminal stays always visible.
- **Dashboard branding** — Added logo and favicon.
- **Copy button** — One-click copy on output blocks.

### Bug Fixes
- Fix PULSE protocol tags split across terminal line wraps.
- Fix PULSE regex to match spinner-prefixed lines from terminal output.
- Fix PULSE parsing false positives; simplify confidence levels to Low/High.
- Fix block jumping and text block splitting in live stream renderer.
- Fix `task-sync` hook not passing `session_id` when looking up tasks.
- Fix user messages missing from session history indexing.
- Fix history chat assistant messages not rendering as markdown.

### Improvements
- Scope text-selection pause to the terminal pane only (no longer pauses the whole dashboard).
- Pause capture updates while user is selecting text.
- Improved summary generation prompt.
- `+new` defaults to corral root directory.

### Docs
- Added dashboard testing instructions to `DEVELOP.md`.

## v0.2.0

Initial public release.
