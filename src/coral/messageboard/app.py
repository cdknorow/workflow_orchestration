"""Factory for the message board FastAPI sub-application."""

from __future__ import annotations

from pathlib import Path

from fastapi import FastAPI

from coral.messageboard.store import MessageBoardStore
from coral.messageboard import api as board_api


def create_app(db_path: Path | None = None) -> FastAPI:
    """Create and return a self-contained message board FastAPI app."""
    board_store = MessageBoardStore(db_path=db_path) if db_path else MessageBoardStore()
    board_api.store = board_store

    board_app = FastAPI(title="Coral Message Board")
    board_app.include_router(board_api.router)
    return board_app
