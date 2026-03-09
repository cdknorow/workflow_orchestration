"""Session-related database operations: index, FTS, display names, notes, tags, settings, live state."""

from __future__ import annotations

import re
from datetime import datetime, timezone
from typing import Any

from corral.store.connection import DatabaseManager


def _extract_first_header(text: str) -> str:
    """Extract the first markdown header from text, or return empty string."""
    if not text:
        return ""
    m = re.search(r"^#{1,6}\s+(.+)$", text, re.MULTILINE)
    return m.group(1).strip() if m else ""


class SessionStore(DatabaseManager):
    """Session-related DB operations: index, FTS, notes, tags, settings, display names, live state."""

    def __init__(self, *args, **kwargs) -> None:
        super().__init__(*args, **kwargs)
        self._session_id_cache: dict[str, str | None] = {}  # agent_name -> session_id

    # ── User Settings ──────────────────────────────────────────────────────

    async def get_settings(self) -> dict[str, str]:
        """Return all user settings as {key: value}."""
        conn = await self._get_conn()
        rows = await (await conn.execute("SELECT key, value FROM user_settings")).fetchall()
        return {r["key"]: r["value"] for r in rows}

    async def set_setting(self, key: str, value: str) -> None:
        conn = await self._get_conn()
        await conn.execute(
            "INSERT INTO user_settings (key, value) VALUES (?, ?) "
            "ON CONFLICT(key) DO UPDATE SET value = excluded.value",
            (key, value),
        )
        await conn.commit()

    # ── Session Notes ──────────────────────────────────────────────────────

    async def get_session_notes(self, session_id: str) -> dict[str, Any]:
        conn = await self._get_conn()
        row = await (await conn.execute(
            "SELECT notes_md, auto_summary, is_user_edited, updated_at FROM session_meta WHERE session_id = ?",
            (session_id,),
        )).fetchone()
        if row:
            return {
                "notes_md": row["notes_md"],
                "auto_summary": row["auto_summary"],
                "is_user_edited": bool(row["is_user_edited"]),
                "updated_at": row["updated_at"],
            }
        return {"notes_md": "", "auto_summary": "", "is_user_edited": False, "updated_at": None}

    async def save_session_notes(self, session_id: str, notes_md: str) -> None:
        now = datetime.now(timezone.utc).isoformat()
        conn = await self._get_conn()
        await conn.execute(
            """INSERT INTO session_meta (session_id, notes_md, is_user_edited, created_at, updated_at)
               VALUES (?, ?, 1, ?, ?)
               ON CONFLICT(session_id) DO UPDATE SET
                   notes_md = excluded.notes_md,
                   is_user_edited = 1,
                   updated_at = excluded.updated_at""",
            (session_id, notes_md, now, now),
        )
        await conn.commit()

    async def save_auto_summary(self, session_id: str, summary: str) -> None:
        now = datetime.now(timezone.utc).isoformat()
        conn = await self._get_conn()
        row = await (await conn.execute(
            "SELECT is_user_edited FROM session_meta WHERE session_id = ?",
            (session_id,),
        )).fetchone()
        if row and row["is_user_edited"]:
            return
        await conn.execute(
            """INSERT INTO session_meta (session_id, auto_summary, created_at, updated_at)
               VALUES (?, ?, ?, ?)
               ON CONFLICT(session_id) DO UPDATE SET
                   auto_summary = excluded.auto_summary,
                   updated_at = excluded.updated_at""",
            (session_id, summary, now, now),
        )
        await conn.commit()

    # ── Display Names ──────────────────────────────────────────────────────

    async def get_display_name(self, session_id: str) -> str | None:
        conn = await self._get_conn()
        row = await (await conn.execute(
            "SELECT display_name FROM session_meta WHERE session_id = ?",
            (session_id,),
        )).fetchone()
        return row["display_name"] if row else None

    async def set_display_name(self, session_id: str, display_name: str) -> None:
        now = datetime.now(timezone.utc).isoformat()
        conn = await self._get_conn()
        await conn.execute(
            """INSERT INTO session_meta (session_id, display_name, created_at, updated_at)
               VALUES (?, ?, ?, ?)
               ON CONFLICT(session_id) DO UPDATE SET
                   display_name = excluded.display_name,
                   updated_at = excluded.updated_at""",
            (session_id, display_name, now, now),
        )
        await conn.commit()

    async def get_display_names(self, session_ids: list[str]) -> dict[str, str]:
        if not session_ids:
            return {}
        conn = await self._get_conn()
        placeholders = ",".join("?" for _ in session_ids)
        rows = await (await conn.execute(
            f"SELECT session_id, display_name FROM session_meta WHERE session_id IN ({placeholders}) AND display_name IS NOT NULL",
            session_ids,
        )).fetchall()
        return {r["session_id"]: r["display_name"] for r in rows}

    async def migrate_display_name(self, old_session_id: str, new_session_id: str) -> None:
        name = await self.get_display_name(old_session_id)
        if name:
            await self.set_display_name(new_session_id, name)

    # ── Tags ───────────────────────────────────────────────────────────────

    async def list_tags(self) -> list[dict[str, Any]]:
        conn = await self._get_conn()
        rows = await (await conn.execute("SELECT id, name, color FROM tags ORDER BY name")).fetchall()
        return [dict(r) for r in rows]

    async def create_tag(self, name: str, color: str = "#58a6ff") -> dict[str, Any]:
        conn = await self._get_conn()
        cur = await conn.execute(
            "INSERT INTO tags (name, color) VALUES (?, ?)", (name, color),
        )
        await conn.commit()
        return {"id": cur.lastrowid, "name": name, "color": color}

    async def delete_tag(self, tag_id: int) -> None:
        conn = await self._get_conn()
        await conn.execute("DELETE FROM tags WHERE id = ?", (tag_id,))
        await conn.commit()

    async def get_session_tags(self, session_id: str) -> list[dict[str, Any]]:
        conn = await self._get_conn()
        rows = await (await conn.execute(
            "SELECT t.id, t.name, t.color FROM tags t "
            "JOIN session_tags st ON st.tag_id = t.id WHERE st.session_id = ? ORDER BY t.name",
            (session_id,),
        )).fetchall()
        return [dict(r) for r in rows]

    async def add_session_tag(self, session_id: str, tag_id: int) -> None:
        conn = await self._get_conn()
        await conn.execute(
            "INSERT OR IGNORE INTO session_tags (session_id, tag_id) VALUES (?, ?)",
            (session_id, tag_id),
        )
        await conn.commit()

    async def remove_session_tag(self, session_id: str, tag_id: int) -> None:
        conn = await self._get_conn()
        await conn.execute(
            "DELETE FROM session_tags WHERE session_id = ? AND tag_id = ?",
            (session_id, tag_id),
        )
        await conn.commit()

    # ── Session Index ──────────────────────────────────────────────────────

    async def upsert_session_index(
        self,
        session_id: str,
        source_type: str,
        source_file: str,
        first_timestamp: str | None,
        last_timestamp: str | None,
        message_count: int,
        display_summary: str,
        file_mtime: float,
    ) -> None:
        now = datetime.now(timezone.utc).isoformat()
        conn = await self._get_conn()
        await conn.execute(
            """INSERT OR REPLACE INTO session_index
               (session_id, source_type, source_file, first_timestamp, last_timestamp,
                message_count, display_summary, indexed_at, file_mtime)
               VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)""",
            (session_id, source_type, source_file, first_timestamp, last_timestamp,
             message_count, display_summary, now, file_mtime),
        )
        await conn.commit()

    async def upsert_fts(self, session_id: str, body: str) -> None:
        conn = await self._get_conn()
        try:
            await conn.execute("DELETE FROM session_fts WHERE session_id = ?", (session_id,))
            await conn.execute(
                "INSERT INTO session_fts (session_id, body) VALUES (?, ?)",
                (session_id, body),
            )
            await conn.commit()
        except Exception:
            pass  # FTS5 may not be available

    async def enqueue_for_summarization(self, session_id: str) -> None:
        conn = await self._get_conn()
        await conn.execute(
            "INSERT OR IGNORE INTO summarizer_queue (session_id, status) VALUES (?, 'pending')",
            (session_id,),
        )
        await conn.commit()

    async def mark_summarized(self, session_id: str, status: str, error: str | None = None) -> None:
        now = datetime.now(timezone.utc).isoformat()
        conn = await self._get_conn()
        await conn.execute(
            "UPDATE summarizer_queue SET status = ?, attempted_at = ?, error_msg = ? WHERE session_id = ?",
            (status, now, error, session_id),
        )
        await conn.commit()

    async def get_pending_summaries(self, limit: int = 5) -> list[str]:
        conn = await self._get_conn()
        rows = await (await conn.execute(
            "SELECT session_id FROM summarizer_queue WHERE status = 'pending' LIMIT ?",
            (limit,),
        )).fetchall()
        return [r["session_id"] for r in rows]

    async def get_indexed_mtimes(self) -> dict[str, float]:
        """Return {source_file: file_mtime} for all indexed sessions."""
        conn = await self._get_conn()
        rows = await (await conn.execute(
            "SELECT source_file, file_mtime FROM session_index"
        )).fetchall()
        result: dict[str, float] = {}
        for r in rows:
            existing = result.get(r["source_file"], 0.0)
            if r["file_mtime"] > existing:
                result[r["source_file"]] = r["file_mtime"]
        return result

    async def list_sessions_paged(
        self,
        page: int = 1,
        page_size: int = 50,
        search: str | None = None,
        tag_id: int | None = None,
        source_type: str | None = None,
    ) -> dict[str, Any]:
        """Paginated session listing with optional full-text search, tag filter, and source filter."""
        conn = await self._get_conn()
        params: list[Any] = []
        where_clauses: list[str] = []

        from_clause = "session_index si"
        select_fields = (
            "si.session_id, si.source_type, si.source_file, "
            "si.first_timestamp, si.last_timestamp, si.message_count, "
            "si.display_summary"
        )
        order_clause = "si.last_timestamp DESC"

        if search:
            from_clause += " JOIN session_fts fts ON fts.session_id = si.session_id"
            where_clauses.append("session_fts MATCH ?")
            params.append(search)
            order_clause = "rank"

        if tag_id is not None:
            where_clauses.append(
                "si.session_id IN (SELECT session_id FROM session_tags WHERE tag_id = ?)"
            )
            params.append(tag_id)

        if source_type:
            where_clauses.append("si.source_type = ?")
            params.append(source_type)

        where_sql = (" WHERE " + " AND ".join(where_clauses)) if where_clauses else ""

        count_sql = f"SELECT COUNT(*) as cnt FROM {from_clause}{where_sql}"
        count_row = await (await conn.execute(count_sql, params)).fetchone()
        total = count_row["cnt"] if count_row else 0

        offset = (page - 1) * page_size
        query = (
            f"SELECT {select_fields} FROM {from_clause}{where_sql} "
            f"ORDER BY {order_clause} LIMIT ? OFFSET ?"
        )
        rows = await (await conn.execute(query, params + [page_size, offset])).fetchall()

        session_ids = [r["session_id"] for r in rows]

        # Enrich with metadata (notes/tags)
        meta_map: dict[str, dict[str, Any]] = {}
        if session_ids:
            placeholders = ",".join("?" for _ in session_ids)
            meta_rows = await (await conn.execute(
                f"SELECT session_id, notes_md, auto_summary, is_user_edited "
                f"FROM session_meta WHERE session_id IN ({placeholders})",
                session_ids,
            )).fetchall()
            for r in meta_rows:
                content = r["notes_md"] or r["auto_summary"] or ""
                meta_map[r["session_id"]] = {
                    "has_notes": bool(r["notes_md"]) or bool(r["auto_summary"]),
                    "is_user_edited": bool(r["is_user_edited"]),
                    "summary_title": _extract_first_header(content),
                }

            tag_rows = await (await conn.execute(
                f"SELECT st.session_id, t.id, t.name, t.color "
                f"FROM session_tags st JOIN tags t ON t.id = st.tag_id "
                f"WHERE st.session_id IN ({placeholders}) ORDER BY t.name",
                session_ids,
            )).fetchall()
            tags_map: dict[str, list[dict[str, Any]]] = {}
            for r in tag_rows:
                tags_map.setdefault(r["session_id"], []).append({
                    "id": r["id"], "name": r["name"], "color": r["color"],
                })
        else:
            tags_map = {}

        # Enrich with git branch info
        branch_map: dict[str, str] = {}
        if session_ids:
            branch_rows = await (await conn.execute(
                f"""SELECT gs.session_id, gs.branch
                   FROM git_snapshots gs
                   INNER JOIN (
                       SELECT session_id, MAX(recorded_at) as max_ts
                       FROM git_snapshots
                       WHERE session_id IN ({placeholders})
                       GROUP BY session_id
                   ) latest ON gs.session_id = latest.session_id
                               AND gs.recorded_at = latest.max_ts""",
                session_ids,
            )).fetchall()
            for r in branch_rows:
                branch_map[r["session_id"]] = r["branch"]

        sessions = []
        for r in rows:
            sid = r["session_id"]
            meta = meta_map.get(sid, {})
            sessions.append({
                "session_id": sid,
                "source_type": r["source_type"],
                "source_file": r["source_file"],
                "first_timestamp": r["first_timestamp"],
                "last_timestamp": r["last_timestamp"],
                "message_count": r["message_count"],
                "summary": r["display_summary"],
                "summary_title": meta.get("summary_title", ""),
                "has_notes": meta.get("has_notes", False),
                "tags": tags_map.get(sid, []),
                "branch": branch_map.get(sid),
            })

        return {
            "sessions": sessions,
            "total": total,
            "page": page,
            "page_size": page_size,
        }

    # ── Agent Live State ──────────────────────────────────────────────────

    async def get_agent_session_id(self, agent_name: str) -> str | None:
        """Return the current session_id for a live agent, or None if unknown."""
        _sentinel = object()
        cached = self._session_id_cache.get(agent_name, _sentinel)
        if cached is not _sentinel:
            return cached

        conn = await self._get_conn()
        row = await (await conn.execute(
            "SELECT current_session_id FROM agent_live_state WHERE agent_name = ?",
            (agent_name,),
        )).fetchone()
        result = row["current_session_id"] if row else None
        self._session_id_cache[agent_name] = result
        return result

    async def set_agent_session_id(self, agent_name: str, session_id: str) -> None:
        self._session_id_cache[agent_name] = session_id
        conn = await self._get_conn()
        await conn.execute(
            "INSERT INTO agent_live_state (agent_name, current_session_id) VALUES (?, ?) "
            "ON CONFLICT(agent_name) DO UPDATE SET current_session_id = excluded.current_session_id",
            (agent_name, session_id),
        )
        await conn.commit()

    async def clear_agent_session_id(self, agent_name: str) -> None:
        self._session_id_cache[agent_name] = None
        conn = await self._get_conn()
        await conn.execute(
            "INSERT INTO agent_live_state (agent_name, current_session_id) VALUES (?, NULL) "
            "ON CONFLICT(agent_name) DO UPDATE SET current_session_id = NULL",
            (agent_name,),
        )
        await conn.commit()

    # ── Live Sessions (persistent session tracking) ─────────────────────

    async def register_live_session(
        self, session_id: str, agent_type: str, agent_name: str,
        working_dir: str, display_name: str | None = None,
        resume_from_id: str | None = None,
        flags: list[str] | None = None,
    ) -> None:
        import json as _json
        conn = await self._get_conn()
        now = datetime.now(timezone.utc).isoformat()
        flags_json = _json.dumps(flags) if flags else None
        await conn.execute(
            "INSERT OR REPLACE INTO live_sessions "
            "(session_id, agent_type, agent_name, working_dir, display_name, resume_from_id, flags, created_at) "
            "VALUES (?, ?, ?, ?, ?, ?, ?, ?)",
            (session_id, agent_type, agent_name, working_dir, display_name, resume_from_id, flags_json, now),
        )
        await conn.commit()

    async def update_live_session_display_name(self, session_id: str, display_name: str) -> None:
        conn = await self._get_conn()
        await conn.execute(
            "UPDATE live_sessions SET display_name = ? WHERE session_id = ?",
            (display_name, session_id),
        )
        await conn.commit()

    async def unregister_live_session(self, session_id: str) -> None:
        conn = await self._get_conn()
        await conn.execute("DELETE FROM live_sessions WHERE session_id = ?", (session_id,))
        await conn.commit()

    async def replace_live_session(self, old_session_id: str, new_session_id: str, agent_type: str, agent_name: str, working_dir: str, display_name: str | None = None, resume_from_id: str | None = None, flags: list[str] | None = None) -> None:
        import json as _json
        conn = await self._get_conn()
        now = datetime.now(timezone.utc).isoformat()
        if flags is None:
            row = await (await conn.execute(
                "SELECT flags FROM live_sessions WHERE session_id = ?", (old_session_id,)
            )).fetchone()
            if row and row["flags"]:
                flags_json = row["flags"]
            else:
                flags_json = None
        else:
            flags_json = _json.dumps(flags) if flags else None
        await conn.execute("DELETE FROM live_sessions WHERE session_id = ?", (old_session_id,))
        await conn.execute(
            "INSERT OR REPLACE INTO live_sessions "
            "(session_id, agent_type, agent_name, working_dir, display_name, resume_from_id, flags, created_at) "
            "VALUES (?, ?, ?, ?, ?, ?, ?, ?)",
            (new_session_id, agent_type, agent_name, working_dir, display_name, resume_from_id, flags_json, now),
        )
        await conn.commit()

    async def get_all_live_sessions(self) -> list[dict[str, Any]]:
        import json as _json
        conn = await self._get_conn()
        rows = await (await conn.execute(
            "SELECT session_id, agent_type, agent_name, working_dir, display_name, resume_from_id, flags, created_at "
            "FROM live_sessions ORDER BY created_at"
        )).fetchall()
        results = []
        for r in rows:
            d = dict(r)
            if d.get("flags"):
                try:
                    d["flags"] = _json.loads(d["flags"])
                except (ValueError, TypeError):
                    d["flags"] = None
            results.append(d)
        return results

    # ── Bulk queries for enriching history list ─────────────────────────────

    async def get_all_session_metadata(self) -> dict[str, dict[str, Any]]:
        """Return {session_id: {tags: [...], has_notes: bool}} for all known sessions."""
        conn = await self._get_conn()
        meta_rows = await (await conn.execute(
            "SELECT session_id, notes_md, auto_summary, is_user_edited FROM session_meta"
        )).fetchall()

        result: dict[str, dict[str, Any]] = {}
        for r in meta_rows:
            has_notes = bool(r["notes_md"]) or bool(r["auto_summary"])
            result[r["session_id"]] = {
                "has_notes": has_notes,
                "is_user_edited": bool(r["is_user_edited"]),
                "tags": [],
            }

        tag_rows = await (await conn.execute(
            """SELECT st.session_id, t.id, t.name, t.color
               FROM session_tags st
               JOIN tags t ON t.id = st.tag_id
               ORDER BY t.name"""
        )).fetchall()

        for r in tag_rows:
            sid = r["session_id"]
            if sid not in result:
                result[sid] = {"has_notes": False, "is_user_edited": False, "tags": []}
            result[sid]["tags"].append({
                "id": r["id"],
                "name": r["name"],
                "color": r["color"],
            })

        return result
