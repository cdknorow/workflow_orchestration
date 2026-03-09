"""API routes for scheduled jobs and run history."""

from __future__ import annotations

import logging
from typing import TYPE_CHECKING

from fastapi import APIRouter

from corral.tools.cron_parser import validate_cron, next_fire_time
from datetime import datetime, timezone
from zoneinfo import ZoneInfo

if TYPE_CHECKING:
    from corral.store.schedule import ScheduleStore

log = logging.getLogger(__name__)

router = APIRouter(prefix="/api/scheduled", tags=["Scheduled Jobs"])

# Module-level dependency, set by web_server.py during app setup
store: ScheduleStore = None  # type: ignore[assignment]


@router.get("/jobs")
async def list_jobs():
    """List all scheduled jobs with their last run info."""
    jobs = await store.list_scheduled_jobs()
    # Enrich each job with its last run
    enriched = []
    for job in jobs:
        last_run = await store.get_last_run_for_job(job["id"])
        job["last_run"] = last_run
        # Compute next fire time
        try:
            tz = ZoneInfo(job["timezone"])
            now = datetime.now(tz)
            nft = next_fire_time(job["cron_expr"], now)
            job["next_fire_at"] = nft.isoformat()
        except Exception:
            job["next_fire_at"] = None
        enriched.append(job)
    return {"jobs": enriched}


@router.get("/jobs/{job_id}")
async def get_job(job_id: int):
    """Get a single scheduled job."""
    job = await store.get_scheduled_job(job_id)
    if not job:
        return {"error": "Job not found"}
    return job


@router.post("/jobs")
async def create_job(body: dict):
    """Create a new scheduled job."""
    required = ["name", "cron_expr", "repo_path", "prompt"]
    for field in required:
        if not body.get(field):
            return {"error": f"'{field}' is required"}

    if not validate_cron(body["cron_expr"]):
        return {"error": f"Invalid cron expression: {body['cron_expr']}"}

    job = await store.create_scheduled_job(
        name=body["name"],
        cron_expr=body["cron_expr"],
        repo_path=body["repo_path"],
        prompt=body["prompt"],
        description=body.get("description", ""),
        timezone_name=body.get("timezone", "UTC"),
        agent_type=body.get("agent_type", "claude"),
        base_branch=body.get("base_branch", "main"),
        enabled=body.get("enabled", True),
        max_duration_s=body.get("max_duration_s", 3600),
        cleanup_worktree=body.get("cleanup_worktree", True),
        flags=body.get("flags", ""),
    )
    return job


@router.put("/jobs/{job_id}")
async def update_job(job_id: int, body: dict):
    """Update a scheduled job."""
    if "cron_expr" in body and not validate_cron(body["cron_expr"]):
        return {"error": f"Invalid cron expression: {body['cron_expr']}"}

    job = await store.update_scheduled_job(job_id, **body)
    if not job:
        return {"error": "Job not found"}
    return job


@router.delete("/jobs/{job_id}")
async def delete_job(job_id: int):
    """Delete a scheduled job and all its run history."""
    await store.delete_scheduled_job(job_id)
    return {"ok": True}


@router.post("/jobs/{job_id}/toggle")
async def toggle_job(job_id: int):
    """Toggle a job's enabled state."""
    job = await store.get_scheduled_job(job_id)
    if not job:
        return {"error": "Job not found"}
    updated = await store.update_scheduled_job(job_id, enabled=not job["enabled"])
    return updated


@router.get("/jobs/{job_id}/runs")
async def list_runs(job_id: int, limit: int = 20):
    """List run history for a job."""
    runs = await store.get_runs_for_job(job_id, limit=limit)
    return {"runs": runs}


@router.get("/runs/recent")
async def recent_runs(limit: int = 50):
    """List recent runs across all jobs."""
    runs = await store.list_all_recent_runs(limit=limit)
    return {"runs": runs}


@router.post("/validate-cron")
async def validate_cron_endpoint(body: dict):
    """Validate a cron expression and return next fire times."""
    expr = body.get("cron_expr", "")
    tz_name = body.get("timezone", "UTC")

    if not validate_cron(expr):
        return {"valid": False, "error": "Invalid cron expression"}

    try:
        tz = ZoneInfo(tz_name)
        now = datetime.now(tz)
        upcoming = []
        cursor = now
        for _ in range(5):
            nft = next_fire_time(expr, cursor)
            upcoming.append(nft.isoformat())
            cursor = nft
        return {"valid": True, "next_fire_times": upcoming}
    except Exception as e:
        return {"valid": False, "error": str(e)}
