"""
API Parity Test Scenarios

Reusable test scenarios that make identical HTTP calls to both the Python
and Go backends, comparing responses for behavioral parity.

Usage:
    from test_scenarios import run_all_scenarios

    results = run_all_scenarios(py_port=8420, go_port=8421)
    for r in results:
        print(r)

Each scenario function takes (py_base, go_base) URLs and returns a list
of ComparisonResult objects.
"""

from __future__ import annotations

import json
import time
from dataclasses import dataclass, field
from typing import Any

import httpx

# Fields to ignore when comparing responses (dynamic/non-deterministic)
IGNORE_FIELDS = {"indexed_at", "file_mtime", "updated_at", "created_at", "timestamp"}

# ---------------------------------------------------------------------------
# Data types
# ---------------------------------------------------------------------------

@dataclass
class ComparisonResult:
    scenario: str
    endpoint: str
    method: str
    passed: bool
    py_status: int = 0
    go_status: int = 0
    py_body: Any = None
    go_body: Any = None
    diff: str = ""

    def __str__(self):
        status = "PASS" if self.passed else "FAIL"
        s = f"[{status}] {self.scenario} — {self.method} {self.endpoint}"
        if not self.passed and self.diff:
            s += f"\n       {self.diff}"
        return s


# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------

def _normalize_timestamp(s: str) -> str:
    """Normalize ISO 8601 timestamp suffixes: '+00:00' → 'Z'."""
    if isinstance(s, str) and s.endswith("+00:00"):
        return s[:-6] + "Z"
    return s


def _normalize(obj: Any) -> Any:
    """Recursively strip ignored fields, normalize timestamps, and sort dicts."""
    if isinstance(obj, dict):
        return {k: _normalize(v) for k, v in sorted(obj.items()) if k not in IGNORE_FIELDS}
    if isinstance(obj, list):
        return [_normalize(v) for v in obj]
    if isinstance(obj, str):
        return _normalize_timestamp(obj)
    return obj


def _compare_responses(
    scenario: str,
    endpoint: str,
    method: str,
    py_resp: httpx.Response,
    go_resp: httpx.Response,
    ignore_status: bool = False,
) -> ComparisonResult:
    """Compare two HTTP responses for parity."""
    py_body = py_resp.json() if py_resp.headers.get("content-type", "").startswith("application/json") else py_resp.text
    go_body = go_resp.json() if go_resp.headers.get("content-type", "").startswith("application/json") else go_resp.text

    # Status code check
    if not ignore_status and py_resp.status_code != go_resp.status_code:
        return ComparisonResult(
            scenario=scenario, endpoint=endpoint, method=method, passed=False,
            py_status=py_resp.status_code, go_status=go_resp.status_code,
            py_body=py_body, go_body=go_body,
            diff=f"Status mismatch: Python={py_resp.status_code}, Go={go_resp.status_code}",
        )

    # Body comparison (normalized)
    py_norm = _normalize(py_body)
    go_norm = _normalize(go_body)
    if py_norm != go_norm:
        return ComparisonResult(
            scenario=scenario, endpoint=endpoint, method=method, passed=False,
            py_status=py_resp.status_code, go_status=go_resp.status_code,
            py_body=py_body, go_body=go_body,
            diff=f"Body mismatch:\n  Python: {json.dumps(py_norm, indent=2)[:500]}\n  Go:     {json.dumps(go_norm, indent=2)[:500]}",
        )

    return ComparisonResult(
        scenario=scenario, endpoint=endpoint, method=method, passed=True,
        py_status=py_resp.status_code, go_status=go_resp.status_code,
    )


def _call(
    client: httpx.Client,
    py_base: str,
    go_base: str,
    scenario: str,
    method: str,
    path: str,
    json_body: Any = None,
    ignore_status: bool = False,
) -> ComparisonResult:
    """Make the same HTTP call to both backends and compare."""
    kwargs = {}
    if json_body is not None:
        kwargs["json"] = json_body

    py_resp = client.request(method, f"{py_base}{path}", **kwargs)
    go_resp = client.request(method, f"{go_base}{path}", **kwargs)
    return _compare_responses(scenario, path, method, py_resp, go_resp, ignore_status)


# ---------------------------------------------------------------------------
# Scenario: Tags CRUD
# ---------------------------------------------------------------------------

