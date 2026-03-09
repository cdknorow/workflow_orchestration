# 05 — Webhook Notifications

## 1. Goal

Push outbound HTTP notifications to Slack, Discord, or any generic webhook endpoint when notable
agent events occur: a CONFIDENCE:Low signal fires, an agent goes idle (staleness threshold
exceeded), or a session's status/goal changes. Operators running Corral unattended — overnight
or across multiple worktrees — need ambient awareness without watching the dashboard. A single
well-placed Slack message about a low-confidence decision is worth hours of log review.

The feature adds three things:
1. A persistent webhook registry (URLs, filter rules, platform type) stored in SQLite.
2. A `WebhookDispatcher` background service that flushes queued deliveries with retry, and an
   `IdleDetector` that synthesizes idle events.
3. A configuration UI in the dashboard — a "Webhooks" modal with list, create/edit form,
   delivery history, and a test-fire button.

---

## 2. Database Schema

All new tables are managed by `WebhookStore._ensure_schema()` in the new file
`src/corral/store/webhooks.py`, following the same pattern as `SessionStore`, `GitStore`, and
`TaskStore` (each sub-store extends `DatabaseManager` and owns its own schema).

### 2.1 `webhook_configs`

```sql
CREATE TABLE IF NOT EXISTS webhook_configs (
    id                     INTEGER PRIMARY KEY AUTOINCREMENT,
    name                   TEXT NOT NULL,
    platform               TEXT NOT NULL,          -- 'slack' | 'discord' | 'generic'
    url                    TEXT NOT NULL,
    enabled                INTEGER NOT NULL DEFAULT 1,
    -- Comma-separated event_types to match, or '*' for all
    event_filter           TEXT NOT NULL DEFAULT '*',
    -- Seconds of agent inactivity before firing an idle notification; 0 = disabled
    idle_threshold_seconds INTEGER NOT NULL DEFAULT 0,
    -- Restrict to a specific agent_name; NULL = all agents
    agent_filter           TEXT,
    -- Only notify on CONFIDENCE:Low (payload starts with "Low ")
    low_confidence_only    INTEGER NOT NULL DEFAULT 0,
    -- Consecutive failure count for circuit breaker
    consecutive_failures   INTEGER NOT NULL DEFAULT 0,
    created_at             TEXT NOT NULL,
    updated_at             TEXT NOT NULL
);
```

### 2.2 `webhook_deliveries`

Append-only delivery log, auto-pruned to 200 rows per webhook (excluding pending rows to
prevent losing in-flight retries).

```sql
CREATE TABLE IF NOT EXISTS webhook_deliveries (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    webhook_id    INTEGER NOT NULL,
    agent_name    TEXT NOT NULL,
    session_id    TEXT,
    event_type    TEXT NOT NULL,
    event_summary TEXT NOT NULL,
    status        TEXT NOT NULL DEFAULT 'pending',  -- 'pending' | 'delivered' | 'failed'
    http_status   INTEGER,
    error_msg     TEXT,
    attempt_count INTEGER NOT NULL DEFAULT 0,
    next_retry_at TEXT,                             -- ISO-8601; NULL = deliver immediately
    delivered_at  TEXT,
    created_at    TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_webhook_deliveries_webhook
    ON webhook_deliveries(webhook_id, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_webhook_deliveries_pending
    ON webhook_deliveries(status, next_retry_at)
    WHERE status = 'pending';
```

**Note on foreign keys:** The codebase already enables `PRAGMA foreign_keys=ON` in
`DatabaseManager._get_conn()` (`store/connection.py:28`). However, `webhook_deliveries` does
**not** use a `REFERENCES` clause because `executescript()` implicitly issues `COMMIT` which
disables the pragma for statements within the script. Instead, cascading deletes are handled
manually in `WebhookStore.delete_webhook_config()`.

### 2.3 Where the DDL goes

The `CREATE TABLE` and `CREATE INDEX` statements go in `WebhookStore._ensure_schema()` inside
the new file `src/corral/store/webhooks.py`. This follows the established pattern: each
sub-store class extends `DatabaseManager` and defines its own `_ensure_schema()` override.

The shared connection mechanism in `CorralStore._get_conn()` (`store/__init__.py:33-43`)
ensures the webhook tables are created on the same database file as all other tables.

---

## 3. Backend Changes

### 3.1 New file: `src/corral/store/webhooks.py`

New sub-store for all webhook CRUD and delivery queue operations. Extends `DatabaseManager`
following the pattern of `store/tasks.py` and `store/git.py`.

```python
"""Webhook configuration and delivery database operations."""

from __future__ import annotations

from datetime import datetime, timezone, timedelta
from typing import Any

from corral.store.connection import DatabaseManager


class WebhookStore(DatabaseManager):
    """Webhook configs and delivery queue CRUD operations."""

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
        event_filter: str = "*", idle_threshold_seconds: int = 0,
        agent_filter: str | None = None, low_confidence_only: bool = False,
    ) -> dict[str, Any]:
        now = datetime.now(timezone.utc).isoformat()
        conn = await self._get_conn()
        cur = await conn.execute(
            """INSERT INTO webhook_configs
               (name, platform, url, enabled, event_filter,
                idle_threshold_seconds, agent_filter, low_confidence_only,
                consecutive_failures, created_at, updated_at)
               VALUES (?, ?, ?, 1, ?, ?, ?, ?, 0, ?, ?)""",
            (name, platform, url, event_filter, idle_threshold_seconds,
             agent_filter, int(low_confidence_only), now, now),
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
        # Prune to 200 deliveries per webhook, excluding pending rows
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

    async def get_last_event_times_by_agent(self) -> dict[str, str]:
        conn = await self._get_conn()
        rows = await (await conn.execute(
            "SELECT agent_name, MAX(created_at) as last_ts "
            "FROM agent_events GROUP BY agent_name"
        )).fetchall()
        return {r["agent_name"]: r["last_ts"] for r in rows if r["last_ts"]}

    async def idle_notification_exists(
        self, webhook_id: int, agent_name: str, threshold_seconds: int
    ) -> bool:
        cutoff = (
            datetime.now(timezone.utc) - timedelta(seconds=threshold_seconds)
        ).isoformat()
        conn = await self._get_conn()
        row = await (await conn.execute(
            "SELECT COUNT(*) as cnt FROM webhook_deliveries "
            "WHERE webhook_id = ? AND agent_name = ? AND event_type = 'idle' "
            "AND created_at >= ?",
            (webhook_id, agent_name, cutoff),
        )).fetchone()
        return bool(row and row["cnt"] > 0)

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
```

### 3.2 Register `WebhookStore` in `src/corral/store/__init__.py`

Add `WebhookStore` as a fourth sub-store in `CorralStore`, following the existing pattern:

