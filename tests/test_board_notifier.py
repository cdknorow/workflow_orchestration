"""Tests for the MessageBoardNotifier background task."""

import pytest
import pytest_asyncio
from unittest.mock import AsyncMock, patch

from coral.messageboard.store import MessageBoardStore
from coral.background_tasks.board_notifier import MessageBoardNotifier


@pytest_asyncio.fixture
async def board_store(tmp_path):
    s = MessageBoardStore(db_path=tmp_path / "test_board.db")
    yield s
    await s.close()


@pytest_asyncio.fixture
async def notifier(board_store):
    return MessageBoardNotifier(board_store)


def _make_agent(session_id, agent_name="test-agent"):
    return {
        "agent_type": "claude",
        "agent_name": agent_name,
        "session_id": session_id,
        "tmux_session": f"claude-{session_id}",
        "log_path": f"/tmp/claude_coral_{session_id}.log",
        "working_directory": f"/tmp/{agent_name}",
    }


@pytest.mark.asyncio
@patch("coral.background_tasks.board_notifier.send_to_tmux", new_callable=AsyncMock)
@patch("coral.background_tasks.board_notifier.discover_coral_agents", new_callable=AsyncMock)
async def test_notifies_agent_with_unread_messages(mock_discover, mock_send, board_store, notifier):
    """Agent with unread messages should receive a nudge."""
    mock_discover.return_value = [_make_agent("agent-1", "worktree_1")]
    mock_send.return_value = None  # no error

    await board_store.subscribe("proj1", "agent-1", "Backend Dev")
    await board_store.subscribe("proj1", "agent-2", "Frontend Dev")
    await board_store.post_message("proj1", "agent-2", "@notify-all Need help with schema")

    result = await notifier.run_once()
    assert result["notified"] == 1
    mock_send.assert_called_once()
    call_args = mock_send.call_args
    assert "1 unread message on" in call_args[0][1]


@pytest.mark.asyncio
@patch("coral.background_tasks.board_notifier.send_to_tmux", new_callable=AsyncMock)
@patch("coral.background_tasks.board_notifier.discover_coral_agents", new_callable=AsyncMock)
async def test_does_not_renotify_same_count(mock_discover, mock_send, board_store, notifier):
    """Should not re-notify if unread count hasn't changed."""
    mock_discover.return_value = [_make_agent("agent-1")]
    mock_send.return_value = None

    await board_store.subscribe("proj1", "agent-1", "Backend Dev")
    await board_store.subscribe("proj1", "agent-2", "Frontend Dev")
    await board_store.post_message("proj1", "agent-2", "@agent-1 msg 1")

    # First run: should notify
    result1 = await notifier.run_once()
    assert result1["notified"] == 1

    # Second run: same unread count, should NOT notify
    result2 = await notifier.run_once()
    assert result2["notified"] == 0
    assert mock_send.call_count == 1


@pytest.mark.asyncio
@patch("coral.background_tasks.board_notifier.send_to_tmux", new_callable=AsyncMock)
@patch("coral.background_tasks.board_notifier.discover_coral_agents", new_callable=AsyncMock)
async def test_renotifies_on_new_messages(mock_discover, mock_send, board_store, notifier):
    """Should re-notify when new messages arrive (unread count changes)."""
    mock_discover.return_value = [_make_agent("agent-1")]
    mock_send.return_value = None

    await board_store.subscribe("proj1", "agent-1", "Backend Dev")
    await board_store.subscribe("proj1", "agent-2", "Frontend Dev")
    await board_store.post_message("proj1", "agent-2", "@notify-all msg 1")

    await notifier.run_once()
    assert mock_send.call_count == 1

    # New message arrives
    await board_store.post_message("proj1", "agent-2", "@notify-all msg 2")
    result = await notifier.run_once()
    assert result["notified"] == 1
    assert mock_send.call_count == 2


@pytest.mark.asyncio
@patch("coral.background_tasks.board_notifier.send_to_tmux", new_callable=AsyncMock)
@patch("coral.background_tasks.board_notifier.discover_coral_agents", new_callable=AsyncMock)
async def test_clears_state_when_messages_read(mock_discover, mock_send, board_store, notifier):
    """After agent reads messages, notification state should clear."""
    mock_discover.return_value = [_make_agent("agent-1")]
    mock_send.return_value = None

    await board_store.subscribe("proj1", "agent-1", "Backend Dev")
    await board_store.subscribe("proj1", "agent-2", "Frontend Dev")
    await board_store.post_message("proj1", "agent-2", "@notify-all msg 1")

    await notifier.run_once()
    assert "agent-1" in notifier._notified

    # Agent reads messages
    await board_store.read_messages("proj1", "agent-1")
    await notifier.run_once()
    assert "agent-1" not in notifier._notified


@pytest.mark.asyncio
@patch("coral.background_tasks.board_notifier.send_to_tmux", new_callable=AsyncMock)
@patch("coral.background_tasks.board_notifier.discover_coral_agents", new_callable=AsyncMock)
async def test_no_notification_for_unsubscribed_agent(mock_discover, mock_send, board_store, notifier):
    """Agents not subscribed to any board should not be notified."""
    mock_discover.return_value = [_make_agent("agent-1")]
    mock_send.return_value = None

    # agent-1 is NOT subscribed
    result = await notifier.run_once()
    assert result["notified"] == 0
    mock_send.assert_not_called()


@pytest.mark.asyncio
@patch("coral.background_tasks.board_notifier.send_to_tmux", new_callable=AsyncMock)
@patch("coral.background_tasks.board_notifier.discover_coral_agents", new_callable=AsyncMock)
async def test_no_notification_without_mention(mock_discover, mock_send, board_store, notifier):
    """Messages without @mention should not trigger notifications."""
    mock_discover.return_value = [_make_agent("agent-1")]
    mock_send.return_value = None

    await board_store.subscribe("proj1", "agent-1", "Backend Dev")
    await board_store.subscribe("proj1", "agent-2", "Frontend Dev")
    await board_store.post_message("proj1", "agent-2", "just a general update, no mention")

    result = await notifier.run_once()
    assert result["notified"] == 0
    mock_send.assert_not_called()


@pytest.mark.asyncio
@patch("coral.background_tasks.board_notifier.send_to_tmux", new_callable=AsyncMock)
@patch("coral.background_tasks.board_notifier.discover_coral_agents", new_callable=AsyncMock)
async def test_cleans_up_stale_sessions(mock_discover, mock_send, board_store, notifier):
    """Should clean up _notified entries for sessions no longer live."""
    mock_discover.return_value = [_make_agent("agent-1")]
    mock_send.return_value = None

    await board_store.subscribe("proj1", "agent-1", "Dev")
    await board_store.subscribe("proj1", "agent-2", "Dev")
    await board_store.post_message("proj1", "agent-2", "@notify-all msg")

    await notifier.run_once()
    assert "agent-1" in notifier._notified

    # Agent disappears from tmux
    mock_discover.return_value = []
    await notifier.run_once()
    assert "agent-1" not in notifier._notified
