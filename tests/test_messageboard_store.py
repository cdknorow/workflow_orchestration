"""Tests for the message board store layer."""

import pytest
import pytest_asyncio

from coral.messageboard.store import MessageBoardStore


@pytest_asyncio.fixture
async def store(tmp_path):
    s = MessageBoardStore(db_path=tmp_path / "test_board.db")
    yield s
    await s.close()


@pytest.mark.asyncio
async def test_subscribe_creates_subscriber(store):
    result = await store.subscribe("proj1", "agent-1", "Backend Dev")
    assert result["project"] == "proj1"
    assert result["session_id"] == "agent-1"
    assert result["job_title"] == "Backend Dev"
    assert result["last_read_id"] == 0


@pytest.mark.asyncio
async def test_subscribe_upsert_preserves_cursor(store):
    await store.subscribe("proj1", "agent-1", "Backend Dev")
    # Post a message from another agent and read to advance cursor
    await store.subscribe("proj1", "agent-2", "Frontend Dev")
    await store.post_message("proj1", "agent-2", "hello")
    msgs = await store.read_messages("proj1", "agent-1")
    assert len(msgs) == 1

    # Re-subscribe with new title
    result = await store.subscribe("proj1", "agent-1", "Senior Backend Dev")
    assert result["job_title"] == "Senior Backend Dev"
    # Cursor should be preserved (not reset to 0)
    assert result["last_read_id"] > 0


@pytest.mark.asyncio
async def test_unsubscribe(store):
    await store.subscribe("proj1", "agent-1", "Dev")
    removed = await store.unsubscribe("proj1", "agent-1")
    assert removed is True

    # Unsubscribing non-existent returns False
    removed = await store.unsubscribe("proj1", "agent-1")
    assert removed is False


@pytest.mark.asyncio
async def test_list_subscribers(store):
    await store.subscribe("proj1", "agent-1", "Backend")
    await store.subscribe("proj1", "agent-2", "Frontend")
    await store.subscribe("proj2", "agent-3", "QA")

    subs = await store.list_subscribers("proj1")
    assert len(subs) == 2
    session_ids = {s["session_id"] for s in subs}
    assert session_ids == {"agent-1", "agent-2"}


@pytest.mark.asyncio
async def test_post_message(store):
    msg = await store.post_message("proj1", "agent-1", "Found a bug")
    assert msg["id"] is not None
    assert msg["project"] == "proj1"
    assert msg["content"] == "Found a bug"


@pytest.mark.asyncio
async def test_read_messages_excludes_own_and_advances_cursor(store):
    await store.subscribe("proj1", "agent-1", "Backend")
    await store.subscribe("proj1", "agent-2", "Frontend")

    await store.post_message("proj1", "agent-1", "msg from 1")
    await store.post_message("proj1", "agent-2", "msg from 2")

    # agent-1 reads: should only see agent-2's message
    msgs = await store.read_messages("proj1", "agent-1")
    assert len(msgs) == 1
    assert msgs[0]["content"] == "msg from 2"
    assert msgs[0]["job_title"] == "Frontend"

    # agent-2 reads: should only see agent-1's message
    msgs = await store.read_messages("proj1", "agent-2")
    assert len(msgs) == 1
    assert msgs[0]["content"] == "msg from 1"


@pytest.mark.asyncio
async def test_read_messages_twice_returns_empty_on_second(store):
    await store.subscribe("proj1", "agent-1", "Backend")
    await store.subscribe("proj1", "agent-2", "Frontend")

    await store.post_message("proj1", "agent-2", "hello")

    msgs1 = await store.read_messages("proj1", "agent-1")
    assert len(msgs1) == 1

    # Second read with no new messages
    msgs2 = await store.read_messages("proj1", "agent-1")
    assert len(msgs2) == 0


@pytest.mark.asyncio
async def test_read_messages_unsubscribed_returns_empty(store):
    msgs = await store.read_messages("proj1", "nonexistent")
    assert msgs == []


@pytest.mark.asyncio
async def test_list_projects(store):
    await store.subscribe("proj1", "agent-1", "Dev")
    await store.subscribe("proj2", "agent-2", "Dev")
    await store.post_message("proj1", "agent-1", "hello")
    await store.post_message("proj1", "agent-1", "world")

    projects = await store.list_projects()
    assert len(projects) == 2
    p1 = next(p for p in projects if p["project"] == "proj1")
    assert p1["subscriber_count"] == 1
    assert p1["message_count"] == 2


@pytest.mark.asyncio
async def test_auto_prune(store):
    await store.subscribe("proj1", "agent-1", "Dev")
    # Post 510 messages
    for i in range(510):
        await store.post_message("proj1", "agent-1", f"msg {i}")

    # Check only 500 remain
    conn = await store._get_conn()
    rows = await conn.execute_fetchall(
        "SELECT COUNT(*) as cnt FROM board_messages WHERE project = 'proj1'"
    )
    assert rows[0]["cnt"] == 500


@pytest.mark.asyncio
async def test_get_webhook_targets(store):
    await store.subscribe("proj1", "agent-1", "Dev", webhook_url="http://example.com/hook")
    await store.subscribe("proj1", "agent-2", "Dev")
    await store.subscribe("proj1", "agent-3", "Dev", webhook_url="http://example.com/hook2")

    targets = await store.get_webhook_targets("proj1", "agent-1")
    assert len(targets) == 1
    assert targets[0]["session_id"] == "agent-3"


@pytest.mark.asyncio
async def test_delete_project(store):
    await store.subscribe("proj1", "agent-1", "Dev")
    await store.post_message("proj1", "agent-1", "hello")

    await store.delete_project("proj1")

    subs = await store.list_subscribers("proj1")
    assert len(subs) == 0
    projects = await store.list_projects()
    assert len(projects) == 0