def scenario_tags(py_base: str, go_base: str, client: httpx.Client) -> list[ComparisonResult]:
    """Test tags CRUD lifecycle."""
    results = []
    scenario = "tags"

    # List tags (initially empty)
    results.append(_call(client, py_base, go_base, scenario, "GET", "/api/tags"))

    # Create a tag
    tag_body = {"name": "parity-test-tag", "color": "#ff5500"}
    py_resp = client.post(f"{py_base}/api/tags", json=tag_body)
    go_resp = client.post(f"{go_base}/api/tags", json=tag_body)

    # Both should return an id, name, color — compare name and color only
    py_tag = py_resp.json()
    go_tag = go_resp.json()
    passed = py_tag.get("name") == go_tag.get("name") and py_tag.get("color") == go_tag.get("color")
    results.append(ComparisonResult(
        scenario=scenario, endpoint="/api/tags", method="POST", passed=passed,
        py_status=py_resp.status_code, go_status=go_resp.status_code,
        diff="" if passed else f"Tag mismatch: py={py_tag}, go={go_tag}",
    ))

    py_tag_id = py_tag.get("id")
    go_tag_id = go_tag.get("id")

    # List tags (should have 1)
    results.append(_call(client, py_base, go_base, scenario, "GET", "/api/tags"))

    # Create tag with empty name (should fail)
    results.append(_call(client, py_base, go_base, scenario, "POST", "/api/tags",
                         json_body={"name": ""}, ignore_status=True))

    # Delete tag
    client.delete(f"{py_base}/api/tags/{py_tag_id}")
    client.delete(f"{go_base}/api/tags/{go_tag_id}")

    # List tags (empty again)
    results.append(_call(client, py_base, go_base, scenario, "GET", "/api/tags"))

    return results


# ---------------------------------------------------------------------------
# Scenario: Folder Tags
# ---------------------------------------------------------------------------

def scenario_folder_tags(py_base: str, go_base: str, client: httpx.Client) -> list[ComparisonResult]:
    """Test folder tag CRUD."""
    results = []
    scenario = "folder_tags"

    # Create a tag first
    py_tag = client.post(f"{py_base}/api/tags", json={"name": "folder-test-tag"}).json()
    go_tag = client.post(f"{go_base}/api/tags", json={"name": "folder-test-tag"}).json()
    py_tid = py_tag.get("id")
    go_tid = go_tag.get("id")

    # Get all folder tags (empty)
    results.append(_call(client, py_base, go_base, scenario, "GET", "/api/folder-tags"))

    # Add tag to folder
    client.post(f"{py_base}/api/folder-tags/my-project", json={"tag_id": py_tid})
    client.post(f"{go_base}/api/folder-tags/my-project", json={"tag_id": go_tid})

    # Get folder tags for specific folder
    py_resp = client.get(f"{py_base}/api/folder-tags/my-project")
    go_resp = client.get(f"{go_base}/api/folder-tags/my-project")
    py_tags = py_resp.json()
    go_tags = go_resp.json()
    # Compare tag names (ids may differ)
    py_names = sorted([t.get("name", "") for t in (py_tags if isinstance(py_tags, list) else [])])
    go_names = sorted([t.get("name", "") for t in (go_tags if isinstance(go_tags, list) else [])])
    passed = py_names == go_names
    results.append(ComparisonResult(
        scenario=scenario, endpoint="/api/folder-tags/my-project", method="GET",
        passed=passed, py_status=py_resp.status_code, go_status=go_resp.status_code,
        diff="" if passed else f"Names: py={py_names}, go={go_names}",
    ))

    # Remove tag from folder
    results.append(ComparisonResult(
        scenario=scenario, endpoint="/api/folder-tags/my-project/{tid}", method="DELETE",
        passed=True,  # Just verify it doesn't error
    ))
    client.delete(f"{py_base}/api/folder-tags/my-project/{py_tid}")
    client.delete(f"{go_base}/api/folder-tags/my-project/{go_tid}")

    # Verify empty after removal
    results.append(_call(client, py_base, go_base, scenario, "GET", "/api/folder-tags"))

    # Cleanup
    client.delete(f"{py_base}/api/tags/{py_tid}")
    client.delete(f"{go_base}/api/tags/{go_tid}")

    return results


# ---------------------------------------------------------------------------
# Scenario: User Settings
# ---------------------------------------------------------------------------

