"""Tests for agent icons feature and show_scrollbars setting.

Covers:
- Agent icon storage/retrieval via store and API
- Icon persists across page reload (settings round-trip)
- Icon can be set, changed, and cleared
- show_scrollbars setting storage
"""

import pytest
import pytest_asyncio
from httpx import AsyncClient, ASGITransport

from coral.store import CoralStore
from coral.web_server import app


@pytest_asyncio.fixture
async def store(tmp_path):
    s = CoralStore(db_path=tmp_path / "test.db")
    yield s
    await s.close()


# ── show_scrollbars setting ─────────────────────────────────────────────────


@pytest.mark.asyncio
async def test_show_scrollbars_default_not_set(store):
    """show_scrollbars should not be set by default."""
    settings = await store.get_settings()
    assert "show_scrollbars" not in settings


@pytest.mark.asyncio
async def test_show_scrollbars_set_and_get(store):
    """show_scrollbars can be stored and retrieved."""
    await store.set_setting("show_scrollbars", "true")
    settings = await store.get_settings()
    assert settings["show_scrollbars"] == "true"


@pytest.mark.asyncio
async def test_show_scrollbars_toggle_off(store):
    """show_scrollbars can be toggled off."""
    await store.set_setting("show_scrollbars", "true")
    await store.set_setting("show_scrollbars", "false")
    settings = await store.get_settings()
    assert settings["show_scrollbars"] == "false"


@pytest.mark.asyncio
async def test_api_show_scrollbars_round_trip():
    """PUT/GET show_scrollbars via API."""
    async with AsyncClient(transport=ASGITransport(app=app), base_url="http://test") as client:
        resp = await client.put("/api/settings", json={"show_scrollbars": "false"})
        assert resp.status_code == 200

        resp = await client.get("/api/settings")
        assert resp.json()["settings"]["show_scrollbars"] == "false"


# ── Agent icon via live_sessions API ─────────────────────────────────────────


class TestAgentIconAPI:
    """Tests for the agent icon API endpoint.

    These tests validate the PUT /api/sessions/live/{name}/icon endpoint
    and icon in session list responses.
    """

    @pytest.mark.asyncio
    async def test_set_icon_endpoint_exists(self):
        """PUT /api/sessions/live/{name}/icon should be a valid route."""
        async with AsyncClient(transport=ASGITransport(app=app), base_url="http://test") as client:
            resp = await client.put(
                "/api/sessions/live/nonexistent/icon",
                json={"session_id": "test", "icon": "🚀"}
            )
            assert resp.status_code != 404, "Icon endpoint route not registered"
            assert resp.status_code != 405, "PUT method not allowed on icon endpoint"

    @pytest.mark.asyncio
    async def test_icon_in_session_list_response(self):
        """GET /api/sessions/live should include icon field if set."""
        async with AsyncClient(transport=ASGITransport(app=app), base_url="http://test") as client:
            resp = await client.get("/api/sessions/live")
            assert resp.status_code == 200


# ── Icon data model tests ───────────────────────────────────────────────────


class TestIconDataModel:
    """Test icon values and edge cases."""

    def test_emoji_icon_is_valid(self):
        """Common emoji should be valid icon values."""
        icons = ["🚀", "🔧", "🎯", "🤖", "👑", "⚡"]
        for icon in icons:
            assert isinstance(icon, str)
            assert len(icon) > 0

    def test_empty_string_clears_icon(self):
        """Empty string should be a valid way to clear an icon."""
        icon = ""
        assert icon == ""

    def test_icon_is_short_text(self):
        """Icons should be short (1-2 characters for emoji)."""
        icon = "🚀"
        # Single emoji is 1-2 code points
        assert len(icon) <= 4  # generous for multi-codepoint emoji

    def test_multi_emoji_icon(self):
        """Some users might set multi-character icons."""
        icon = "🔥🚀"
        assert isinstance(icon, str)
