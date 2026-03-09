# Scheduled/Recurring Sessions

## 1. Goal

This feature adds a first-class scheduling layer to Corral so that named jobs can
auto-launch agent sessions on a cron-like schedule — for example, "run the test
suite every night at 02:00," or "check for outdated dependencies every Monday."

Value delivered:
- Operators define the recurring work once and forget it; Corral handles launch,
  logging, and archival automatically.
- Execution history links each scheduled run to the live session it produced,
  giving full traceability through the existing session history viewer.
- The dashboard surface (job list, schedule builder, run history) uses the same
  UI conventions already present in Corral so no new mental models are needed.
- Failure visibility: missed runs and agent errors surface as distinct states,
  not silent gaps.
- Ephemeral Worktrees: To avoid conflicts and ensure clean execution state, scheduled sessions automatically launch into dynamically generated git worktrees that can be configured to self-destruct upon completion.

---

## 2. Patterns and Conventions Found

The following existing patterns govern all design decisions in this plan.

| Concern | Existing pattern | Location |
|---|---|---|
| Background service loop | `async def run_forever(interval)` → `asyncio.sleep` | `git_poller.py:23`, `session_indexer.py:207` |
| Service wired into lifespan | `asyncio.create_task(service.run_forever(...))` | `web_server.py:90-92` |
| DB schema definition | `conn.executescript(...)` inside `_ensure_schema` | `session_store.py:54` |
| Schema migrations | `ALTER TABLE ... ADD COLUMN` wrapped in `try/except OperationalError` | `session_store.py:181-208` |
| Store method signature | `async def verb_noun(self, ...) -> dict / list / None` | `session_store.py` throughout |
| JS module system | ES module imports, functions exported, globals exposed via `window.X` | `app.js:1-76` |
| Modal pattern | `show*/hide*` function pair, `display:flex / display:none` toggle | `modals.js:9-17` |
| Toast notifications | `showToast(message, isError)` | `utils.js:13` |
| Sidebar sections | `<section class="sidebar-section">` with `<h2>` header | `index.html:21-49` |
| Tab panels | `<button class="agentic-tab">` + `<div class="agentic-panel">` | `index.html:109-143` |
| State singleton | `import { state } from './state.js'` | Every JS module |
| No external scheduler library | Only `fastapi`, `uvicorn`, `jinja2`, `aiosqlite` in deps | `pyproject.toml:12-17` |

**New Patterns Introduced:**
- **Store Modularity:** Introducing `src/corral/store/schedule.py` (`ScheduleStore`) to keep scheduling-specific database access isolated.
- **API Modularity:** Introducing `src/corral/api/schedule.py` (FastAPI `APIRouter`) to keep `web_server.py` clean.
- **Frontend Templates:** Leveraging Jinja's `{% include %}` for cleaner template structure instead of bloating `index.html`.

---

## 3. Database Schema

### 3.1 New Tables

Add the following DDL to the `executescript` block inside `src/corral/store/connection.py`'s `_ensure_schema` method (which will be initialized during startup). Or, embed within the existing schema logic if keeping a single schema source of truth.

