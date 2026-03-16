"""Tests for remote board subscription storage, API, and poller."""

import pytest
import pytest_asyncio

from coral.store.remote_boards import RemoteBoardStore


@pytest_asyncio.fixture
async def store(tmp_path):
    """Create an isolated RemoteBoardStore with a temp DB."""
    s = RemoteBoardStore(db_path=tmp_path / "test.db")
    yield s
    await s.close()


@pytest.mark.asyncio
async def test_add_subscription(store):
    """Can add a remote board subscription."""
    sub = await store.add(
        session_id="agent-1",
        remote_server="http://remote:8420",
        project="proj1",
        job_title="Dev",
    )
    assert sub["session_id"] == "agent-1"
    assert sub["remote_server"] == "http://remote:8420"
    assert sub["project"] == "proj1"
    assert sub["job_title"] == "Dev"
    assert sub["last_notified_unread"] == 0
    assert sub["id"] is not None


@pytest.mark.asyncio
async def test_add_duplicate_updates_job_title(store):
    """Adding the same subscription again updates job_title (upsert)."""
    await store.add("agent-1", "http://remote:8420", "proj1", "Dev")
    sub = await store.add("agent-1", "http://remote:8420", "proj1", "Lead Dev")
    assert sub["job_title"] == "Lead Dev"

    # Should still be only one row
    all_subs = await store.list_all()
    assert len(all_subs) == 1


@pytest.mark.asyncio
async def test_list_all(store):
    """list_all returns all subscriptions."""
    await store.add("agent-1", "http://remote:8420", "proj1", "Dev")
    await store.add("agent-2", "http://remote:9000", "proj2", "QA")

    subs = await store.list_all()
    assert len(subs) == 2
    assert subs[0]["session_id"] == "agent-1"
    assert subs[1]["session_id"] == "agent-2"


@pytest.mark.asyncio
async def test_list_all_empty(store):
    """list_all returns empty list when no subscriptions."""
    subs = await store.list_all()
    assert subs == []


@pytest.mark.asyncio
async def test_remove_subscription(store):
    """Can remove a subscription by session_id."""
    await store.add("agent-1", "http://remote:8420", "proj1", "Dev")
    removed = await store.remove("agent-1")
    assert removed is True

    subs = await store.list_all()
    assert subs == []


@pytest.mark.asyncio
async def test_remove_nonexistent(store):
    """Removing a non-existent subscription returns False."""
    removed = await store.remove("nonexistent")
    assert removed is False


@pytest.mark.asyncio
async def test_remove_deletes_all_for_session(store):
    """remove() deletes ALL subscriptions for a session_id."""
    await store.add("agent-1", "http://remote:8420", "proj1", "Dev")
    await store.add("agent-1", "http://remote:9000", "proj2", "Dev")
    await store.add("agent-2", "http://remote:8420", "proj1", "QA")

    removed = await store.remove("agent-1")
    assert removed is True

    subs = await store.list_all()
    assert len(subs) == 1
    assert subs[0]["session_id"] == "agent-2"


@pytest.mark.asyncio
async def test_update_last_notified(store):
    """Can update the last_notified_unread field."""
    sub = await store.add("agent-1", "http://remote:8420", "proj1", "Dev")
    assert sub["last_notified_unread"] == 0

    await store.update_last_notified(sub["id"], 5)

    subs = await store.list_all()
    assert subs[0]["last_notified_unread"] == 5


@pytest.mark.asyncio
async def test_unique_constraint(store):
    """Same (session_id, remote_server, project) should upsert, not duplicate."""
    await store.add("agent-1", "http://remote:8420", "proj1", "Dev")
    await store.add("agent-1", "http://remote:8420", "proj1", "Dev")

    subs = await store.list_all()
    assert len(subs) == 1


@pytest.mark.asyncio
async def test_different_servers_same_project(store):
    """Same session on different servers should create separate subscriptions."""
    await store.add("agent-1", "http://server-a:8420", "proj1", "Dev")
    await store.add("agent-1", "http://server-b:8420", "proj1", "Dev")

    subs = await store.list_all()
    assert len(subs) == 2
