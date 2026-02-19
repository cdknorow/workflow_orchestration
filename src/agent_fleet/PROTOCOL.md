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

### Summary Reporting Protocol

In addition to `||STATUS:||` lines, you **must** emit a summary line to describe your high-level goal. This is displayed in a **Goal** box on your agent card in the dashboard.

**Rule:** You **must** emit a `||SUMMARY:||` line **after receiving your very first message**, and again whenever your overall goal changes significantly.

**Format:**

```
||SUMMARY: <One-sentence description of your overall goal>||
```

**Examples:**

```
||SUMMARY: Implementing the user authentication feature end-to-end||
||SUMMARY: Debugging the flaky integration test in test_payments.py||
||SUMMARY: Refactoring the database layer to use the repository pattern||
```

**Guidelines:**

1. **Always** emit a summary after the **first user message** — no exceptions.
2. Emit again if your high-level goal shifts significantly.
3. Describes *what you are trying to accomplish* — not *what you are doing right now* (that is `||STATUS:||`).
4. Keep it under 120 characters (one line).
5. Update it infrequently — it should remain stable across many `||STATUS:||` updates.

If you do not emit a `||SUMMARY:||` line, the Goal box on your dashboard card will remain empty and the operator will have no context for what you are working on.

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
