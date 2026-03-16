"""RemoteBoardPoller — polls remote Coral servers for unread board messages.

For agents subscribed to boards on remote servers, this background task
periodically checks for unread messages and sends tmux nudges locally.
"""

from __future__ import annotations

import asyncio
import logging

import httpx

from coral.store.remote_boards import RemoteBoardStore
from coral.tools.session_manager import discover_coral_agents
from coral.tools.tmux_manager import send_to_tmux

log = logging.getLogger(__name__)


class RemoteBoardPoller:
    """Background task that polls remote boards and nudges local agents."""

    def __init__(self, remote_store: RemoteBoardStore) -> None:
        self._store = remote_store
        self._client: httpx.AsyncClient | None = None

    async def _get_client(self) -> httpx.AsyncClient:
        if self._client is None:
            self._client = httpx.AsyncClient(timeout=10)
        return self._client

    async def close(self) -> None:
        if self._client is not None:
            await self._client.aclose()
            self._client = None

    async def run_forever(self, interval: float = 30) -> None:
        try:
            while True:
                try:
                    await self.run_once()
                except Exception:
                    log.exception("RemoteBoardPoller error")
                await asyncio.sleep(interval)
        finally:
            await self.close()

    async def run_once(self) -> dict[str, int]:
        """Check all remote subscriptions for unread messages. Returns {"notified": n}."""
        subs = await self._store.list_all()
        if not subs:
            return {"notified": 0}

        # Build a map of local tmux sessions for nudging
        agents = await discover_coral_agents()
        agent_map: dict[str, dict] = {}
        for agent in agents:
            sid = agent.get("session_id")
            tmux_session = agent.get("tmux_session") or ""
            if sid:
                agent_map[sid] = agent
            if tmux_session:
                agent_map[tmux_session] = agent

        client = await self._get_client()
        notified_count = 0

        for sub in subs:
            session_id = sub["session_id"]
            remote_server = sub["remote_server"].rstrip("/")
            project = sub["project"]

            # Find the local agent to nudge
            agent = agent_map.get(session_id)
            if not agent:
                continue

            # Check unread count on remote server
            try:
                url = f"{remote_server}/api/board/{project}/messages/check?session_id={session_id}"
                resp = await client.get(url)
                resp.raise_for_status()
                data = resp.json()
            except Exception as e:
                log.debug("Failed to poll remote %s for %s: %s", remote_server, session_id, e)
                continue

            unread = data.get("unread", 0)
            if unread == 0:
                # Clear notification state
                if sub["last_notified_unread"] != 0:
                    await self._store.update_last_notified(sub["id"], 0)
                continue

            # Only notify if unread count changed since last notification
            if sub["last_notified_unread"] == unread:
                continue

            # Send tmux nudge
            plural = "s" if unread != 1 else ""
            nudge = f"You have {unread} unread message{plural} on the message board. Run 'coral-board read' to see them."
            err = await send_to_tmux(
                agent["agent_name"], nudge, session_id=agent.get("session_id"),
            )
            if err:
                log.debug("Failed to nudge %s: %s", agent["agent_name"], err)
                continue

            await self._store.update_last_notified(sub["id"], unread)
            notified_count += 1
            log.debug("Nudged %s about %d unread remote message(s)", agent["agent_name"], unread)

        return {"notified": notified_count}
