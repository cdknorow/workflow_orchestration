"""REST API for managing remote board subscriptions and proxying remote board requests.

Allows the CLI to register/unregister remote board subscriptions with the
local Coral server, so the RemoteBoardPoller can deliver tmux nudges.

Also provides proxy endpoints that forward board API calls to remote Coral servers,
so the dashboard can display remote board data without direct connections.
"""

from __future__ import annotations

import logging

from fastapi import APIRouter, HTTPException
from pydantic import BaseModel

from coral.store.remote_boards import RemoteBoardStore

log = logging.getLogger(__name__)

router = APIRouter(prefix="/api/board/remotes", tags=["board-remotes"])

# Injected by web_server.py at startup
store: RemoteBoardStore | None = None


class RemoteSubRequest(BaseModel):
    session_id: str
    remote_server: str
    project: str
    job_title: str


class RemoteSubDeleteRequest(BaseModel):
    session_id: str


@router.post("")
async def add_remote_subscription(req: RemoteSubRequest):
    """Register a remote board subscription for local tmux notification."""
    if store is None:
        raise HTTPException(503, "Remote board store not initialized")
    sub = await store.add(
        session_id=req.session_id,
        remote_server=req.remote_server,
        project=req.project,
        job_title=req.job_title,
    )
    return sub


@router.delete("")
async def remove_remote_subscription(req: RemoteSubDeleteRequest):
    """Remove all remote board subscriptions for a session."""
    if store is None:
        raise HTTPException(503, "Remote board store not initialized")
    removed = await store.remove(session_id=req.session_id)
    return {"removed": removed}


@router.get("")
async def list_remote_subscriptions():
    """List all remote board subscriptions."""
    if store is None:
        raise HTTPException(503, "Remote board store not initialized")
    return await store.list_all()


# ── Proxy Endpoints ────────────────────────────────────────────────────────
# Forward board API calls to remote Coral servers so the dashboard can
# display remote board data without direct browser-to-remote connections.


def _is_safe_remote_server(url: str) -> bool:
    """Validate that a remote server URL is safe (not targeting private/internal networks)."""
    import ipaddress
    import socket
    from urllib.parse import urlparse

    try:
        parsed = urlparse(url)
    except Exception:
        return False

    if parsed.scheme not in ("http", "https"):
        return False
    if not parsed.hostname:
        return False

    # Resolve the hostname to check the actual IP
    try:
        addr_infos = socket.getaddrinfo(parsed.hostname, parsed.port or 80, proto=socket.IPPROTO_TCP)
    except socket.gaierror:
        return False

    for family, _, _, _, sockaddr in addr_infos:
        ip = ipaddress.ip_address(sockaddr[0])
        if ip.is_private or ip.is_loopback or ip.is_link_local or ip.is_reserved:
            return False

    return True


async def _validate_remote_server(remote_server: str) -> None:
    """Validate that the remote_server is a registered subscription target."""
    if store is None:
        raise HTTPException(503, "Remote board store not initialized")

    # Check against registered remote subscriptions
    subs = await store.list_all()
    registered_servers = {s["remote_server"].rstrip("/") for s in subs}
    if remote_server.rstrip("/") not in registered_servers:
        raise HTTPException(403, "Remote server is not registered. Add a subscription first.")

    # Block private/internal IPs to prevent SSRF
    if not _is_safe_remote_server(remote_server):
        raise HTTPException(403, "Remote server resolves to a private or reserved IP address")


async def _proxy_get(remote_server: str, path: str, timeout: float = 5.0) -> dict | list:
    """Forward a GET request to a remote Coral server's board API."""
    import httpx

    await _validate_remote_server(remote_server)

    url = f"{remote_server.rstrip('/')}/api/board{path}"
    try:
        async with httpx.AsyncClient(timeout=timeout) as client:
            resp = await client.get(url)
            resp.raise_for_status()
            return resp.json()
    except httpx.TimeoutException:
        raise HTTPException(504, f"Remote server timed out: {remote_server}")
    except httpx.HTTPStatusError as e:
        raise HTTPException(e.response.status_code, f"Remote server error: {e.response.text}")
    except Exception as e:
        raise HTTPException(502, f"Cannot reach remote server {remote_server}: {e}")


@router.get("/proxy/{remote_server:path}/projects")
async def proxy_projects(remote_server: str):
    """Proxy: list projects on a remote board server."""
    return await _proxy_get(remote_server, "/projects")


@router.get("/proxy/{remote_server:path}/{project}/messages/all")
async def proxy_messages(remote_server: str, project: str, limit: int = 200):
    """Proxy: list messages on a remote board."""
    return await _proxy_get(remote_server, f"/{project}/messages/all?limit={limit}")


@router.get("/proxy/{remote_server:path}/{project}/subscribers")
async def proxy_subscribers(remote_server: str, project: str):
    """Proxy: list subscribers on a remote board."""
    return await _proxy_get(remote_server, f"/{project}/subscribers")


@router.get("/proxy/{remote_server:path}/{project}/messages/check")
async def proxy_check_unread(remote_server: str, project: str, session_id: str):
    """Proxy: check unread messages on a remote board."""
    return await _proxy_get(remote_server, f"/{project}/messages/check?session_id={session_id}")
