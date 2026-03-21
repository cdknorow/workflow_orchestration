"""Shared test fixtures — ensures every test run uses isolated temp databases.

This prevents SQLite lock contention when multiple agents run tests concurrently.
All stores are patched to use per-test temp directories instead of ~/.coral/.
"""

from __future__ import annotations

import atexit
import os
import threading
from contextlib import asynccontextmanager
from pathlib import Path
from unittest.mock import AsyncMock

import pytest


def _stop_aiosqlite_thread(conn):
    """Send the stop sentinel to an aiosqlite Connection's worker thread.

    aiosqlite's worker thread blocks on SimpleQueue.get(). The only way to
    unblock it is to send a function that returns _STOP_RUNNING_SENTINEL.
    """
    try:
        if conn._connection is not None:
            conn._connection.close()
            conn._connection = None
        from aiosqlite.core import _STOP_RUNNING_SENTINEL
        conn._tx.put_nowait((None, lambda: _STOP_RUNNING_SENTINEL))
        if hasattr(conn, "_thread") and conn._thread is not None:
            conn._thread.join(timeout=2)
    except Exception:
        pass


def _force_close_store(store):
    """Synchronously force-close a store's aiosqlite connection if open."""
    conn = getattr(store, "_conn", None)
    if conn is None:
        return
    store._conn = None
    store._schema_ensured = False
    _stop_aiosqlite_thread(conn)


_pytest_exit_status = 0


def pytest_sessionfinish(session, exitstatus):
    """Record the exit status for use by pytest_unconfigure."""
    global _pytest_exit_status
    _pytest_exit_status = exitstatus


def pytest_unconfigure(config):
    """Force-exit if aiosqlite worker threads would hang the process.

    aiosqlite spawns non-daemon threads that prevent Python from exiting.
    After all fixtures are torn down, any remaining worker threads are
    truly orphaned. Force-exit to prevent indefinite hang.
    """
    alive = [t for t in threading.enumerate()
             if "_connection_worker_thread" in t.name and t.is_alive()]
    if alive:
        os._exit(_pytest_exit_status)


# All module-level stores that may open aiosqlite connections during tests.
_ALL_STORES = []


def _register_stores():
    """Collect references to all module-level store singletons."""
    global _ALL_STORES
    if _ALL_STORES:
        return
    stores = []
    try:
        import coral.web_server as ws
        stores.append(ws.store)
        stores.append(ws.schedule_store)
    except (ImportError, AttributeError):
        pass
    try:
        import coral.messageboard.api as board_api_mod
        if board_api_mod.store is not None:
            stores.append(board_api_mod.store)
    except (ImportError, AttributeError):
        pass
    _ALL_STORES = stores


@pytest.fixture(autouse=True)
def _isolate_databases(tmp_path, monkeypatch):
    """Redirect all default DB paths to a per-test temp directory.

    This runs automatically for every test, ensuring no test touches
    the real ~/.coral/sessions.db or ~/.coral/messageboard.db.
    """
    tmp_db_dir = tmp_path / ".coral"
    tmp_db_dir.mkdir()

    # Set CORAL_DATA_DIR env var so get_data_dir() returns the temp path.
    monkeypatch.setenv("CORAL_DATA_DIR", str(tmp_db_dir))

    # Patch the module-level DB_DIR and DB_PATH in both store modules
    import coral.store.connection as conn_mod
    import coral.messageboard.store as board_mod

    monkeypatch.setattr(conn_mod, "DB_DIR", tmp_db_dir)
    monkeypatch.setattr(conn_mod, "DB_PATH", tmp_db_dir / "sessions.db")
    monkeypatch.setattr(board_mod, "DB_DIR", tmp_db_dir)
    monkeypatch.setattr(board_mod, "DB_PATH", tmp_db_dir / "messageboard.db")

    try:
        import coral.store.remote_boards as remote_mod
        monkeypatch.setattr(remote_mod, "DB_DIR", tmp_db_dir)
        monkeypatch.setattr(remote_mod, "DB_PATH", tmp_db_dir / "remote_boards.db")
    except (ImportError, AttributeError):
        pass

    # Collect store references and close any stale connections
    _register_stores()
    for store in _ALL_STORES:
        _force_close_store(store)

    # Redirect all stores to temp DB paths
    for store in _ALL_STORES:
        try:
            import coral.web_server as ws
            if store is ws.store or store is ws.schedule_store:
                store._db_path = tmp_db_dir / "sessions.db"
                continue
        except (ImportError, AttributeError):
            pass
        store._db_path = tmp_db_dir / "messageboard.db"

    # Replace the app's lifespan with a no-op version for tests.
    try:
        import coral.web_server as ws

        @asynccontextmanager
        async def _test_lifespan(app):
            app.state.startup_complete = True
            yield

        ws.app.router.lifespan_context = _test_lifespan
    except (ImportError, AttributeError):
        pass

    # Mock discover_coral_agents to avoid tmux calls in tests.
    # Note: we do NOT mock resume_persistent_sessions itself because
    # test_persistent_sessions.py needs to call the real implementation.
    monkeypatch.setattr(
        "coral.tools.session_manager.discover_coral_agents",
        AsyncMock(return_value=[]),
    )

    yield

    # Teardown: force-close all module-level store connections
    for store in _ALL_STORES:
        _force_close_store(store)
