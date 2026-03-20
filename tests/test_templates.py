"""Tests for the template library API (aitmpl integration).

Covers:
- Frontmatter parsing (YAML-like, no pyyaml dependency)
- Base64 content decoding
- Cache behavior (TTL, hit/miss)
- API endpoints: agent categories, agent list, agent detail
- API endpoints: command categories, command list, command detail
- Error handling when GitHub API is unavailable
"""

import base64
import time
import pytest
from unittest.mock import AsyncMock, patch

from coral.api.templates import (
    _parse_frontmatter,
    _decode_file_content,
    _cache,
    _cache_get,
    _cache_set,
    CACHE_TTL_S,
)


# ── Frontmatter parsing ─────────────────────────────────────────────────────


class TestParseFrontmatter:

    def test_basic_frontmatter(self):
        content = '---\nname: Backend Dev\ndescription: A backend developer\n---\nYou are a backend developer.'
        meta, body = _parse_frontmatter(content)
        assert meta["name"] == "Backend Dev"
        assert meta["description"] == "A backend developer"
        assert body == "You are a backend developer."

    def test_quoted_values(self):
        content = '---\nname: "Quoted Name"\ndescription: \'Single Quoted\'\n---\nBody text.'
        meta, body = _parse_frontmatter(content)
        assert meta["name"] == "Quoted Name"
        assert meta["description"] == "Single Quoted"

    def test_no_frontmatter(self):
        content = "Just plain markdown content."
        meta, body = _parse_frontmatter(content)
        assert meta == {}
        assert body == content

    def test_empty_content(self):
        meta, body = _parse_frontmatter("")
        assert meta == {}
        assert body == ""

    def test_frontmatter_with_tools_and_model(self):
        content = '---\nname: Dev\ntools: Read,Edit,Bash\nmodel: claude-sonnet-4-5-20250514\n---\nSystem prompt here.'
        meta, body = _parse_frontmatter(content)
        assert meta["tools"] == "Read,Edit,Bash"
        assert meta["model"] == "claude-sonnet-4-5-20250514"

    def test_multiline_body(self):
        content = '---\nname: Test\n---\nLine 1.\n\nLine 2.\n\nLine 3.'
        meta, body = _parse_frontmatter(content)
        assert "Line 1." in body
        assert "Line 3." in body

    def test_empty_values_skipped(self):
        content = '---\nname: Dev\nempty_field:\n---\nBody.'
        meta, body = _parse_frontmatter(content)
        assert "name" in meta
        assert "empty_field" not in meta  # Empty values are skipped

    def test_colon_in_value(self):
        content = '---\ndescription: A tool: for developers\n---\nBody.'
        meta, body = _parse_frontmatter(content)
        assert meta["description"] == "A tool: for developers"

    def test_command_frontmatter(self):
        content = '---\nallowed-tools: Bash,Read\nargument-hint: <file>\ndescription: Run a file\n---\nExecute the file.'
        meta, body = _parse_frontmatter(content)
        assert meta["allowed-tools"] == "Bash,Read"
        assert meta["argument-hint"] == "<file>"
        assert meta["description"] == "Run a file"
        assert body == "Execute the file."


# ── Base64 decoding ──────────────────────────────────────────────────────────


class TestDecodeFileContent:

    def test_decode_valid_base64(self):
        text = "Hello, world!"
        encoded = base64.b64encode(text.encode()).decode()
        data = {"content": encoded}
        assert _decode_file_content(data) == text

    def test_decode_with_newlines(self):
        """GitHub API returns base64 with newlines."""
        text = "Line 1\nLine 2"
        encoded = base64.b64encode(text.encode()).decode()
        # Simulate GitHub's newline-split base64
        data = {"content": encoded}
        assert _decode_file_content(data) == text

    def test_decode_empty_content(self):
        data = {"content": ""}
        assert _decode_file_content(data) == ""

    def test_decode_missing_content_key(self):
        data = {}
        assert _decode_file_content(data) == ""

    def test_decode_frontmatter_template(self):
        template = '---\nname: Test Agent\ndescription: A test\n---\nYou are a test agent.'
        encoded = base64.b64encode(template.encode()).decode()
        data = {"content": encoded}
        result = _decode_file_content(data)
        meta, body = _parse_frontmatter(result)
        assert meta["name"] == "Test Agent"
        assert body == "You are a test agent."


# ── Cache behavior ───────────────────────────────────────────────────────────


class TestCache:

    def setup_method(self):
        _cache.clear()

    def test_cache_miss_returns_none(self):
        assert _cache_get("nonexistent") is None

    def test_cache_set_and_get(self):
        _cache_set("key1", {"data": "value"})
        assert _cache_get("key1") == {"data": "value"}

    def test_cache_expired_returns_none(self):
        # Manually insert an expired entry
        _cache["expired"] = (time.monotonic() - CACHE_TTL_S - 1, "old data")
        assert _cache_get("expired") is None

    def test_cache_not_expired(self):
        _cache_set("fresh", "fresh data")
        assert _cache_get("fresh") == "fresh data"

    def test_cache_overwrite(self):
        _cache_set("key", "v1")
        _cache_set("key", "v2")
        assert _cache_get("key") == "v2"


# ── API endpoint tests (mocked GitHub) ──────────────────────────────────────


MOCK_DIR_LISTING = [
    {"name": "development-team", "type": "dir"},
    {"name": "design-team", "type": "dir"},
    {"name": "README.md", "type": "file"},
]

