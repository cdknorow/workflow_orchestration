"""Webhook configuration and delivery database operations."""

from __future__ import annotations

from datetime import datetime, timezone
from typing import Any

from coral.store.connection import DatabaseManager


class WebhookStore(DatabaseManager):
    """Webhook configs and delivery queue CRUD operations."""

    # Deferred delivery pruning — only prune every N inserts
    _delivery_insert_count: int = 0
    _DELIVERY_PRUNE_INTERVAL: int = 50

    async def _ensure_schema(self, conn) -> None:
        await conn.executescript("""
            CREATE TABLE IF NOT EXISTS webhook_configs (
                id                     INTEGER PRIMARY KEY AUTOINCREMENT,
                name                   TEXT NOT NULL,
                platform               TEXT NOT NULL,
                url                    TEXT NOT NULL,
                enabled                INTEGER NOT NULL DEFAULT 1,
                event_filter           TEXT NOT NULL DEFAULT '*',
                idle_threshold_seconds INTEGER NOT NULL DEFAULT 0,
                agent_filter           TEXT,
                low_confidence_only    INTEGER NOT NULL DEFAULT 0,
                consecutive_failures   INTEGER NOT NULL DEFAULT 0,
                created_at             TEXT NOT NULL,
                updated_at             TEXT NOT NULL
            );

            CREATE TABLE IF NOT EXISTS webhook_deliveries (
                id            INTEGER PRIMARY KEY AUTOINCREMENT,
                webhook_id    INTEGER NOT NULL,
                agent_name    TEXT NOT NULL,
                session_id    TEXT,
                event_type    TEXT NOT NULL,
                event_summary TEXT NOT NULL,
                status        TEXT NOT NULL DEFAULT 'pending',
                http_status   INTEGER,
                error_msg     TEXT,
                attempt_count INTEGER NOT NULL DEFAULT 0,
                next_retry_at TEXT,
                delivered_at  TEXT,
                created_at    TEXT NOT NULL
            );

            CREATE INDEX IF NOT EXISTS idx_webhook_deliveries_webhook
                ON webhook_deliveries(webhook_id, created_at DESC);

            CREATE INDEX IF NOT EXISTS idx_webhook_deliveries_pending
                ON webhook_deliveries(status, next_retry_at)
                WHERE status = 'pending';
        """)
        await conn.commit()

    # ── Webhook Configs ───────────────────────────────────────────────────

    async def list_webhook_configs(
        self, enabled_only: bool = False
    ) -> list[dict[str, Any]]:
        conn = await self._get_conn()
        sql = ("SELECT * FROM webhook_configs WHERE enabled = 1 ORDER BY created_at"
               if enabled_only else
               "SELECT * FROM webhook_configs ORDER BY created_at")
        rows = await (await conn.execute(sql)).fetchall()
        return [dict(r) for r in rows]

    async def get_webhook_config(self, webhook_id: int) -> dict[str, Any] | None:
        conn = await self._get_conn()
        row = await (await conn.execute(
            "SELECT * FROM webhook_configs WHERE id = ?", (webhook_id,)
        )).fetchone()
        return dict(row) if row else None

    async def create_webhook_config(
        self, name: str, platform: str, url: str,
        agent_filter: str | None = None,
    ) -> dict[str, Any]:
        now = datetime.now(timezone.utc).isoformat()
        conn = await self._get_conn()
        cur = await conn.execute(
            """INSERT INTO webhook_configs
               (name, platform, url, enabled, event_filter,
                idle_threshold_seconds, agent_filter, low_confidence_only,
                consecutive_failures, created_at, updated_at)
               VALUES (?, ?, ?, 1, '*', 0, ?, 0, 0, ?, ?)""",
            (name, platform, url, agent_filter, now, now),
        )
        await conn.commit()
        return await self.get_webhook_config(cur.lastrowid)

    async def update_webhook_config(self, webhook_id: int, **fields) -> None:
        now = datetime.now(timezone.utc).isoformat()
        conn = await self._get_conn()
        allowed = {
            "name", "platform", "url", "enabled", "event_filter",
            "idle_threshold_seconds", "agent_filter", "low_confidence_only",
            "consecutive_failures",
        }
        set_clauses = ["updated_at = ?"]
        params: list[Any] = [now]
        for key, val in fields.items():
            if key in allowed:
                set_clauses.append(f"{key} = ?")
                params.append(val)
        params.append(webhook_id)
        await conn.execute(
            f"UPDATE webhook_configs SET {', '.join(set_clauses)} WHERE id = ?",
            params,
        )
        await conn.commit()

    async def delete_webhook_config(self, webhook_id: int) -> None:
        conn = await self._get_conn()
        # Manual cascade: delete deliveries first (no FK constraint in schema)
        await conn.execute(
            "DELETE FROM webhook_deliveries WHERE webhook_id = ?", (webhook_id,)
        )
        await conn.execute(
            "DELETE FROM webhook_configs WHERE id = ?", (webhook_id,)
        )
        await conn.commit()

    # ── Webhook Deliveries ────────────────────────────────────────────────

    async def create_webhook_delivery(
        self, webhook_id: int, agent_name: str, event_type: str,
        event_summary: str, session_id: str | None = None,
    ) -> dict[str, Any]:
        now = datetime.now(timezone.utc).isoformat()
        conn = await self._get_conn()
        cur = await conn.execute(
            """INSERT INTO webhook_deliveries
               (webhook_id, agent_name, session_id, event_type,
                event_summary, status, attempt_count, created_at)
               VALUES (?, ?, ?, ?, ?, 'pending', 0, ?)""",
            (webhook_id, agent_name, session_id, event_type, event_summary, now),
        )
        delivery_id = cur.lastrowid
        # Deferred prune: only prune every N inserts to reduce lock contention
        WebhookStore._delivery_insert_count += 1
        if WebhookStore._delivery_insert_count >= WebhookStore._DELIVERY_PRUNE_INTERVAL:
            WebhookStore._delivery_insert_count = 0
            await conn.execute(
                "DELETE FROM webhook_deliveries WHERE webhook_id = ? "
                "AND status != 'pending' AND id NOT IN "
                "(SELECT id FROM webhook_deliveries WHERE webhook_id = ? "
                " AND status != 'pending' ORDER BY id DESC LIMIT 200)",
                (webhook_id, webhook_id),
            )
        await conn.commit()
        return {
            "id": delivery_id, "webhook_id": webhook_id,
            "status": "pending", "created_at": now,
            "agent_name": agent_name, "event_type": event_type,
            "event_summary": event_summary, "attempt_count": 0,
        }

    async def mark_webhook_delivery(
        self, delivery_id: int, status: str,
        http_status: int | None = None, error_msg: str | None = None,
        attempt_count: int | None = None, next_retry_at: str | None = None,
    ) -> None:
        now = datetime.now(timezone.utc).isoformat()
        conn = await self._get_conn()
        delivered_at = now if status == "delivered" else None
        await conn.execute(
            """UPDATE webhook_deliveries
               SET status = ?, http_status = ?, error_msg = ?,
                   attempt_count = COALESCE(?, attempt_count),
                   next_retry_at = ?, delivered_at = ?
               WHERE id = ?""",
            (status, http_status, error_msg, attempt_count,
             next_retry_at, delivered_at, delivery_id),
        )
        await conn.commit()

    async def get_pending_webhook_deliveries(
        self, limit: int = 50
    ) -> list[dict[str, Any]]:
        now = datetime.now(timezone.utc).isoformat()
        conn = await self._get_conn()
        rows = await (await conn.execute(
            """SELECT * FROM webhook_deliveries
               WHERE status = 'pending'
                 AND (next_retry_at IS NULL OR next_retry_at <= ?)
               ORDER BY created_at LIMIT ?""",
            (now, limit),
        )).fetchall()
        return [dict(r) for r in rows]

    async def list_webhook_deliveries(
        self, webhook_id: int, limit: int = 50
    ) -> list[dict[str, Any]]:
        conn = await self._get_conn()
        rows = await (await conn.execute(
            "SELECT * FROM webhook_deliveries WHERE webhook_id = ? "
            "ORDER BY created_at DESC LIMIT ?",
            (webhook_id, limit),
        )).fetchall()
        return [dict(r) for r in rows]

    async def increment_consecutive_failures(self, webhook_id: int) -> int:
        """Increment failure counter and return new value."""
        conn = await self._get_conn()
        await conn.execute(
            "UPDATE webhook_configs SET consecutive_failures = consecutive_failures + 1 "
            "WHERE id = ?", (webhook_id,),
        )
        await conn.commit()
        cfg = await self.get_webhook_config(webhook_id)
        return cfg["consecutive_failures"] if cfg else 0

    async def reset_consecutive_failures(self, webhook_id: int) -> None:
        conn = await self._get_conn()
        await conn.execute(
            "UPDATE webhook_configs SET consecutive_failures = 0 WHERE id = ?",
            (webhook_id,),
        )
        await conn.commit()

    async def auto_disable_webhook(self, webhook_id: int, reason: str) -> None:
        now = datetime.now(timezone.utc).isoformat()
        conn = await self._get_conn()
        await conn.execute(
            "UPDATE webhook_configs SET enabled = 0, updated_at = ? WHERE id = ?",
            (now, webhook_id),
        )
        await conn.commit()
