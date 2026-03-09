"""Idle agent detection — synthesizes webhook events when agents go quiet."""

from __future__ import annotations

import asyncio
import logging
from datetime import datetime, timezone

log = logging.getLogger(__name__)


class IdleDetector:
    """Periodically checks for idle agents and creates webhook deliveries."""

    def __init__(self, store) -> None:
        self._store = store

    async def run_forever(self, interval: float = 60) -> None:
        while True:
            try:
                await self.run_once()
            except Exception:
                log.exception("IdleDetector error")
            await asyncio.sleep(interval)

    async def run_once(self) -> dict[str, int]:
        """Check all idle-enabled webhooks. Returns {"notifications": n}."""
        configs = await self._store.list_webhook_configs(enabled_only=True)
        idle_configs = [c for c in configs if c["idle_threshold_seconds"] > 0]
        if not idle_configs:
            return {"notifications": 0}

        last_active = await self._store.get_last_event_times_by_agent()
        now = datetime.now(timezone.utc)
        count = 0

        for cfg in idle_configs:
            threshold = cfg["idle_threshold_seconds"]
            for agent_name, last_ts_str in last_active.items():
                if cfg["agent_filter"] and cfg["agent_filter"] != agent_name:
                    continue
                try:
                    last_ts = datetime.fromisoformat(last_ts_str)
                    if last_ts.tzinfo is None:
                        last_ts = last_ts.replace(tzinfo=timezone.utc)
                    staleness = (now - last_ts).total_seconds()
                except Exception:
                    continue
                if staleness >= threshold:
                    already = await self._store.idle_notification_exists(
                        cfg["id"], agent_name, threshold
                    )
                    if not already:
                        await self._store.create_webhook_delivery(
                            webhook_id=cfg["id"],
                            agent_name=agent_name,
                            session_id=None,
                            event_type="idle",
                            event_summary=(
                                f"Agent idle for {int(staleness // 60)} minutes"
                            ),
                        )
                        count += 1
        return {"notifications": count}
