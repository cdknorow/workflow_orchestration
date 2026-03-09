"""Schedule-related database operations: scheduled jobs and run history."""

from __future__ import annotations

from datetime import datetime, timezone
from typing import Any

from corral.store.connection import DatabaseManager


class ScheduleStore(DatabaseManager):
    """CRUD operations for scheduled_jobs and scheduled_runs tables."""

    # ── Scheduled Jobs ─────────────────────────────────────────────────────

    async def list_scheduled_jobs(self, enabled_only: bool = False) -> list[dict[str, Any]]:
        conn = await self._get_conn()
        if enabled_only:
            rows = await (await conn.execute(
                "SELECT * FROM scheduled_jobs WHERE enabled = 1 ORDER BY name"
            )).fetchall()
        else:
            rows = await (await conn.execute(
                "SELECT * FROM scheduled_jobs ORDER BY name"
            )).fetchall()
        return [dict(r) for r in rows]

    async def get_scheduled_job(self, job_id: int) -> dict[str, Any] | None:
        conn = await self._get_conn()
        row = await (await conn.execute(
            "SELECT * FROM scheduled_jobs WHERE id = ?", (job_id,)
        )).fetchone()
        return dict(row) if row else None

    async def create_scheduled_job(
        self,
        name: str,
        cron_expr: str,
        repo_path: str,
        prompt: str,
        description: str = "",
        timezone_name: str = "UTC",
        agent_type: str = "claude",
        base_branch: str = "main",
        enabled: bool = True,
        max_duration_s: int = 3600,
        cleanup_worktree: bool = True,
        flags: str = "",
    ) -> dict[str, Any]:
        now = datetime.now(timezone.utc).isoformat()
        conn = await self._get_conn()
        cur = await conn.execute(
            """INSERT INTO scheduled_jobs
               (name, description, cron_expr, timezone, agent_type, repo_path,
                base_branch, prompt, enabled, max_duration_s, cleanup_worktree,
                flags, created_at, updated_at)
               VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)""",
            (name, description, cron_expr, timezone_name, agent_type, repo_path,
             base_branch, prompt, int(enabled), max_duration_s, int(cleanup_worktree),
             flags, now, now),
        )
        await conn.commit()
        return await self.get_scheduled_job(cur.lastrowid)  # type: ignore[return-value]

    async def update_scheduled_job(self, job_id: int, **fields: Any) -> dict[str, Any] | None:
        allowed = {
            "name", "description", "cron_expr", "timezone", "agent_type",
            "repo_path", "base_branch", "prompt", "enabled",
            "max_duration_s", "cleanup_worktree", "flags",
        }
        updates = {k: v for k, v in fields.items() if k in allowed}
        if not updates:
            return await self.get_scheduled_job(job_id)
        # Convert booleans to int for SQLite
        for k in ("enabled", "cleanup_worktree"):
            if k in updates:
                updates[k] = int(updates[k])
        updates["updated_at"] = datetime.now(timezone.utc).isoformat()
        set_clause = ", ".join(f"{k} = ?" for k in updates)
        params = list(updates.values()) + [job_id]
        conn = await self._get_conn()
        await conn.execute(f"UPDATE scheduled_jobs SET {set_clause} WHERE id = ?", params)
        await conn.commit()
        return await self.get_scheduled_job(job_id)

    async def delete_scheduled_job(self, job_id: int) -> None:
        conn = await self._get_conn()
        await conn.execute("DELETE FROM scheduled_jobs WHERE id = ?", (job_id,))
        await conn.commit()

    # ── Scheduled Runs ─────────────────────────────────────────────────────

    async def create_scheduled_run(
        self, job_id: int, scheduled_at: str, status: str = "pending"
    ) -> int:
        now = datetime.now(timezone.utc).isoformat()
        conn = await self._get_conn()
        cur = await conn.execute(
            """INSERT INTO scheduled_runs (job_id, status, scheduled_at, created_at)
               VALUES (?, ?, ?, ?)""",
            (job_id, status, scheduled_at, now),
        )
        await conn.commit()
        return cur.lastrowid  # type: ignore[return-value]

    async def update_scheduled_run(self, run_id: int, **fields: Any) -> None:
        allowed = {
            "session_id", "worktree_path", "status",
            "started_at", "finished_at", "exit_reason", "error_msg",
        }
        updates = {k: v for k, v in fields.items() if k in allowed}
        if not updates:
            return
        set_clause = ", ".join(f"{k} = ?" for k in updates)
        params = list(updates.values()) + [run_id]
        conn = await self._get_conn()
        await conn.execute(f"UPDATE scheduled_runs SET {set_clause} WHERE id = ?", params)
        await conn.commit()

    async def get_runs_for_job(
        self, job_id: int, limit: int = 20
    ) -> list[dict[str, Any]]:
        conn = await self._get_conn()
        rows = await (await conn.execute(
            "SELECT * FROM scheduled_runs WHERE job_id = ? ORDER BY scheduled_at DESC LIMIT ?",
            (job_id, limit),
        )).fetchall()
        return [dict(r) for r in rows]

    async def get_last_run_for_job(self, job_id: int) -> dict[str, Any] | None:
        conn = await self._get_conn()
        row = await (await conn.execute(
            "SELECT * FROM scheduled_runs WHERE job_id = ? ORDER BY scheduled_at DESC LIMIT 1",
            (job_id,),
        )).fetchone()
        return dict(row) if row else None

    async def get_active_run_for_job(self, job_id: int) -> dict[str, Any] | None:
        """Return the currently running run for a job, or None."""
        conn = await self._get_conn()
        row = await (await conn.execute(
            "SELECT * FROM scheduled_runs WHERE job_id = ? AND status IN ('pending', 'running') "
            "ORDER BY scheduled_at DESC LIMIT 1",
            (job_id,),
        )).fetchone()
        return dict(row) if row else None

    async def list_all_recent_runs(self, limit: int = 50) -> list[dict[str, Any]]:
        """Return recent runs across all jobs, enriched with job name."""
        conn = await self._get_conn()
        rows = await (await conn.execute(
            """SELECT r.*, j.name as job_name
               FROM scheduled_runs r
               JOIN scheduled_jobs j ON j.id = r.job_id
               ORDER BY r.scheduled_at DESC LIMIT ?""",
            (limit,),
        )).fetchall()
        return [dict(r) for r in rows]
