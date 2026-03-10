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
                "SELECT * FROM scheduled_jobs WHERE enabled = 1 AND name != '__oneshot__' ORDER BY name"
            )).fetchall()
        else:
            rows = await (await conn.execute(
                "SELECT * FROM scheduled_jobs WHERE name != '__oneshot__' ORDER BY name"
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

    # ── One-shot / Live Jobs ─────────────────────────────────────────────────

    async def get_or_create_sentinel_job(self) -> int:
        """Return the id of the __oneshot__ sentinel job, creating it if needed."""
        conn = await self._get_conn()
        row = await (await conn.execute(
            "SELECT id FROM scheduled_jobs WHERE name = '__oneshot__'"
        )).fetchone()
        if row:
            return row["id"]
        now = datetime.now(timezone.utc).isoformat()
        cur = await conn.execute(
            """INSERT INTO scheduled_jobs
               (name, cron_expr, timezone, agent_type, repo_path, prompt,
                enabled, max_duration_s, cleanup_worktree, created_at, updated_at)
               VALUES ('__oneshot__', '0 0 31 2 *', 'UTC', 'claude', '/dev/null',
                       'sentinel', 0, 3600, 1, ?, ?)""",
            (now, now),
        )
        await conn.commit()
        return cur.lastrowid  # type: ignore[return-value]

    async def create_oneshot_run(
        self,
        scheduled_at: str,
        display_name: str | None = None,
        webhook_url: str | None = None,
    ) -> int:
        """Create a run record for a one-shot API task."""
        sentinel_id = await self.get_or_create_sentinel_job()
        now = datetime.now(timezone.utc).isoformat()
        conn = await self._get_conn()
        cur = await conn.execute(
            """INSERT INTO scheduled_runs
               (job_id, status, scheduled_at, trigger_type, display_name, webhook_url, created_at)
               VALUES (?, 'pending', ?, 'api', ?, ?, ?)""",
            (sentinel_id, scheduled_at, display_name, webhook_url, now),
        )
        await conn.commit()
        return cur.lastrowid  # type: ignore[return-value]

    async def get_scheduled_run(self, run_id: int) -> dict[str, Any] | None:
        """Return a single run by id."""
        conn = await self._get_conn()
        row = await (await conn.execute(
            "SELECT * FROM scheduled_runs WHERE id = ?", (run_id,)
        )).fetchone()
        return dict(row) if row else None

    async def list_active_runs(self) -> list[dict[str, Any]]:
        """Return all pending/running runs with job name for sidebar display."""
        conn = await self._get_conn()
        rows = await (await conn.execute(
            """SELECT r.*, j.name as job_name
               FROM scheduled_runs r
               LEFT JOIN scheduled_jobs j ON j.id = r.job_id
               WHERE r.status IN ('pending', 'running')
               ORDER BY r.scheduled_at DESC"""
        )).fetchall()
        return [dict(r) for r in rows]

    async def get_running_count(self) -> int:
        """Return count of currently pending/running runs."""
        conn = await self._get_conn()
        row = await (await conn.execute(
            "SELECT COUNT(*) as cnt FROM scheduled_runs WHERE status IN ('pending', 'running')"
        )).fetchone()
        return row["cnt"] if row else 0

    async def get_all_job_session_ids(self) -> set[str]:
        """Return session_ids for ALL runs (any status) to filter from live sessions."""
        conn = await self._get_conn()
        rows = await (await conn.execute(
            "SELECT session_id FROM scheduled_runs WHERE session_id IS NOT NULL"
        )).fetchall()
        return {r["session_id"] for r in rows}

    async def list_oneshot_runs(
        self, limit: int = 50, status: str | None = None
    ) -> list[dict[str, Any]]:
        """Return recent one-shot (API-triggered) runs."""
        conn = await self._get_conn()
        sql = """SELECT * FROM scheduled_runs
                 WHERE trigger_type = 'api'"""
        params: list[Any] = []
        if status:
            sql += " AND status = ?"
            params.append(status)
        sql += " ORDER BY scheduled_at DESC LIMIT ?"
        params.append(limit)
        rows = await (await conn.execute(sql, params)).fetchall()
        return [dict(r) for r in rows]
