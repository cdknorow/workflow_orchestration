# Changelog

## 0.6.2

### Bug Fixes

- **Fix session cycling when multiple agents share the same directory** — Terminal WebSocket connections no longer cycle between sessions that share a working directory (e.g. two agents both in `worktree_2`). Added a generation counter to suppress stale `onclose` reconnect handlers, and a guard to skip reconnecting if already connected to the same session.
- **Fix WebSocket corral handler overwriting session_id on name match** — When a session restarts and multiple sessions share the same directory name, the dashboard no longer accidentally switches to the wrong session via the name-based fallback.
- **Fix git snapshots collision for same-directory sessions** — Changed the `git_snapshots` UNIQUE constraint from `(agent_name, commit_hash)` to `(session_id, commit_hash)` so each session tracks its own git history. Includes an automatic DB migration for existing databases.
- **Fix git poller skipping second session in shared directory** — The git poller now stores snapshots for all sessions in a directory, not just the first one discovered.
- **Fix git state lookups to use session_id** — Live session API and WebSocket now look up git state by `session_id` first, falling back to `agent_name` for backwards compatibility.
- **Fix frontend task/note creation missing session_id** — Tasks and notes created from the dashboard now include `session_id` in the request body, ensuring data is properly scoped per session.