```python
from corral.store.webhooks import WebhookStore

class CorralStore(DatabaseManager):
    def __init__(self, db_path: Path = DB_PATH) -> None:
        super().__init__(db_path)
        self._sessions = SessionStore(db_path)
        self._git = GitStore(db_path)
        self._tasks = TaskStore(db_path)
        self._webhooks = WebhookStore(db_path)   # NEW

    async def _get_conn(self):
        conn = await super()._get_conn()
        # Share connection with all sub-stores
        self._sessions._conn = self._conn
        self._sessions._schema_ensured = True
        self._git._conn = self._conn
        self._git._schema_ensured = True
        self._tasks._conn = self._conn
        self._tasks._schema_ensured = True
        self._webhooks._conn = self._conn         # NEW
        self._webhooks._schema_ensured = True      # NEW
        return conn
```

Then add a `# ── Delegate: WebhookStore methods ──` section at the bottom with delegation
methods for all 14 webhook methods, following the same `await self._get_conn(); return await
self._webhooks.method(...)` pattern used by every other delegation.

### 3.3 New file: `src/corral/background_tasks/webhook_dispatcher.py`

Core delivery engine. Follows the `GitPoller` pattern: `__init__(store)`, `run_forever(interval)`,
`run_once()`. Does **not** contain idle detection (that is a separate class — see §3.4).

```python
"""Webhook notification dispatcher for Corral."""

from __future__ import annotations

import asyncio
import logging
from datetime import datetime, timezone, timedelta
from typing import Any
from urllib.parse import urlparse

log = logging.getLogger(__name__)

RETRY_DELAYS = [30, 120, 600]  # 3 attempts: 30s, 2m, 10m
CIRCUIT_BREAKER_THRESHOLD = 10  # Auto-disable after N consecutive failures


class WebhookDispatcher:
    """Flushes pending webhook deliveries with retry and circuit breaker."""

    def __init__(self, store) -> None:
        self._store = store
        self._client = None  # httpx.AsyncClient, created lazily

    async def _get_client(self):
        import httpx
        if self._client is None or self._client.is_closed:
            self._client = httpx.AsyncClient(timeout=10.0)
        return self._client

    async def close(self) -> None:
        if self._client and not self._client.is_closed:
            await self._client.aclose()

    # ── Entry point called from API layer ─────────────────────────────

    async def dispatch(
        self,
        agent_name: str,
        event_type: str,
        summary: str,
        session_id: str | None = None,
    ) -> None:
        """Match event against enabled webhooks and enqueue deliveries."""
        try:
            configs = await self._store.list_webhook_configs(enabled_only=True)
            for cfg in configs:
                if self._matches(cfg, agent_name, event_type, summary):
                    await self._store.create_webhook_delivery(
                        webhook_id=cfg["id"],
                        agent_name=agent_name,
                        session_id=session_id,
                        event_type=event_type,
                        event_summary=summary,
                    )
        except Exception:
            log.exception("WebhookDispatcher.dispatch error")

    # ── Background flush loop ─────────────────────────────────────────

    async def run_forever(self, interval: float = 15) -> None:
        while True:
            try:
                await self.run_once()
            except Exception:
                log.exception("WebhookDispatcher flush error")
            await asyncio.sleep(interval)

    async def run_once(self) -> dict[str, int]:
        """Flush pending deliveries. Returns {"delivered": n, "failed": n}."""
        pending = await self._store.get_pending_webhook_deliveries(limit=50)
        delivered = 0
        failed = 0
        for delivery in pending:
            success = await self.deliver_now(delivery)
            if success:
                delivered += 1
            else:
                failed += 1
        return {"delivered": delivered, "failed": failed}

    # ── Event matching ────────────────────────────────────────────────

    def _matches(
        self, cfg: dict, agent_name: str, event_type: str, summary: str
    ) -> bool:
        if cfg["agent_filter"] and cfg["agent_filter"] != agent_name:
            return False
        if cfg["event_filter"] != "*":
            allowed = {e.strip() for e in cfg["event_filter"].split(",")}
            if event_type not in allowed:
                return False
        if cfg["low_confidence_only"]:
            if event_type != "confidence":
                return False
            if not summary.lower().startswith("low "):
                return False
        return True

    # ── HTTP delivery ─────────────────────────────────────────────────

    async def deliver_now(self, delivery: dict) -> bool:
        """Attempt immediate delivery. Returns True on success.

        This is a public method so the /test endpoint can bypass the queue.
        """
        cfg = await self._store.get_webhook_config(delivery["webhook_id"])
        if not cfg or not cfg["enabled"]:
            await self._store.mark_webhook_delivery(
                delivery["id"], status="failed",
                error_msg="Webhook disabled or deleted",
            )
            return False
        payload = _build_payload(cfg["platform"], delivery)
        attempt = delivery["attempt_count"] + 1
        try:
            client = await self._get_client()
            resp = await client.post(cfg["url"], json=payload)
            if 200 <= resp.status_code < 300:
                await self._store.mark_webhook_delivery(
                    delivery["id"], status="delivered",
                    http_status=resp.status_code, attempt_count=attempt,
                )
                await self._store.reset_consecutive_failures(cfg["id"])
                return True
            else:
                body = resp.text[:200]
                await self._schedule_retry_or_fail(
                    cfg, delivery, attempt, resp.status_code, body
                )
                return False
        except Exception as exc:
            await self._schedule_retry_or_fail(
                cfg, delivery, attempt, None, str(exc)[:200]
            )
            return False

    async def _schedule_retry_or_fail(
        self,
        cfg: dict,
        delivery: dict,
        attempt: int,
        http_status: int | None,
        error_msg: str,
    ) -> None:
        # Circuit breaker: auto-disable after N consecutive failures
        failure_count = await self._store.increment_consecutive_failures(cfg["id"])
        if failure_count >= CIRCUIT_BREAKER_THRESHOLD:
            await self._store.auto_disable_webhook(
                cfg["id"],
                f"Auto-disabled after {failure_count} consecutive failures"
            )
            log.warning(
                "Webhook %s (%s) auto-disabled after %d consecutive failures",
                cfg["id"], cfg["name"], failure_count,
            )

        if attempt > len(RETRY_DELAYS):
            await self._store.mark_webhook_delivery(
                delivery["id"], status="failed",
                http_status=http_status, error_msg=error_msg,
                attempt_count=attempt,
            )
            return
        delay = RETRY_DELAYS[attempt - 1]
        next_retry = (
            datetime.now(timezone.utc) + timedelta(seconds=delay)
        ).isoformat()
        await self._store.mark_webhook_delivery(
            delivery["id"], status="pending",
            http_status=http_status, error_msg=error_msg,
            attempt_count=attempt, next_retry_at=next_retry,
        )


# ── Payload builders (module-level, stateless) ────────────────────────


def _build_payload(platform: str, delivery: dict) -> dict:
    builders = {
        "slack": _slack_payload,
        "discord": _discord_payload,
    }
    return builders.get(platform, _generic_payload)(delivery)


def _slack_payload(delivery: dict) -> dict:
    emoji = {
        "confidence": ":warning:",
        "idle":       ":zzz:",
        "stop":       ":red_circle:",
        "status":     ":large_blue_circle:",
        "goal":       ":white_circle:",
    }.get(delivery["event_type"], ":bell:")
    return {
        "blocks": [{
            "type": "section",
            "text": {
                "type": "mrkdwn",
                "text": (
                    f"{emoji} *Corral — {delivery['event_type'].upper()}*\n"
                    f"*Agent:* `{delivery['agent_name']}`\n"
                    f"*Message:* {delivery['event_summary']}"
                ),
            },
        }]
    }


def _discord_payload(delivery: dict) -> dict:
    color = {
        "confidence": 0xD29922,  # amber
        "idle":       0xF85149,  # red
        "stop":       0xF85149,
    }.get(delivery["event_type"], 0x58A6FF)  # blue default
    return {
        "embeds": [{
            "title": f"Corral — {delivery['event_type'].upper()}",
            "description": delivery["event_summary"],
            "color": color,
            "fields": [{
                "name": "Agent",
                "value": f"`{delivery['agent_name']}`",
                "inline": True,
            }],
            "footer": {"text": "Corral"},
        }]
    }


def _generic_payload(delivery: dict) -> dict:
    return {
        "agent_name": delivery["agent_name"],
        "session_id": delivery["session_id"],
        "event_type": delivery["event_type"],
        "summary": delivery["event_summary"],
        "timestamp": delivery["created_at"],
        "source": "corral",
    }
```