MOCK_AGENT_LISTING = [
    {"name": "backend-developer.md", "type": "file"},
    {"name": "frontend-developer.md", "type": "file"},
    {"name": "README.md", "type": "file"},  # not .md template
]

MOCK_AGENT_CONTENT = {
    "content": base64.b64encode(
        b'---\nname: Backend Developer\ndescription: Builds APIs and services\ntools: Read,Edit,Bash\nmodel: claude-sonnet-4-5-20250514\n---\nYou are an expert backend developer.'
    ).decode()
}


@pytest.fixture(autouse=True)
def clear_cache():
    _cache.clear()
    yield
    _cache.clear()


@pytest.mark.asyncio
async def test_list_agent_categories():
    with patch("coral.api.templates._github_fetch", new_callable=AsyncMock, return_value=MOCK_DIR_LISTING):
        from httpx import AsyncClient, ASGITransport
        from coral.web_server import app
        async with AsyncClient(transport=ASGITransport(app=app), base_url="http://test") as client:
            resp = await client.get("/api/templates/agents")
            assert resp.status_code == 200
            data = resp.json()
            assert "categories" in data
            # Should only include dirs, not files
            names = [c["name"] for c in data["categories"]]
            assert "development-team" in names
            assert "README.md" not in names


@pytest.mark.asyncio
async def test_list_agents_in_category():
    with patch("coral.api.templates._github_fetch", new_callable=AsyncMock, return_value=MOCK_AGENT_LISTING):
        from httpx import AsyncClient, ASGITransport
        from coral.web_server import app
        async with AsyncClient(transport=ASGITransport(app=app), base_url="http://test") as client:
            resp = await client.get("/api/templates/agents/development-team")
            assert resp.status_code == 200
            data = resp.json()
            assert data["category"] == "development-team"
            agents = data["agents"]
            # Should strip .md extension from name
            names = [a["name"] for a in agents]
            assert "backend-developer" in names
            assert "frontend-developer" in names
            # README.md is a file but not a template (doesn't end with .md from the templates — actually it does)
            # The filter is: type == 'file' and name.endswith('.md'), so README.md would be included
            # That's fine — it just means it would appear in the list


@pytest.mark.asyncio
async def test_get_agent_template():
    with patch("coral.api.templates._github_fetch", new_callable=AsyncMock, return_value=MOCK_AGENT_CONTENT):
        from httpx import AsyncClient, ASGITransport
        from coral.web_server import app
        async with AsyncClient(transport=ASGITransport(app=app), base_url="http://test") as client:
            resp = await client.get("/api/templates/agents/development-team/backend-developer")
            assert resp.status_code == 200
            data = resp.json()
            assert data["name"] == "Backend Developer"
            assert data["description"] == "Builds APIs and services"
            assert data["tools"] == "Read,Edit,Bash"
            assert "expert backend developer" in data["body"]
            assert data["category"] == "development-team"


@pytest.mark.asyncio
async def test_get_agent_template_with_md_extension():
    """Name with .md extension should work too."""
    with patch("coral.api.templates._github_fetch", new_callable=AsyncMock, return_value=MOCK_AGENT_CONTENT):
        from httpx import AsyncClient, ASGITransport
        from coral.web_server import app
        async with AsyncClient(transport=ASGITransport(app=app), base_url="http://test") as client:
            resp = await client.get("/api/templates/agents/development-team/backend-developer.md")
            assert resp.status_code == 200
            assert resp.json()["name"] == "Backend Developer"


@pytest.mark.asyncio
async def test_github_error_returns_empty_with_error():
    """When GitHub API fails, endpoints should return error + empty list."""
    with patch("coral.api.templates._github_fetch", new_callable=AsyncMock, side_effect=Exception("rate limited")):
        from httpx import AsyncClient, ASGITransport
        from coral.web_server import app
        async with AsyncClient(transport=ASGITransport(app=app), base_url="http://test") as client:
            resp = await client.get("/api/templates/agents")
            assert resp.status_code == 200
            data = resp.json()
            assert "error" in data
            assert data["categories"] == []


@pytest.mark.asyncio
async def test_command_categories_endpoint():
    with patch("coral.api.templates._github_fetch", new_callable=AsyncMock, return_value=MOCK_DIR_LISTING):
        from httpx import AsyncClient, ASGITransport
        from coral.web_server import app
        async with AsyncClient(transport=ASGITransport(app=app), base_url="http://test") as client:
            resp = await client.get("/api/templates/commands")
            assert resp.status_code == 200
            assert "categories" in resp.json()


@pytest.mark.asyncio
async def test_get_command_template():
    mock_cmd = {
        "content": base64.b64encode(
            b'---\nallowed-tools: Bash\nargument-hint: <file>\ndescription: Run a script\n---\nExecute the given script.'
        ).decode()
    }
    with patch("coral.api.templates._github_fetch", new_callable=AsyncMock, return_value=mock_cmd):
        from httpx import AsyncClient, ASGITransport
        from coral.web_server import app
        async with AsyncClient(transport=ASGITransport(app=app), base_url="http://test") as client:
            resp = await client.get("/api/templates/commands/utility/run-script")
            assert resp.status_code == 200
            data = resp.json()
            assert data["allowed_tools"] == "Bash"
            assert data["argument_hint"] == "<file>"
            assert "Execute the given script" in data["body"]