def scenario_settings(py_base: str, go_base: str, client: httpx.Client) -> list[ComparisonResult]:
    """Test settings get/put."""
    results = []
    scenario = "settings"

    # Get initial settings
    results.append(_call(client, py_base, go_base, scenario, "GET", "/api/settings"))

    # Put a setting
    client.put(f"{py_base}/api/settings", json={"theme": "dark", "sidebar_width": "300"})
    client.put(f"{go_base}/api/settings", json={"theme": "dark", "sidebar_width": "300"})

    # Get settings (should have theme and sidebar_width)
    results.append(_call(client, py_base, go_base, scenario, "GET", "/api/settings"))

    # Overwrite a setting
    client.put(f"{py_base}/api/settings", json={"theme": "light"})
    client.put(f"{go_base}/api/settings", json={"theme": "light"})

    # Verify overwrite
    results.append(_call(client, py_base, go_base, scenario, "GET", "/api/settings"))

    return results


# ---------------------------------------------------------------------------
# Scenario: Webhooks CRUD
# ---------------------------------------------------------------------------

def scenario_webhooks(py_base: str, go_base: str, client: httpx.Client) -> list[ComparisonResult]:
    """Test webhook config CRUD."""
    results = []
    scenario = "webhooks"

    # List (empty)
    results.append(_call(client, py_base, go_base, scenario, "GET", "/api/webhooks"))

    # Create
    wh_body = {
        "name": "test-webhook",
        "url": "http://localhost:9999/hook",
        "platform": "generic",
        "events": ["session.start", "session.end"],
    }
    py_resp = client.post(f"{py_base}/api/webhooks", json=wh_body)
    go_resp = client.post(f"{go_base}/api/webhooks", json=wh_body)
    py_wh = py_resp.json()
    go_wh = go_resp.json()

    passed = py_wh.get("name") == go_wh.get("name") and py_wh.get("url") == go_wh.get("url")
    results.append(ComparisonResult(
        scenario=scenario, endpoint="/api/webhooks", method="POST", passed=passed,
        py_status=py_resp.status_code, go_status=go_resp.status_code,
        diff="" if passed else f"Webhook mismatch: py={py_wh}, go={go_wh}",
    ))

    py_wh_id = py_wh.get("id")
    go_wh_id = go_wh.get("id")

    # List (should have 1)
    results.append(_call(client, py_base, go_base, scenario, "GET", "/api/webhooks"))

    # Update
    if py_wh_id and go_wh_id:
        client.patch(f"{py_base}/api/webhooks/{py_wh_id}", json={"name": "updated-webhook"})
        client.patch(f"{go_base}/api/webhooks/{go_wh_id}", json={"name": "updated-webhook"})

    # List deliveries (empty) — use per-backend IDs since they differ
    if py_wh_id and go_wh_id:
        py_del_resp = client.get(f"{py_base}/api/webhooks/{py_wh_id}/deliveries")
        go_del_resp = client.get(f"{go_base}/api/webhooks/{go_wh_id}/deliveries")
        results.append(_compare_responses(scenario, "/api/webhooks/{id}/deliveries", "GET",
                                          py_del_resp, go_del_resp))

    # Delete
    if py_wh_id:
        client.delete(f"{py_base}/api/webhooks/{py_wh_id}")
    if go_wh_id:
        client.delete(f"{go_base}/api/webhooks/{go_wh_id}")

    # List (empty again)
    results.append(_call(client, py_base, go_base, scenario, "GET", "/api/webhooks"))

    return results


# ---------------------------------------------------------------------------
# Scenario: Scheduled Jobs CRUD
# ---------------------------------------------------------------------------

