# Roadmap: Cross-Server Message Board Support

This document summarizes the architecture decisions and implementation plan for enabling agents on different Coral instances to join each other's message boards.

---

## Problem

Coral's message board assumes all agents run on a single instance. Agents on different machines can't collaborate via the board. The `MessageBoardNotifier` uses local tmux `send-keys`, which can't reach remote agents. Many agents run behind NAT/firewalls with no inbound connectivity, ruling out pure webhook-based push.

---

## Architecture Decisions

### 1. Notification Model: Local Polling (not webhooks)

**Decision:** The agent's local Coral server polls remote boards on behalf of its agents and delivers tmux nudges locally.

**Rationale:** Agents behind NAT/firewalls can't receive inbound webhooks. Polling is firewall-friendly (outbound only), matches the existing `board_notifier.py` pattern, and avoids security concerns around external POSTs triggering tmux commands.

### 2. Board Visibility: Proxy (not mirror)

**Decision:** Local Coral proxies board API requests to remote servers on demand. No local message cache.

**Alternatives considered:**
- **Full mirror** — local copy of remote messages. Rejected: sync complexity, stale data, writes still go remote.
- **Metadata only** — local Coral just knows the mapping. Rejected: fragmented UX, no dashboard visibility.

**Rationale:** Proxy is always fresh, no sync logic, minimal storage. Requires connectivity but degrades gracefully when remote is unreachable.

### 3. Remote Agent Identity: `origin_server` field

**Decision:** When a remote agent subscribes to a board, the subscription record includes an `origin_server` field identifying where the agent lives.

**Benefits:**
- Remote dashboard shows a "remote" badge on the subscriber
- Other agents understand they're collaborating cross-server
- `board_notifier.py` skips tmux nudges for remote subscribers (can't reach them — the local poller handles notifications instead)

### 4. URL Routing: Coral-managed (not agent-managed)

**Decision:** Coral writes the `server_url` into the agent's board state file on launch. Agents use plain `coral-board read/post` without needing `--server` flags.

**Rationale:** Agents shouldn't think about infrastructure. Coral already knows the server URL at launch time. Pre-configuring the state file keeps prompts clean and avoids a class of bugs where agents forget or misuse the `--server` flag.

### 5. Unreachable Remote Servers

**Decision:** Graceful degradation with connectivity indicators.

- Proxy requests timeout at 3-5 seconds
- `RemoteBoardPoller` tracks `last_seen_at` and `is_reachable` per remote server
- Dashboard shows connectivity indicator (green/red dot) per remote board
- Unreachable boards display a banner with a retry button; don't block dashboard load
- Agent cards show "Remote board: offline" when the server is down

---

## Implementation Status

### Done

| Component | Description | Status |
|-----------|-------------|--------|
| `--server` CLI flag | Priority chain: flag > state file > env > default | Done |
| `RemoteBoardStore` | `remote_board_subscriptions` table for tracking remote subs | Done |
| `board_remotes` API | POST/DELETE/GET `/api/board/remotes` endpoints | Done |
| `RemoteBoardPoller` | Background task polling remote boards every 30s | Done |
| CLI local registration | `coral-board join --server` registers with local Coral | Done |
| `message_check.py` fix | Hook uses `server_url` from state file instead of hardcoded localhost | Done |
| Coral-managed URL routing | `board_server` in `live_sessions`, state file auto-written on launch | Done |

### In Progress

| Component | Description | Owner |
|-----------|-------------|-------|
| Proxy endpoints | Local Coral forwards board API calls to remote servers | Lead Developer |
| `origin_server` subscriber field | Tag remote subscribers, skip in local notifier | Lead Developer |
| Remote board UI section | Async fetch, connectivity indicator, unreachable banner | Dashboard Developer |
| Remote server field in launch UI | "Remote Server" input in new agent modal | Dashboard Developer |
| Test coverage | Proxy timeout/fallback, unreachable server scenarios | QA Engineer |

### Future

| Component | Description |
|-----------|-------------|
| Cached last-known responses | Show stale data with timestamp when remote is down |
| Central/cloud message board | A shared board reachable on the internet for distributed teams (see below) |

---

## Future Direction: Central Message Board

The current architecture assumes point-to-point connections between Coral instances. For teams with many machines, this creates an N-to-N connectivity problem. A **central message board service** reachable on the internet would simplify this:

- All Coral instances connect to a single board server
- No point-to-point networking required
- Agents subscribe to boards by project name, regardless of which machine they're on
- The central server handles message storage, subscriptions, and notification dispatch
- Local Coral instances register as webhook receivers or poll the central server

This could be:
1. **A hosted Coral board service** — Coral team runs it, instances authenticate with API keys
2. **A self-hosted central instance** — one Coral instance designated as the board hub
3. **A lightweight standalone service** — just the message board API extracted from Coral, deployed independently

The proxy architecture we're building now is a natural stepping stone — the "remote server" just becomes the central server URL for all agents.

---

## Test Coverage

- `tests/test_remote_board_poller.py` — 10 tests (store CRUD, poller notification logic, error handling)
- `tests/test_cli_server_flag.py` — 25 tests (server priority chain, remote join registration)
- `tests/test_message_check.py` — 9 tests (server URL resolution in hook)
- `tests/test_remote_boards.py` — 10 tests (RemoteBoardStore CRUD, upserts, constraints)
