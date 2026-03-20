"""API routes for browsing and importing community agent/command templates.

Proxies the davila7/claude-code-templates GitHub repo with in-memory caching.
"""

from __future__ import annotations

import base64
import logging
import time
from typing import Any

import httpx
from fastapi import APIRouter

log = logging.getLogger(__name__)

router = APIRouter()

REPO = "davila7/claude-code-templates"
GITHUB_API = f"https://api.github.com/repos/{REPO}/contents"
CACHE_TTL_S = 3600  # 1 hour

# Simple in-memory cache: {url: (timestamp, data)}
_cache: dict[str, tuple[float, Any]] = {}


def _cache_get(key: str) -> Any | None:
    entry = _cache.get(key)
    if entry and (time.monotonic() - entry[0]) < CACHE_TTL_S:
        return entry[1]
    return None


def _cache_set(key: str, data: Any) -> None:
    _cache[key] = (time.monotonic(), data)


async def _github_fetch(path: str) -> Any:
    """Fetch from GitHub API with caching."""
    url = f"{GITHUB_API}/{path}"
    cached = _cache_get(url)
    if cached is not None:
        return cached

    async with httpx.AsyncClient(timeout=15) as client:
        resp = await client.get(url, headers={"Accept": "application/vnd.github.v3+json"})
        resp.raise_for_status()
        data = resp.json()

    _cache_set(url, data)
    return data


def _parse_frontmatter(content: str) -> tuple[dict[str, str], str]:
    """Parse YAML-like frontmatter from markdown content without pyyaml."""
    parts = content.split("---", 2)
    if len(parts) >= 3:
        meta: dict[str, str] = {}
        for line in parts[1].strip().split("\n"):
            if ":" in line:
                key, val = line.split(":", 1)
                key = key.strip()
                val = val.strip().strip('"').strip("'")
                if val:
                    meta[key] = val
        return meta, parts[2].strip()
    return {}, content


def _decode_file_content(data: dict) -> str:
    """Decode base64 file content from GitHub API response."""
    content_b64 = data.get("content", "")
    return base64.b64decode(content_b64).decode("utf-8", errors="replace")


# ── Agent templates ───────────────────────────────────────────────────


@router.get("/api/templates/agents")
async def list_agent_categories():
    """List agent template categories (top-level directories)."""
    try:
        data = await _github_fetch("cli-tool/components/agents")
        categories = [
            {"name": item["name"], "type": item["type"]}
            for item in data
            if item["type"] == "dir"
        ]
        return {"categories": categories}
    except Exception as e:
        log.warning("Failed to fetch agent categories: %s", e)
        return {"error": str(e), "categories": []}


@router.get("/api/templates/agents/{category}")
async def list_agents_in_category(category: str):
    """List agent templates in a category."""
    try:
        data = await _github_fetch(f"cli-tool/components/agents/{category}")
        agents = [
            {"name": item["name"].replace(".md", ""), "filename": item["name"]}
            for item in data
            if item["type"] == "file" and item["name"].endswith(".md")
        ]
        return {"agents": agents, "category": category}
    except Exception as e:
        log.warning("Failed to fetch agents in %s: %s", category, e)
        return {"error": str(e), "agents": []}


@router.get("/api/templates/agents/{category}/{name}")
async def get_agent_template(category: str, name: str):
    """Get a specific agent template with parsed frontmatter."""
    filename = name if name.endswith(".md") else f"{name}.md"
    try:
        data = await _github_fetch(f"cli-tool/components/agents/{category}/{filename}")
        content = _decode_file_content(data)
        meta, body = _parse_frontmatter(content)
        return {
            "name": meta.get("name", name),
            "description": meta.get("description", ""),
            "tools": meta.get("tools", ""),
            "model": meta.get("model", ""),
            "body": body,
            "category": category,
        }
    except Exception as e:
        log.warning("Failed to fetch agent template %s/%s: %s", category, name, e)
        return {"error": str(e)}


# ── Command templates ─────────────────────────────────────────────────


@router.get("/api/templates/commands")
async def list_command_categories():
    """List command template categories."""
    try:
        data = await _github_fetch("cli-tool/components/commands")
        categories = [
            {"name": item["name"], "type": item["type"]}
            for item in data
            if item["type"] == "dir"
        ]
        return {"categories": categories}
    except Exception as e:
        log.warning("Failed to fetch command categories: %s", e)
        return {"error": str(e), "categories": []}


@router.get("/api/templates/commands/{category}")
async def list_commands_in_category(category: str):
    """List command templates in a category."""
    try:
        data = await _github_fetch(f"cli-tool/components/commands/{category}")
        commands = [
            {"name": item["name"].replace(".md", ""), "filename": item["name"]}
            for item in data
            if item["type"] == "file" and item["name"].endswith(".md")
        ]
        return {"commands": commands, "category": category}
    except Exception as e:
        log.warning("Failed to fetch commands in %s: %s", category, e)
        return {"error": str(e), "commands": []}


@router.get("/api/templates/commands/{category}/{name}")
async def get_command_template(category: str, name: str):
    """Get a specific command template with parsed frontmatter."""
    filename = name if name.endswith(".md") else f"{name}.md"
    try:
        data = await _github_fetch(f"cli-tool/components/commands/{category}/{filename}")
        content = _decode_file_content(data)
        meta, body = _parse_frontmatter(content)
        return {
            "name": meta.get("name", name),
            "description": meta.get("description", ""),
            "allowed_tools": meta.get("allowed-tools", ""),
            "argument_hint": meta.get("argument-hint", ""),
            "body": body,
            "category": category,
        }
    except Exception as e:
        log.warning("Failed to fetch command template %s/%s: %s", category, name, e)
        return {"error": str(e)}