### 3.4 New file: `src/corral/background_tasks/idle_detector.py`

Separated from the dispatcher for independent testability. Runs on its own cycle and
synthesizes `idle` event deliveries.

```python
"""Idle agent detection — synthesizes webhook events when agents go quiet."""

from __future__ import annotations

import asyncio
import logging
from datetime import datetime, timezone

log = logging.getLogger(__name__)


class IdleDetector:
    """Periodically checks for idle agents and creates webhook deliveries."""

    def __init__(self, store) -> None:
        self._store = store

    async def run_forever(self, interval: float = 60) -> None:
        while True:
            try:
                await self.run_once()
            except Exception:
                log.exception("IdleDetector error")
            await asyncio.sleep(interval)

    async def run_once(self) -> dict[str, int]:
        """Check all idle-enabled webhooks. Returns {"notifications": n}."""
        configs = await self._store.list_webhook_configs(enabled_only=True)
        idle_configs = [c for c in configs if c["idle_threshold_seconds"] > 0]
        if not idle_configs:
            return {"notifications": 0}

        last_active = await self._store.get_last_event_times_by_agent()
        now = datetime.now(timezone.utc)
        count = 0

        for cfg in idle_configs:
            threshold = cfg["idle_threshold_seconds"]
            for agent_name, last_ts_str in last_active.items():
                if cfg["agent_filter"] and cfg["agent_filter"] != agent_name:
                    continue
                try:
                    last_ts = datetime.fromisoformat(last_ts_str)
                    if last_ts.tzinfo is None:
                        last_ts = last_ts.replace(tzinfo=timezone.utc)
                    staleness = (now - last_ts).total_seconds()
                except Exception:
                    continue
                if staleness >= threshold:
                    already = await self._store.idle_notification_exists(
                        cfg["id"], agent_name, threshold
                    )
                    if not already:
                        await self._store.create_webhook_delivery(
                            webhook_id=cfg["id"],
                            agent_name=agent_name,
                            session_id=None,
                            event_type="idle",
                            event_summary=(
                                f"Agent idle for {int(staleness // 60)} minutes"
                            ),
                        )
                        count += 1
        return {"notifications": count}
```

### 3.5 Register in `src/corral/background_tasks/__init__.py`

Add imports so the new classes are accessible from the package:

```python
from corral.background_tasks.webhook_dispatcher import WebhookDispatcher
from corral.background_tasks.idle_detector import IdleDetector
```

### 3.6 Hook dispatch at the API layer (not the store)

Webhook dispatch is triggered from the API layer where events arrive, **not** from within the
store. This keeps the data layer free of side effects.

In `src/corral/api/live_sessions.py`, add a module-level reference:

```python
# Module-level dependency, set by web_server.py during app setup
webhook_dispatcher: WebhookDispatcher | None = None  # type: ignore
```

Then modify the two call sites where events are inserted:

**In `_track_status_summary_events()` (after each `store.insert_agent_event` call):**
```python
if status and status != prev.get("status"):
    await store.insert_agent_event(
        agent_name, "status", status, session_id=session_id,
    )
    if webhook_dispatcher:
        asyncio.create_task(
            webhook_dispatcher.dispatch(agent_name, "status", status, session_id)
        )
```

(Same pattern for the `goal` event below it.)

**In the `POST /api/sessions/live/{name}/events` endpoint (after `store.insert_agent_event`):**
```python
event = await store.insert_agent_event(
    name, event_type, summary,
    tool_name=tool_name, session_id=session_id, detail_json=detail_json,
)
if webhook_dispatcher:
    asyncio.create_task(
        webhook_dispatcher.dispatch(name, event_type, summary, session_id)
    )
return event
```

**Why `asyncio.create_task` instead of `asyncio.ensure_future`:** Both work, but `create_task`
is the modern idiom and returns a `Task` object that can be tracked. The dispatch is
fire-and-forget — errors are caught inside `dispatch()` itself.

### 3.7 Wire into `lifespan()` in `src/corral/web_server.py`

In the `lifespan` async context manager, after the existing background task setup (line 69):

```python
from corral.background_tasks import WebhookDispatcher, IdleDetector

dispatcher = WebhookDispatcher(store)
idle_detector = IdleDetector(store)

# Wire dispatcher into the API layer
live_sessions_api.webhook_dispatcher = dispatcher
app.state.webhook_dispatcher = dispatcher

webhook_task = asyncio.create_task(dispatcher.run_forever(interval=15))
idle_task = asyncio.create_task(idle_detector.run_forever(interval=60))
```

In the teardown section (after `yield`, before `await store.close()`):

```python
webhook_task.cancel()
idle_task.cancel()
try:
    await asyncio.wait_for(dispatcher.close(), timeout=5)
except asyncio.TimeoutError:
    log.warning("Webhook dispatcher close timed out")
```

Also add the dispatcher to `_set_store()` so tests can swap it:

```python
def _set_store(new_store):
    global store
    store = new_store
    live_sessions_api.store = new_store
    history_api.store = new_store
    system_api.store = new_store
    webhooks_api.store = new_store  # NEW
```

### 3.8 New file: `src/corral/api/webhooks.py`

New API router module following the pattern of `api/system.py` and `api/history.py`.