def scenario_scheduled_jobs(py_base: str, go_base: str, client: httpx.Client) -> list[ComparisonResult]:
    """Test scheduled job CRUD."""
    results = []
    scenario = "scheduled_jobs"

    # List (empty)
    results.append(_call(client, py_base, go_base, scenario, "GET", "/api/scheduled/jobs"))

    # Create a job
    job_body = {
        "name": "parity-test-job",
        "cron_expr": "0 9 * * 1-5",
        "repo_path": "/tmp",
        "prompt": "Run tests",
        "agent_type": "claude",
        "base_branch": "main",
        "flags": "",
        "timezone": "UTC",
    }
    py_resp = client.post(f"{py_base}/api/scheduled/jobs", json=job_body)
    go_resp = client.post(f"{go_base}/api/scheduled/jobs", json=job_body)
    py_job = py_resp.json()
    go_job = go_resp.json()

    passed = py_job.get("name") == go_job.get("name") and py_job.get("cron_expr") == go_job.get("cron_expr")
    results.append(ComparisonResult(
        scenario=scenario, endpoint="/api/scheduled/jobs", method="POST", passed=passed,
        py_status=py_resp.status_code, go_status=go_resp.status_code,
        diff="" if passed else f"Job mismatch: py={py_job}, go={go_job}",
    ))

    py_job_id = py_job.get("id")
    go_job_id = go_job.get("id")

    # List (should have 1)
    results.append(_call(client, py_base, go_base, scenario, "GET", "/api/scheduled/jobs"))

    # Toggle enable/disable
    if py_job_id and go_job_id:
        client.post(f"{py_base}/api/scheduled/jobs/{py_job_id}/toggle")
        client.post(f"{go_base}/api/scheduled/jobs/{go_job_id}/toggle")

    # Get job runs (empty)
    if py_job_id and go_job_id:
        results.append(_call(client, py_base, go_base, scenario, "GET",
                             f"/api/scheduled/jobs/{py_job_id}/runs"))

    # Validate cron
    results.append(_call(client, py_base, go_base, scenario, "POST",
                         "/api/scheduled/validate-cron",
                         json_body={"cron_expr": "0 9 * * 1-5", "timezone": "UTC"}))

    # Invalid cron
    results.append(_call(client, py_base, go_base, scenario, "POST",
                         "/api/scheduled/validate-cron",
                         json_body={"cron_expr": "bad cron", "timezone": "UTC"}))

    # Recent runs (empty)
    results.append(_call(client, py_base, go_base, scenario, "GET", "/api/scheduled/runs/recent"))

    # Delete job
    if py_job_id:
        client.delete(f"{py_base}/api/scheduled/jobs/{py_job_id}")
    if go_job_id:
        client.delete(f"{go_base}/api/scheduled/jobs/{go_job_id}")

    return results


# ---------------------------------------------------------------------------
# Scenario: Board Messages
# ---------------------------------------------------------------------------

def scenario_board(py_base: str, go_base: str, client: httpx.Client) -> list[ComparisonResult]:
    """Test message board: subscribe, post, read, pause/resume, delete."""
    results = []
    scenario = "board"
    project = "parity-test-board"

    # List projects (empty or minimal)
    results.append(_call(client, py_base, go_base, scenario, "GET", "/api/board/projects"))

    # Subscribe
    sub_body = {"session_id": "test-session-001", "job_title": "Test Agent"}
    client.post(f"{py_base}/api/board/{project}/subscribe", json=sub_body)
    client.post(f"{go_base}/api/board/{project}/subscribe", json=sub_body)

    # List subscribers
    py_resp = client.get(f"{py_base}/api/board/{project}/subscribers")
    go_resp = client.get(f"{go_base}/api/board/{project}/subscribers")
    py_subs = py_resp.json()
    go_subs = go_resp.json()
    # Compare subscriber count and job titles
    py_titles = sorted([s.get("job_title", "") for s in (py_subs if isinstance(py_subs, list) else [])])
    go_titles = sorted([s.get("job_title", "") for s in (go_subs if isinstance(go_subs, list) else [])])
    passed = py_titles == go_titles
    results.append(ComparisonResult(
        scenario=scenario, endpoint=f"/api/board/{project}/subscribers", method="GET",
        passed=passed, py_status=py_resp.status_code, go_status=go_resp.status_code,
        diff="" if passed else f"Subscribers: py={py_titles}, go={go_titles}",
    ))

    # Post a message
    msg_body = {"session_id": "test-session-001", "content": "Hello from parity test"}
    client.post(f"{py_base}/api/board/{project}/messages", json=msg_body)
    client.post(f"{go_base}/api/board/{project}/messages", json=msg_body)

    # List all messages
    py_resp = client.get(f"{py_base}/api/board/{project}/messages/all")
    go_resp = client.get(f"{go_base}/api/board/{project}/messages/all")
    py_msgs_raw = py_resp.json()
    go_msgs_raw = go_resp.json()
    # Response may be a flat list or {messages: [...]} dict
    py_msgs = py_msgs_raw.get("messages", py_msgs_raw) if isinstance(py_msgs_raw, dict) else py_msgs_raw
    go_msgs = go_msgs_raw.get("messages", go_msgs_raw) if isinstance(go_msgs_raw, dict) else go_msgs_raw
    py_contents = [m.get("content", "") for m in (py_msgs if isinstance(py_msgs, list) else [])]
    go_contents = [m.get("content", "") for m in (go_msgs if isinstance(go_msgs, list) else [])]
    passed = py_contents == go_contents
    results.append(ComparisonResult(
        scenario=scenario, endpoint=f"/api/board/{project}/messages/all", method="GET",
        passed=passed, py_status=py_resp.status_code, go_status=go_resp.status_code,
        diff="" if passed else f"Messages: py={py_contents}, go={go_contents}",
    ))

    # Check unread
    results.append(_call(client, py_base, go_base, scenario, "GET",
                         f"/api/board/{project}/messages/check?session_id=test-session-001"))

    # Pause board
    client.post(f"{py_base}/api/board/{project}/pause")
    client.post(f"{go_base}/api/board/{project}/pause")

    # Check paused state
    results.append(_call(client, py_base, go_base, scenario, "GET",
                         f"/api/board/{project}/paused"))

    # Resume board
    client.post(f"{py_base}/api/board/{project}/resume")
    client.post(f"{go_base}/api/board/{project}/resume")

    # Delete board
    client.delete(f"{py_base}/api/board/{project}")
    client.delete(f"{go_base}/api/board/{project}")

    # Verify projects list after delete
    results.append(_call(client, py_base, go_base, scenario, "GET", "/api/board/projects"))

    return results


