"""API routes for webhook configuration and delivery history."""

from __future__ import annotations

import logging
from typing import TYPE_CHECKING
from urllib.parse import urlparse

from fastapi import APIRouter, Query

if TYPE_CHECKING:
    from coral.store import CoralStore

log = logging.getLogger(__name__)

router = APIRouter()

# Module-level dependencies, set by web_server.py during app setup
store: CoralStore = None  # type: ignore[assignment]
_app = None  # Set by web_server for accessing app.state


VALID_PLATFORMS = {"slack", "discord", "generic"}


def _validate_url(url: str, platform: str) -> str | None:
    """Return error message if URL is invalid, else None."""
    try:
        parsed = urlparse(url)
    except Exception:
        return "Invalid URL"
    if parsed.scheme not in ("http", "https"):
        return "URL must use http or https"
    if parsed.scheme == "http" and parsed.hostname not in ("localhost", "127.0.0.1"):
        return "HTTP (non-HTTPS) is only allowed for localhost"
    if not parsed.netloc:
        return "URL must have a hostname"
    return None


@router.get("/api/webhooks")
async def list_webhooks():
    """List all webhook configurations."""
    return await store.list_webhook_configs()


@router.post("/api/webhooks")
async def create_webhook(body: dict):
    """Create a new webhook configuration."""
    name = body.get("name", "").strip()
    platform = body.get("platform", "generic").strip()
    url = body.get("url", "").strip()
    if not name or not url:
        return {"error": "name and url are required"}
    if platform not in VALID_PLATFORMS:
        return {"error": f"platform must be one of: {', '.join(sorted(VALID_PLATFORMS))}"}
    url_error = _validate_url(url, platform)
    if url_error:
        return {"error": url_error}
    return await store.create_webhook_config(
        name=name,
        platform=platform,
        url=url,
        agent_filter=body.get("agent_filter") or None,
    )


@router.patch("/api/webhooks/{webhook_id}")
async def update_webhook(webhook_id: int, body: dict):
    """Update fields on a webhook configuration."""
    if "url" in body:
        url_error = _validate_url(body["url"], body.get("platform", "generic"))
        if url_error:
            return {"error": url_error}
    await store.update_webhook_config(webhook_id, **body)
    return {"ok": True}


@router.delete("/api/webhooks/{webhook_id}")
async def delete_webhook(webhook_id: int):
    """Delete a webhook configuration and all its delivery history."""
    await store.delete_webhook_config(webhook_id)
    return {"ok": True}


@router.post("/api/webhooks/{webhook_id}/test")
async def test_webhook(webhook_id: int):
    """Send a test notification immediately via direct delivery."""
    cfg = await store.get_webhook_config(webhook_id)
    if not cfg:
        return {"error": "Webhook not found"}
    delivery = await store.create_webhook_delivery(
        webhook_id=webhook_id,
        agent_name="coral-test",
        session_id=None,
        event_type="needs_input",
        event_summary="Test notification from Coral dashboard",
    )
    dispatcher = getattr(_app.state, "webhook_dispatcher", None) if _app else None
    if dispatcher:
        await dispatcher.deliver_now(delivery)
    # Re-fetch to get updated status after delivery attempt
    deliveries = await store.list_webhook_deliveries(webhook_id, limit=1)
    return deliveries[0] if deliveries else {"ok": True}


@router.get("/api/webhooks/{webhook_id}/deliveries")
async def list_deliveries(
    webhook_id: int, limit: int = Query(50, ge=1, le=200)
):
    """Get recent delivery history for a webhook."""
    return await store.list_webhook_deliveries(webhook_id, limit=limit)
