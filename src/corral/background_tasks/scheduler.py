"""Background scheduler service — polls scheduled_jobs and fires due runs."""

from __future__ import annotations

import asyncio
import logging
from datetime import datetime, timezone
from zoneinfo import ZoneInfo

from corral.store.schedule import ScheduleStore
from corral.tools.cron_parser import next_fire_time
from corral.tools.utils import run_cmd

log = logging.getLogger(__name__)

TICK_INTERVAL = 30  # seconds between scheduler polls


class JobScheduler:
    """Polls scheduled_jobs, fires due runs, monitors running sessions, manages worktrees."""

    def __init__(self, store: ScheduleStore) -> None:
        self._store = store
        self._running: dict[int, asyncio.Task] = {}  # run_id -> watchdog task

    async def run_forever(self) -> None:
        log.info("JobScheduler started (tick every %ds)", TICK_INTERVAL)
        while True:
            try:
                await self._tick()
            except Exception:
                log.exception("JobScheduler tick error")
            await asyncio.sleep(TICK_INTERVAL)

    async def _tick(self) -> None:
        jobs = await self._store.list_scheduled_jobs(enabled_only=True)
        now_utc = datetime.now(timezone.utc)
        for job in jobs:
            try:
                await self._evaluate_job(job, now_utc)
            except Exception:
                log.exception("Error evaluating job %s (id=%s)", job["name"], job["id"])

        # Clean up finished watchdog tasks
        finished = [rid for rid, t in self._running.items() if t.done()]
        for rid in finished:
            self._running.pop(rid, None)

    async def _evaluate_job(self, job: dict, now_utc: datetime) -> None:
        """Check if a job is due to fire."""
        # Skip if there's already an active run
        active = await self._store.get_active_run_for_job(job["id"])
        if active:
            return

        tz = ZoneInfo(job["timezone"])
        now_local = now_utc.astimezone(tz)

        last_run = await self._store.get_last_run_for_job(job["id"])
        if last_run:
            # Compute next fire after the last scheduled_at
            last_scheduled = datetime.fromisoformat(last_run["scheduled_at"])
            if last_scheduled.tzinfo is None:
                last_scheduled = last_scheduled.replace(tzinfo=tz)
            nft = next_fire_time(job["cron_expr"], last_scheduled)
        else:
            # First run ever — fire at the next occurrence after job creation
            created = datetime.fromisoformat(job["created_at"])
            if created.tzinfo is None:
                created = created.replace(tzinfo=tz)
            nft = next_fire_time(job["cron_expr"], created)

        if nft <= now_local:
            await self._fire_job(job, nft)

    async def _fire_job(self, job: dict, scheduled_at: datetime) -> None:
        """Create a worktree, launch an agent session, and record the run."""
        from corral.tools.session_manager import launch_claude_session

        log.info("Firing scheduled job '%s' (id=%d)", job["name"], job["id"])

        run_id = await self._store.create_scheduled_run(
            job["id"], scheduled_at.isoformat(), status="pending"
        )

        repo_path = job["repo_path"]
        base_branch = job.get("base_branch", "main")
        worktree_dir = f"{repo_path}_scheduled_run_{run_id}"

        try:
            # Create git worktree
            rc, _, stderr = await run_cmd(
                "git", "-C", repo_path, "worktree", "add",
                worktree_dir, base_branch, timeout=30.0,
            )
            if rc != 0:
                error = f"git worktree add failed: {stderr}"
                log.error(error)
                await self._store.update_scheduled_run(
                    run_id, status="failed", error_msg=error,
                    finished_at=datetime.now(timezone.utc).isoformat(),
                )
                return
        except Exception as e:
            error = f"worktree creation error: {e}"
            log.exception(error)
            await self._store.update_scheduled_run(
                run_id, status="failed", error_msg=error,
                finished_at=datetime.now(timezone.utc).isoformat(),
            )
            return

        # Launch agent session in the worktree
        try:
            flags_str = job.get("flags", "")
            flags_list = flags_str.split() if flags_str else None

            result = await launch_claude_session(
                working_dir=worktree_dir,
                agent_type=job.get("agent_type", "claude"),
                display_name=f"[sched] {job['name']}",
                flags=flags_list,
            )

            if "error" in result:
                error = result["error"]
                log.error("Agent launch failed for job '%s': %s", job["name"], error)
                await self._store.update_scheduled_run(
                    run_id, status="failed", error_msg=error,
                    finished_at=datetime.now(timezone.utc).isoformat(),
                )
                # Clean up worktree on launch failure
                await self._cleanup_worktree(repo_path, worktree_dir)
                return

            session_id = result["session_id"]
            session_name = result["session_name"]
            now = datetime.now(timezone.utc).isoformat()

            await self._store.update_scheduled_run(
                run_id,
                session_id=session_id,
                worktree_path=worktree_dir,
                status="running",
                started_at=now,
            )

            # Send the prompt to the agent
            from corral.tools.tmux_manager import send_to_tmux
            agent_name = result.get("session_name", "").split("-")[0] or "claude"
            await asyncio.sleep(2)  # Give agent time to initialize
            err = await send_to_tmux(
                agent_name, job["prompt"], session_id=session_id
            )
            if err:
                log.warning("Failed to send prompt to session %s: %s", session_id, err)
                # Send via raw tmux as fallback
                await run_cmd(
                    "tmux", "send-keys", "-t", session_name, "-l", job["prompt"]
                )
            await run_cmd("tmux", "send-keys", "-t", session_name, "Enter")

            # Spawn watchdog
            task = asyncio.create_task(
                self._watchdog(run_id, job, session_id, session_name, worktree_dir)
            )
            self._running[run_id] = task

        except Exception as e:
            error = f"launch error: {e}"
            log.exception(error)
            await self._store.update_scheduled_run(
                run_id, status="failed", error_msg=error,
                finished_at=datetime.now(timezone.utc).isoformat(),
            )
            await self._cleanup_worktree(repo_path, worktree_dir)

    async def _watchdog(
        self, run_id: int, job: dict, session_id: str,
        session_name: str, worktree_path: str,
    ) -> None:
        """Monitor a running session; auto-kill on timeout; cleanup worktree on exit."""
        max_duration = job.get("max_duration_s", 3600)
        elapsed = 0
        poll_interval = 30

        while elapsed < max_duration:
            await asyncio.sleep(poll_interval)
            elapsed += poll_interval

            # Check if tmux session is still alive
            rc, _, _ = await run_cmd("tmux", "has-session", "-t", session_name, timeout=5.0)
            if rc != 0:
                # Session ended naturally
                log.info("Scheduled run %d finished (session gone)", run_id)
                await self._store.update_scheduled_run(
                    run_id, status="completed",
                    exit_reason="agent_done",
                    finished_at=datetime.now(timezone.utc).isoformat(),
                )
                break
        else:
            # Timed out — kill the session
            log.warning("Scheduled run %d timed out after %ds, killing", run_id, max_duration)
            await run_cmd("tmux", "kill-session", "-t", session_name, timeout=5.0)
            await self._store.update_scheduled_run(
                run_id, status="killed",
                exit_reason="timeout",
                finished_at=datetime.now(timezone.utc).isoformat(),
            )

        # Cleanup worktree if configured
        if job.get("cleanup_worktree", 1):
            repo_path = job["repo_path"]
            await self._cleanup_worktree(repo_path, worktree_path)

    async def _cleanup_worktree(self, repo_path: str, worktree_path: str) -> None:
        """Remove a git worktree."""
        try:
            rc, _, stderr = await run_cmd(
                "git", "-C", repo_path, "worktree", "remove", "--force",
                worktree_path, timeout=30.0,
            )
            if rc != 0:
                log.warning("Failed to remove worktree %s: %s", worktree_path, stderr)
            else:
                log.info("Cleaned up worktree %s", worktree_path)
        except Exception:
            log.exception("Error cleaning up worktree %s", worktree_path)
