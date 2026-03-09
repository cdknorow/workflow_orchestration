"""API routes for system-level operations: settings, tags, filesystem."""

from __future__ import annotations

import os
import logging
from typing import TYPE_CHECKING

from fastapi import APIRouter

if TYPE_CHECKING:
    from corral.store import CorralStore

log = logging.getLogger(__name__)

router = APIRouter()

# Module-level dependency, set by web_server.py during app setup
store: CorralStore = None  # type: ignore[assignment]


@router.get("/api/settings")
async def get_settings():
    """Return all global user settings."""
    settings = await store.get_settings()
    return {"settings": settings}


@router.put("/api/settings")
async def put_settings(body: dict):
    """Upsert one or more global user settings."""
    for key, value in body.items():
        await store.set_setting(str(key), str(value))
    return {"ok": True}


@router.get("/api/filesystem/list")
async def list_filesystem(path: str = "~"):
    """List directories at a given path for the directory browser."""
    expanded = os.path.expanduser(path)
    if not os.path.isdir(expanded):
        return {"error": f"Not a directory: {path}", "entries": []}

    entries = []
    try:
        for name in sorted(os.listdir(expanded), key=str.lower):
            full = os.path.join(expanded, name)
            if os.path.isdir(full) and not name.startswith("."):
                entries.append(name)
    except PermissionError:
        return {"error": "Permission denied", "entries": []}

    return {"path": expanded, "entries": entries}


@router.get("/api/tags")
async def list_tags():
    """List all tags."""
    return await store.list_tags()


@router.post("/api/tags")
async def create_tag(body: dict):
    """Create a new tag."""
    name = body.get("name", "").strip()
    if not name:
        return {"error": "Tag name is required"}
    color = body.get("color", "#58a6ff")
    try:
        tag = await store.create_tag(name, color)
        return tag
    except Exception as e:
        return {"error": str(e)}


@router.delete("/api/tags/{tag_id}")
async def delete_tag(tag_id: int):
    """Delete a tag."""
    await store.delete_tag(tag_id)
    return {"ok": True}
