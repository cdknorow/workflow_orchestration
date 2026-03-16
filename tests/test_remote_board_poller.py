"""Tests for remote board subscriptions, API, and poller."""

import json
import asyncio
from pathlib import Path
from unittest.mock import AsyncMock, patch, MagicMock

import pytest
import pytest_asyncio

from coral.store.remote_boards import RemoteBoardStore
from coral.background_tasks.remote_board_poller import RemoteBoardPoller


# ── RemoteBoardStore tests ──────────────────────────────────────────────────


@pytest_asyncio.fixture
async def remote_store(tmp_path):
    store = RemoteBoardStore(db_path=tmp_path / "test.db")
    yield store
    await store.close()


@pytest.mark.asyncio
async def test_add_and_list(remote_store):
    sub = await remote_store.add("agent-1", "http://remote:8420", "proj1", "Dev")
    assert sub["session_id"] == "agent-1"
    assert sub["remote_server"] == "http://remote:8420"
    assert sub["project"] == "proj1"
    assert sub["job_title"] == "Dev"
    assert sub["last_notified_unread"] == 0

    all_subs = await remote_store.list_all()
    assert len(all_subs) == 1
    assert all_subs[0]["session_id"] == "agent-1"


@pytest.mark.asyncio
async def test_add_upsert(remote_store):
    """Adding the same subscription updates the job_title."""
    await remote_store.add("agent-1", "http://remote:8420", "proj1", "Dev")
    await remote_store.add("agent-1", "http://remote:8420", "proj1", "Lead Dev")
    all_subs = await remote_store.list_all()
    assert len(all_subs) == 1
    assert all_subs[0]["job_title"] == "Lead Dev"


@pytest.mark.asyncio
async def test_remove(remote_store):
    await remote_store.add("agent-1", "http://remote:8420", "proj1", "Dev")
    await remote_store.add("agent-2", "http://remote:8420", "proj1", "QA")
    removed = await remote_store.remove("agent-1")
    assert removed is True
    all_subs = await remote_store.list_all()
    assert len(all_subs) == 1
    assert all_subs[0]["session_id"] == "agent-2"


@pytest.mark.asyncio
async def test_remove_nonexistent(remote_store):
    removed = await remote_store.remove("nobody")
    assert removed is False


@pytest.mark.asyncio
async def test_update_last_notified(remote_store):
    sub = await remote_store.add("agent-1", "http://remote:8420", "proj1", "Dev")
    await remote_store.update_last_notified(sub["id"], 5)
    all_subs = await remote_store.list_all()
    assert all_subs[0]["last_notified_unread"] == 5


# ── RemoteBoardPoller tests ────────────────────────────────────────────────


@pytest.mark.asyncio
async def test_poller_no_subs(remote_store):
    """Poller does nothing when there are no subscriptions."""
    poller = RemoteBoardPoller(remote_store)
    result = await poller.run_once()
    assert result == {"notified": 0}
    await poller.close()


@pytest.mark.asyncio
async def test_poller_notifies_on_unread(remote_store):
    """Poller sends a nudge when remote reports unread messages."""
    await remote_store.add("agent-1", "http://remote:8420", "proj1", "Dev")

    mock_agent = {
        "session_id": "agent-1",
        "tmux_session": "claude-agent-1",
        "agent_name": "agent-1",
        "agent_type": "claude",
    }

    with patch("coral.background_tasks.remote_board_poller.discover_coral_agents",
               new_callable=AsyncMock, return_value=[mock_agent]), \
         patch("coral.background_tasks.remote_board_poller.send_to_tmux",
               new_callable=AsyncMock, return_value=None) as mock_send:

        poller = RemoteBoardPoller(remote_store)

        # Mock httpx client
        mock_response = MagicMock()
        mock_response.json.return_value = {"unread": 3}
        mock_response.raise_for_status = MagicMock()

        mock_client = AsyncMock()
        mock_client.get = AsyncMock(return_value=mock_response)
        poller._client = mock_client

        result = await poller.run_once()
        assert result == {"notified": 1}
        mock_send.assert_called_once()
        assert "3 unread messages" in mock_send.call_args[0][1]

        await poller.close()


@pytest.mark.asyncio
async def test_poller_skips_when_count_unchanged(remote_store):
    """Poller doesn't re-nudge when unread count hasn't changed."""
    sub = await remote_store.add("agent-1", "http://remote:8420", "proj1", "Dev")
    # Simulate already notified about 3 messages
    await remote_store.update_last_notified(sub["id"], 3)

    mock_agent = {
        "session_id": "agent-1",
        "tmux_session": "claude-agent-1",
        "agent_name": "agent-1",
        "agent_type": "claude",
    }

    with patch("coral.background_tasks.remote_board_poller.discover_coral_agents",
               new_callable=AsyncMock, return_value=[mock_agent]), \
         patch("coral.background_tasks.remote_board_poller.send_to_tmux",
               new_callable=AsyncMock) as mock_send:

        poller = RemoteBoardPoller(remote_store)

        mock_response = MagicMock()
        mock_response.json.return_value = {"unread": 3}
        mock_response.raise_for_status = MagicMock()

        mock_client = AsyncMock()
        mock_client.get = AsyncMock(return_value=mock_response)
        poller._client = mock_client

        result = await poller.run_once()
        assert result == {"notified": 0}
        mock_send.assert_not_called()

        await poller.close()


