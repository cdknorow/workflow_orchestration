"""Git snapshot database operations."""

from __future__ import annotations

from datetime import datetime, timezone
from typing import Any

from corral.store.connection import DatabaseManager


class GitStore(DatabaseManager):
    """Git snapshot CRUD operations."""

    async def upsert_git_snapshot(
        self,
        agent_name: str,
        agent_type: str,
        working_directory: str,
        branch: str,
        commit_hash: str,
        commit_subject: str,
        commit_timestamp: str | None,
        session_id: str | None = None,
        remote_url: str | None = None,
    ) -> None:
        now = datetime.now(timezone.utc).isoformat()
        conn = await self._get_conn()
        await conn.execute(
            """INSERT OR IGNORE INTO git_snapshots
               (agent_name, agent_type, working_directory, branch,
                commit_hash, commit_subject, commit_timestamp,
                session_id, remote_url, recorded_at)
               VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)""",
            (agent_name, agent_type, working_directory, branch,
             commit_hash, commit_subject, commit_timestamp,
             session_id, remote_url, now),
        )
        # Always update branch/working_directory on the latest row for this agent
        update_params = [branch, working_directory, now]
        update_set = "branch = ?, working_directory = ?, recorded_at = ?"
        if session_id is not None:
            update_set += ", session_id = ?"
            update_params.append(session_id)
        if remote_url is not None:
            update_set += ", remote_url = ?"
            update_params.append(remote_url)
        update_params.extend([agent_name, commit_hash])
        await conn.execute(
            f"""UPDATE git_snapshots
               SET {update_set}
               WHERE agent_name = ? AND commit_hash = ?""",
            update_params,
        )
        await conn.commit()

    async def get_git_snapshots(self, agent_name: str, limit: int = 20) -> list[dict[str, Any]]:
        conn = await self._get_conn()
        rows = await (await conn.execute(
            """SELECT agent_name, agent_type, working_directory, branch,
                      commit_hash, commit_subject, commit_timestamp,
                      session_id, remote_url, recorded_at
               FROM git_snapshots
               WHERE agent_name = ?
               ORDER BY recorded_at DESC
               LIMIT ?""",
            (agent_name, limit),
        )).fetchall()
        return [dict(r) for r in rows]

    async def get_latest_git_state(self, agent_name: str) -> dict[str, Any] | None:
        conn = await self._get_conn()
        row = await (await conn.execute(
            """SELECT agent_name, agent_type, working_directory, branch,
                      commit_hash, commit_subject, commit_timestamp,
                      session_id, remote_url, recorded_at
               FROM git_snapshots
               WHERE agent_name = ?
               ORDER BY recorded_at DESC
               LIMIT 1""",
            (agent_name,),
        )).fetchone()
        return dict(row) if row else None

    async def get_all_latest_git_state(self) -> dict[str, dict[str, Any]]:
        """Return {agent_name: {branch, commit_hash, ...}} for all agents."""
        conn = await self._get_conn()
        rows = await (await conn.execute(
            """SELECT g.*
               FROM git_snapshots g
               INNER JOIN (
                   SELECT agent_name, MAX(recorded_at) as max_ts
                   FROM git_snapshots
                   GROUP BY agent_name
               ) latest ON g.agent_name = latest.agent_name
                          AND g.recorded_at = latest.max_ts"""
        )).fetchall()
        return {r["agent_name"]: dict(r) for r in rows}

    async def get_git_snapshots_for_session(self, session_id: str, limit: int = 100) -> list[dict[str, Any]]:
        """Return git commits linked to a session by session_id or by time range."""
        conn = await self._get_conn()
        rows = await (await conn.execute(
            """SELECT agent_name, agent_type, working_directory, branch,
                      commit_hash, commit_subject, commit_timestamp,
                      session_id, remote_url, recorded_at
               FROM git_snapshots
               WHERE session_id = ?
               ORDER BY commit_timestamp ASC
               LIMIT ?""",
            (session_id, limit),
        )).fetchall()
        if rows:
            return [dict(r) for r in rows]

        # Fallback: match by time range from session_index
        row = await (await conn.execute(
            "SELECT first_timestamp, last_timestamp FROM session_index WHERE session_id = ?",
            (session_id,),
        )).fetchone()
        if not row or not row["first_timestamp"] or not row["last_timestamp"]:
            return []

        rows = await (await conn.execute(
            """SELECT agent_name, agent_type, working_directory, branch,
                      commit_hash, commit_subject, commit_timestamp,
                      session_id, remote_url, recorded_at
               FROM git_snapshots
               WHERE commit_timestamp >= ? AND commit_timestamp <= ?
               ORDER BY commit_timestamp ASC
               LIMIT ?""",
            (row["first_timestamp"], row["last_timestamp"], limit),
        )).fetchall()
        return [dict(r) for r in rows]