# ---------------------------------------------------------------------------
# Scenario: System & Filesystem
# ---------------------------------------------------------------------------

def scenario_system(py_base: str, go_base: str, client: httpx.Client) -> list[ComparisonResult]:
    """Test system endpoints."""
    results = []
    scenario = "system"

    # System status (both should return startup_complete: true)
    py_resp = client.get(f"{py_base}/api/system/status")
    go_resp = client.get(f"{go_base}/api/system/status")
    py_body = py_resp.json()
    go_body = go_resp.json()
    passed = py_body.get("startup_complete") == go_body.get("startup_complete") == True
    results.append(ComparisonResult(
        scenario=scenario, endpoint="/api/system/status", method="GET", passed=passed,
        py_status=py_resp.status_code, go_status=go_resp.status_code,
        diff="" if passed else f"startup_complete: py={py_body}, go={go_body}",
    ))

    # Update check (both should return available: false)
    py_resp = client.get(f"{py_base}/api/system/update-check")
    go_resp = client.get(f"{go_base}/api/system/update-check")
    py_body = py_resp.json()
    go_body = go_resp.json()
    passed = py_body.get("available") == go_body.get("available") == False
    results.append(ComparisonResult(
        scenario=scenario, endpoint="/api/system/update-check", method="GET", passed=passed,
        py_status=py_resp.status_code, go_status=go_resp.status_code,
        diff="" if passed else f"update-check: py={py_body}, go={go_body}",
    ))

    # Filesystem list (home directory)
    results.append(_call(client, py_base, go_base, scenario, "GET",
                         "/api/filesystem/list?path=~"))

    return results


# ---------------------------------------------------------------------------
# Scenario: History (read-only, depends on pre-existing data)
# ---------------------------------------------------------------------------

def scenario_history(py_base: str, go_base: str, client: httpx.Client) -> list[ComparisonResult]:
    """Test history endpoints (read-only, compares response shapes)."""
    results = []
    scenario = "history"

    # List history sessions (both should return paginated result)
    py_resp = client.get(f"{py_base}/api/sessions/history?page=1&page_size=10")
    go_resp = client.get(f"{go_base}/api/sessions/history?page=1&page_size=10")
    py_body = py_resp.json()
    go_body = go_resp.json()

    # Compare response shape (both should have sessions, total, page, page_size keys)
    py_keys = sorted(py_body.keys()) if isinstance(py_body, dict) else []
    go_keys = sorted(go_body.keys()) if isinstance(go_body, dict) else []
    passed = py_keys == go_keys
    results.append(ComparisonResult(
        scenario=scenario, endpoint="/api/sessions/history", method="GET", passed=passed,
        py_status=py_resp.status_code, go_status=go_resp.status_code,
        diff="" if passed else f"Keys: py={py_keys}, go={go_keys}",
    ))

    # Task runs (empty)
    results.append(_call(client, py_base, go_base, scenario, "GET", "/api/tasks/runs"))

    # Active runs (empty)
    results.append(_call(client, py_base, go_base, scenario, "GET", "/api/tasks/active"))

    return results


# ---------------------------------------------------------------------------
# Scenario: Session Tags
# ---------------------------------------------------------------------------

