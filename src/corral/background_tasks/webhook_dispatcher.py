"""Webhook notification dispatcher for Corral."""

from __future__ import annotations

import asyncio
import logging
from datetime import datetime, timezone, timedelta
from typing import Any
from urllib.parse import urlparse

log = logging.getLogger(__name__)

RETRY_DELAYS = [30, 120, 600]  # 3 attempts: 30s, 2m, 10m
CIRCUIT_BREAKER_THRESHOLD = 10  # Auto-disable after N consecutive failures


class WebhookDispatcher:
    """Flushes pending webhook deliveries with retry and circuit breaker."""

    def __init__(self, store) -> None:
        self._store = store
        self._client = None  # httpx.AsyncClient, created lazily

    async def _get_client(self):
        import httpx
        if self._client is None or self._client.is_closed:
            self._client = httpx.AsyncClient(timeout=10.0)
        return self._client

    async def close(self) -> None:
        if self._client and not self._client.is_closed:
            await self._client.aclose()

    # ── Entry point called from API layer ─────────────────────────────

    async def dispatch(
        self,
        agent_name: str,
        event_type: str,
        summary: str,
        session_id: str | None = None,
    ) -> None:
        """Match event against enabled webhooks and enqueue deliveries."""
        try:
            configs = await self._store.list_webhook_configs(enabled_only=True)
            for cfg in configs:
                if self._matches(cfg, agent_name, event_type, summary):
                    await self._store.create_webhook_delivery(
                        webhook_id=cfg["id"],
                        agent_name=agent_name,
                        session_id=session_id,
                        event_type=event_type,
                        event_summary=summary,
                    )
        except Exception:
            log.exception("WebhookDispatcher.dispatch error")

    # ── Background flush loop ─────────────────────────────────────────

    async def run_forever(self, interval: float = 15) -> None:
        while True:
            try:
                await self.run_once()
            except Exception:
                log.exception("WebhookDispatcher flush error")
            await asyncio.sleep(interval)

    async def run_once(self) -> dict[str, int]:
        """Flush pending deliveries. Returns {"delivered": n, "failed": n}."""
        pending = await self._store.get_pending_webhook_deliveries(limit=50)
        delivered = 0
        failed = 0
        for delivery in pending:
            success = await self.deliver_now(delivery)
            if success:
                delivered += 1
            else:
                failed += 1
        return {"delivered": delivered, "failed": failed}

    # ── Event matching ────────────────────────────────────────────────

    def _matches(
        self, cfg: dict, agent_name: str, event_type: str, summary: str
    ) -> bool:
        if cfg["agent_filter"] and cfg["agent_filter"] != agent_name:
            return False
        if cfg["event_filter"] != "*":
            allowed = {e.strip() for e in cfg["event_filter"].split(",")}
            if event_type not in allowed:
                return False
        if cfg["low_confidence_only"]:
            if event_type != "confidence":
                return False
            if not summary.lower().startswith("low "):
                return False
        return True

    # ── HTTP delivery ─────────────────────────────────────────────────

    async def deliver_now(self, delivery: dict) -> bool:
        """Attempt immediate delivery. Returns True on success.

        This is a public method so the /test endpoint can bypass the queue.
        """
        cfg = await self._store.get_webhook_config(delivery["webhook_id"])
        if not cfg or not cfg["enabled"]:
            await self._store.mark_webhook_delivery(
                delivery["id"], status="failed",
                error_msg="Webhook disabled or deleted",
            )
            return False
        payload = _build_payload(cfg["platform"], delivery)
        attempt = delivery["attempt_count"] + 1
        try:
            client = await self._get_client()
            resp = await client.post(cfg["url"], json=payload)
            if 200 <= resp.status_code < 300:
                await self._store.mark_webhook_delivery(
                    delivery["id"], status="delivered",
                    http_status=resp.status_code, attempt_count=attempt,
                )
                await self._store.reset_consecutive_failures(cfg["id"])
                return True
            else:
                body = resp.text[:200]
                await self._schedule_retry_or_fail(
                    cfg, delivery, attempt, resp.status_code, body
                )
                return False
        except Exception as exc:
            await self._schedule_retry_or_fail(
                cfg, delivery, attempt, None, str(exc)[:200]
            )
            return False

    async def _schedule_retry_or_fail(
        self,
        cfg: dict,
        delivery: dict,
        attempt: int,
        http_status: int | None,
        error_msg: str,
    ) -> None:
        # Circuit breaker: auto-disable after N consecutive failures
        failure_count = await self._store.increment_consecutive_failures(cfg["id"])
        if failure_count >= CIRCUIT_BREAKER_THRESHOLD:
            await self._store.auto_disable_webhook(
                cfg["id"],
                f"Auto-disabled after {failure_count} consecutive failures"
            )
            log.warning(
                "Webhook %s (%s) auto-disabled after %d consecutive failures",
                cfg["id"], cfg["name"], failure_count,
            )

        if attempt > len(RETRY_DELAYS):
            await self._store.mark_webhook_delivery(
                delivery["id"], status="failed",
                http_status=http_status, error_msg=error_msg,
                attempt_count=attempt,
            )
            return
        delay = RETRY_DELAYS[attempt - 1]
        next_retry = (
            datetime.now(timezone.utc) + timedelta(seconds=delay)
        ).isoformat()
        await self._store.mark_webhook_delivery(
            delivery["id"], status="pending",
            http_status=http_status, error_msg=error_msg,
            attempt_count=attempt, next_retry_at=next_retry,
        )


# ── Payload builders (module-level, stateless) ────────────────────────


def _build_payload(platform: str, delivery: dict) -> dict:
    builders = {
        "slack": _slack_payload,
        "discord": _discord_payload,
    }
    return builders.get(platform, _generic_payload)(delivery)


def _slack_payload(delivery: dict) -> dict:
    emoji = {
        "confidence": ":warning:",
        "idle":       ":zzz:",
        "stop":       ":red_circle:",
        "status":     ":large_blue_circle:",
        "goal":       ":white_circle:",
    }.get(delivery["event_type"], ":bell:")
    return {
        "blocks": [{
            "type": "section",
            "text": {
                "type": "mrkdwn",
                "text": (
                    f"{emoji} *Corral — {delivery['event_type'].upper()}*\n"
                    f"*Agent:* `{delivery['agent_name']}`\n"
                    f"*Message:* {delivery['event_summary']}"
                ),
            },
        }]
    }


def _discord_payload(delivery: dict) -> dict:
    color = {
        "confidence": 0xD29922,  # amber
        "idle":       0xF85149,  # red
        "stop":       0xF85149,
    }.get(delivery["event_type"], 0x58A6FF)  # blue default
    return {
        "embeds": [{
            "title": f"Corral — {delivery['event_type'].upper()}",
            "description": delivery["event_summary"],
            "color": color,
            "fields": [{
                "name": "Agent",
                "value": f"`{delivery['agent_name']}`",
                "inline": True,
            }],
            "footer": {"text": "Corral"},
        }]
    }


def _generic_payload(delivery: dict) -> dict:
    return {
        "agent_name": delivery["agent_name"],
        "session_id": delivery.get("session_id"),
        "event_type": delivery["event_type"],
        "summary": delivery["event_summary"],
        "timestamp": delivery["created_at"],
        "source": "corral",
    }
