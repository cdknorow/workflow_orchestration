"""API routes for historical session data."""

from __future__ import annotations

import asyncio
import logging
from typing import Optional, TYPE_CHECKING

from fastapi import APIRouter, Query

from corral.tools.session_manager import (
    load_history_sessions,
    load_history_session_messages,
)

if TYPE_CHECKING:
    from corral.store import CorralStore

log = logging.getLogger(__name__)

router = APIRouter()

# Module-level dependency, set by web_server.py during app setup
store: CorralStore = None  # type: ignore[assignment]


@router.get("/api/sessions/history")
async def get_history_sessions(
    page: int = Query(1, ge=1),
    page_size: int = Query(50, ge=1, le=200),
    q: Optional[str] = Query(None),
    fts_mode: str = Query("phrase"),
    # Legacy single-value params (backward compat)
    tag_id: Optional[int] = Query(None),
    source_type: Optional[str] = Query(None),
    # New multi-value params (comma-separated strings)
    tag_ids: Optional[str] = Query(None),
    tag_logic: str = Query("AND"),
    source_types: Optional[str] = Query(None),
    date_from: Optional[str] = Query(None),
    date_to: Optional[str] = Query(None),
    min_duration_sec: Optional[int] = Query(None, ge=0),
    max_duration_sec: Optional[int] = Query(None, ge=0),
):
    """Paginated history sessions with advanced search filters."""
    import re as _re

    # Parse comma-separated tag_ids
    resolved_tag_ids: list[int] = []
    if tag_ids:
        resolved_tag_ids = [int(x) for x in tag_ids.split(",") if x.strip().isdigit()]
    if tag_id is not None and tag_id not in resolved_tag_ids:
        resolved_tag_ids.append(tag_id)

    # Parse comma-separated source_types
    resolved_source_types: list[str] | None = None
    if source_types:
        resolved_source_types = [s.strip() for s in source_types.split(",") if s.strip()]
    elif source_type:
        resolved_source_types = [source_type]

    # Validate date format
    _DATE_RE = _re.compile(r"^\d{4}-\d{2}-\d{2}$")
    if date_from and not _DATE_RE.match(date_from):
        date_from = None
    if date_to and not _DATE_RE.match(date_to):
        date_to = None
    if date_from and date_to and date_from > date_to:
        date_from, date_to = date_to, date_from

    # Guard duration bounds
    if min_duration_sec is not None and max_duration_sec is not None:
        if min_duration_sec > max_duration_sec:
            min_duration_sec, max_duration_sec = max_duration_sec, min_duration_sec

    # Normalize tag_logic and fts_mode
    if tag_logic not in ("AND", "OR"):
        tag_logic = "AND"
    if fts_mode not in ("phrase", "and", "or"):
        fts_mode = "phrase"

    # Build keyword args for the new parameters
    advanced_kwargs = dict(
        fts_mode=fts_mode,
        tag_ids=resolved_tag_ids or None,
        tag_logic=tag_logic,
        source_types=resolved_source_types,
        date_from=date_from,
        date_to=date_to,
        min_duration_sec=min_duration_sec,
        max_duration_sec=max_duration_sec,
    )

    result = await store.list_sessions_paged(page, page_size, q, **advanced_kwargs)

    # Cold-start fallback
    has_any_filter = (
        q or resolved_tag_ids or resolved_source_types
        or date_from or date_to
        or min_duration_sec is not None or max_duration_sec is not None
    )

    if result["total"] == 0 and not has_any_filter:
        indexer = (
            getattr(_app, "state", None)
            and getattr(_app.state, "indexer", None)
            if _app else None
        )
        if indexer:
            try:
                await indexer.run_once()
                result = await store.list_sessions_paged(
                    page, page_size, q, **advanced_kwargs
                )
            except Exception:
                pass

        # If still empty, fall back to old file-scan method
        if result["total"] == 0:
            sessions = load_history_sessions()
            metadata = await store.get_all_session_metadata()
            for s in sessions:
                meta = metadata.get(s["session_id"])
                if meta:
                    s["tags"] = meta["tags"]
                    s["has_notes"] = meta["has_notes"]
                else:
                    s["tags"] = []
                    s["has_notes"] = False
            return {
                "sessions": sessions,
                "total": len(sessions),
                "page": 1,
                "page_size": len(sessions),
            }

    return result


