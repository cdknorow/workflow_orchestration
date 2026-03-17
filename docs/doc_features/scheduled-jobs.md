# Doc Feature Guide: Scheduled Jobs

## Overview

Scheduled Jobs allow users to define recurring agent tasks that run automatically on a cron schedule. When the scheduler fires, Coral creates an isolated git worktree, launches an agent with a configured prompt, monitors it with a watchdog timer, and records the full run history. This enables hands-off automation like nightly test suites, recurring code reviews, and dependency maintenance.

---

## Key Source Files & Architecture

| File | Role |
|------|------|
| `src/coral/store/schedule.py` | SQLite CRUD for `scheduled_jobs` and `scheduled_runs` tables |
| `src/coral/api/schedule.py` | FastAPI routes for job management and run history |
| `src/coral/background_tasks/scheduler.py` | Background service — evaluates cron expressions, launches jobs, runs watchdog |
| `src/coral/tools/cron_parser.py` | Pure-Python 5-field cron expression parser and evaluator |

### Database Tables

| Table | Purpose |
|-------|---------|
| `scheduled_jobs` | Job definitions — name, cron expression, timezone, repo path, base branch, agent type, prompt, flags, max duration, cleanup setting |
| `scheduled_runs` | Execution history — job_id, session_id, worktree_path, status, scheduled/started/finished timestamps, exit reason, error message |

### Architecture Flow

1. **Scheduler background task** runs every ~30 seconds, checks all enabled jobs against their cron expressions
2. When a job fires, the scheduler:
   - Creates a `scheduled_runs` entry with status `pending`
   - Creates an isolated git worktree at `{repo}/.coral-jobs/{job_id}/{run_id}` from the base branch
   - Launches an agent (Claude or Gemini) in the worktree with the configured prompt and flags
   - Links the run to the created `live_sessions` entry
3. **Watchdog** monitors running jobs and kills agents that exceed `max_duration_s`
4. On completion, the run status updates to `completed`, `killed`, or `failed`
5. If `cleanup_worktree` is enabled, the worktree is deleted after the run finishes

---

## User-Facing Functionality & Workflows

### Creating a Job

1. Click **+New Job** in the Scheduled Jobs sidebar section
2. Fill in the modal:
   - Job Name, Description (optional)
   - Cron Expression (with live preview of next 3 fire times)
   - Timezone (IANA timezone name)
   - Repo Path, Base Branch
   - Agent Type (Claude/Gemini)
   - Prompt text
   - Flags (optional CLI flags)
   - Max Duration (seconds, minimum 60)
   - Cleanup Worktree toggle
3. Click **Create**

### Managing Jobs

- **View details**: Click a job in the sidebar → info grid + prompt + run history table
- **Edit**: Click Edit to modify any field (changes take effect on next run)
- **Pause/Resume**: Toggle to stop/restart scheduling
- **Delete**: Permanently removes job and all run history (cascade)
- **Run history**: Table showing scheduled time, status (color-coded badge), duration, exit reason, and clickable session link

### One-Time Jobs (API-only)

Jobs can also be triggered as one-shot runs via the API with `trigger_type: 'api'`, useful for CI/CD integration or programmatic agent launches.

---

## Suggested MkDocs Page Structure

### Title: "Scheduled Jobs"

1. **Introduction** — What scheduled jobs are and use cases
   - Nightly test suites, recurring maintenance, automated code review
2. **Creating a Scheduled Job** — Step-by-step with modal screenshot
   - All fields explained in a table
   - Live cron preview feature
3. **Cron Schedule Reference**
   - Five-field format diagram
   - Special characters (*, comma, dash, slash)
   - Common examples table
   - Timezone support
4. **Managing Jobs** — View, edit, pause/resume, delete
   - Sidebar indicators (status dot, paused badge)
5. **Run History** — What's tracked per run
   - Status badges: pending, running, completed, killed, failed
   - Duration, exit reason, session link
6. **How It Works** — Architecture
   - Scheduler polling, worktree creation, agent launch, watchdog, cleanup
7. **Configuration** — Global and per-job settings tables
   - `CORAL_MAX_CONCURRENT_JOBS`, poll intervals, worktree path pattern
8. **API Reference** — Endpoint table
   - CRUD for jobs, toggle pause, run history, cron validation

### Screenshots to Include

- Create Scheduled Job modal with cron preview
- Job detail view showing info grid, prompt, and run history
- Sidebar showing scheduled jobs with status indicators
- Run history table with color-coded status badges

### Code Examples

- Common cron expressions with explanations
- API curl examples for creating jobs and triggering one-shot runs

---

## Important Details for Technical Writer

1. **Cron parser**: Coral includes its own pure-Python cron parser (`cron_parser.py`) — no external cron library dependency.
2. **OR logic for day fields**: When both day-of-month and day-of-week are non-wildcard, the job fires when EITHER matches (OR, not AND).
3. **Worktree isolation**: Each run gets a completely fresh worktree branched from `base_branch`. This prevents scheduled work from conflicting with manual development.
4. **Max concurrent jobs**: Controlled by `CORAL_MAX_CONCURRENT_JOBS` env var (default 5). Excess jobs queue until a slot opens.
5. **Session tagging**: Scheduled job sessions can be filtered in history view. The `is_job` flag in `live_sessions` marks them.
6. **Cascade delete**: Deleting a job removes ALL run history and session links permanently.
7. **Timezone handling**: All cron evaluation happens in the configured timezone. Default is UTC.
8. **One-time jobs**: The `scheduled_runs` table has a `trigger_type` column (`cron` or `api`) and optional `webhook_url` for callback on completion.
