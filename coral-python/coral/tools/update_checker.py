"""Check PyPI for newer versions of agent-coral."""

from __future__ import annotations

import logging
from importlib.metadata import version as installed_version

import httpx

logger = logging.getLogger(__name__)

PYPI_URL = "https://pypi.org/pypi/agent-coral/json"
GITHUB_RELEASES_URL = "https://api.github.com/repos/cdknorow/coral/releases/latest"


class UpdateInfo:
    def __init__(self):
        self.available = False
        self.current = ""
        self.latest = ""
        self.release_notes = ""
        self.release_url = ""


async def check_for_update(info: UpdateInfo) -> UpdateInfo:
    """Fetch latest version from PyPI and release notes from GitHub."""
    try:
        info.current = installed_version("agent-coral")
    except Exception:
        info.current = "unknown"
        return info

    async with httpx.AsyncClient(timeout=10) as client:
        # Get latest version from PyPI
        try:
            resp = await client.get(PYPI_URL)
            resp.raise_for_status()
            data = resp.json()
            info.latest = data["info"]["version"]
        except Exception as e:
            logger.debug(f"PyPI check failed: {e}")
            return info

        # Compare versions (simple tuple comparison avoids packaging dep)
        try:
            current_parts = tuple(int(x) for x in info.current.split("."))
            latest_parts = tuple(int(x) for x in info.latest.split("."))
        except (ValueError, AttributeError):
            return info

        if latest_parts <= current_parts:
            info.available = False
            return info

        info.available = True

        # Fetch release notes from GitHub
        try:
            gh_resp = await client.get(GITHUB_RELEASES_URL)
            gh_resp.raise_for_status()
            gh_data = gh_resp.json()
            info.release_notes = gh_data.get("body", "")
            info.release_url = gh_data.get("html_url", "")
        except Exception as e:
            logger.debug(f"GitHub release fetch failed: {e}")

    return info
