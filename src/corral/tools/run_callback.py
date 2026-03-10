"""Simple HTTP POST callback for one-shot task run status updates."""

from __future__ import annotations

import asyncio
import logging

log = logging.getLogger(__name__)


async def send_run_callback(webhook_url: str, payload: dict, retries: int = 3) -> None:
    """POST payload to webhook_url with simple retry. Fire-and-forget."""
    import httpx

    delays = [5, 15, 60]
    for attempt in range(retries):
        try:
            async with httpx.AsyncClient(timeout=10.0) as client:
                resp = await client.post(webhook_url, json=payload)
                if 200 <= resp.status_code < 300:
                    log.debug("Webhook callback to %s succeeded (status %d)", webhook_url, resp.status_code)
                    return
                log.warning("Webhook callback to %s returned %d", webhook_url, resp.status_code)
        except Exception as e:
            log.warning("Webhook callback to %s failed (attempt %d): %s", webhook_url, attempt + 1, e)
        if attempt < retries - 1:
            await asyncio.sleep(delays[attempt])
    log.warning("Webhook callback to %s failed after %d attempts", webhook_url, retries)