```sql
-- Job definitions
CREATE TABLE IF NOT EXISTS scheduled_jobs (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    name            TEXT NOT NULL,           -- Human label, e.g. "Nightly Tests"
    description     TEXT DEFAULT '',
    cron_expr       TEXT NOT NULL,           -- 5-field cron: "0 2 * * *"
    timezone        TEXT NOT NULL DEFAULT 'UTC',
    agent_type      TEXT NOT NULL DEFAULT 'claude',
    repo_path       TEXT NOT NULL,           -- Absolute path to the base repository
    base_branch     TEXT DEFAULT 'main',     -- Branch to check out for the worktree
    prompt          TEXT NOT NULL,           -- Initial prompt sent to the agent
    enabled         INTEGER NOT NULL DEFAULT 1,
    max_duration_s  INTEGER NOT NULL DEFAULT 3600,  -- Kill agent after N seconds
    cleanup_worktree INTEGER NOT NULL DEFAULT 1, -- 1 = Delete worktree after run
    created_at      TEXT NOT NULL,
    updated_at      TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_scheduled_jobs_enabled
    ON scheduled_jobs(enabled, id);

-- Execution history
CREATE TABLE IF NOT EXISTS scheduled_runs (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    job_id          INTEGER NOT NULL REFERENCES scheduled_jobs(id) ON DELETE CASCADE,
    session_id      TEXT,                    -- UUID of the agent session launched
    worktree_path   TEXT,                    -- The ephemeral worktree generated
    status          TEXT NOT NULL DEFAULT 'pending',
    -- status values: pending | running | completed | failed | missed | killed
    scheduled_at    TEXT NOT NULL,           -- ISO-8601 of intended fire time
    started_at      TEXT,                    -- Actual launch time
    finished_at     TEXT,                    -- Completion / kill / fail time
    exit_reason     TEXT,                    -- 'timeout' | 'agent_done' | 'error' | 'manual'
    error_msg       TEXT,
    created_at      TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_scheduled_runs_job
    ON scheduled_runs(job_id, scheduled_at DESC);

CREATE INDEX IF NOT EXISTS idx_scheduled_runs_session
    ON scheduled_runs(session_id);

CREATE INDEX IF NOT EXISTS idx_scheduled_runs_status
    ON scheduled_runs(status, scheduled_at DESC);
```

### 3.2 Schema Summary

| Table | Purpose |
|---|---|
| `scheduled_jobs` | Job definitions: defining repository, branch, execution schedule, prompt |
| `scheduled_runs` | Execution log: tracking the dynamic worktree path, session IDs, and run outcomes |

---

## 4. Scheduling Engine

### 4.1 Cron Parser

Create `src/corral/tools/cron_parser.py`.
Provides pure python standard library cron parsing: `parse_field`, `next_fire_time`, and `validate_cron`.

### 4.2 Timezone Handling
Store schedules in the job's declared `timezone` (default `UTC`). Utilize `zoneinfo.ZoneInfo`.

---

## 5. Backend Changes

### 5.1 Schedule Store
Create `src/corral/store/schedule.py` with `ScheduleStore`.
This class will encapsulate all CRUD operations for the schemas defined above, ensuring `store/connection.py` does not become bloated. It exposes methods like:
- `list_enabled_scheduled_jobs()`
- `create_scheduled_job()`
- `update_scheduled_run()`
- `get_last_run_for_job()`

### 5.2 Scheduler Service

Create `src/corral/background_tasks/scheduler.py`.

```python
\"\"\"Background scheduler service for Corral scheduled jobs.\"\"\"
from __future__ import annotations

import asyncio
import logging
from datetime import datetime, timezone
import os
import uuid

from corral.store.schedule import ScheduleStore
from corral.tools.session_manager import launch_claude_session
from corral.tools.tmux_manager import send_to_tmux, kill_session
from corral.utils import run_cmd

log = logging.getLogger(__name__)

TICK_INTERVAL = 30  # seconds between scheduler polls

class JobScheduler:
    \"\"\"Polls scheduled_jobs, fires due runs, monitors running sessions, manages worktrees.\"\"\"

    def __init__(self, store: ScheduleStore) -> None:
        self._store = store
        self._running: dict[int, asyncio.Task] = {}  # run_id -> watchdog task

    async def run_forever(self) -> None: ...
    async def _tick(self) -> None: ...
    
    async def _evaluate_job(self, job: dict, now: datetime) -> None:
        \"\"\"Evaluate if a job should fire.\"\"\"
        # ... logic to check next_fire_time vs now and ensure no running overlaps ...

    async def _fire_job(self, job: dict, scheduled_at: datetime) -> None:
        \"\"\"Generate a worktree, launch an agent session, and record the run.\"\"\"
        # 1. Create scheduled run record
        # 2. Determine a safe isolated run_dir path
        # 3. `git -C {repo_path} worktree add {run_dir} {base_branch}`
        # 4. launch_claude_session(working_dir=run_dir, ...)
        # 5. Populate scheduled_run with session_id and worktree_path
        # 6. Send prompt via send_to_tmux
        # 7. Spawn _watchdog task

    async def _watchdog(self, run_id: int, job: dict, session_id: str, worktree_path: str) -> None:
        \"\"\"Monitor a running session, auto-kill on timeout, and cleanup worktree on exit.\"\"\"
        # 1. Poll every 30 seconds to check if session is active
        # 2. On natural exit or timeout:
        # 3. Update result in schedule_store
        # 4. If `job["cleanup_worktree"]` is 1:
        #    `git worktree remove --force {worktree_path}`
```