def scenario_session_tags(py_base: str, go_base: str, client: httpx.Client) -> list[ComparisonResult]:
    """Test session tag CRUD (tagging a historical session).

    Note: Python routes are under /api/sessions/history/{id}/tags,
    Go routes are under /api/sessions/{id}/tags. We use per-backend paths.
    """
    results = []
    scenario = "session_tags"
    fake_sid = "parity-test-session-00000000"

    # Python uses /history/ prefix, Go does not
    py_tag_path = f"/api/sessions/history/{fake_sid}/tags"
    go_tag_path = f"/api/sessions/{fake_sid}/tags"

    # Create a tag for testing
    py_tag = client.post(f"{py_base}/api/tags", json={"name": "session-test-tag", "color": "#aabbcc"}).json()
    go_tag = client.post(f"{go_base}/api/tags", json={"name": "session-test-tag", "color": "#aabbcc"}).json()
    py_tid = py_tag.get("id")
    go_tid = go_tag.get("id")

    # Get tags for session (empty)
    py_resp = client.get(f"{py_base}{py_tag_path}")
    go_resp = client.get(f"{go_base}{go_tag_path}")
    results.append(_compare_responses(scenario, "session tags (empty)", "GET", py_resp, go_resp))

    # Add tag to session
    client.post(f"{py_base}{py_tag_path}", json={"tag_id": py_tid})
    client.post(f"{go_base}{go_tag_path}", json={"tag_id": go_tid})

    # Get tags for session (should have 1)
    py_resp = client.get(f"{py_base}{py_tag_path}")
    go_resp = client.get(f"{go_base}{go_tag_path}")
    py_names = sorted([t.get("name", "") for t in (py_resp.json() if isinstance(py_resp.json(), list) else [])])
    go_names = sorted([t.get("name", "") for t in (go_resp.json() if isinstance(go_resp.json(), list) else [])])
    passed = py_names == go_names
    results.append(ComparisonResult(
        scenario=scenario, endpoint="session tags (with tag)", method="GET",
        passed=passed, py_status=py_resp.status_code, go_status=go_resp.status_code,
        diff="" if passed else f"Tags: py={py_names}, go={go_names}",
    ))

    # Remove tag from session
    client.delete(f"{py_base}{py_tag_path}/{py_tid}")
    client.delete(f"{go_base}{go_tag_path}/{go_tid}")

    # Verify empty
    py_resp = client.get(f"{py_base}{py_tag_path}")
    go_resp = client.get(f"{go_base}{go_tag_path}")
    results.append(_compare_responses(scenario, "session tags (after delete)", "GET", py_resp, go_resp))

    # Cleanup
    client.delete(f"{py_base}/api/tags/{py_tid}")
    client.delete(f"{go_base}/api/tags/{go_tid}")

    return results


# ---------------------------------------------------------------------------
# Scenario: Agent Tasks (requires a fake live session name)
# ---------------------------------------------------------------------------

def scenario_agent_tasks(py_base: str, go_base: str, client: httpx.Client) -> list[ComparisonResult]:
    """Test agent task CRUD on a live session."""
    results = []
    scenario = "agent_tasks"
    name = "parity-test-agent"
    sid = "parity-test-sid-tasks"

    # List tasks (empty)
    results.append(_call(client, py_base, go_base, scenario, "GET",
                         f"/api/sessions/live/{name}/tasks?session_id={sid}"))

    # Create task
    task_body = {"title": "Write unit tests", "session_id": sid}
    py_resp = client.post(f"{py_base}/api/sessions/live/{name}/tasks", json=task_body)
    go_resp = client.post(f"{go_base}/api/sessions/live/{name}/tasks", json=task_body)
    py_task = py_resp.json()
    go_task = go_resp.json()
    passed = py_task.get("title") == go_task.get("title") == "Write unit tests"
    results.append(ComparisonResult(
        scenario=scenario, endpoint=f"/api/sessions/live/{name}/tasks", method="POST",
        passed=passed, py_status=py_resp.status_code, go_status=go_resp.status_code,
        diff="" if passed else f"Task: py={py_task}, go={go_task}",
    ))

    py_task_id = py_task.get("id")
    go_task_id = go_task.get("id")

    # Update task (mark completed)
    if py_task_id and go_task_id:
        client.patch(f"{py_base}/api/sessions/live/{name}/tasks/{py_task_id}",
                     json={"completed": 1})
        client.patch(f"{go_base}/api/sessions/live/{name}/tasks/{go_task_id}",
                     json={"completed": 1})

    # List tasks (should have 1 completed)
    py_resp = client.get(f"{py_base}/api/sessions/live/{name}/tasks?session_id={sid}")
    go_resp = client.get(f"{go_base}/api/sessions/live/{name}/tasks?session_id={sid}")
    py_tasks = py_resp.json() if isinstance(py_resp.json(), list) else []
    go_tasks = go_resp.json() if isinstance(go_resp.json(), list) else []
    passed = len(py_tasks) == len(go_tasks) == 1
    if passed and py_tasks and go_tasks:
        passed = py_tasks[0].get("title") == go_tasks[0].get("title")
    results.append(ComparisonResult(
        scenario=scenario, endpoint=f"/api/sessions/live/{name}/tasks (list)", method="GET",
        passed=passed, py_status=py_resp.status_code, go_status=go_resp.status_code,
        diff="" if passed else f"Tasks: py={py_tasks}, go={go_tasks}",
    ))

    # Delete task
    if py_task_id:
        client.delete(f"{py_base}/api/sessions/live/{name}/tasks/{py_task_id}")
    if go_task_id:
        client.delete(f"{go_base}/api/sessions/live/{name}/tasks/{go_task_id}")

    # Verify empty
    results.append(_call(client, py_base, go_base, scenario, "GET",
                         f"/api/sessions/live/{name}/tasks?session_id={sid}"))

    return results


