"""FastAPI web server for the Coral Dashboard.

This module handles FastAPI initialization, lifespan (background tasks),
static/template mounting, and router registration. All REST/WebSocket
endpoints live in the ``coral.api`` package.
"""

from __future__ import annotations

import argparse
import asyncio
import logging
import os
from contextlib import asynccontextmanager
from pathlib import Path

from fastapi import FastAPI
from fastapi.responses import HTMLResponse
from fastapi.staticfiles import StaticFiles
from fastapi.templating import Jinja2Templates
from starlette.requests import Request

from fastapi.middleware.cors import CORSMiddleware

from coral.store import CoralStore
from coral.tools.jsonl_reader import JsonlSessionReader

# Import routers
from coral.api import live_sessions as live_sessions_api
from coral.api import history as history_api
from coral.api import system as system_api
from coral.api import schedule as schedule_api
from coral.api import webhooks as webhooks_api
from coral.api import tasks as tasks_api
from coral.api import uploads as uploads_api
from coral.api import themes as themes_api
from coral.api import board_remotes as board_remotes_api

from coral.tools.utils import get_package_dir

log = logging.getLogger(__name__)
BASE_DIR = get_package_dir()

# WAL size threshold (1 MB) — checkpoint and vacuum when exceeded
_WAL_COMPACT_THRESHOLD = 1_000_000


async def _compact_databases() -> None:
    """Checkpoint and vacuum SQLite databases if their WAL files are too large."""
    import aiosqlite
    from coral.store.connection import DB_PATH
    from coral.messageboard.store import DB_PATH as BOARD_DB_PATH

    for db_path in [DB_PATH, BOARD_DB_PATH]:
        wal_path = Path(f"{db_path}-wal")
        if not wal_path.exists():
            continue
        wal_size = wal_path.stat().st_size
        if wal_size < _WAL_COMPACT_THRESHOLD:
            continue
        try:
            conn = await aiosqlite.connect(str(db_path))
            await conn.execute("PRAGMA wal_checkpoint(TRUNCATE)")
            try:
                await conn.execute("VACUUM")
            except Exception:
                pass  # VACUUM needs exclusive access; checkpoint alone is fine
            await conn.close()
            log.info("Compacted %s (WAL was %.1f MB)", db_path.name, wal_size / 1_048_576)
        except Exception as e:
            log.warning("Failed to compact %s: %s", db_path.name, e)


@asynccontextmanager
async def lifespan(app: FastAPI):
    """Start background indexer, batch summarizer, git poller, and webhook dispatcher on server startup."""
    from coral.background_tasks import SessionIndexer, BatchSummarizer, GitPoller, WebhookDispatcher, IdleDetector, MessageBoardNotifier
    from coral.background_tasks.remote_board_poller import RemoteBoardPoller
    from coral.store.remote_boards import RemoteBoardStore
    from coral.tools.session_manager import discover_coral_agents, resume_persistent_sessions
    from coral.api.themes import seed_bundled_themes

    # Seed bundled themes (e.g. GhostV3) into ~/.coral/themes/ on first run
    seed_bundled_themes()

    # Compact databases on startup if WAL files are too large
    await _compact_databases()

    # Resume any persistent live sessions that are no longer running in tmux
    # (excludes sessions owned by scheduled/oneshot job runs).
    # Timeout prevents blocking server startup if tmux operations hang.
    try:
        await asyncio.wait_for(
            resume_persistent_sessions(store, schedule_store),
            timeout=30,
        )
    except asyncio.TimeoutError:
        log.warning("resume_persistent_sessions timed out after 30s — skipping remaining sessions")

    # Register any tmux sessions not yet tracked in the live_sessions table.
    live_agents = await discover_coral_agents()
    tracked = {r["session_id"] for r in await store.get_all_live_sessions()}
    for agent in live_agents:
        wd = agent.get("working_directory") or ""
        sid = agent.get("session_id")
        if sid and sid not in tracked:
            await store.register_live_session(
                sid, agent["agent_type"], agent["agent_name"], wd,
            )

    # Seed _last_known from DB so we don't re-insert events already stored.
    live_sessions_api._last_known.update(await store.get_last_known_status_summary())

    indexer = SessionIndexer(store)
    summarizer = BatchSummarizer(store)
    git_poller = GitPoller(store)
    dispatcher = WebhookDispatcher(store)
    idle_detector = IdleDetector(store)

    from coral.messageboard.store import MessageBoardStore
    board_store = MessageBoardStore()
    board_notifier = MessageBoardNotifier(board_store)

    indexer_task = asyncio.create_task(indexer.run_forever(interval=120))
    summarizer_task = asyncio.create_task(summarizer.run_forever())
    git_task = asyncio.create_task(git_poller.run_forever(interval=120))
    webhook_task = asyncio.create_task(dispatcher.run_forever(interval=15))
    idle_task = asyncio.create_task(idle_detector.run_forever(interval=60))
    board_notifier_task = asyncio.create_task(board_notifier.run_forever(interval=30))

    remote_board_store = RemoteBoardStore()
    board_remotes_api.store = remote_board_store
    remote_poller = RemoteBoardPoller(remote_board_store)
    remote_poller_task = asyncio.create_task(remote_poller.run_forever(interval=30))

    # Start job scheduler
    from coral.background_tasks.scheduler import JobScheduler
    scheduler = JobScheduler(schedule_store)
    scheduler_task = asyncio.create_task(scheduler.run_forever())
    app.state.scheduler = scheduler
    tasks_api.scheduler = scheduler

    # Periodic update check (PyPI + GitHub releases)
    from coral.tools.update_checker import UpdateInfo, check_for_update
    app.state.update_info = UpdateInfo()

    async def _periodic_update_check():
        while True:
            await check_for_update(app.state.update_info)
            await asyncio.sleep(6 * 3600)

    update_task = asyncio.create_task(_periodic_update_check())

    # Store indexer and git poller on app state so endpoints can trigger refresh
    app.state.indexer = indexer
    app.state.git_poller = git_poller

    app.state.webhook_dispatcher = dispatcher

    yield

    update_task.cancel()
    indexer_task.cancel()
    summarizer_task.cancel()
    git_task.cancel()
    scheduler_task.cancel()
    webhook_task.cancel()
    idle_task.cancel()
    board_notifier_task.cancel()
    remote_poller_task.cancel()
    try:
        await asyncio.wait_for(remote_poller.close(), timeout=5)
    except asyncio.TimeoutError:
        log.warning("Remote board poller close timed out")
    try:
        await asyncio.wait_for(dispatcher.close(), timeout=5)
    except asyncio.TimeoutError:
        log.warning("Webhook dispatcher close timed out")
    await board_store.close()
    await remote_board_store.close()
    await store.close()