```python
"""API routes for webhook configuration and delivery history."""

from __future__ import annotations

import logging
from typing import TYPE_CHECKING
from urllib.parse import urlparse

from fastapi import APIRouter, Query

if TYPE_CHECKING:
    from corral.store import CorralStore

log = logging.getLogger(__name__)

router = APIRouter()

# Module-level dependencies, set by web_server.py during app setup
store: CorralStore = None  # type: ignore[assignment]
_app = None  # Set by web_server for accessing app.state


VALID_PLATFORMS = {"slack", "discord", "generic"}


def _validate_url(url: str, platform: str) -> str | None:
    """Return error message if URL is invalid, else None."""
    try:
        parsed = urlparse(url)
    except Exception:
        return "Invalid URL"
    if parsed.scheme not in ("http", "https"):
        return "URL must use http or https"
    if parsed.scheme == "http" and parsed.hostname not in ("localhost", "127.0.0.1"):
        return "HTTP (non-HTTPS) is only allowed for localhost"
    if not parsed.netloc:
        return "URL must have a hostname"
    return None


@router.get("/api/webhooks")
async def list_webhooks():
    """List all webhook configurations."""
    return await store.list_webhook_configs()


@router.post("/api/webhooks")
async def create_webhook(body: dict):
    """Create a new webhook configuration."""
    name = body.get("name", "").strip()
    platform = body.get("platform", "generic").strip()
    url = body.get("url", "").strip()
    if not name or not url:
        return {"error": "name and url are required"}
    if platform not in VALID_PLATFORMS:
        return {"error": f"platform must be one of: {', '.join(sorted(VALID_PLATFORMS))}"}
    url_error = _validate_url(url, platform)
    if url_error:
        return {"error": url_error}
    return await store.create_webhook_config(
        name=name,
        platform=platform,
        url=url,
        event_filter=body.get("event_filter", "*"),
        idle_threshold_seconds=int(body.get("idle_threshold_seconds", 0)),
        agent_filter=body.get("agent_filter") or None,
        low_confidence_only=bool(body.get("low_confidence_only", False)),
    )


@router.patch("/api/webhooks/{webhook_id}")
async def update_webhook(webhook_id: int, body: dict):
    """Update fields on a webhook configuration."""
    if "url" in body:
        url_error = _validate_url(body["url"], body.get("platform", "generic"))
        if url_error:
            return {"error": url_error}
    await store.update_webhook_config(webhook_id, **body)
    return {"ok": True}


@router.delete("/api/webhooks/{webhook_id}")
async def delete_webhook(webhook_id: int):
    """Delete a webhook configuration and all its delivery history."""
    await store.delete_webhook_config(webhook_id)
    return {"ok": True}


@router.post("/api/webhooks/{webhook_id}/test")
async def test_webhook(webhook_id: int):
    """Send a test notification immediately via direct delivery."""
    cfg = await store.get_webhook_config(webhook_id)
    if not cfg:
        return {"error": "Webhook not found"}
    delivery = await store.create_webhook_delivery(
        webhook_id=webhook_id,
        agent_name="corral-test",
        session_id=None,
        event_type="status",
        event_summary="Test notification from Corral dashboard",
    )
    dispatcher = getattr(_app.state, "webhook_dispatcher", None) if _app else None
    if dispatcher:
        await dispatcher.deliver_now(delivery)
    # Re-fetch to get updated status after delivery attempt
    deliveries = await store.list_webhook_deliveries(webhook_id, limit=1)
    return deliveries[0] if deliveries else {"ok": True}


@router.get("/api/webhooks/{webhook_id}/deliveries")
async def list_deliveries(
    webhook_id: int, limit: int = Query(50, ge=1, le=200)
):
    """Get recent delivery history for a webhook."""
    return await store.list_webhook_deliveries(webhook_id, limit=limit)
```

### 3.9 Register the webhooks router in `src/corral/web_server.py`

After the existing router imports (line 29):

```python
from corral.api import webhooks as webhooks_api
```

After the existing dependency wiring (line 91):

```python
webhooks_api.store = store
webhooks_api._app = app
```

After the existing `app.include_router` calls (line 96):

```python
app.include_router(webhooks_api.router)
```

### 3.10 No new dependencies

The original plan required `aiohttp>=3.9.0`. This is replaced by `httpx`, which is already
a transitive dependency of FastAPI's test client and is lighter weight. Add to `pyproject.toml`:

```toml
dependencies = [
    "fastapi>=0.104.0",
    "uvicorn[standard]>=0.24.0",
    "jinja2>=3.1.0",
    "aiosqlite>=0.19.0",
    "httpx>=0.25.0",
]
```

---

## 4. Frontend Changes

### 4.1 New file: `src/corral/static/webhooks.js`