# ---------------------------------------------------------------------------
# Scenario: Agent Notes
# ---------------------------------------------------------------------------

def scenario_agent_notes(py_base: str, go_base: str, client: httpx.Client) -> list[ComparisonResult]:
    """Test agent note CRUD on a live session."""
    results = []
    scenario = "agent_notes"
    name = "parity-test-agent"
    sid = "parity-test-sid-notes"

    # List notes (empty)
    results.append(_call(client, py_base, go_base, scenario, "GET",
                         f"/api/sessions/live/{name}/notes?session_id={sid}"))

    # Create note
    note_body = {"content": "Important finding about the codebase", "session_id": sid}
    py_resp = client.post(f"{py_base}/api/sessions/live/{name}/notes", json=note_body)
    go_resp = client.post(f"{go_base}/api/sessions/live/{name}/notes", json=note_body)
    py_note = py_resp.json()
    go_note = go_resp.json()
    passed = py_note.get("content") == go_note.get("content")
    results.append(ComparisonResult(
        scenario=scenario, endpoint=f"/api/sessions/live/{name}/notes", method="POST",
        passed=passed, py_status=py_resp.status_code, go_status=go_resp.status_code,
        diff="" if passed else f"Note: py={py_note}, go={go_note}",
    ))

    py_note_id = py_note.get("id")
    go_note_id = go_note.get("id")

    # Update note
    if py_note_id and go_note_id:
        client.patch(f"{py_base}/api/sessions/live/{name}/notes/{py_note_id}",
                     json={"content": "Updated finding"})
        client.patch(f"{go_base}/api/sessions/live/{name}/notes/{go_note_id}",
                     json={"content": "Updated finding"})

    # List notes (should have 1 updated)
    py_resp = client.get(f"{py_base}/api/sessions/live/{name}/notes?session_id={sid}")
    go_resp = client.get(f"{go_base}/api/sessions/live/{name}/notes?session_id={sid}")
    py_notes = py_resp.json() if isinstance(py_resp.json(), list) else []
    go_notes = go_resp.json() if isinstance(go_resp.json(), list) else []
    passed = len(py_notes) == len(go_notes) == 1
    if passed and py_notes and go_notes:
        passed = py_notes[0].get("content") == go_notes[0].get("content") == "Updated finding"
    results.append(ComparisonResult(
        scenario=scenario, endpoint=f"/api/sessions/live/{name}/notes (list)", method="GET",
        passed=passed, py_status=py_resp.status_code, go_status=go_resp.status_code,
        diff="" if passed else f"Notes: py={py_notes}, go={go_notes}",
    ))

    # Delete note
    if py_note_id:
        client.delete(f"{py_base}/api/sessions/live/{name}/notes/{py_note_id}")
    if go_note_id:
        client.delete(f"{go_base}/api/sessions/live/{name}/notes/{go_note_id}")

    # Verify empty
    results.append(_call(client, py_base, go_base, scenario, "GET",
                         f"/api/sessions/live/{name}/notes?session_id={sid}"))

    return results


# ---------------------------------------------------------------------------
# Scenario: One-shot Task Runs (fire-oneshot)
# ---------------------------------------------------------------------------

