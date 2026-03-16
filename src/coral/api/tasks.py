"""REST API for one-shot task runs (Jobs)."""

from __future__ import annotations

import os
from pathlib import Path
from typing import TYPE_CHECKING

from fastapi import APIRouter, Query

if TYPE_CHECKING:
    from coral.background_tasks.scheduler import JobScheduler
    from coral.store.schedule import ScheduleStore

router = APIRouter(prefix="/api/tasks", tags=["Tasks"])

# Module-level dependencies, set by web_server.py
store: ScheduleStore = None  # type: ignore[assignment]
scheduler: JobScheduler = None  # type: ignore[assignment]


@router.post("/run")
async def submit_task(body: dict):
    """Submit a one-shot agent task."""
    from coral.background_tasks.scheduler import ConcurrencyLimitError

    prompt = body.get("prompt", "").strip()
    if not prompt:
        return {"error": "'prompt' is required"}, 400

    repo_path = body.get("repo_path", "").strip()
    if not repo_path:
        return {"error": "'repo_path' is required"}, 400

    if not Path(repo_path).is_dir():
        return {"error": f"repo_path '{repo_path}' does not exist"}, 400

    flags = body.get("flags", "")
    if body.get("auto_accept", False):
        skip_flag = "--dangerously-skip-permissions"
        if skip_flag not in flags:
            flags = f"{skip_flag} {flags}".strip()

    webhook_url = body.get("webhook_url")
    if webhook_url:
        from coral.api.webhooks import _validate_url
        url_error = _validate_url(webhook_url, "generic")
        if url_error:
            return {"error": f"Invalid webhook_url: {url_error}"}, 400

    config = {
        "prompt": prompt,
        "repo_path": repo_path,
        "agent_type": body.get("agent_type", "claude"),
        "base_branch": body.get("base_branch", "main"),
        "create_worktree": body.get("create_worktree", True),
        "max_duration_s": body.get("max_duration_s", 3600),
        "cleanup_worktree": body.get("cleanup_worktree", True),
        "flags": flags,
        "display_name": body.get("display_name"),
        "webhook_url": webhook_url,
        "auto_accept": body.get("auto_accept", False),
        "max_auto_accepts": body.get("max_auto_accepts", 10),
    }

    try:
        run_id = await scheduler.fire_oneshot(config)
    except ConcurrencyLimitError as e:
        return {"error": str(e)}, 429

    return {"run_id": run_id, "status": "pending"}


@router.get("/runs/{run_id}")
async def get_run(run_id: int):
    """Poll status of a specific run."""
    run = await store.get_scheduled_run(run_id)
    if not run:
        return {"error": "Run not found"}, 404
    return {
        "id": run["id"],
        "status": run["status"],
        "trigger_type": run.get("trigger_type", "cron"),
        "session_id": run.get("session_id"),
        "display_name": run.get("display_name"),
        "worktree_path": run.get("worktree_path"),
        "scheduled_at": run.get("scheduled_at"),
        "started_at": run.get("started_at"),
        "finished_at": run.get("finished_at"),
        "exit_reason": run.get("exit_reason"),
        "error_msg": run.get("error_msg"),
    }


@router.post("/runs/{run_id}/kill")
async def kill_run(run_id: int):
    """Cancel a running or pending task."""
    ok = await scheduler.kill_run(run_id)
    if not ok:
        return {"error": "Run not found or not active"}, 404
    return {"ok": True}


@router.get("/runs")
async def list_runs(limit: int = Query(50, ge=1, le=200), status: str | None = None):
    """List recent one-shot task runs."""
    runs = await store.list_oneshot_runs(limit=limit, status=status)
    return {"runs": runs}


@router.get("/active")
async def list_active_runs():
    """List all active runs (both cron and API-triggered)."""
    runs = await store.list_active_runs()
    return {"runs": runs}
