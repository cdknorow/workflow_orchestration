"""FastAPI router for the inter-agent message board."""

from __future__ import annotations

import asyncio
import logging
from typing import Any

import httpx
from fastapi import APIRouter, HTTPException
from pydantic import BaseModel

from coral.messageboard.store import MessageBoardStore

log = logging.getLogger(__name__)

router = APIRouter()

# Set by app.py during create_app()
store: MessageBoardStore = None  # type: ignore[assignment]


# ── Request models ───────────────────────────────────────────────────────

class SubscribeRequest(BaseModel):
    session_id: str
    job_title: str
    webhook_url: str | None = None


class UnsubscribeRequest(BaseModel):
    session_id: str


class PostMessageRequest(BaseModel):
    session_id: str
    content: str


# ── Endpoints ────────────────────────────────────────────────────────────

@router.get("/projects")
async def list_projects():
    return await store.list_projects()


@router.get("/{project}/subscribers")
async def list_subscribers(project: str):
    return await store.list_subscribers(project)


@router.post("/{project}/subscribe")
async def subscribe(project: str, body: SubscribeRequest):
    return await store.subscribe(
        project, body.session_id, body.job_title, body.webhook_url
    )


@router.delete("/{project}/subscribe")
async def unsubscribe(project: str, body: UnsubscribeRequest):
    removed = await store.unsubscribe(project, body.session_id)
    if not removed:
        raise HTTPException(status_code=404, detail="Subscriber not found")
    return {"ok": True}


@router.post("/{project}/messages")
async def post_message(project: str, body: PostMessageRequest):
    message = await store.post_message(project, body.session_id, body.content)

    # Fire-and-forget webhook dispatch
    asyncio.create_task(_dispatch_webhooks(project, body.session_id, message))

    return message


@router.get("/{project}/messages")
async def read_messages(project: str, session_id: str, limit: int = 50):
    return await store.read_messages(project, session_id, limit)


@router.delete("/{project}")
async def delete_project(project: str):
    await store.delete_project(project)
    return {"ok": True}


# ── Webhook dispatch ─────────────────────────────────────────────────────

async def _dispatch_webhooks(
    project: str, sender_session_id: str, message: dict[str, Any]
) -> None:
    targets = await store.get_webhook_targets(project, sender_session_id)
    if not targets:
        return

    # Look up sender's job_title
    subscribers = await store.list_subscribers(project)
    sender_title = "Unknown"
    for s in subscribers:
        if s["session_id"] == sender_session_id:
            sender_title = s["job_title"]
            break

    payload = {
        "project": project,
        "message": {
            "id": message["id"],
            "session_id": message["session_id"],
            "job_title": sender_title,
            "content": message["content"],
            "created_at": message["created_at"],
        },
    }

    async with httpx.AsyncClient(timeout=5.0) as client:
        tasks = []
        for target in targets:
            tasks.append(_send_webhook(client, target["webhook_url"], payload))
        await asyncio.gather(*tasks, return_exceptions=True)


async def _send_webhook(
    client: httpx.AsyncClient, url: str, payload: dict[str, Any]
) -> None:
    try:
        await client.post(url, json=payload)
    except Exception:
        log.debug("Webhook delivery failed for %s", url, exc_info=True)
