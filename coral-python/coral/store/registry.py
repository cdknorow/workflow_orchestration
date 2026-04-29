"""Singleton store registry — provides shared store instances.

All code that needs a CoralStore or MessageBoardStore should import from here
instead of creating ad-hoc instances.  This ensures a single SQLite connection
per database file, eliminating connection leaks and reducing write-lock
contention.

Usage::

    from coral.store.registry import get_store, get_board_store

    store = get_store()
    board_store = get_board_store()
"""

from __future__ import annotations

from typing import TYPE_CHECKING

if TYPE_CHECKING:
    from coral.store import CoralStore
    from coral.messageboard.store import MessageBoardStore

_store: "CoralStore | None" = None
_board_store: "MessageBoardStore | None" = None


def get_store() -> "CoralStore":
    """Return the shared CoralStore singleton (lazy-created on first call)."""
    global _store
    if _store is None:
        from coral.store import CoralStore
        _store = CoralStore()
    return _store


def get_board_store() -> "MessageBoardStore":
    """Return the shared MessageBoardStore singleton (lazy-created on first call)."""
    global _board_store
    if _board_store is None:
        from coral.messageboard.store import MessageBoardStore
        _board_store = MessageBoardStore()
    return _board_store


def set_store(store: "CoralStore") -> None:
    """Set the shared CoralStore (called by web_server.py during startup)."""
    global _store
    _store = store


def set_board_store(board_store: "MessageBoardStore") -> None:
    """Set the shared MessageBoardStore (called by web_server.py during startup)."""
    global _board_store
    _board_store = board_store


async def close_all() -> None:
    """Close all shared store connections. Called on shutdown."""
    global _store, _board_store
    if _store is not None:
        await _store.close()
        _store = None
    if _board_store is not None:
        await _board_store.close()
        _board_store = None
