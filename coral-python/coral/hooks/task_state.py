"""CLI entry point for the PostToolUse hook that syncs agent tasks to the Coral dashboard.

This is the Coral-side orchestration: read hook JSON from stdin, use the
appropriate agent to parse task events, and sync them to the dashboard via
the API.
"""

import json
import os
import sys

from coral.agents import get_agent
from coral.hooks.utils import cache_dir, coral_api, debug_log, resolve_agent_type, resolve_session_id


def _cache_write(task_id: str, subject: str) -> None:
    """Map an agent task ID to its subject for later lookup."""
    try:
        with open(os.path.join(cache_dir(), f"task_{task_id}"), "w") as f:
            f.write(subject)
    except OSError:
        pass


def _cache_read(task_id: str) -> str:
    try:
        with open(os.path.join(cache_dir(), f"task_{task_id}")) as f:
            return f.read().strip()
    except OSError:
        return ""


def main():
    """Read hook JSON from stdin, call Coral API to create/complete tasks."""
    try:
        raw = sys.stdin.read()
        d = json.loads(raw)
    except (json.JSONDecodeError, ValueError):
        return

    debug_log(f"INPUT: {raw[:500]}")

    port = os.environ.get("CORAL_PORT", "8420")
    base = f"http://localhost:{port}"

    session_id = resolve_session_id(d.get("session_id"))
    agent_type = resolve_agent_type(base, session_id)
    agent = get_agent(agent_type)

    agent_name = agent.resolve_agent_name(d)
    if not agent_name:
        return

    task_event = agent.parse_task_event(d)
    if task_event is None:
        return

    session_id = task_event.get("session_id")

    debug_log(f"task_event={json.dumps(task_event)} agent={agent_name}")

    if task_event["action"] == "create":
        subject = task_event["subject"]
        payload = {"title": subject}
        if session_id:
            payload["session_id"] = session_id
        coral_api(base, "POST", f"/api/sessions/live/{agent_name}/tasks", payload)
        # Cache using the ID so TaskUpdate can look it up later
        cache_id = task_event["task_id"]
        debug_log(f"TaskCreate: cache_id={cache_id} subject={subject}")
        if cache_id:
            _cache_write(cache_id, subject)

    elif task_event["action"] == "update":
        task_id = task_event["task_id"]
        subject = task_event["subject"]
        status = task_event["status"]

        # If this update includes a subject, cache it
        if task_id and subject:
            _cache_write(task_id, subject)

        if status in ("completed", "in_progress"):
            # Resolve the title: from event, from cache
            title = subject or (_cache_read(task_id) if task_id else "")
            debug_log(f"TaskUpdate {status}: task_id={task_id} resolved_title={title}")
            completed_value = 1 if status == "completed" else 2
            if title:
                qs = f"?session_id={session_id}" if session_id else ""
                tasks = coral_api(base, "GET", f"/api/sessions/live/{agent_name}/tasks{qs}")
                debug_log(f"Dashboard tasks: {json.dumps([t.get('title') for t in (tasks or [])])}")
                if tasks:
                    for t in tasks:
                        if t.get("title") == title and t.get("completed") != 1:
                            debug_log(f"Setting {status}: dashboard_id={t['id']}")
                            coral_api(base, "PATCH", f"/api/sessions/live/{agent_name}/tasks/{t['id']}", {"completed": completed_value})
                            break


if __name__ == "__main__":
    try:
        main()
    except Exception:
        pass  # Never block the agent
