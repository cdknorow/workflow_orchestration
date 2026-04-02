# Agent Team Persistence

Updated: 2026-04-02

## Problem

Agent teams are currently ephemeral — they exist only as a set of live sessions sharing a board name. The team config is either passed inline at launch and discarded, or stored as a JSON file on disk. This creates several problems:

1. **No team identity** — Once launched, there's no record of which sessions belong to a team, what config was used, or when it was created. The "team" is inferred from shared board membership.

2. **Worktree orphaning** — When a team uses a git worktree, the worktree path is stored per-session. If sessions are killed individually or the server restarts, the relationship to the worktree is lost and cleanup becomes unreliable.

3. **No team lifecycle** — You can't sleep/wake a team atomically, view team history, or relaunch a previously-used team config. Each launch is a fresh start.

4. **Config loss** — If a team is launched inline (not from a saved file), the config is gone after launch. You can't inspect what was launched or modify it for a relaunch.

5. **No team-level metadata** — There's nowhere to store team-level state like worktree path, launch time, status, or the relationship between a team and its agents.

## Design

### Core Concept

A **Team** is a first-class entity stored in the database. It holds the team configuration, tracks its lifecycle (created → running → sleeping → stopped), owns a worktree (optional), and maintains a relationship to its member sessions.

### Data Model

#### `teams` table

| Column | Type | Description |
|--------|------|-------------|
| `id` | INTEGER PK | Auto-increment |
| `name` | TEXT UNIQUE | Team name (also used as board_name) |
| `config_json` | TEXT | Full team config JSON (the launch payload) |
| `status` | TEXT | `running`, `sleeping`, `stopped` |
| `working_dir` | TEXT | The directory agents work in (may be a worktree, repo, or plain directory) |
| `is_worktree` | INTEGER | 1 if `working_dir` was created as a git worktree at launch (cleanup on stop) |
| `created_at` | TEXT | ISO 8601 timestamp |
| `updated_at` | TEXT | ISO 8601 timestamp |
| `stopped_at` | TEXT | When the team was stopped/killed (null if active) |

#### `team_members` table

Tracks each agent's membership in a team. This is the definitive record of who was in the team — persists after sessions are killed so teams can be resurrected.

| Column | Type | Description |
|--------|------|-------------|
| `id` | INTEGER PK | Auto-increment |
| `team_id` | INTEGER FK | References `teams.id` |
| `agent_name` | TEXT | Display name (e.g. "Lead Developer") |
| `agent_config_json` | TEXT | Per-agent config snapshot (prompt, capabilities, model, etc.) |
| `session_id` | TEXT | Current/last session ID (null if never launched) |
| `status` | TEXT | `active`, `sleeping`, `stopped` |
| `created_at` | TEXT | When the member was added |
| `stopped_at` | TEXT | When the member was stopped/killed (null if active) |

When an individual agent is killed while the team is still running, that member's status becomes `stopped` but the team stays `running`. When the team is killed or sleeps, all active members transition together.

#### `live_sessions` updates

Add a `team_id` column (nullable INTEGER, FK to `teams.id`) so each session knows which team it belongs to. This replaces the current inference-by-board-name approach.

```sql
ALTER TABLE live_sessions ADD COLUMN team_id INTEGER REFERENCES teams(id) ON DELETE SET NULL;
```

### Schema

```sql
CREATE TABLE IF NOT EXISTS teams (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    name            TEXT NOT NULL UNIQUE,
    config_json     TEXT NOT NULL,
    status          TEXT NOT NULL DEFAULT 'running'
        CHECK (status IN ('running', 'sleeping', 'stopped')),
    working_dir     TEXT NOT NULL,
    is_worktree     INTEGER NOT NULL DEFAULT 0,
    created_at      TEXT NOT NULL,
    updated_at      TEXT NOT NULL,
    stopped_at      TEXT
);

CREATE INDEX IF NOT EXISTS idx_teams_status ON teams(status);

CREATE TABLE IF NOT EXISTS team_members (
    id                INTEGER PRIMARY KEY AUTOINCREMENT,
    team_id           INTEGER NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
    agent_name        TEXT NOT NULL,
    agent_config_json TEXT NOT NULL,
    session_id        TEXT,
    status            TEXT NOT NULL DEFAULT 'active'
        CHECK (status IN ('active', 'sleeping', 'stopped')),
    created_at        TEXT NOT NULL,
    stopped_at        TEXT
);

CREATE INDEX IF NOT EXISTS idx_team_members_team ON team_members(team_id, status);
```

