"""Background scheduler service — polls scheduled_jobs and fires due runs."""

from __future__ import annotations

import asyncio
import logging
import os
from datetime import datetime, timezone
from zoneinfo import ZoneInfo

from corral.store.schedule import ScheduleStore
from corral.tools.cron_parser import next_fire_time
from corral.tools.utils import run_cmd

log = logging.getLogger(__name__)

TICK_INTERVAL = 30  # seconds between scheduler polls


class ConcurrencyLimitError(Exception):
    """Raised when the max concurrent run limit is reached."""

    def __init__(self, limit: int) -> None:
        self.limit = limit
        super().__init__(f"Concurrent task limit reached (max: {limit}). Try again later.")


class JobScheduler:
    """Polls scheduled_jobs, fires due runs, monitors running sessions, manages worktrees."""

    def __init__(self, store: ScheduleStore, max_concurrent: int | None = None) -> None:
        self._store = store
        self._running: dict[int, asyncio.Task] = {}  # run_id -> watchdog task
        self._max_concurrent = max_concurrent or int(os.environ.get("CORRAL_MAX_CONCURRENT_JOBS", "5"))

    @property
    def running_count(self) -> int:
        return len([t for t in self._running.values() if not t.done()])

    async def run_forever(self) -> None:
        log.info("JobScheduler started (tick every %ds, max concurrent: %d)", TICK_INTERVAL, self._max_concurrent)
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
            # Hide the sentinel job from scheduling
            if job["name"] == "__oneshot__":
                continue
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

        # Skip if at concurrency limit
        if self.running_count >= self._max_concurrent:
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
        """Create a run record for a cron job and delegate to _launch_run."""
        log.info("Firing scheduled job '%s' (id=%d)", job["name"], job["id"])

        run_id = await self._store.create_scheduled_run(
            job["id"], scheduled_at.isoformat(), status="pending"
        )

        config = {
            "repo_path": job["repo_path"],
            "base_branch": job.get("base_branch", "main"),
            "agent_type": job.get("agent_type", "claude"),
            "prompt": job["prompt"],
            "display_name": f"[sched] {job['name']}",
            "flags": job.get("flags", ""),
            "max_duration_s": job.get("max_duration_s", 3600),
            "cleanup_worktree": bool(job.get("cleanup_worktree", 1)),
            "create_worktree": True,
            "tag": "scheduled",
        }

        await self._launch_run(run_id, config)

    async def fire_oneshot(self, config: dict) -> int:
        """Public method for the API to submit a one-shot task run.

        Returns the run_id. Raises ConcurrencyLimitError if at capacity.
        """
        if self.running_count >= self._max_concurrent:
            raise ConcurrencyLimitError(self._max_concurrent)

        now = datetime.now(timezone.utc).isoformat()
        run_id = await self._store.create_oneshot_run(
            scheduled_at=now,
            display_name=config.get("display_name"),
            webhook_url=config.get("webhook_url"),
        )

        launch_config = {
            "repo_path": config["repo_path"],
            "base_branch": config.get("base_branch", "main"),
            "agent_type": config.get("agent_type", "claude"),
            "prompt": config["prompt"],
            "display_name": config.get("display_name") or f"Task #{run_id}",
            "flags": config.get("flags", ""),
            "max_duration_s": config.get("max_duration_s", 3600),
            "cleanup_worktree": config.get("cleanup_worktree", True),
            "create_worktree": config.get("create_worktree", True),
            "webhook_url": config.get("webhook_url"),
            "tag": "task",
        }

        # Launch in background so the API can return immediately
        asyncio.create_task(self._launch_run(run_id, launch_config))

        return run_id

    async def kill_run(self, run_id: int) -> bool:
        """Kill a running task by run_id. Returns True if killed."""
        run = await self._store.get_scheduled_run(run_id)
        if not run or run["status"] not in ("pending", "running"):
            return False

        if run.get("session_id"):
            agent_type = run.get("agent_type", "claude") if "agent_type" in run else "claude"
            # Try to find and kill the tmux session
            session_name = f"{agent_type}-{run['session_id']}"
            await run_cmd("tmux", "kill-session", "-t", session_name, timeout=5.0)

        await self._store.update_scheduled_run(
            run_id, status="killed", exit_reason="user_cancelled",
            finished_at=datetime.now(timezone.utc).isoformat(),
        )

        # Cancel watchdog task if tracked
        task = self._running.pop(run_id, None)
        if task and not task.done():
            task.cancel()

        # Fire webhook callback
        webhook_url = run.get("webhook_url")
        if webhook_url:
            await self._fire_webhook(webhook_url, run_id, run.get("session_id"), "killed", "user_cancelled", run.get("started_at"))

        return True

    async def _launch_run(self, run_id: int, config: dict) -> None:
        """Core launch logic shared by cron jobs and one-shot tasks."""
        from corral.tools.session_manager import launch_claude_session

        repo_path = config["repo_path"]
        create_worktree = config.get("create_worktree", True)
        working_dir = repo_path
        worktree_dir = None

        if create_worktree:
            base_branch = config.get("base_branch", "main")
            worktree_dir = f"{repo_path}_task_run_{run_id}"
            try:
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
                    await self._fire_webhook_for_run(run_id, "failed")
                    return
            except Exception as e:
                error = f"worktree creation error: {e}"
                log.exception(error)
                await self._store.update_scheduled_run(
                    run_id, status="failed", error_msg=error,
                    finished_at=datetime.now(timezone.utc).isoformat(),
                )
                await self._fire_webhook_for_run(run_id, "failed")
                return
            working_dir = worktree_dir

        # Launch agent session
        try:
            flags_str = config.get("flags", "")
            flags_list = flags_str.split() if flags_str else None

            result = await launch_claude_session(
                working_dir=working_dir,
                agent_type=config.get("agent_type", "claude"),
                display_name=config.get("display_name"),
                flags=flags_list,
            )

            if "error" in result:
                error = result["error"]
                log.error("Agent launch failed for run %d: %s", run_id, error)
                await self._store.update_scheduled_run(
                    run_id, status="failed", error_msg=error,
                    finished_at=datetime.now(timezone.utc).isoformat(),
                )
                if worktree_dir:
                    await self._cleanup_worktree(repo_path, worktree_dir)
                await self._fire_webhook_for_run(run_id, "failed")
                return

            session_id = result["session_id"]
            session_name = result["session_name"]
            now = datetime.now(timezone.utc).isoformat()

            # Tag the session
            tag = config.get("tag", "task")
            await self._tag_session(session_id, tag)

            await self._store.update_scheduled_run(
                run_id,
                session_id=session_id,
                worktree_path=worktree_dir or working_dir,
                status="running",
                started_at=now,
            )

            # Fire "running" webhook
            await self._fire_webhook_for_run(run_id, "running")

            # Send the prompt to the agent
            from corral.tools.tmux_manager import send_to_tmux
            agent_name = result.get("session_name", "").split("-")[0] or "claude"
            await asyncio.sleep(2)  # Give agent time to initialize
            err = await send_to_tmux(
                agent_name, config["prompt"], session_id=session_id
            )
            if err:
                log.warning("Failed to send prompt to session %s: %s", session_id, err)
                await run_cmd(
                    "tmux", "send-keys", "-t", session_name, "-l", config["prompt"]
                )
            await run_cmd("tmux", "send-keys", "-t", session_name, "Enter")

            # Spawn watchdog
            watchdog_config = {
                "repo_path": repo_path,
                "max_duration_s": config.get("max_duration_s", 3600),
                "cleanup_worktree": config.get("cleanup_worktree", True) and worktree_dir is not None,
            }
            task = asyncio.create_task(
                self._watchdog(run_id, watchdog_config, session_id, session_name, worktree_dir)
            )
            self._running[run_id] = task

        except Exception as e:
            error = f"launch error: {e}"
            log.exception(error)
            await self._store.update_scheduled_run(
                run_id, status="failed", error_msg=error,
                finished_at=datetime.now(timezone.utc).isoformat(),
            )
            if worktree_dir:
                await self._cleanup_worktree(repo_path, worktree_dir)
            await self._fire_webhook_for_run(run_id, "failed")

    async def _watchdog(
        self, run_id: int, config: dict, session_id: str,
        session_name: str, worktree_path: str | None,
    ) -> None:
        """Monitor a running session; auto-kill on timeout; cleanup worktree on exit."""
        max_duration = config.get("max_duration_s", 3600)
        elapsed = 0
        poll_interval = 30

        while elapsed < max_duration:
            await asyncio.sleep(poll_interval)
            elapsed += poll_interval

            rc, _, _ = await run_cmd("tmux", "has-session", "-t", session_name, timeout=5.0)
            if rc != 0:
                log.info("Run %d finished (session gone)", run_id)
                await self._store.update_scheduled_run(
                    run_id, status="completed",
                    exit_reason="agent_done",
                    finished_at=datetime.now(timezone.utc).isoformat(),
                )
                await self._fire_webhook_for_run(run_id, "completed")
                break
        else:
            log.warning("Run %d timed out after %ds, killing", run_id, max_duration)
            await run_cmd("tmux", "kill-session", "-t", session_name, timeout=5.0)
            await self._store.update_scheduled_run(
                run_id, status="killed",
                exit_reason="timeout",
                finished_at=datetime.now(timezone.utc).isoformat(),
            )
            await self._fire_webhook_for_run(run_id, "killed")

        # Cleanup worktree if configured
        if config.get("cleanup_worktree") and worktree_path:
            repo_path = config["repo_path"]
            await self._cleanup_worktree(repo_path, worktree_path)

    async def _fire_webhook_for_run(self, run_id: int, status: str) -> None:
        """Look up the run's webhook_url and fire a callback if set."""
        try:
            run = await self._store.get_scheduled_run(run_id)
            if not run:
                return
            webhook_url = run.get("webhook_url")
            if not webhook_url:
                return
            await self._fire_webhook(
                webhook_url, run_id, run.get("session_id"), status,
                run.get("exit_reason"), run.get("started_at"),
            )
        except Exception:
            log.exception("Error firing webhook for run %d", run_id)

    async def _fire_webhook(
        self, webhook_url: str, run_id: int, session_id: str | None,
        status: str, exit_reason: str | None, started_at: str | None,
    ) -> None:
        """Fire a webhook callback in the background."""
        from corral.tools.run_callback import send_run_callback

        finished_at = datetime.now(timezone.utc).isoformat() if status in ("completed", "killed", "failed") else None
        duration_s = None
        if started_at and finished_at:
            try:
                start = datetime.fromisoformat(started_at)
                end = datetime.fromisoformat(finished_at)
                duration_s = int((end - start).total_seconds())
            except Exception:
                pass

        payload = {
            "run_id": run_id,
            "session_id": session_id,
            "status": status,
            "exit_reason": exit_reason,
            "started_at": started_at,
            "finished_at": finished_at,
            "duration_s": duration_s,
            "source": "corral",
        }
        asyncio.create_task(send_run_callback(webhook_url, payload))

    async def _tag_session(self, session_id: str, tag_name: str) -> None:
        """Ensure a tag exists and apply it to the session."""
        try:
            from corral.store import CorralStore
            store = CorralStore()
            tags = await store.list_tags()
            tag = next((t for t in tags if t["name"] == tag_name), None)
            if not tag:
                tag = await store.create_tag(tag_name, "#f78166")
            await store.add_session_tag(session_id, tag["id"])
            await store.close()
        except Exception:
            log.exception("Failed to tag session %s as '%s'", session_id, tag_name)

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