```javascript
/* Webhook configuration management */

import { showToast, escapeHtml } from './utils.js';

// ── Valid event types for multi-select ────────────────────────────────────

const EVENT_TYPES = [
    { value: "status",     label: "Status" },
    { value: "goal",       label: "Goal" },
    { value: "confidence", label: "Confidence" },
    { value: "idle",       label: "Idle" },
    { value: "stop",       label: "Stop" },
];

// ── Modal open/close ──────────────────────────────────────────────────────

export function showWebhookModal() {
    document.getElementById("webhook-modal").style.display = "flex";
    loadWebhookList();
}

export function hideWebhookModal() {
    document.getElementById("webhook-modal").style.display = "none";
    _showView("list");
}

// ── Internal view switcher ────────────────────────────────────────────────

function _showView(view) {
    document.getElementById("webhook-list-view").style.display =
        view === "list" ? "flex" : "none";
    document.getElementById("webhook-form-view").style.display =
        view === "form" ? "flex" : "none";
    document.getElementById("webhook-history-view").style.display =
        view === "history" ? "flex" : "none";
}

export function showWebhookList() { _showView("list"); loadWebhookList(); }

// ── List ──────────────────────────────────────────────────────────────────

async function loadWebhookList() {
    try {
        const resp = await fetch("/api/webhooks");
        const webhooks = await resp.json();
        renderWebhookList(webhooks);
    } catch (e) {
        showToast("Failed to load webhooks", true);
    }
}

function renderWebhookList(webhooks) {
    const container = document.getElementById("webhook-list-body");
    if (!webhooks.length) {
        container.innerHTML =
            '<div class="webhook-empty">No webhooks configured. Click "+ Add Webhook".</div>';
        return;
    }
    container.innerHTML = webhooks.map(w => {
        const autoDisabled = !w.enabled && w.consecutive_failures >= 10;
        const statusLabel = autoDisabled
            ? `<span class="webhook-auto-disabled">Auto-disabled (${w.consecutive_failures} failures)</span>`
            : '';
        return `
        <div class="webhook-row">
            <div class="webhook-row-left">
                <span class="webhook-status-dot ${w.enabled ? 'dot-active' : 'dot-inactive'}"></span>
                <div class="webhook-row-info">
                    <span class="webhook-name">${escapeHtml(w.name)}</span>
                    <span class="webhook-meta">
                        ${escapeHtml(w.platform)} &middot;
                        filter: ${escapeHtml(w.event_filter)}
                        ${w.idle_threshold_seconds > 0
                            ? ` &middot; idle &ge;${w.idle_threshold_seconds}s`
                            : ''}
                    </span>
                    ${statusLabel}
                </div>
            </div>
            <div class="webhook-row-actions">
                <button class="btn btn-small" onclick="testWebhook(${w.id})">Test</button>
                <button class="btn btn-small" onclick="showWebhookHistory(${w.id})">History</button>
                <button class="btn btn-small" onclick="showWebhookEdit(${w.id})">Edit</button>
                <button class="btn btn-small btn-danger"
                    onclick="deleteWebhook(${w.id})">Delete</button>
            </div>
        </div>`;
    }).join('');
}

// ── Create / Edit form ────────────────────────────────────────────────────

function _renderEventFilterCheckboxes(selected) {
    const selectedSet = selected === "*"
        ? new Set(EVENT_TYPES.map(e => e.value))
        : new Set(selected.split(",").map(s => s.trim()));
    return EVENT_TYPES.map(e => `
        <label class="checkbox-label checkbox-inline">
            <input type="checkbox" class="wh-event-cb" value="${e.value}"
                   ${selectedSet.has(e.value) ? 'checked' : ''}>
            ${e.label}
        </label>
    `).join('');
}

function _getSelectedEventFilter() {
    const checked = [...document.querySelectorAll('.wh-event-cb:checked')]
        .map(cb => cb.value);
    if (checked.length === 0 || checked.length === EVENT_TYPES.length) return "*";
    return checked.join(",");
}

export function showWebhookCreate() {
    _showView("form");
    document.getElementById("webhook-form-title").textContent = "Add Webhook";
    document.getElementById("webhook-form").reset();
    document.getElementById("webhook-form-id").value = "";
    document.getElementById("wh-enabled").checked = true;
    document.getElementById("wh-event-filter-group").innerHTML =
        _renderEventFilterCheckboxes("*");
}

export async function showWebhookEdit(webhookId) {
    try {
        const resp = await fetch("/api/webhooks");
        const all = await resp.json();
        const w = all.find(x => x.id === webhookId);
        if (!w) return;
        _showView("form");
        document.getElementById("webhook-form-title").textContent = "Edit Webhook";
        document.getElementById("webhook-form-id").value = w.id;
        document.getElementById("wh-name").value = w.name;
        document.getElementById("wh-platform").value = w.platform;
        document.getElementById("wh-url").value = w.url;
        document.getElementById("wh-event-filter-group").innerHTML =
            _renderEventFilterCheckboxes(w.event_filter);
        document.getElementById("wh-idle-threshold").value = w.idle_threshold_seconds;
        document.getElementById("wh-agent-filter").value = w.agent_filter || "";
        document.getElementById("wh-low-confidence-only").checked =
            !!w.low_confidence_only;
        document.getElementById("wh-enabled").checked = !!w.enabled;
    } catch (e) {
        showToast("Failed to load webhook", true);
    }
}

export async function saveWebhook() {
    const id = document.getElementById("webhook-form-id").value;
    const payload = {
        name:    document.getElementById("wh-name").value.trim(),
        platform: document.getElementById("wh-platform").value,
        url:     document.getElementById("wh-url").value.trim(),
        event_filter: _getSelectedEventFilter(),
        idle_threshold_seconds:
            parseInt(document.getElementById("wh-idle-threshold").value || "0"),
        agent_filter:
            document.getElementById("wh-agent-filter").value.trim() || null,
        low_confidence_only:
            document.getElementById("wh-low-confidence-only").checked ? 1 : 0,
        enabled: document.getElementById("wh-enabled").checked ? 1 : 0,
    };
    if (!payload.name || !payload.url) {
        showToast("Name and URL are required", true);
        return;
    }
    try {
        if (id) {
            const resp = await fetch(`/api/webhooks/${id}`, {
                method: "PATCH",
                headers: { "Content-Type": "application/json" },
                body: JSON.stringify(payload),
            });
            const result = await resp.json();
            if (result.error) { showToast(result.error, true); return; }
            showToast("Webhook updated");
        } else {
            const resp = await fetch("/api/webhooks", {
                method: "POST",
                headers: { "Content-Type": "application/json" },
                body: JSON.stringify(payload),
            });
            const result = await resp.json();
            if (result.error) { showToast(result.error, true); return; }
            showToast("Webhook created");
        }
        showWebhookList();
    } catch (e) {
        showToast("Failed to save webhook", true);
    }
}

// ── Delete ────────────────────────────────────────────────────────────────

export async function deleteWebhook(webhookId) {
    if (!confirm("Delete this webhook and all its history?")) return;
    try {
        await fetch(`/api/webhooks/${webhookId}`, { method: "DELETE" });
        showToast("Webhook deleted");
        loadWebhookList();
    } catch (e) {
        showToast("Failed to delete webhook", true);
    }
}

// ── Test ──────────────────────────────────────────────────────────────────

export async function testWebhook(webhookId) {
    showToast("Sending test notification...");
    try {
        const resp = await fetch(`/api/webhooks/${webhookId}/test`, {
            method: "POST",
        });
        const result = await resp.json();
        if (result.status === "delivered") {
            showToast("Test delivered successfully");
        } else if (result.error) {
            showToast(`Test failed: ${result.error}`, true);
        } else {
            showToast(`Queued (status: ${result.status || "pending"})`);
        }
    } catch (e) {
        showToast("Test failed", true);
    }
}

// ── Delivery history ──────────────────────────────────────────────────────

export async function showWebhookHistory(webhookId) {
    _showView("history");
    try {
        const resp = await fetch(
            `/api/webhooks/${webhookId}/deliveries?limit=50`
        );
        const deliveries = await resp.json();
        renderWebhookHistory(deliveries);
    } catch (e) {
        showToast("Failed to load history", true);
    }
}

function renderWebhookHistory(deliveries) {
    const container = document.getElementById("webhook-history-body");
    if (!deliveries.length) {
        container.innerHTML =
            '<div class="webhook-empty">No deliveries yet.</div>';
        return;
    }
    container.innerHTML = deliveries.map(d => {
        const statusCls =
            d.status === "delivered" ? "delivery-ok" :
            d.status === "failed"    ? "delivery-fail" : "delivery-pending";
        const ts = d.created_at
            ? new Date(d.created_at).toLocaleString() : "\u2014";
        const http = d.http_status ? ` (HTTP ${d.http_status})` : "";
        return `
            <div class="delivery-row ${statusCls}">
                <span class="delivery-status">${escapeHtml(d.status)}</span>
                <span class="delivery-agent">${escapeHtml(d.agent_name)}</span>
                <span class="delivery-event">${escapeHtml(d.event_type)}</span>
                <span class="delivery-summary" title="${escapeHtml(d.event_summary)}">
                    ${escapeHtml(d.event_summary)}
                </span>
                <span class="delivery-ts">${ts}</span>
                ${d.error_msg
                    ? `<span class="delivery-error">${escapeHtml(d.error_msg)}${http}</span>`
                    : ''}
            </div>
        `;
    }).join('');
}
```