@pytest.mark.asyncio
async def test_poller_handles_remote_error(remote_store):
    """Poller handles connection errors gracefully."""
    await remote_store.add("agent-1", "http://remote:8420", "proj1", "Dev")

    mock_agent = {
        "session_id": "agent-1",
        "tmux_session": "claude-agent-1",
        "agent_name": "agent-1",
        "agent_type": "claude",
    }

    with patch("coral.background_tasks.remote_board_poller.discover_coral_agents",
               new_callable=AsyncMock, return_value=[mock_agent]):

        poller = RemoteBoardPoller(remote_store)

        mock_client = AsyncMock()
        mock_client.get = AsyncMock(side_effect=Exception("Connection refused"))
        poller._client = mock_client

        result = await poller.run_once()
        assert result == {"notified": 0}

        await poller.close()


@pytest.mark.asyncio
async def test_poller_clears_notification_on_zero_unread(remote_store):
    """Poller resets last_notified_unread when remote reports 0 unread."""
    sub = await remote_store.add("agent-1", "http://remote:8420", "proj1", "Dev")
    await remote_store.update_last_notified(sub["id"], 5)

    mock_agent = {
        "session_id": "agent-1",
        "tmux_session": "claude-agent-1",
        "agent_name": "agent-1",
        "agent_type": "claude",
    }

    with patch("coral.background_tasks.remote_board_poller.discover_coral_agents",
               new_callable=AsyncMock, return_value=[mock_agent]):

        poller = RemoteBoardPoller(remote_store)

        mock_response = MagicMock()
        mock_response.json.return_value = {"unread": 0}
        mock_response.raise_for_status = MagicMock()

        mock_client = AsyncMock()
        mock_client.get = AsyncMock(return_value=mock_response)
        poller._client = mock_client

        await poller.run_once()

        # Check that last_notified_unread was reset
        subs = await remote_store.list_all()
        assert subs[0]["last_notified_unread"] == 0

        await poller.close()


@pytest.mark.asyncio
async def test_poller_continues_after_one_remote_fails(remote_store):
    """Poller processes remaining subscriptions when one remote fails."""
    await remote_store.add("agent-1", "http://bad-server:8420", "proj1", "Dev")
    await remote_store.add("agent-2", "http://good-server:8420", "proj2", "QA")

    mock_agents = [
        {"session_id": "agent-1", "tmux_session": "claude-agent-1", "agent_name": "agent-1"},
        {"session_id": "agent-2", "tmux_session": "claude-agent-2", "agent_name": "agent-2"},
    ]

    get_call_urls = []

    async def mock_get(url, **kwargs):
        get_call_urls.append(url)
        if "bad-server" in url:
            raise Exception("Connection refused")
        resp = MagicMock()
        resp.json.return_value = {"unread": 2}
        resp.raise_for_status = MagicMock()
        return resp

    with patch("coral.background_tasks.remote_board_poller.discover_coral_agents",
               new_callable=AsyncMock, return_value=mock_agents), \
         patch("coral.background_tasks.remote_board_poller.send_to_tmux",
               new_callable=AsyncMock, return_value=None) as mock_send:

        poller = RemoteBoardPoller(remote_store)
        mock_client = AsyncMock()
        mock_client.get = mock_get
        poller._client = mock_client

        result = await poller.run_once()

    # Both remotes were attempted
    assert len(get_call_urls) == 2
    # Only the good server's agent was notified
    assert result == {"notified": 1}
    mock_send.assert_called_once()
    assert "agent-2" in mock_send.call_args[0][0]

    await poller.close()


@pytest.mark.asyncio
async def test_poller_skips_when_tmux_send_fails(remote_store):
    """Poller doesn't update last_notified when tmux nudge fails."""
    await remote_store.add("agent-1", "http://remote:8420", "proj1", "Dev")

    mock_agent = {
        "session_id": "agent-1",
        "tmux_session": "claude-agent-1",
        "agent_name": "agent-1",
    }

    with patch("coral.background_tasks.remote_board_poller.discover_coral_agents",
               new_callable=AsyncMock, return_value=[mock_agent]), \
         patch("coral.background_tasks.remote_board_poller.send_to_tmux",
               new_callable=AsyncMock, return_value="session not found"):

        poller = RemoteBoardPoller(remote_store)

        mock_response = MagicMock()
        mock_response.json.return_value = {"unread": 4}
        mock_response.raise_for_status = MagicMock()

        mock_client = AsyncMock()
        mock_client.get = AsyncMock(return_value=mock_response)
        poller._client = mock_client

        result = await poller.run_once()

    assert result == {"notified": 0}
    # last_notified_unread should NOT have been updated
    subs = await remote_store.list_all()
    assert subs[0]["last_notified_unread"] == 0

    await poller.close()


@pytest.mark.asyncio
async def test_poller_skips_agent_not_in_local_map(remote_store):
    """Poller skips subscription when the agent isn't found in local tmux."""
    await remote_store.add("remote-only-agent", "http://remote:8420", "proj1", "Dev")

    # Local agents don't include "remote-only-agent"
    local_agents = [
        {"session_id": "different-agent", "tmux_session": "claude-different", "agent_name": "other"},
    ]

    with patch("coral.background_tasks.remote_board_poller.discover_coral_agents",
               new_callable=AsyncMock, return_value=local_agents):

        poller = RemoteBoardPoller(remote_store)
        result = await poller.run_once()

    assert result == {"notified": 0}
    await poller.close()
