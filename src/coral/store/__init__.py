"""Coral store package — modular database layer.

The CoralStore class composes all domain-specific stores (sessions, git, tasks)
behind a single shared SQLite connection. It is the primary interface used by
the web server and background services.
"""

from __future__ import annotations

import functools
from pathlib import Path
from typing import Any

from coral.store.connection import DatabaseManager, DB_PATH
from coral.store.sessions import SessionStore
from coral.store.git import GitStore
from coral.store.tasks import TaskStore
from coral.store.schedule import ScheduleStore
from coral.store.webhooks import WebhookStore


class CoralStore(DatabaseManager):
    """Unified store that delegates to domain-specific sub-stores sharing one connection.

    All sub-stores share the same _conn and _db_path so there is only one
    SQLite connection per CoralStore instance.

    Methods are auto-delegated to the appropriate sub-store. Each delegated
    call ensures the shared connection is established before forwarding.
    """

    # Map sub-store attr names to their classes for __getattr__ delegation
    _SUB_STORES = {
        "_sessions": SessionStore,
        "_git": GitStore,
        "_tasks": TaskStore,
        "_schedule": ScheduleStore,
        "_webhooks": WebhookStore,
    }

    def __init__(self, db_path: Path = DB_PATH) -> None:
        super().__init__(db_path)
        self._sessions = SessionStore(db_path)
        self._git = GitStore(db_path)
        self._tasks = TaskStore(db_path)
        self._schedule = ScheduleStore(db_path)
        self._webhooks = WebhookStore(db_path)

    async def _get_conn(self):
        """Ensure all sub-stores share the same connection."""
        conn = await super()._get_conn()
        for attr_name in self._SUB_STORES:
            sub = getattr(self, attr_name)
            sub._conn = self._conn
            sub._schema_ensured = True
        return conn

    async def close(self) -> None:
        await super().close()
        for attr_name in self._SUB_STORES:
            getattr(self, attr_name)._conn = None

    def __getattr__(self, name: str):
        """Auto-delegate unknown methods to sub-stores with connection sync.

        Looks up `name` on each sub-store; if found, returns an async wrapper
        that calls _get_conn() first, then forwards the call.
        Only public methods (no leading underscore) are delegated.
        """
        if name.startswith("_"):
            raise AttributeError(f"'{type(self).__name__}' object has no attribute '{name}'")
        for attr_name in self._SUB_STORES:
            sub = object.__getattribute__(self, attr_name)
            method = getattr(sub, name, None)
            if method is not None and callable(method):
                @functools.wraps(method)
                async def _delegated(*args, _m=method, **kwargs):
                    await self._get_conn()
                    return await _m(*args, **kwargs)
                return _delegated
        raise AttributeError(f"'{type(self).__name__}' object has no attribute '{name}'")