### 4.2 Webhook modal HTML — added to `src/corral/templates/includes/modals.html`

Append to the end of `modals.html`, following the pattern of existing modals:

```html
<!-- Webhooks Modal -->
<div id="webhook-modal" class="modal" style="display:none">
    <div class="modal-content modal-content-wide">
        <h3>Webhook Notifications</h3>

        <!-- List view -->
        <div id="webhook-list-view" style="display:flex;flex-direction:column;gap:12px">
            <div style="display:flex;justify-content:flex-end">
                <button class="btn btn-small btn-primary" onclick="showWebhookCreate()">+ Add Webhook</button>
            </div>
            <div id="webhook-list-body"></div>
        </div>

        <!-- Create / Edit form -->
        <div id="webhook-form-view" style="display:none;flex-direction:column;gap:12px">
            <h4 id="webhook-form-title">Add Webhook</h4>
            <input type="hidden" id="webhook-form-id">
            <form id="webhook-form" class="webhook-form"
                  onsubmit="event.preventDefault(); saveWebhook()">
                <label>Name
                    <input id="wh-name" type="text"
                           placeholder="e.g. Slack #agents" required>
                </label>
                <label>Platform
                    <select id="wh-platform">
                        <option value="slack">Slack</option>
                        <option value="discord">Discord</option>
                        <option value="generic">Generic HTTP POST</option>
                    </select>
                </label>
                <label>Webhook URL
                    <input id="wh-url" type="url"
                           placeholder="https://hooks.slack.com/..." required>
                </label>
                <label>Event Types
                    <div id="wh-event-filter-group" class="event-filter-group"></div>
                </label>
                <label>Idle Threshold (seconds — 0 to disable)
                    <input id="wh-idle-threshold" type="number" min="0" value="0">
                </label>
                <label>Agent Filter (blank = all agents)
                    <input id="wh-agent-filter" type="text"
                           placeholder="claude-agent-1">
                </label>
                <label class="checkbox-label">
                    <input id="wh-low-confidence-only" type="checkbox">
                    Only notify on CONFIDENCE:Low (skip CONFIDENCE:High)
                </label>
                <label class="checkbox-label">
                    <input id="wh-enabled" type="checkbox" checked>
                    Enabled
                </label>
                <div style="display:flex;gap:8px;margin-top:4px">
                    <button type="submit" class="btn btn-primary">Save</button>
                    <button type="button" class="btn"
                            onclick="showWebhookList()">Cancel</button>
                </div>
            </form>
        </div>

        <!-- Delivery history view -->
        <div id="webhook-history-view"
             style="display:none;flex-direction:column;gap:8px">
            <button class="btn btn-small"
                    onclick="showWebhookList()">&#8592; Back</button>
            <div id="webhook-history-body"></div>
        </div>
    </div>
</div>
```

### 4.3 Bell icon button in the top bar

In `templates/index.html`, locate the `.top-bar` `<header>` element. Add after the existing
brand/button group:

```html
<button class="top-bar-btn" onclick="showWebhookModal()"
        title="Webhook notifications">
    <svg width="16" height="16" viewBox="0 0 16 16" fill="none"
         stroke="currentColor" stroke-width="1.5" stroke-linecap="round"
         stroke-linejoin="round">
        <path d="M8 1a5 5 0 0 1 5 5v2.5l1.5 2H1.5L3 8.5V6A5 5 0 0 1 8 1z"/>
        <path d="M6 13a2 2 0 0 0 4 0"/>
    </svg>
</button>
```

### 4.4 Wire into `src/corral/static/app.js`

Add import at the top (after the existing imports):

```javascript
import {
    showWebhookModal, hideWebhookModal, showWebhookCreate,
    showWebhookList, showWebhookEdit, saveWebhook, deleteWebhook,
    testWebhook, showWebhookHistory,
} from './webhooks.js';
```

Add window assignments after the existing block:

```javascript
window.showWebhookModal  = showWebhookModal;
window.hideWebhookModal  = hideWebhookModal;
window.showWebhookCreate = showWebhookCreate;
window.showWebhookList   = showWebhookList;
window.showWebhookEdit   = showWebhookEdit;
window.saveWebhook       = saveWebhook;
window.deleteWebhook     = deleteWebhook;
window.testWebhook       = testWebhook;
window.showWebhookHistory = showWebhookHistory;
```

### 4.5 Close handlers in `src/corral/static/modals.js`

In the `document.addEventListener("click", ...)` handler (line 311), add:

```javascript
const webhookModal = document.getElementById("webhook-modal");
if (e.target === webhookModal) window.hideWebhookModal?.();
```

In the `keydown` Escape handler (line 338), add:

```javascript
window.hideWebhookModal?.();
```

Uses `window.hideWebhookModal?.()` to avoid importing from `webhooks.js` and creating a
circular dependency.

### 4.6 CSS additions to `src/corral/static/style.css`

Append at end of file:

