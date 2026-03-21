"""Agent tasks, notes, and events database operations."""

from __future__ import annotations

import asyncio
import logging
from datetime import datetime, timezone
from typing import Any

from coral.store.connection import DatabaseManager

log = logging.getLogger(__name__)

# ── Event Write Batcher ────────────────────────────────────────────────────
# Queues insert_agent_event calls in memory and flushes them to the DB in
# a single transaction every _FLUSH_INTERVAL seconds (or when the queue
# reaches _FLUSH_SIZE items).  This dramatically reduces write-lock
# contention when many agents fire events concurrently via hooks.

_FLUSH_INTERVAL: float = 2.0   # seconds
_FLUSH_SIZE: int = 50           # max events before force flush
_PRUNE_INTERVAL: int = 2       # prune old events every N flushes

_event_queue: list[tuple] = []
_flush_task: asyncio.Task | None = None
_flush_count: int = 0


async def _flush_events(store: "TaskStore") -> None:
    """Flush queued events to the DB in a single transaction."""
    global _event_queue, _flush_count
    if not _event_queue:
        return
    batch = _event_queue[:]
    _event_queue = []
    try:
        conn = await store._get_conn()
        await conn.executemany(
            "INSERT INTO agent_events "
            "(agent_name, session_id, event_type, tool_name, summary, detail_json, created_at) "
            "VALUES (?, ?, ?, ?, ?, ?, ?)",
            batch,
        )
        # Deferred prune: only run every N flushes
        _flush_count += 1
        if _flush_count >= _PRUNE_INTERVAL:
            _flush_count = 0
            # Prune to 500 events per agent for agents in this batch
            pruned_agents: set[str] = set()
            for row in batch:
                agent_name = row[0]
                if agent_name not in pruned_agents:
                    pruned_agents.add(agent_name)
                    await conn.execute(
                        "DELETE FROM agent_events WHERE agent_name = ? AND id NOT IN "
                        "(SELECT id FROM agent_events WHERE agent_name = ? "
                        "ORDER BY id DESC LIMIT 500)",
                        (agent_name, agent_name),
                    )
        await conn.commit()
    except Exception:
        log.exception("Failed to flush %d events to DB", len(batch))


async def _flush_loop(store: "TaskStore") -> None:
    """Background loop that flushes events periodically."""
    while True:
        await asyncio.sleep(_FLUSH_INTERVAL)
        await _flush_events(store)


def _ensure_flush_task(store: "TaskStore") -> None:
    """Start the background flush loop if not already running."""
    global _flush_task
    if _flush_task is not None and not _flush_task.done():
        return
    try:
        loop = asyncio.get_running_loop()
        _flush_task = loop.create_task(_flush_loop(store))
    except RuntimeError:
        pass  # No event loop — sync context, skip