app = FastAPI(title="Coral Dashboard", lifespan=lifespan)

# CORS: allow same-origin by default (browser enforces this).
# Explicitly allow localhost origins for cross-tab/port scenarios.
# This blocks cross-site attacks from arbitrary external origins.
app.add_middleware(
    CORSMiddleware,
    allow_origin_regex=r"^https?://(localhost|127\.0\.0\.1)(:\d+)?$",
    allow_credentials=True,
    allow_methods=["*"],
    allow_headers=["*"],
)

store = CoralStore()
jsonl_reader = JsonlSessionReader()

# Schedule store shares the same DB but is a separate instance for the scheduler
from coral.store.schedule import ScheduleStore
schedule_store = ScheduleStore()

# Wire up module-level dependencies in router modules
live_sessions_api.store = store
live_sessions_api.jsonl_reader = jsonl_reader
live_sessions_api.schedule_store = schedule_store
history_api.store = store
history_api._app = app
system_api.store = store
schedule_api.store = schedule_store
webhooks_api.store = store
webhooks_api._app = app
tasks_api.store = schedule_store

# Register routers
app.include_router(live_sessions_api.router)
app.include_router(history_api.router)
app.include_router(system_api.router)
app.include_router(schedule_api.router)
app.include_router(webhooks_api.router)
app.include_router(tasks_api.router)
app.include_router(uploads_api.router)
app.include_router(themes_api.router)
app.include_router(board_remotes_api.router)

# Mount self-contained message board sub-app
from coral.messageboard.app import create_app as create_board_app
app.mount("/api/board", create_board_app())

# Mount static files and templates
app.mount("/static", StaticFiles(directory=str(BASE_DIR / "static")), name="static")
templates = Jinja2Templates(directory=str(BASE_DIR / "templates"))

# Backward-compatible aliases so existing code/tests that reference
# ``coral.web_server._track_status_summary_events`` etc. still work.
_track_status_summary_events = live_sessions_api._track_status_summary_events
_last_known = live_sessions_api._last_known


def _set_store(new_store):
    """Update the store on web_server and all router modules (used by tests)."""
    global store, schedule_store
    store = new_store
    live_sessions_api.store = new_store
    history_api.store = new_store
    system_api.store = new_store
    webhooks_api.store = new_store


def _set_schedule_store(new_store):
    """Update the schedule store (used by tests)."""
    global schedule_store
    schedule_store = new_store
    schedule_api.store = new_store
    tasks_api.store = new_store
    live_sessions_api.schedule_store = new_store


# ── Dashboard SPA ──────────────────────────────────────────────────────────


@app.get("/", response_class=HTMLResponse)
async def index(request: Request):
    """Serve the coral dashboard SPA."""
    return templates.TemplateResponse(request=request, name="index.html", context={"coral_root": os.getcwd()})


@app.get("/diff", response_class=HTMLResponse)
async def diff_view(request: Request):
    """Serve the standalone diff viewer page."""
    return templates.TemplateResponse(request=request, name="diff.html")


# ── Entry Point ──────────────────────────────────────────────────────────────


def main():
    import threading
    import webbrowser
    import uvicorn

    parser = argparse.ArgumentParser(description="Coral Dashboard")
    parser.add_argument("--host", default="0.0.0.0", help="Host to bind to (default: 0.0.0.0)")
    parser.add_argument("--port", type=int, default=8420, help="Port to bind to (default: 8420)")
    parser.add_argument("--reload", action="store_true", help="Enable auto-reload for development")
    parser.add_argument("--no-browser", action="store_true", help="Don't open the browser on startup")
    args = parser.parse_args()

    if not args.no_browser:
        url = f"http://localhost:{args.port}"

        def _open_browser():
            try:
                webbrowser.open(url)
            except Exception:
                pass

        t = threading.Timer(1.5, _open_browser)
        t.daemon = True
        t.start()

    uvicorn.run(
        "coral.web_server:app",
        host=args.host,
        port=args.port,
        reload=args.reload,
    )


if __name__ == "__main__":
    main()
