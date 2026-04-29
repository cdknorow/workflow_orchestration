"""Storage for remote message board subscriptions.

Tracks which local agents are subscribed to boards on remote Coral servers,
so the RemoteBoardPoller can check for unread messages and deliver tmux nudges.
"""

from __future__ import annotations

from datetime import datetime, timezone
from pathlib import Path
from typing import Any

import aiosqlite

from coral.config import get_data_dir


def get_db_path() -> Path:
    return get_data_dir() / "sessions.db"


# Kept for backward compatibility
DB_DIR = Path.home() / ".coral"
DB_PATH = DB_DIR / "sessions.db"


class RemoteBoardStore:
    """Manages the remote_board_subscriptions table in the main Coral DB."""

    def __init__(self, db_path: Path | None = None) -> None:
        db_path = db_path or get_db_path()
        self._db_path = db_path
        self._conn: aiosqlite.Connection | None = None
        self._schema_ensured = False

    async def _get_conn(self) -> aiosqlite.Connection:
        if self._conn is not None:
            return self._conn
        self._db_path.parent.mkdir(parents=True, exist_ok=True)
        conn = await aiosqlite.connect(str(self._db_path))
        conn.row_factory = aiosqlite.Row
        await conn.execute("PRAGMA journal_mode=WAL")
        if not self._schema_ensured:
            self._schema_ensured = True
            await self._ensure_schema(conn)
        self._conn = conn
        return conn

    async def close(self) -> None:
        if self._conn is not None:
            await self._conn.close()
            self._conn = None

    async def _ensure_schema(self, conn: aiosqlite.Connection) -> None:
        await conn.executescript("""
            CREATE TABLE IF NOT EXISTS remote_board_subscriptions (
                id                   INTEGER PRIMARY KEY AUTOINCREMENT,
                session_id           TEXT NOT NULL,
                remote_server        TEXT NOT NULL,
                project              TEXT NOT NULL,
                job_title            TEXT NOT NULL,
                last_notified_unread INTEGER NOT NULL DEFAULT 0,
                created_at           TEXT NOT NULL,
                UNIQUE(session_id, remote_server, project)
            );
        """)

    async def add(
        self,
        session_id: str,
        remote_server: str,
        project: str,
        job_title: str,
    ) -> dict[str, Any]:
        conn = await self._get_conn()
        now = datetime.now(timezone.utc).isoformat()
        await conn.execute(
            """INSERT INTO remote_board_subscriptions
               (session_id, remote_server, project, job_title, created_at)
               VALUES (?, ?, ?, ?, ?)
               ON CONFLICT(session_id, remote_server, project)
               DO UPDATE SET job_title = excluded.job_title""",
            (session_id, remote_server, project, job_title, now),
        )
        await conn.commit()
        rows = await conn.execute_fetchall(
            """SELECT * FROM remote_board_subscriptions
               WHERE session_id = ? AND remote_server = ? AND project = ?""",
            (session_id, remote_server, project),
        )
        return dict(rows[0])

    async def remove(self, session_id: str) -> bool:
        conn = await self._get_conn()
        cursor = await conn.execute(
            "DELETE FROM remote_board_subscriptions WHERE session_id = ?",
            (session_id,),
        )
        await conn.commit()
        return cursor.rowcount > 0

    async def list_all(self) -> list[dict[str, Any]]:
        conn = await self._get_conn()
        rows = await conn.execute_fetchall(
            "SELECT * FROM remote_board_subscriptions ORDER BY created_at"
        )
        return [dict(r) for r in rows]

    async def update_last_notified(self, sub_id: int, unread: int) -> None:
        conn = await self._get_conn()
        await conn.execute(
            "UPDATE remote_board_subscriptions SET last_notified_unread = ? WHERE id = ?",
            (unread, sub_id),
        )
        await conn.commit()