```css
/* ── Webhook Modal ──────────────────────────────────────────────────────── */

.webhook-row {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 10px 12px;
    background: var(--bg-tertiary);
    border-radius: 6px;
    border: 1px solid var(--border);
    gap: 12px;
    margin-bottom: 6px;
}

.webhook-row-left {
    display: flex;
    align-items: center;
    gap: 10px;
    min-width: 0;
    flex: 1;
}

.webhook-row-info {
    display: flex;
    flex-direction: column;
    gap: 2px;
    min-width: 0;
}

.webhook-name {
    font-size: 13px;
    font-weight: 500;
    color: var(--text-primary);
}

.webhook-meta {
    font-size: 11px;
    color: var(--text-muted);
}

.webhook-auto-disabled {
    font-size: 11px;
    color: var(--error);
    font-style: italic;
}

.webhook-status-dot {
    width: 8px;
    height: 8px;
    border-radius: 50%;
    flex-shrink: 0;
}

.dot-active   { background: var(--success); }
.dot-inactive { background: var(--text-muted); }

.webhook-row-actions {
    display: flex;
    gap: 6px;
    flex-shrink: 0;
}

.webhook-empty {
    color: var(--text-muted);
    font-size: 13px;
    text-align: center;
    padding: 24px 0;
}

/* Event filter checkboxes */

.event-filter-group {
    display: flex;
    flex-wrap: wrap;
    gap: 8px 16px;
    padding: 4px 0;
}

.checkbox-inline {
    flex-direction: row !important;
    align-items: center;
    gap: 6px !important;
    font-size: 13px !important;
    color: var(--text-primary) !important;
    cursor: pointer;
    white-space: nowrap;
}

/* Create/edit form */

.webhook-form {
    display: flex;
    flex-direction: column;
    gap: 10px;
}

.webhook-form label {
    display: flex;
    flex-direction: column;
    gap: 4px;
    font-size: 12px;
    color: var(--text-secondary);
}

.webhook-form input[type="text"],
.webhook-form input[type="url"],
.webhook-form input[type="number"],
.webhook-form select {
    background: var(--bg-tertiary);
    border: 1px solid var(--border);
    border-radius: 4px;
    color: var(--text-primary);
    font-size: 13px;
    padding: 6px 8px;
    outline: none;
}

.webhook-form input:focus,
.webhook-form select:focus {
    border-color: var(--accent);
}

.checkbox-label {
    flex-direction: row !important;
    align-items: center;
    gap: 8px !important;
    cursor: pointer;
    font-size: 13px !important;
    color: var(--text-primary) !important;
}

/* Delivery history */

.delivery-row {
    display: grid;
    grid-template-columns: 80px 130px 90px 1fr 150px;
    align-items: center;
    gap: 8px;
    padding: 7px 8px;
    border-radius: 4px;
    font-size: 12px;
    border-left: 3px solid var(--border);
    margin-bottom: 4px;
}

.delivery-ok      { border-left-color: var(--success); }
.delivery-fail    { border-left-color: var(--error); }
.delivery-pending { border-left-color: var(--warning); }

.delivery-status {
    font-weight: 600;
    text-transform: uppercase;
    font-size: 10px;
    letter-spacing: 0.04em;
}

.delivery-ok   .delivery-status { color: var(--success); }
.delivery-fail .delivery-status { color: var(--error); }
.delivery-pending .delivery-status { color: var(--warning); }

.delivery-agent { color: var(--accent); font-family: monospace; font-size: 11px; }
.delivery-event { color: var(--text-secondary); }

.delivery-summary {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    color: var(--text-secondary);
}

.delivery-ts {
    color: var(--text-muted);
    font-size: 11px;
    text-align: right;
}

.delivery-error {
    grid-column: 1 / -1;
    color: var(--error);
    font-size: 11px;
    padding-left: 4px;
    font-family: monospace;
}
```

---

## 5. Supported Platforms

| Platform | Payload format | Auth mechanism |
|---|---|---|
| **Slack** | Block Kit JSON — `blocks` array with a single `section/mrkdwn` block | Secret embedded in the Incoming Webhook URL |
| **Discord** | Webhook embeds JSON — `embeds` array with `title`, `description`, `color`, `fields` | Secret embedded in the Discord Webhook URL |
| **Generic** | Flat JSON: `agent_name`, `session_id`, `event_type`, `summary`, `timestamp`, `source` | None built-in; embed a secret in the URL path if needed |

Platform is selected at webhook creation time and stored in `webhook_configs.platform`.
The `_build_payload()` function dispatches to the correct builder via a dict lookup.
Adding a new platform later requires only a new builder function and a new `<option>` in the
form.

---

## 6. Event Matching

`WebhookDispatcher._matches()` applies three independent gates, evaluated in order. An event
must pass all gates to generate a delivery record.

**Gate 1 — Agent filter**
`cfg["agent_filter"]` is compared to `agent_name` by exact string equality.
`NULL` in the database means all agents pass.

**Gate 2 — Event type filter**
`cfg["event_filter"]` is either `"*"` (all pass) or a comma-separated list of `event_type`
strings (e.g., `"confidence,idle"`). The event's `event_type` must be a member of that set.

Valid `event_type` values from the existing system:
- `status` — PULSE:STATUS changes (deduped: only fires on value change)
- `goal` — PULSE:SUMMARY changes (deduped: only fires on value change)
- `confidence` — PULSE:CONFIDENCE (every occurrence fires)
- `idle` — synthesized by `IdleDetector`, not from PULSE
- `stop` — emitted by `hooks/agentic_state.py` on agent Stop hook

**Gate 3 — Low-confidence-only filter**
When `cfg["low_confidence_only"]` is true, the event must have `event_type == "confidence"`
AND `summary` must begin with `"Low "` (case-insensitive prefix check). This matches the
format stored by `hooks/agentic_state.py`: PULSE:CONFIDENCE payloads are stored verbatim as
the `summary` column value (e.g., `"Low Unfamiliar with this auth library"`).

---

## 7. Message Formatting

### Slack (Block Kit)

```
:warning: *Corral — CONFIDENCE*
*Agent:* `claude-agent-1`
*Message:* Low Unfamiliar with this auth library — guessing at the API
```

### Discord (Embed)

- **Title:** `Corral — CONFIDENCE`
- **Description:** `Low Unfamiliar with this auth library — guessing at the API`
- **Color:** `0xD29922` (amber, matching `--warning` from `style.css`)
- **Field:** `Agent: claude-agent-1`
- **Footer:** `Corral`

### Generic HTTP POST body

```json
{
  "agent_name": "claude-agent-1",
  "session_id": "abc123...",
  "event_type": "confidence",
  "summary": "Low Unfamiliar with this auth library — guessing at the API",
  "timestamp": "2026-03-05T14:22:00.000000+00:00",
  "source": "corral"
}
```

### Idle notification

The `event_summary` is synthesized as: `"Agent idle for N minutes"`.
Platform formatting is identical to other event types — the same payload builders handle it.

---

## 8. Edge Cases

### Outbound rate limiting

The dispatcher processes at most 50 pending deliveries per 15-second cycle. For high-frequency
event setups, narrow `event_filter` to `confidence` or `idle` — STATUS events are already
deduplicated upstream by `_track_status_summary_events()` in `api/live_sessions.py` (only
fires on value change), so they produce at most one delivery per distinct status string per
agent.

### Delivery failures and retry

Three attempts with backoff: 30 seconds, then 2 minutes, then 10 minutes. After the third
failure the delivery is marked `failed` with the last HTTP status and error message. The
History UI shows these failures.

### Circuit breaker

After 10 consecutive delivery failures across any deliveries for a webhook, the webhook is
automatically disabled (`enabled = 0`). The UI shows "Auto-disabled (N failures)" with a
visual indicator. The operator can re-enable the webhook from the edit form after fixing the
URL. Successful deliveries reset the consecutive failure counter to 0.

### Delivery pruning safety

The prune query in `create_webhook_delivery` excludes `status = 'pending'` rows to prevent
deleting deliveries that are mid-retry but haven't been delivered yet.

### Duplicate idle notifications

`idle_notification_exists()` queries `webhook_deliveries` for any idle delivery for the same
`(webhook_id, agent_name)` pair within the last `threshold_seconds` window. This ensures one
idle notification per idle window even if `check_idle_agents()` runs many times. The window
resets once the agent becomes active again (next event will push `last_ts` forward).

