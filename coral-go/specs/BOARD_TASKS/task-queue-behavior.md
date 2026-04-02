# Task Queue Behavior

Updated: 2026-04-02

Describes the runtime behavior of the board task queue — how tasks are claimed, how agents are notified, and how the system enforces sequential work.

## Sequential Claiming

Agents must complete their current task before claiming a new one. This prevents agents from batch-claiming all assigned tasks at once and ensures focused, sequential work.

### Rules

1. **One active task per agent** — `ClaimTask` checks if the subscriber has any `in_progress` tasks. If they do, the claim is rejected with HTTP 409 Conflict and the message: `"complete your current task before claiming a new one"`.

2. **No specific task claiming** — The CLI command `coral-board task claim` does not accept a task ID. It always claims the next best available task by priority (critical > high > medium > low), then by ID (oldest first).

3. **Assigned tasks first** — When claiming, tasks assigned to the subscriber are prioritized over unassigned tasks. An agent will always get their assigned work before picking up open tasks from the pool.

4. **Claim shows full details** — On successful claim, the CLI prints the task title, priority, and full body (instructions). Agents immediately see what they need to do.

### Atomicity

The claim uses a `NOT EXISTS` subquery within the UPDATE statement itself. SQLite's single-writer model guarantees this is atomic — no TOCTOU race is possible.

```sql
UPDATE board_tasks
SET status = 'in_progress', assigned_to = ?, claimed_at = ?
WHERE id = ? AND board_id = ? AND status = 'pending'
AND NOT EXISTS (
    SELECT 1 FROM board_tasks
    WHERE board_id = ? AND assigned_to = ? AND status = 'in_progress'
)
```

## Task Detail

Agents can view their current in-progress task at any time:

```bash
coral-board task current
```

This calls `POST /api/board/{project}/tasks/current` with the subscriber ID and returns the full task object (title, body, priority, status, timestamps).

## Notification System

Task notifications use two channels: **board audit messages** for the audit trail and **direct terminal nudges** for immediate agent notification.

### Board Audit Messages

All task state changes post a message to the board from the sender `"Coral Task Queue"`:

```
[Task #4 claimed by Frontend Dev] Fix auth bypass
[Task #4 completed by Frontend Dev] Fixed — added CSRF token validation
@Lead Developer You have tasks available — run 'coral-board task claim' to start
```

**These messages do not count as unread.** Messages from `"Coral Task Queue"` are excluded from `CheckUnread` and `GetAllUnreadCounts` across all receive modes (all, mentions, group). This prevents the board notifier from sending redundant nudges for task queue activity.

The messages still appear when agents run `coral-board read` — they serve as an audit trail, not a notification mechanism.

### Direct Terminal Nudges

Agents receive immediate terminal nudges (injected into their tmux session) in three scenarios:

#### 1. Task Created — Assigned Agent

When a task is created with an assignee, the assigned agent gets a direct nudge if they have no active in-progress task. If they're busy, the notification is deferred — they'll be nudged when they complete their current task.

```
You have tasks available. Run 'coral-board task claim' to start.
```

#### 2. Task Created — Unassigned

When an unassigned task is created, a random idle agent is selected and nudged. "Idle" means the agent is an active board subscriber with no in-progress tasks.

```sql
SELECT * FROM board_subscribers
WHERE project = ? AND is_active = 1 AND session_name != ''
AND subscriber_id NOT IN (
    SELECT assigned_to FROM board_tasks
    WHERE board_id = ? AND status = 'in_progress' AND assigned_to IS NOT NULL
)
ORDER BY RANDOM() LIMIT 1
```

If no idle agents exist, no nudge is sent. The task will be picked up when an agent finishes their current work (see below).

#### 3. Task Completed — Next Task Available

When an agent completes a task, the system checks if they have more pending tasks (assigned to them, or unassigned). If so:

1. A board audit message is posted: `@Agent You have tasks available — run 'coral-board task claim' to start`
2. A direct terminal nudge is sent to the agent's session

This creates the claim-complete-claim loop: agents are continuously fed work as long as tasks remain in the queue.

### Nudge Message

All nudges use the same text (defined as `taskNudge` const):

```
You have tasks available. Run 'coral-board task claim' to start.
```

The nudge intentionally does not include task IDs or titles. This prevents agents from attempting to claim specific tasks (which the CLI doesn't support) and avoids stale references if another agent claims the task between the nudge and the claim attempt.

### Deferred Notifications

When a task is assigned to an agent who already has an active task, the board message is posted without an `@mention`:

```
[Task #5 (high) assigned to Lead Developer — notification deferred while they have an active task] Fix auth bypass
```

The agent will be nudged when they complete their current task via the completion handler's next-task check.

## Flow Diagram

```
Task Created (assigned)
  ├── Post audit message to board (Coral Task Queue sender)
  ├── Agent idle? → Direct terminal nudge
  └── Agent busy? → Deferred (nudge comes on completion)

Task Created (unassigned)
  ├── Post audit message to board (Coral Task Queue sender)
  └── Find random idle agent → Direct terminal nudge

Agent runs: coral-board task claim
  ├── Has active task? → 409 "complete your current task"
  ├── Assigned task available? → Claim it, return full details
  ├── Unassigned task available? → Claim it, return full details
  └── Nothing available? → "No available tasks"

Agent runs: coral-board task complete <id>
  ├── Mark task completed, post audit message
  ├── More tasks pending? → Post audit + direct terminal nudge
  └── No more tasks? → Done, agent waits
```

## CLI Commands

```bash
coral-board task add "title" [--body "details"] [--priority P] [--assignee "Agent"]
coral-board task list
coral-board task claim              # Claim next available (no task ID argument)
coral-board task current            # Show current in-progress task
coral-board task complete <id> [--message "note"]
coral-board task cancel <id>
coral-board task reassign <id> [--to "Agent"]
```

## API Endpoints

| Method | Path | Description |
|--------|------|-------------|
| POST | `/api/board/{project}/tasks` | Create task |
| GET | `/api/board/{project}/tasks` | List all tasks |
| POST | `/api/board/{project}/tasks/claim` | Claim next available task (409 if busy) |
| POST | `/api/board/{project}/tasks/current` | Get current in-progress task |
| POST | `/api/board/{project}/tasks/{id}/complete` | Complete a task |
| POST | `/api/board/{project}/tasks/{id}/cancel` | Cancel a task |
| POST | `/api/board/{project}/tasks/{id}/reassign` | Reassign a task |