### Lifecycle

```
┌─────────┐   launch-team   ┌─────────┐
│         │ ──────────────→  │ running │
│ (none)  │                  │         │
└─────────┘                  └────┬────┘
                                  │
                    ┌─────────────┼─────────────┐
                    │ sleep       │ kill         │ kill-all
                    ▼             ▼              ▼
              ┌──────────┐  ┌─────────┐    ┌─────────┐
              │ sleeping │  │ stopped │    │ stopped │
              └────┬─────┘  └─────────┘    └─────────┘
                   │ wake        ▲
                   ▼             │ kill
              ┌─────────┐───────┘
              │ running │
              └─────────┘
```

**running** → All agents are live in tmux/PTY sessions. Board is active.

**sleeping** → Agents killed but sessions preserved in DB. Board paused. Team config and worktree retained. Can be woken to resume.

**stopped** → Team is finished. All sessions killed. Board unpaused. Worktree cleaned up (if `cleanup_worktree` is set). Config retained for history/relaunch.

### Team Operations

#### Launch Team
`POST /api/sessions/launch-team`

1. Create a `teams` row with the config, status=`running`
2. If `worktree: true`, create the worktree and store the path
3. Launch each agent session with `team_id` set
4. Return team ID, agent details, and worktree path

#### Kill Team
`POST /api/sessions/live/team/{boardName}/kill`

1. Snapshot: mark all `active` team_members as `stopped`, record `stopped_at`
2. Kill all live sessions belonging to the team
3. Set team status to `stopped`, record `stopped_at`
4. If `is_worktree = 1`, clean up the worktree
5. Unsubscribe agents from board

The snapshot at step 1 is critical — it captures which agents were still running at the time of kill. Members already individually stopped before the team kill retain their earlier `stopped_at`.

#### Kill Individual Agent
When a single agent is killed while its team is still running:

1. Mark that `team_member` as `stopped`
2. Kill the session as normal
3. Team stays `running` (other agents continue)

This means the team's member list naturally tracks who's still active vs who was killed early.

#### Sleep Team
`POST /api/sessions/live/team/{boardName}/sleep`

1. Mark all `active` team_members as `sleeping`
2. Kill tmux/PTY sessions (sessions stay in DB as sleeping)
3. Set team status to `sleeping`
4. Pause the board
5. Working directory preserved (worktree or otherwise)

#### Wake Team
`POST /api/sessions/live/team/{boardName}/wake`

1. Find `sleeping` team_members — these are the agents to relaunch
2. Relaunch each using their `agent_config_json`
3. Update member `status` to `active`, update `session_id`
4. Set team status to `running`
5. Unpause the board

Only sleeping members are relaunched. Members that were individually killed before the sleep stay `stopped`.

#### Resurrect Team
`POST /api/teams/{name}/resurrect`

Resurrects a `stopped` team — relaunches the agents that were active at the time it was killed.

1. Find the team by name, verify status is `stopped`
2. Collect members whose `stopped_at` matches the team's `stopped_at` — these are the agents that were active when the team was killed (not individually killed earlier)
3. If `is_worktree = 1`, create a fresh worktree from the same base branch (the old one was cleaned up)
4. Relaunch each member using their `agent_config_json`
5. Update member `status` to `active`, assign new `session_id`
6. Set team status to `running`, clear `stopped_at`

