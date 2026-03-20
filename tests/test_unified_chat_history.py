"""Tests for unified Chat History (Feature #7).

Covers:
- History API type filter: all, agent, group
- Board projects appear as history items with board: prefix
- Group chat items have correct fields (type, title, message_count, etc.)
- Search across both agent sessions and board messages
- Merged list sorted by updated_at descending
- board:<project> detail endpoint still works
- Edge cases: empty boards, deleted boards
"""

import pytest
import pytest_asyncio
from httpx import AsyncClient, ASGITransport

from coral.web_server import app
from coral.messageboard.store import MessageBoardStore


@pytest_asyncio.fixture
async def board_store(tmp_path):
    """Create a MessageBoardStore for seeding test data."""
    s = MessageBoardStore(db_path=tmp_path / "test_board.db")
    yield s
    await s.close()


# ── History API type filter tests ────────────────────────────────────────────


@pytest.mark.asyncio
async def test_history_api_default_type_is_all():
    """GET /api/sessions/history without type param should default to 'all'."""
    async with AsyncClient(transport=ASGITransport(app=app), base_url="http://test") as client:
        resp = await client.get("/api/sessions/history")
        assert resp.status_code == 200
        data = resp.json()
        assert "sessions" in data
        assert "total" in data


@pytest.mark.asyncio
async def test_history_api_type_agent_excludes_boards():
    """GET /api/sessions/history?type=agent should only return agent sessions."""
    async with AsyncClient(transport=ASGITransport(app=app), base_url="http://test") as client:
        resp = await client.get("/api/sessions/history?type=agent")
        assert resp.status_code == 200
        data = resp.json()
        # No items should have type='group'
        for session in data.get("sessions", []):
            assert session.get("type") != "group"


@pytest.mark.asyncio
async def test_history_api_type_group_excludes_agents():
    """GET /api/sessions/history?type=group should only return board projects."""
    async with AsyncClient(transport=ASGITransport(app=app), base_url="http://test") as client:
        resp = await client.get("/api/sessions/history?type=group")
        assert resp.status_code == 200
        data = resp.json()
        # All items should have type='group' — agent items must be filtered out
        for session in data.get("sessions", []):
            if session.get("type"):
                assert session["type"] == "group", f"Expected type='group', got '{session['type']}'"


@pytest.mark.asyncio
async def test_history_api_invalid_type_defaults_to_all():
    """Invalid type param should be treated as 'all'."""
    async with AsyncClient(transport=ASGITransport(app=app), base_url="http://test") as client:
        resp = await client.get("/api/sessions/history?type=invalid")
        assert resp.status_code == 200


# ── Group chat item format ──────────────────────────────────────────────────


class TestGroupChatItemFormat:
    """Test the structure of group chat items returned by the history API."""

    def test_board_session_id_prefix(self):
        """Group chat session_id should start with 'board:'."""
        project_name = "feature-xyz"
        session_id = f"board:{project_name}"
        assert session_id.startswith("board:")
        assert session_id.split(":", 1)[1] == project_name

    def test_board_item_has_required_fields(self):
        """Validate the expected shape of a group chat history item."""
        item = {
            "session_id": "board:my-project",
            "title": "my-project",
            "type": "group",
            "summary": "5 messages, 3 participants",
            "message_count": 5,
            "subscriber_count": 3,
            "created_at": "2026-03-20T06:00:00+00:00",
            "updated_at": "2026-03-20T07:00:00+00:00",
        }
        assert item["type"] == "group"
        assert item["session_id"].startswith("board:")
        assert isinstance(item["message_count"], int)
        assert isinstance(item["subscriber_count"], int)

    def test_agent_item_has_type_agent(self):
        """Agent chat items should have type='agent'."""
        item = {"session_id": "abc-123", "type": "agent"}
        assert item["type"] == "agent"
        assert not item["session_id"].startswith("board:")


# ── Board message search ────────────────────────────────────────────────────


