"""CLI entry point for the PostToolUse hook that syncs Claude Code tasks to the Corral dashboard."""

import json
import os
import re
import sys
import urllib.request


def _api(base: str, method: str, path: str, data=None):
    url = base + path
    body = json.dumps(data).encode() if data else None
    req = urllib.request.Request(url, data=body, method=method)
    if body:
        req.add_header("Content-Type", "application/json")
    try:
        with urllib.request.urlopen(req, timeout=3) as r:
            return json.loads(r.read())
    except Exception:
        return None


def _cache_dir() -> str:
    d = os.path.join(os.environ.get("TMPDIR", "/tmp"), "corral_task_cache")
    os.makedirs(d, exist_ok=True)
    return d


def _cache_write(task_id: str, subject: str) -> None:
    """Map a Claude Code task ID to its subject for later lookup."""
    try:
        with open(os.path.join(_cache_dir(), f"task_{task_id}"), "w") as f:
            f.write(subject)
    except OSError:
        pass


def _cache_read(task_id: str) -> str:
    try:
        with open(os.path.join(_cache_dir(), f"task_{task_id}")) as f:
            return f.read().strip()
    except OSError:
        return ""


def _parse_response(resp) -> dict:
    """Extract task id and subject from tool_response (may be dict or string)."""
    result = {"task_id": "", "subject": ""}
    if isinstance(resp, dict):
        # Structured response: {"task": {"id": "9", "subject": "..."}} or {"taskId": "9", ...}
        task = resp.get("task", {})
        if isinstance(task, dict):
            result["task_id"] = str(task.get("id", ""))
            result["subject"] = task.get("subject", "")
        if not result["task_id"]:
            result["task_id"] = str(resp.get("taskId", ""))
    resp_str = resp if isinstance(resp, str) else json.dumps(resp)
    if not result["task_id"]:
        m = re.search(r"Task #(\d+)", resp_str)
        if m:
            result["task_id"] = m.group(1)
    return result


def _debug_log(msg: str) -> None:
    """Append to debug log if CORRAL_HOOK_DEBUG is set."""
    if not os.environ.get("CORRAL_HOOK_DEBUG"):
        return
    try:
        with open(os.path.join(_cache_dir(), "debug.log"), "a") as f:
            f.write(msg + "\n")
    except OSError:
        pass


def main():
    """Read hook JSON from stdin, call Corral API to create/complete tasks."""
    try:
        raw = sys.stdin.read()
        d = json.loads(raw)
    except (json.JSONDecodeError, ValueError):
        return

    _debug_log(f"INPUT: {raw[:500]}")

    port = os.environ.get("CORRAL_PORT", "8420")
    base = f"http://localhost:{port}"

    tool = d.get("tool_name", "")
    inp = d.get("tool_input", {}) if isinstance(d.get("tool_input"), dict) else {}
    task_id = str(inp.get("taskId", ""))
    subject = inp.get("subject", "")
    status = inp.get("status", "")
    resp_parsed = _parse_response(d.get("tool_response", ""))

    cwd = d.get("cwd", "")
    agent_name = os.path.basename(cwd.rstrip("/"))
    if not agent_name:
        return

    session_id = d.get("session_id")

    _debug_log(f"tool={tool} task_id={task_id} subject={subject} status={status} resp={resp_parsed} agent={agent_name}")

    if tool == "TaskCreate" and subject:
        payload = {"title": subject}
        if session_id:
            payload["session_id"] = session_id
        _api(base, "POST", f"/api/sessions/live/{agent_name}/tasks", payload)
        # Cache using the ID from the response so TaskUpdate can look it up
        cache_id = resp_parsed["task_id"] or task_id
        _debug_log(f"TaskCreate: cache_id={cache_id} subject={subject}")
        if cache_id:
            _cache_write(cache_id, subject)

    elif tool == "TaskUpdate":
        # If this update includes a subject, cache it
        if task_id and subject:
            _cache_write(task_id, subject)
        if status in ("completed", "in_progress"):
            # Resolve the title: from input, from cache, from response
            title = subject or resp_parsed.get("subject", "") or (_cache_read(task_id) if task_id else "")
            _debug_log(f"TaskUpdate {status}: task_id={task_id} resolved_title={title}")
            # completed=1 means done, completed=2 means in_progress
            completed_value = 1 if status == "completed" else 2
            if title:
                qs = f"?session_id={session_id}" if session_id else ""
                tasks = _api(base, "GET", f"/api/sessions/live/{agent_name}/tasks{qs}")
                _debug_log(f"Dashboard tasks: {json.dumps([t.get('title') for t in (tasks or [])])}")
                if tasks:
                    for t in tasks:
                        if t.get("title") == title and t.get("completed") != 1:
                            _debug_log(f"Setting {status}: dashboard_id={t['id']}")
                            _api(base, "PATCH", f"/api/sessions/live/{agent_name}/tasks/{t['id']}", {"completed": completed_value})
                            break


if __name__ == "__main__":
    try:
        main()
    except Exception:
        pass  # Never block Claude