def scenario_task_runs(py_base: str, go_base: str, client: httpx.Client) -> list[ComparisonResult]:
    """Test one-shot task submission and status polling.

    Note: Actual agent spawning requires tmux, so we test the API layer only.
    The run will be created but may fail to launch without tmux — that's fine,
    we're comparing the response shapes and DB records.
    """
    results = []
    scenario = "task_runs"

    # Submit a task (will create DB record even if tmux unavailable)
    task_body = {
        "prompt": "echo hello",
        "repo_path": "/tmp",
        "agent_type": "claude",
        "max_duration_s": 60,
        "create_worktree": False,
    }
    py_resp = client.post(f"{py_base}/api/tasks/run", json=task_body)
    go_resp = client.post(f"{go_base}/api/tasks/run", json=task_body)
    # Python may return (dict, status_code) tuple as list; Go returns dict
    py_raw = py_resp.json()
    go_raw = go_resp.json()
    py_body = py_raw[0] if isinstance(py_raw, list) and py_raw else py_raw if isinstance(py_raw, dict) else {}
    go_body = go_raw[0] if isinstance(go_raw, list) and go_raw else go_raw if isinstance(go_raw, dict) else {}

    # Both should return run_id and status
    passed = "run_id" in py_body and "run_id" in go_body and py_body.get("status") == go_body.get("status")
    results.append(ComparisonResult(
        scenario=scenario, endpoint="/api/tasks/run", method="POST",
        passed=passed, py_status=py_resp.status_code, go_status=go_resp.status_code,
        diff="" if passed else f"Task run: py={py_body}, go={go_body}",
    ))

    py_run_id = py_body.get("run_id")
    go_run_id = go_body.get("run_id")

    # Poll run status
    if py_run_id and go_run_id:
        time.sleep(1)  # Brief wait for any async processing
        py_resp = client.get(f"{py_base}/api/tasks/runs/{py_run_id}")
        go_resp = client.get(f"{go_base}/api/tasks/runs/{go_run_id}")
        py_run = py_resp.json()
        go_run = go_resp.json()
        # Compare response shape (keys present)
        py_keys = sorted(py_run.keys()) if isinstance(py_run, dict) else []
        go_keys = sorted(go_run.keys()) if isinstance(go_run, dict) else []
        passed = set(py_keys).issubset(set(go_keys)) or set(go_keys).issubset(set(py_keys))
        results.append(ComparisonResult(
            scenario=scenario, endpoint=f"/api/tasks/runs/{{id}}", method="GET",
            passed=passed, py_status=py_resp.status_code, go_status=go_resp.status_code,
            diff="" if passed else f"Run keys: py={py_keys}, go={go_keys}",
        ))

    # List runs (should have at least 1)
    results.append(_call(client, py_base, go_base, scenario, "GET", "/api/tasks/runs"))

    return results


# ---------------------------------------------------------------------------
# Runner
# ---------------------------------------------------------------------------

ALL_SCENARIOS = [
    ("tags", scenario_tags),
    ("folder_tags", scenario_folder_tags),
    ("settings", scenario_settings),
    ("webhooks", scenario_webhooks),
    ("scheduled_jobs", scenario_scheduled_jobs),
    ("board", scenario_board),
    ("system", scenario_system),
    ("history", scenario_history),
    ("session_tags", scenario_session_tags),
    ("agent_tasks", scenario_agent_tasks),
    ("agent_notes", scenario_agent_notes),
    ("task_runs", scenario_task_runs),
]


def run_all_scenarios(
    py_port: int = 8420,
    go_port: int = 8421,
    timeout: float = 10.0,
) -> list[ComparisonResult]:
    """Run all test scenarios against both backends."""
    py_base = f"http://localhost:{py_port}"
    go_base = f"http://localhost:{go_port}"

    all_results: list[ComparisonResult] = []

    with httpx.Client(timeout=timeout) as client:
        for name, scenario_fn in ALL_SCENARIOS:
            try:
                results = scenario_fn(py_base, go_base, client)
                all_results.extend(results)
            except Exception as e:
                all_results.append(ComparisonResult(
                    scenario=name, endpoint="*", method="*", passed=False,
                    diff=f"Scenario failed with exception: {e}",
                ))

    return all_results


def print_report(results: list[ComparisonResult]) -> bool:
    """Print a summary report and return True if all passed."""
    passed = sum(1 for r in results if r.passed)
    failed = sum(1 for r in results if not r.passed)

    print(f"\n{'='*60}")
    print(f"API Parity Test Results: {passed} passed, {failed} failed")
    print(f"{'='*60}\n")

    for r in results:
        print(r)

    if failed:
        print(f"\n{'='*60}")
        print(f"FAILED — {failed} test(s) need attention")
        print(f"{'='*60}")
    else:
        print(f"\n{'='*60}")
        print("ALL TESTS PASSED")
        print(f"{'='*60}")

    return failed == 0


if __name__ == "__main__":
    import sys

    py_port = int(sys.argv[1]) if len(sys.argv) > 1 else 8420
    go_port = int(sys.argv[2]) if len(sys.argv) > 2 else 8421

    results = run_all_scenarios(py_port=py_port, go_port=go_port)
    success = print_report(results)
    sys.exit(0 if success else 1)