@router.post("/api/indexer/refresh")
async def trigger_indexer_refresh():
    """Trigger an immediate re-index."""
    indexer = getattr(_app.state, "indexer", None) if _app else None
    if not indexer:
        return {"error": "Indexer not available"}
    result = await indexer.run_once()
    return result


@router.get("/api/sessions/history/{session_id}")
async def get_history_session_detail(session_id: str):
    """Get all messages for a historical session."""
    messages = load_history_session_messages(session_id)
    if not messages:
        return {"error": f"Session '{session_id}' not found"}
    return {"session_id": session_id, "messages": messages}


@router.get("/api/sessions/history/{session_id}/git")
async def get_history_session_git(session_id: str):
    """Return git commits that occurred during a historical session's time range."""
    snapshots = await store.get_git_snapshots_for_session(session_id)
    return {"session_id": session_id, "commits": snapshots}


@router.get("/api/sessions/history/{session_id}/tasks")
async def get_history_session_tasks(session_id: str):
    """Get tasks for a historical session (read-only)."""
    return await store.list_tasks_by_session(session_id)


@router.get("/api/sessions/history/{session_id}/agent-notes")
async def get_history_session_agent_notes(session_id: str):
    """Get agent notes for a historical session (read-only)."""
    return await store.list_notes_by_session(session_id)


@router.get("/api/sessions/history/{session_id}/events")
async def get_history_session_events(session_id: str, limit: int = Query(200, ge=1, le=500)):
    """Get activity events for a historical session (read-only)."""
    return await store.list_events_by_session(session_id, limit)


@router.get("/api/sessions/history/{session_id}/notes")
async def get_session_notes(session_id: str):
    """Get notes and auto-summary for a session. Triggers auto-summarization if empty."""
    notes = await store.get_session_notes(session_id)

    if not notes["notes_md"] and not notes["auto_summary"]:
        try:
            from corral.background_tasks import AutoSummarizer
            summarizer = AutoSummarizer(store)
            asyncio.create_task(summarizer.summarize_session(session_id))
            notes["summarizing"] = True
        except ImportError:
            notes["summarizing"] = False

    return notes


@router.put("/api/sessions/history/{session_id}/notes")
async def save_session_notes(session_id: str, body: dict):
    """Save user-edited markdown notes for a session."""
    notes_md = body.get("notes_md", "")
    await store.save_session_notes(session_id, notes_md)
    return {"ok": True}


@router.post("/api/sessions/history/{session_id}/resummarize")
async def resummarize_session(session_id: str):
    """Force re-generate auto-summary for a session."""
    try:
        from corral.background_tasks import AutoSummarizer
        summarizer = AutoSummarizer(store)
        summary = await summarizer.summarize_session(session_id)
        return {"ok": True, "auto_summary": summary}
    except ImportError:
        return {"error": "claude-agent-sdk not installed"}
    except Exception as e:
        return {"error": str(e)}


# ── Tags on historical sessions ────────────────────────────────────────────


@router.get("/api/sessions/history/{session_id}/tags")
async def get_session_tags(session_id: str):
    return await store.get_session_tags(session_id)


@router.post("/api/sessions/history/{session_id}/tags")
async def add_session_tag(session_id: str, body: dict):
    tag_id = body.get("tag_id")
    if tag_id is None:
        return {"error": "tag_id is required"}
    await store.add_session_tag(session_id, int(tag_id))
    return {"ok": True}


@router.delete("/api/sessions/history/{session_id}/tags/{tag_id}")
async def remove_session_tag(session_id: str, tag_id: int):
    await store.remove_session_tag(session_id, tag_id)
    return {"ok": True}


# App reference for accessing app.state.indexer — set by web_server.py
_app = None
