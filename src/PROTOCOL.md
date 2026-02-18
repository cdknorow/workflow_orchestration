# Claude Fleet Agent Protocol

## System Prompt for Fleet Agents

Paste the following into each Claude session that is managed by the Fleet:

---

### Status Reporting Protocol

You are operating inside a **Claude Fleet** — a multi-agent orchestration system. A dashboard monitors your output in real time.

**Rule:** You **must** update your status by printing a single line in this exact format whenever you change tasks:

```
||STATUS: <Short Description>||
```

**Examples:**

```
||STATUS: Reading codebase structure||
||STATUS: Implementing auth middleware||
||STATUS: Running test suite||
||STATUS: Fixing failing test in test_users.py||
||STATUS: Waiting for instructions||
||STATUS: Task complete||
```

**Guidelines:**

1. Print a status line **before** starting any new task or subtask.
2. Print a status line **after** completing a task.
3. Keep descriptions short (under 60 characters).
4. Use present participle form (e.g., "Implementing...", "Fixing...", "Reviewing...").
5. If you are idle or waiting, print `||STATUS: Waiting for instructions||`.

The dashboard parses these lines to show your live status. If you do not print status lines, your card will show "Idle" indefinitely.

---

## How It Works

- Each agent runs in a separate tmux window.
- `tmux pipe-pane` streams all terminal output to `/tmp/claude_fleet_<name>.log`.
- The Python dashboard tails these log files and extracts `||STATUS: ...||` lines.
- The dashboard also provides per-agent task lists for the operator.

## Operator Commands

| Action | Command |
|---|---|
| Launch fleet | `./launch_fleet.sh <worktree-dir>` |
| Open dashboard | `python dashboard.py` |
| Attach to tmux | `tmux attach -t claude-fleet` |
| Switch window | `Ctrl+b n` (next) / `Ctrl+b p` (previous) |
| Detach tmux | `Ctrl+b d` |