### 5.3 REST Endpoints via APIRouter
Create `src/corral/api/schedule.py`.

```python
from fastapi import APIRouter
from pydantic import BaseModel

router = APIRouter(prefix="/api/scheduled", tags=["Scheduled Jobs"])

@router.get("/jobs")
async def list_jobs(): ...

@router.post("/jobs")
async def create_job(body: JobCreateSchema): ...

@router.get("/jobs/{job_id}/runs")
async def list_runs(job_id: int): ...

@router.post("/validate-cron")
async def validate_cron(body: CronValidateSchema): ...
```

In `src/corral/web_server.py`, mount this cleanly:
```python
from corral.schedule_api import schedule_router
app.include_router(schedule_router)
```

### 5.4 Lifespan Integration
In `web_server.py` lifespan context:
```python
from corral.scheduler import JobScheduler
from corral.schedule_store import ScheduleStore

schedule_store = ScheduleStore()
scheduler = JobScheduler(schedule_store)
scheduler_task = asyncio.create_task(scheduler.run_forever())
app.state.scheduler = scheduler
```

---

## 6. Frontend Changes

### 6.1 Modular Components

**New JS Modules (`src/corral/static/`):**
- `scheduler.js`: Fetches /api/scheduled interfaces, handles UI toggling, interactions.
- `schedule_builder.js`: Complex cron expression UI interactions and API validation.

**New Jinja Templates (`src/corral/templates/`):**
- `scheduler.html`: The main page content fragment for the job list and history.
- `scheduler_modal.html`: The modal code for job creation and configuration.

### 6.2 Modifying `index.html`

Use Jinja injections to keep `index.html` clean:

```html
<!-- Inside the Sidebar -->
<section class="sidebar-section">
    <div class="sidebar-section-header">
        <h2>Scheduled</h2>
        <button class="btn btn-small btn-primary sidebar-new-btn" onclick="showJobModal()">+ Job</button>
    </div>
    <ul id="scheduled-jobs-list" class="session-list"></ul>
</section>

<!-- Inside the Main View Container -->
{% include 'scheduler.html' %}

<!-- Bottom of document with Modals -->
{% include 'scheduler_modal.html' %}
```

### 6.3 State Management
Update `state.js` and `app.js` similarly to integrate bindings for `scheduler.js`.

---

## 7. Edge Cases and Failure Handling

- **Worktree Conflicts:** By default, named worktrees can collision if run concurrently. Since the app appends a UUID or `run_id` to the run path (e.g. `../myrepo_scheduled_run_12`), guaranteed isolation is provided.
- **Agent Launch Failure:** If `launch_claude_session` fails immediately, no watchdog is spawned. The worktree is forcibly cleaned up inline.
- **Overlapping Runs:** If a job attempts to run while its previous invocation is still active, it is marked as `missed` without queueing or interfering. With dynamic worktrees, technical overlap is possible without git collision, but bounded execution maintains clean system state.
- **Missed Schedules (Offline):** The poller detects offline gaps and schedules a single catch-up run to prevent system flooding.

---

## 8. Implementation Build Sequence

- **Phase 1 — Core Scheduling Engine:** Implement `cron_parser.py`, `ScheduleStore`, and DB schema setup.
- **Phase 2 — Isolated Execution Flow:** Implement `JobScheduler` ensuring `git worktree add` and `remove` commands correctly fire within `_fire_job` and `_watchdog`.
- **Phase 3 — APIRouter:** Build `api/schedule.py` extracting business logic from `web_server`.
- **Phase 4 — Frontend Scaffolding:** Create Jinja includes and update `static/` files.
- **Phase 5 — Integration Tests:** End-to-end testing of dynamic worktrees, cron validations, overlapping misses, and task termination cleanup.