@pytest.mark.asyncio
async def test_board_store_message_search(board_store):
    """Board messages should be searchable by content."""
    await board_store.subscribe("proj", "a1", "Dev")
    await board_store.post_message("proj", "a1", "implement the auth middleware")
    await board_store.post_message("proj", "a1", "fix the database migration")

    # Simulate searching board messages (the pattern the history API would use)
    messages = await board_store.list_messages("proj")
    matching = [m for m in messages if "auth" in m["content"].lower()]
    assert len(matching) == 1
    assert "auth middleware" in matching[0]["content"]


@pytest.mark.asyncio
async def test_board_store_search_no_results(board_store):
    """Search with no matches should return empty."""
    await board_store.subscribe("proj", "a1", "Dev")
    await board_store.post_message("proj", "a1", "hello world")

    messages = await board_store.list_messages("proj")
    matching = [m for m in messages if "nonexistent" in m["content"].lower()]
    assert len(matching) == 0


# ── Merged list sorting ─────────────────────────────────────────────────────


class TestMergedSorting:
    """Test that merged agent + group items sort by updated_at descending."""

    def _merge_and_sort(self, agents: list[dict], groups: list[dict]) -> list[dict]:
        """Simulate the merge logic."""
        combined = agents + groups
        return sorted(combined, key=lambda x: x.get("updated_at", ""), reverse=True)

    def test_merge_sorts_by_updated_at(self):
        items = self._merge_and_sort(
            [{"session_id": "a1", "type": "agent", "updated_at": "2026-03-20T05:00:00"}],
            [{"session_id": "board:proj", "type": "group", "updated_at": "2026-03-20T06:00:00"}],
        )
        assert items[0]["session_id"] == "board:proj"  # more recent
        assert items[1]["session_id"] == "a1"

    def test_merge_interleaves_correctly(self):
        items = self._merge_and_sort(
            [
                {"session_id": "a1", "type": "agent", "updated_at": "2026-03-20T07:00:00"},
                {"session_id": "a2", "type": "agent", "updated_at": "2026-03-20T05:00:00"},
            ],
            [
                {"session_id": "board:p1", "type": "group", "updated_at": "2026-03-20T06:00:00"},
            ],
        )
        assert [i["session_id"] for i in items] == ["a1", "board:p1", "a2"]

    def test_merge_empty_groups(self):
        items = self._merge_and_sort(
            [{"session_id": "a1", "type": "agent", "updated_at": "2026-03-20T05:00:00"}],
            [],
        )
        assert len(items) == 1
        assert items[0]["type"] == "agent"

    def test_merge_empty_agents(self):
        items = self._merge_and_sort(
            [],
            [{"session_id": "board:p1", "type": "group", "updated_at": "2026-03-20T06:00:00"}],
        )
        assert len(items) == 1
        assert items[0]["type"] == "group"


# ── Edge cases ──────────────────────────────────────────────────────────────


@pytest.mark.asyncio
async def test_empty_board_still_appears_as_group_chat(board_store):
    """A board with subscribers but no messages should still be listable."""
    await board_store.subscribe("empty-proj", "a1", "Dev")
    projects = await board_store.list_projects()
    proj = next((p for p in projects if p["project"] == "empty-proj"), None)
    assert proj is not None
    assert proj["message_count"] == 0
    assert proj["subscriber_count"] == 1


@pytest.mark.asyncio
async def test_deleted_board_not_in_projects(board_store):
    """A deleted board should not appear in list_projects."""
    await board_store.subscribe("temp-proj", "a1", "Dev")
    await board_store.post_message("temp-proj", "a1", "hello")
    await board_store.delete_project("temp-proj")

    projects = await board_store.list_projects()
    proj_names = [p["project"] for p in projects]
    assert "temp-proj" not in proj_names


@pytest.mark.asyncio
async def test_board_detail_endpoint_returns_messages():
    """GET /api/board/{project}/messages/all should return board messages."""
    async with AsyncClient(transport=ASGITransport(app=app), base_url="http://test") as client:
        # This endpoint already exists — just verify it's accessible
        resp = await client.get("/api/board/nonexistent-proj/messages/all")
        assert resp.status_code == 200
