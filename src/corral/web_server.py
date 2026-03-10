"""FastAPI web server for the Corral Dashboard.

This module handles FastAPI initialization, lifespan (background tasks),
static/template mounting, and router registration. All REST/WebSocket
endpoints live in the ``corral.api`` package.
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

from corral.store import CorralStore
from corral.tools.jsonl_reader import JsonlSessionReader

# Import routers
from corral.api import live_sessions as live_sessions_api
from corral.api import history as history_api
from corral.api import system as system_api
from corral.api import schedule as schedule_api
from corral.api import webhooks as webhooks_api
from corral.api import tasks as tasks_api

log = logging.getLogger(__name__)
BASE_DIR = Path(__file__).parent


@asynccontextmanager
async def lifespan(app: FastAPI):
    """Start background indexer, batch summarizer, git poller, and webhook dispatcher on server startup."""
    from corral.background_tasks import SessionIndexer, BatchSummarizer, GitPoller, WebhookDispatcher, IdleDetector
    from corral.agents import get_agent
    from corral.tools.session_manager import discover_corral_agents, resume_persistent_sessions

    # Resume any persistent live sessions that are no longer running in tmux
    await resume_persistent_sessions(store)

    # Install hooks into each live agent's worktree, and register any
    # tmux sessions not yet tracked in the live_sessions table.
    live_agents = await discover_corral_agents()
    tracked = {r["session_id"] for r in await store.get_all_live_sessions()}
    for agent in live_agents:
        wd = agent.get("working_directory") or ""
        if wd:
            agent_impl = get_agent(agent["agent_type"])
            agent_impl.install_hooks(wd)
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

    indexer_task = asyncio.create_task(indexer.run_forever(interval=120))
    summarizer_task = asyncio.create_task(summarizer.run_forever())
    git_task = asyncio.create_task(git_poller.run_forever(interval=120))
    webhook_task = asyncio.create_task(dispatcher.run_forever(interval=15))
    idle_task = asyncio.create_task(idle_detector.run_forever(interval=60))

    # Start job scheduler
    from corral.background_tasks.scheduler import JobScheduler
    scheduler = JobScheduler(schedule_store)
    scheduler_task = asyncio.create_task(scheduler.run_forever())
    app.state.scheduler = scheduler
    tasks_api.scheduler = scheduler

    # Store indexer on app state so endpoints can trigger refresh
    app.state.indexer = indexer

    app.state.webhook_dispatcher = dispatcher

    yield

    indexer_task.cancel()
    summarizer_task.cancel()
    git_task.cancel()
    scheduler_task.cancel()
    webhook_task.cancel()
    idle_task.cancel()
    try:
        await asyncio.wait_for(dispatcher.close(), timeout=5)
    except asyncio.TimeoutError:
        log.warning("Webhook dispatcher close timed out")
    await store.close()


app = FastAPI(title="Corral Dashboard", lifespan=lifespan)
store = CorralStore()
jsonl_reader = JsonlSessionReader()

# Schedule store shares the same DB but is a separate instance for the scheduler
from corral.store.schedule import ScheduleStore
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

# Mount static files and templates
app.mount("/static", StaticFiles(directory=str(BASE_DIR / "static")), name="static")
templates = Jinja2Templates(directory=str(BASE_DIR / "templates"))

# Backward-compatible aliases so existing code/tests that reference
# ``corral.web_server._track_status_summary_events`` etc. still work.
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
    """Serve the corral dashboard SPA."""
    return templates.TemplateResponse(request=request, name="index.html", context={"corral_root": os.getcwd()})


# ── Entry Point ──────────────────────────────────────────────────────────────


def main():
    import threading
    import webbrowser
    import uvicorn

    parser = argparse.ArgumentParser(description="Corral Dashboard")
    parser.add_argument("--host", default="0.0.0.0", help="Host to bind to (default: 0.0.0.0)")
    parser.add_argument("--port", type=int, default=8420, help="Port to bind to (default: 8420)")
    parser.add_argument("--reload", action="store_true", help="Enable auto-reload for development")
    parser.add_argument("--no-browser", action="store_true", help="Don't open the browser on startup")
    args = parser.parse_args()

    if not args.no_browser:
        url = f"http://localhost:{args.port}"
        threading.Timer(1.5, webbrowser.open, args=(url,)).start()

    uvicorn.run(
        "corral.web_server:app",
        host=args.host,
        port=args.port,
        reload=args.reload,
    )


if __name__ == "__main__":
    main()