This is different from "relaunch" (which uses the original config and starts all agents). Resurrect restores the team's **final state** — only the agents that were still running when it was killed.

#### Relaunch Team
`POST /api/teams/{name}/relaunch`

Launches a fresh instance from the stored `config_json` — all agents from the original config, not just the ones active at kill time. Useful when you want a clean start.

1. Load `config_json` from the stopped team
2. Create new team_members for all agents in the config
3. Launch as if it were a new team (optionally with a fresh worktree)

#### Get Team
`GET /api/teams/{name}`

Returns the team record including config, status, working_dir, and member list with per-member status.

#### List Teams
`GET /api/teams`

Returns all teams with status and summary info. Optionally filter by `?status=running`.

### Worktree Handling

The team stores a `working_dir` and an `is_worktree` flag. The team doesn't care whether the directory is a worktree — it's just the path where agents work. The `is_worktree` flag is metadata that tells the cleanup logic whether `git worktree remove` should be called when the team stops.

**At launch:** If the user checks "Use Git Worktree," the launcher creates a worktree from `base_branch`, sets the team's `working_dir` to the worktree path, and sets `is_worktree = 1`. If no worktree is requested, `working_dir` is the user's chosen directory and `is_worktree = 0`.

**On sleep:** The `working_dir` is preserved regardless. Agents are killed but the directory stays intact for wake.

**On kill/stop:** If `is_worktree = 1`, run `git worktree remove --force` on `working_dir` with a 30-second timeout. If `is_worktree = 0`, do nothing — the directory belongs to the user.

**Orphan protection:** On server startup, scan for teams with status=`running` where no live sessions exist. Mark them as `stopped` and clean up worktrees where `is_worktree = 1`.

### Migration from Current Behavior

**Backward compatibility:** The `launch-team` API continues to work with the existing payload. Internally, it now creates a `teams` row. Old sessions without a `team_id` continue to work — they're treated as ad-hoc sessions not belonging to a team.

**Board inference:** The sidebar can still group sessions by board name for backward compatibility, but should prefer `team_id` grouping when available.

**File-based configs:** `~/.coral/teams/*.json` files remain supported for load/save. They're the serialization format for team configs, while the `teams` table is the runtime state.

### API Summary

| Method | Path | Description |
|--------|------|-------------|
| POST | `/api/sessions/launch-team` | Launch team (creates teams + team_members rows) |
| POST | `/api/sessions/live/team/{name}/kill` | Kill team, snapshot active members, cleanup worktree |
| POST | `/api/sessions/live/team/{name}/sleep` | Sleep team (preserve working dir) |
| POST | `/api/sessions/live/team/{name}/wake` | Wake sleeping members only |
| GET | `/api/teams` | List all teams with status |
| GET | `/api/teams/{name}` | Get team details + member list with per-member status |
| POST | `/api/teams/{name}/resurrect` | Resurrect stopped team (only last-active agents) |
| POST | `/api/teams/{name}/relaunch` | Fresh start from original config (all agents) |
| DELETE | `/api/teams/{name}` | Delete a stopped team record |

### UI Changes

1. **Sidebar:** Show team name as a group header with status badge (running/sleeping/stopped). Clicking the header shows team-level actions (sleep, wake, kill, view config).

2. **Teams tab or section:** List all teams with status, agent count, worktree path, and last activity. Allow relaunch from here.

3. **Launch modal:** No changes needed — it already has the worktree checkbox. The backend now creates the team record automatically.

### Implementation Phases

**Phase 1: Core persistence**
- `teams` table + store CRUD
- `team_id` on `live_sessions`
- LaunchTeam creates team row
- Kill/Sleep/Wake update team status
- Worktree owned by team, cleaned on kill

**Phase 2: API + UI**
- GET /api/teams, GET /api/teams/{name}
- Relaunch endpoint
- Sidebar team grouping with status
- Team detail view

**Phase 3: Lifecycle management**
- Orphan detection on startup
- Team history (stopped teams retained for reference)
- Delete old stopped teams
