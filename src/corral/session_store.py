"""Backward-compatible facade — re-exports CorralStore as SessionStore.

All actual logic has been moved to the ``corral.store`` package:
  - ``corral.store.connection`` — DatabaseManager (connection, schema, migrations)
  - ``corral.store.sessions``   — SessionStore (index, FTS, notes, tags, settings, live state)
  - ``corral.store.git``        — GitStore (git snapshots)
  - ``corral.store.tasks``      — TaskStore (tasks, notes, events)
  - ``corral.store``            — CorralStore (unified facade)

Existing code that does ``from corral.session_store import SessionStore`` will
get ``CorralStore``, which has the exact same API surface.
"""

from corral.store import CorralStore as SessionStore  # noqa: F401
from corral.store.connection import DB_PATH  # noqa: F401

__all__ = ["SessionStore", "DB_PATH"]