### Duplicate CONFIDENCE notifications

CONFIDENCE events are not deduplicated — every PULSE:CONFIDENCE line in the agent log
produces one `agent_events` row (via the hooks) and therefore one potential delivery.
This is intentional: each new confidence signal warrants a fresh notification.

### Webhook URL validation

URLs are validated on create and update: must use HTTPS (or HTTP only for localhost), must
have a valid hostname. URLs are stored in plaintext in `~/.corral/sessions.db`. The database
path is user-home-scoped with default `600` permissions on macOS/Linux. The REST API returns
the full URL on `GET /api/webhooks` — acceptable for a local dashboard.

### Server restart recovery

Deliveries with `status = 'pending'` survive restarts because they persist in SQLite.
The `next_retry_at` column ensures deliveries that were mid-retry wait the remaining
backoff window before being retried again.

### Dispatcher close timeout

The `dispatcher.close()` call in teardown uses `asyncio.wait_for(..., timeout=5)` to prevent
hanging if HTTP connections are stuck during shutdown.

### Circular import prevention

`WebhookDispatcher` imports only `httpx` (lazily) and uses the `store` object passed at
construction. It does not import from `web_server.py` or `api/`. The dispatch call in
`api/live_sessions.py` uses the module-level `webhook_dispatcher` reference set by
`web_server.py` at startup — no static import of the dispatcher module needed in the API
layer (only a `TYPE_CHECKING` import for type hints).

---

## 9. Implementation Order

### Phase 1 — Database and storage (no visible changes; fully testable)

- [ ] Add `httpx>=0.25.0` to `dependencies` in `pyproject.toml`
- [ ] Create `src/corral/store/webhooks.py` with `WebhookStore(DatabaseManager)`
- [ ] Add `WebhookStore` to `CorralStore.__init__`, `_get_conn`, and `close` in
      `src/corral/store/__init__.py`
- [ ] Add all 14 delegation methods to `CorralStore`
- [ ] Smoke test: `python -c "import asyncio; from corral.store import CorralStore; s = CorralStore(); asyncio.run(s.list_webhook_configs())"`

### Phase 2 — Dispatcher and idle detector (testable via direct instantiation)

- [ ] Create `src/corral/background_tasks/webhook_dispatcher.py` with `WebhookDispatcher`
- [ ] Create `src/corral/background_tasks/idle_detector.py` with `IdleDetector`
- [ ] Export both from `src/corral/background_tasks/__init__.py`

### Phase 3 — Wire into server lifecycle

- [ ] Import `WebhookDispatcher` and `IdleDetector` in `web_server.py` `lifespan()`
- [ ] Construct dispatcher and idle detector, store on `app.state`, create background tasks
- [ ] Wire `live_sessions_api.webhook_dispatcher = dispatcher`
- [ ] Add teardown: cancel tasks, close dispatcher with timeout
- [ ] Update `_set_store()` to include `webhooks_api`

### Phase 4 — API layer hooks + REST endpoints

- [ ] Add `webhook_dispatcher` module-level ref to `api/live_sessions.py`
- [ ] Add `asyncio.create_task(webhook_dispatcher.dispatch(...))` at the 3 event insertion
      points in `api/live_sessions.py`
- [ ] Create `src/corral/api/webhooks.py` with 6 endpoints
- [ ] Register router in `web_server.py`
- [ ] `pip install -e .` and start server
- [ ] Test with curl:
      `curl -X POST http://localhost:8420/api/webhooks -H "Content-Type: application/json" \`
      `-d '{"name":"httpbin","platform":"generic","url":"https://httpbin.org/post"}'`
- [ ] `curl -X POST http://localhost:8420/api/webhooks/1/test`
- [ ] `curl http://localhost:8420/api/webhooks/1/deliveries`

### Phase 5 — Frontend

- [ ] Create `src/corral/static/webhooks.js`
- [ ] Add webhook modal HTML to `templates/includes/modals.html`
- [ ] Add bell icon button to top bar in `templates/index.html`
- [ ] Add import and `window.*` assignments in `static/app.js`
- [ ] Add `window.hideWebhookModal?.()` calls to click-outside and Escape handlers in
      `static/modals.js`
- [ ] Append CSS to `static/style.css`
- [ ] `pip install -e .` and verify UI renders — check browser console for JS errors

### Phase 6 — End-to-end validation

- [ ] Create a Slack Incoming Webhook URL at `api.slack.com/apps` (or use
      `https://httpbin.org/post` for local validation)
- [ ] Configure a webhook via the UI with event types = confidence,
      low_confidence_only = checked
- [ ] In a live agent's command pane, send:
      `||PULSE:CONFIDENCE Low Testing webhook delivery from command pane||`
- [ ] Confirm the delivery appears in the History view with `status = delivered`
- [ ] Confirm the Slack message arrives with correct Block Kit formatting
- [ ] Test circuit breaker: create a webhook with a bad URL, trigger 10+ events,
      confirm it auto-disables

---

## 10. File Summary

| File | Action | Key change |
|---|---|---|
| `src/corral/store/webhooks.py` | **Create** | `WebhookStore(DatabaseManager)` — 2 tables, 14 methods including circuit breaker |
| `src/corral/store/__init__.py` | **Modify** | Register `WebhookStore` as sub-store, add 14 delegation methods |
| `src/corral/background_tasks/webhook_dispatcher.py` | **Create** | Delivery engine: matching, HTTP delivery via httpx, retry, circuit breaker |
| `src/corral/background_tasks/idle_detector.py` | **Create** | Idle agent detection, synthesizes webhook events |
| `src/corral/background_tasks/__init__.py` | **Modify** | Export `WebhookDispatcher` and `IdleDetector` |
| `src/corral/api/webhooks.py` | **Create** | 6 REST endpoints with URL validation |
| `src/corral/api/live_sessions.py` | **Modify** | Add `webhook_dispatcher` ref, dispatch after event inserts (3 call sites) |
| `src/corral/web_server.py` | **Modify** | Wire dispatcher + idle detector into lifespan, register webhooks router |
| `src/corral/static/webhooks.js` | **Create** | Modal UI: list, create/edit form with checkbox event filter, delivery history |
| `src/corral/templates/includes/modals.html` | **Modify** | Webhook modal HTML |
| `src/corral/templates/index.html` | **Modify** | Bell icon in top bar |
| `src/corral/static/app.js` | **Modify** | Import + `window.*` registration for 9 webhook functions |
| `src/corral/static/modals.js` | **Modify** | `window.hideWebhookModal?.()` in click-outside and Escape handlers |
| `src/corral/static/style.css` | **Modify** | Webhook modal, event filter checkboxes, and delivery history CSS |
| `pyproject.toml` | **Modify** | Add `httpx>=0.25.0` |
