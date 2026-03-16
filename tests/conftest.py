"""Shared test fixtures — ensures every test run uses isolated temp databases.

This prevents SQLite lock contention when multiple agents run tests concurrently.
All stores are patched to use per-test temp directories instead of ~/.coral/.
"""

from __future__ import annotations

from pathlib import Path
from unittest.mock import patch

import pytest


@pytest.fixture(autouse=True)
def _isolate_databases(tmp_path, monkeypatch):
    """Redirect all default DB paths to a per-test temp directory.

    This runs automatically for every test, ensuring no test touches
    the real ~/.coral/sessions.db or ~/.coral/messageboard.db.
    """
    tmp_db_dir = tmp_path / ".coral"
    tmp_db_dir.mkdir()

    # Patch the module-level DB_DIR and DB_PATH in both store modules
    import coral.store.connection as conn_mod
    import coral.messageboard.store as board_mod

    monkeypatch.setattr(conn_mod, "DB_DIR", tmp_db_dir)
    monkeypatch.setattr(conn_mod, "DB_PATH", tmp_db_dir / "sessions.db")
    monkeypatch.setattr(board_mod, "DB_DIR", tmp_db_dir)
    monkeypatch.setattr(board_mod, "DB_PATH", tmp_db_dir / "messageboard.db")

    # Also patch the remote boards store if it exists
    try:
        import coral.store.remote_boards as remote_mod
        monkeypatch.setattr(remote_mod, "DB_DIR", tmp_db_dir)
        monkeypatch.setattr(remote_mod, "DB_PATH", tmp_db_dir / "remote_boards.db")
    except (ImportError, AttributeError):
        pass