class TaskStore(DatabaseManager):
    """Agent tasks, notes, and events CRUD operations."""

    # ── Agent Tasks ────────────────────────────────────────────────────────

    async def list_agent_tasks(self, agent_name: str, session_id: str | None = None) -> list[dict[str, Any]]:
        conn = await self._get_conn()
        if session_id is not None:
            rows = await (await conn.execute(
                "SELECT id, agent_name, title, completed, sort_order, created_at, updated_at "
                "FROM agent_tasks WHERE agent_name = ? AND session_id = ? ORDER BY sort_order",
                (agent_name, session_id),
            )).fetchall()
        else:
            rows = await (await conn.execute(
                "SELECT id, agent_name, title, completed, sort_order, created_at, updated_at "
                "FROM agent_tasks WHERE agent_name = ? ORDER BY sort_order",
                (agent_name,),
            )).fetchall()
        return [dict(r) for r in rows]

    async def create_agent_task(self, agent_name: str, title: str, session_id: str | None = None) -> dict[str, Any]:
        now = datetime.now(timezone.utc).isoformat()
        conn = await self._get_conn()
        if session_id is not None:
            row = await (await conn.execute(
                "SELECT COALESCE(MAX(sort_order), -1) + 1 AS next_order "
                "FROM agent_tasks WHERE agent_name = ? AND session_id = ?",
                (agent_name, session_id),
            )).fetchone()
        else:
            row = await (await conn.execute(
                "SELECT COALESCE(MAX(sort_order), -1) + 1 AS next_order "
                "FROM agent_tasks WHERE agent_name = ?",
                (agent_name,),
            )).fetchone()
        sort_order = row["next_order"] if row else 0
        cur = await conn.execute(
            "INSERT INTO agent_tasks (agent_name, session_id, title, sort_order, created_at, updated_at) "
            "VALUES (?, ?, ?, ?, ?, ?)",
            (agent_name, session_id, title, sort_order, now, now),
        )
        await conn.commit()
        return {"id": cur.lastrowid, "agent_name": agent_name, "title": title,
                "completed": 0, "sort_order": sort_order,
                "created_at": now, "updated_at": now}

    async def create_agent_task_if_not_exists(self, agent_name: str, title: str, session_id: str | None = None) -> dict[str, Any] | None:
        conn = await self._get_conn()
        if session_id is not None:
            existing = await (await conn.execute(
                "SELECT id, agent_name, title, completed, sort_order, created_at, updated_at "
                "FROM agent_tasks WHERE agent_name = ? AND title = ? AND session_id = ?",
                (agent_name, title, session_id),
            )).fetchone()
        else:
            existing = await (await conn.execute(
                "SELECT id, agent_name, title, completed, sort_order, created_at, updated_at "
                "FROM agent_tasks WHERE agent_name = ? AND title = ?",
                (agent_name, title),
            )).fetchone()
        if existing:
            return dict(existing)
        return await self.create_agent_task(agent_name, title, session_id=session_id)

    async def update_agent_task(self, task_id: int, title: str | None = None,
                                completed: int | None = None,
                                sort_order: int | None = None) -> None:
        now = datetime.now(timezone.utc).isoformat()
        conn = await self._get_conn()
        fields = ["updated_at = ?"]
        params: list[Any] = [now]
        if title is not None:
            fields.append("title = ?")
            params.append(title)
        if completed is not None:
            fields.append("completed = ?")
            params.append(completed)
        if sort_order is not None:
            fields.append("sort_order = ?")
            params.append(sort_order)
        params.append(task_id)
        await conn.execute(
            f"UPDATE agent_tasks SET {', '.join(fields)} WHERE id = ?",
            params,
        )
        await conn.commit()

    async def complete_agent_task_by_title(self, agent_name: str, title: str, session_id: str | None = None) -> None:
        now = datetime.now(timezone.utc).isoformat()
        conn = await self._get_conn()
        if session_id is not None:
            await conn.execute(
                "UPDATE agent_tasks SET completed = 1, updated_at = ? "
                "WHERE agent_name = ? AND title = ? AND session_id = ? AND completed = 0",
                (now, agent_name, title, session_id),
            )
        else:
            await conn.execute(
                "UPDATE agent_tasks SET completed = 1, updated_at = ? "
                "WHERE agent_name = ? AND title = ? AND completed = 0",
                (now, agent_name, title),
            )
        await conn.commit()

    async def delete_agent_task(self, task_id: int) -> None:
        conn = await self._get_conn()
        await conn.execute("DELETE FROM agent_tasks WHERE id = ?", (task_id,))
        await conn.commit()

    async def reorder_agent_tasks(self, agent_name: str, task_ids: list[int]) -> None:
        now = datetime.now(timezone.utc).isoformat()
        conn = await self._get_conn()
        for idx, tid in enumerate(task_ids):
            await conn.execute(
                "UPDATE agent_tasks SET sort_order = ?, updated_at = ? "
                "WHERE id = ? AND agent_name = ?",
                (idx, now, tid, agent_name),
            )
        await conn.commit()

    # ── Agent Notes ────────────────────────────────────────────────────────

    async def list_agent_notes(self, agent_name: str, session_id: str | None = None) -> list[dict[str, Any]]:
        conn = await self._get_conn()
        if session_id is not None:
            rows = await (await conn.execute(
                "SELECT id, agent_name, content, created_at, updated_at "
                "FROM agent_notes WHERE agent_name = ? AND session_id = ? ORDER BY created_at DESC",
                (agent_name, session_id),
            )).fetchall()
        else:
            rows = await (await conn.execute(
                "SELECT id, agent_name, content, created_at, updated_at "
                "FROM agent_notes WHERE agent_name = ? ORDER BY created_at DESC",
                (agent_name,),
            )).fetchall()
        return [dict(r) for r in rows]

    async def create_agent_note(self, agent_name: str, content: str, session_id: str | None = None) -> dict[str, Any]:
        now = datetime.now(timezone.utc).isoformat()
        conn = await self._get_conn()
        cur = await conn.execute(
            "INSERT INTO agent_notes (agent_name, session_id, content, created_at, updated_at) "
            "VALUES (?, ?, ?, ?, ?)",
            (agent_name, session_id, content, now, now),
        )
        await conn.commit()
        return {"id": cur.lastrowid, "agent_name": agent_name, "content": content,
                "created_at": now, "updated_at": now}

    async def update_agent_note(self, note_id: int, content: str) -> None:
        now = datetime.now(timezone.utc).isoformat()
        conn = await self._get_conn()
        await conn.execute(
            "UPDATE agent_notes SET content = ?, updated_at = ? WHERE id = ?",
            (content, now, note_id),
        )
        await conn.commit()

    async def delete_agent_note(self, note_id: int) -> None:
        conn = await self._get_conn()
        await conn.execute("DELETE FROM agent_notes WHERE id = ?", (note_id,))
        await conn.commit()

    # ── Agent Events ──────────────────────────────────────────────────────

    async def insert_agent_event(
        self,
        agent_name: str,
        event_type: str,
        summary: str,
        tool_name: str | None = None,
        session_id: str | None = None,
        detail_json: str | None = None,
    ) -> dict[str, Any]:
        now = datetime.now(timezone.utc).isoformat()
        row = (agent_name, session_id, event_type, tool_name, summary, detail_json, now)
        _event_queue.append(row)
        _ensure_flush_task(self)
        # Force-flush if queue is large
        if len(_event_queue) >= _FLUSH_SIZE:
            await _flush_events(self)
        return {"id": -1, "agent_name": agent_name, "event_type": event_type,
                "tool_name": tool_name, "summary": summary, "detail_json": detail_json,
                "created_at": now}

    async def list_agent_events(self, agent_name: str, limit: int = 50, session_id: str | None = None) -> list[dict[str, Any]]:
        # Flush pending events so reads are consistent
        if _event_queue:
            await _flush_events(self)
        conn = await self._get_conn()
        if session_id is not None:
            rows = await (await conn.execute(
                "SELECT id, agent_name, session_id, event_type, tool_name, summary, detail_json, created_at "
                "FROM agent_events WHERE agent_name = ? AND session_id = ? ORDER BY created_at DESC LIMIT ?",
                (agent_name, session_id, limit),
            )).fetchall()
        else:
            rows = await (await conn.execute(
                "SELECT id, agent_name, session_id, event_type, tool_name, summary, detail_json, created_at "
                "FROM agent_events WHERE agent_name = ? ORDER BY created_at DESC LIMIT ?",
                (agent_name, limit),
            )).fetchall()
        return [dict(r) for r in rows]

    async def get_agent_event_counts(self, agent_name: str, session_id: str | None = None) -> list[dict[str, Any]]:
        if _event_queue:
            await _flush_events(self)
        conn = await self._get_conn()
        if session_id is not None:
            rows = await (await conn.execute(
                "SELECT tool_name, COUNT(*) as count FROM agent_events "
                "WHERE agent_name = ? AND session_id = ? AND tool_name IS NOT NULL GROUP BY tool_name ORDER BY count DESC",
                (agent_name, session_id),
            )).fetchall()
        else:
            rows = await (await conn.execute(
                "SELECT tool_name, COUNT(*) as count FROM agent_events "
                "WHERE agent_name = ? AND tool_name IS NOT NULL GROUP BY tool_name ORDER BY count DESC",
                (agent_name,),
            )).fetchall()
        return [{"tool_name": r["tool_name"], "count": r["count"]} for r in rows]

    async def get_latest_event_types(self, session_ids: list[str]) -> dict[str, tuple[str, str]]:
        """Return the latest (event_type, summary) for each session_id (excluding status/goal/confidence)."""
        if not session_ids:
            return {}
        if _event_queue:
            await _flush_events(self)
        conn = await self._get_conn()
        placeholders = ",".join("?" for _ in session_ids)
        rows = await (await conn.execute(
            f"SELECT session_id, event_type, summary FROM agent_events "
            f"WHERE session_id IN ({placeholders}) "
            f"AND event_type NOT IN ('status', 'goal', 'confidence') "
            f"ORDER BY created_at DESC",
            session_ids,
        )).fetchall()
        result: dict[str, tuple[str, str]] = {}
        for r in rows:
            sid = r["session_id"]
            if sid not in result:
                result[sid] = (r["event_type"], r["summary"] or "")
        return result

    async def get_latest_goals(self, session_ids: list[str]) -> dict[str, str]:
        """Return the latest goal summary for each session_id."""
        if not session_ids:
            return {}
        if _event_queue:
            await _flush_events(self)
        conn = await self._get_conn()
        placeholders = ",".join("?" for _ in session_ids)
        rows = await (await conn.execute(
            f"SELECT session_id, summary FROM agent_events "
            f"WHERE session_id IN ({placeholders}) AND event_type = 'goal' "
            f"ORDER BY created_at DESC",
            session_ids,
        )).fetchall()
        result: dict[str, str] = {}
        for r in rows:
            sid = r["session_id"]
            if sid not in result:
                result[sid] = r["summary"]
        return result

    async def clear_agent_events(self, agent_name: str, session_id: str | None = None) -> None:
        global _event_queue
        # Discard any queued events for this agent before deleting from DB
        if session_id is not None:
            _event_queue = [e for e in _event_queue if not (e[0] == agent_name and e[1] == session_id)]
        else:
            _event_queue = [e for e in _event_queue if e[0] != agent_name]
        conn = await self._get_conn()
        if session_id is not None:
            await conn.execute(
                "DELETE FROM agent_events WHERE agent_name = ? AND session_id = ?",
                (agent_name, session_id),
            )
        else:
            await conn.execute("DELETE FROM agent_events WHERE agent_name = ?", (agent_name,))
        await conn.commit()

    async def get_last_known_status_summary(self) -> dict[str, dict[str, str | None]]:
        """Return the most recent status and goal event per session."""
        conn = await self._get_conn()
        status_rows = await (await conn.execute(
            "SELECT session_id, agent_name, summary FROM agent_events "
            "WHERE event_type = 'status' AND id IN "
            "(SELECT MAX(id) FROM agent_events WHERE event_type = 'status' "
            "GROUP BY COALESCE(session_id, agent_name))"
        )).fetchall()
        goal_rows = await (await conn.execute(
            "SELECT session_id, agent_name, summary FROM agent_events "
            "WHERE event_type = 'goal' AND id IN "
            "(SELECT MAX(id) FROM agent_events WHERE event_type = 'goal' "
            "GROUP BY COALESCE(session_id, agent_name))"
        )).fetchall()
        result: dict[str, dict[str, str | None]] = {}
        for r in status_rows:
            key = r["session_id"] or r["agent_name"]
            result.setdefault(key, {"status": None, "summary": None})
            result[key]["status"] = r["summary"]
        for r in goal_rows:
            key = r["session_id"] or r["agent_name"]
            result.setdefault(key, {"status": None, "summary": None})
            result[key]["summary"] = r["summary"]
        return result

    # ── History queries (by session_id only) ────────────────────────────────

    async def list_tasks_by_session(self, session_id: str) -> list[dict[str, Any]]:
        conn = await self._get_conn()
        rows = await (await conn.execute(
            "SELECT id, agent_name, title, completed, sort_order, created_at, updated_at "
            "FROM agent_tasks WHERE session_id = ? ORDER BY sort_order",
            (session_id,),
        )).fetchall()
        return [dict(r) for r in rows]

    async def list_notes_by_session(self, session_id: str) -> list[dict[str, Any]]:
        conn = await self._get_conn()
        rows = await (await conn.execute(
            "SELECT id, agent_name, content, created_at, updated_at "
            "FROM agent_notes WHERE session_id = ? ORDER BY created_at DESC",
            (session_id,),
        )).fetchall()
        return [dict(r) for r in rows]

    async def list_events_by_session(self, session_id: str, limit: int = 200) -> list[dict[str, Any]]:
        conn = await self._get_conn()
        rows = await (await conn.execute(
            "SELECT id, agent_name, session_id, event_type, tool_name, summary, detail_json, created_at "
            "FROM agent_events WHERE session_id = ? ORDER BY created_at DESC LIMIT ?",
            (session_id, limit),
        )).fetchall()
        return [dict(r) for r in rows]
