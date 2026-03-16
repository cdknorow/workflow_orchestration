"""Tests for hooks/message_check.py server URL resolution fix."""

import json
import os
from unittest import mock

import pytest

from coral.hooks.message_check import _load_board_state, main


@pytest.fixture
def board_state_dir(tmp_path, monkeypatch):
    """Set up an isolated board state directory.

    _load_board_state() reads from ~/.coral/board_state_{name}.json,
    so we redirect ~ to tmp_path and create the .coral subdir.
    """
    monkeypatch.delenv("TMUX", raising=False)
    monkeypatch.delenv("CORAL_URL", raising=False)
    monkeypatch.delenv("CORAL_PORT", raising=False)
    # Use hostname as session name (non-tmux path)
    hostname = "test-host"
    monkeypatch.setattr("platform.node", lambda: hostname)

    # _load_board_state uses os.path.expanduser("~") -> tmp_path
    # then appends ".coral/board_state_{name}.json"
    coral_dir = tmp_path / ".coral"
    coral_dir.mkdir(exist_ok=True)
    monkeypatch.setattr(
        "os.path.expanduser",
        lambda p: str(tmp_path) if p == "~" else os.path.expanduser(p),
    )

    state_file = coral_dir / f"board_state_{hostname}.json"
    return tmp_path, state_file, hostname


def _write_state(state_file, project="testproj", session_id="test-host",
                 server_url=None):
    """Write a board state file."""
    state = {
        "project": project,
        "job_title": "Dev",
        "session_id": session_id,
    }
    if server_url is not None:
        state["server_url"] = server_url
    state_file.write_text(json.dumps(state))


class TestServerUrlResolution:
    """Verify message_check uses the correct server URL priority chain."""

    def test_uses_server_url_from_state(self, board_state_dir, monkeypatch):
        """state.server_url should be used when present."""
        _, state_file, _ = board_state_dir
        _write_state(state_file, server_url="http://remote:9000")

        captured_urls = []

        def mock_coral_api(base, method, path, data=None):
            captured_urls.append(base)
            return {"unread": 0}

        monkeypatch.setattr("coral.hooks.message_check.coral_api", mock_coral_api)
        monkeypatch.setattr("sys.stdin", mock.Mock(read=lambda: ""))

        main()

        assert len(captured_urls) == 1
        assert captured_urls[0] == "http://remote:9000"

    def test_falls_back_to_coral_url_env(self, board_state_dir, monkeypatch):
        """Without server_url in state, use CORAL_URL env var."""
        _, state_file, _ = board_state_dir
        _write_state(state_file)  # no server_url key
        monkeypatch.setenv("CORAL_URL", "http://env-server:5000")

        captured_urls = []

        def mock_coral_api(base, method, path, data=None):
            captured_urls.append(base)
            return {"unread": 0}

        monkeypatch.setattr("coral.hooks.message_check.coral_api", mock_coral_api)
        monkeypatch.setattr("sys.stdin", mock.Mock(read=lambda: ""))

        main()

        assert captured_urls[0] == "http://env-server:5000"

    def test_falls_back_to_localhost_with_port(self, board_state_dir, monkeypatch):
        """Without server_url or CORAL_URL, fall back to localhost:CORAL_PORT."""
        _, state_file, _ = board_state_dir
        _write_state(state_file)
        monkeypatch.setenv("CORAL_PORT", "9999")

        captured_urls = []

        def mock_coral_api(base, method, path, data=None):
            captured_urls.append(base)
            return {"unread": 0}

        monkeypatch.setattr("coral.hooks.message_check.coral_api", mock_coral_api)
        monkeypatch.setattr("sys.stdin", mock.Mock(read=lambda: ""))

        main()

        assert captured_urls[0] == "http://localhost:9999"

    def test_falls_back_to_localhost_default_port(self, board_state_dir, monkeypatch):
        """Without any config, fall back to localhost:8420."""
        _, state_file, _ = board_state_dir
        _write_state(state_file)

        captured_urls = []

        def mock_coral_api(base, method, path, data=None):
            captured_urls.append(base)
            return {"unread": 0}

        monkeypatch.setattr("coral.hooks.message_check.coral_api", mock_coral_api)
        monkeypatch.setattr("sys.stdin", mock.Mock(read=lambda: ""))

        main()

        assert captured_urls[0] == "http://localhost:8420"

    def test_trailing_slash_stripped(self, board_state_dir, monkeypatch):
        """Trailing slash on server_url should be stripped."""
        _, state_file, _ = board_state_dir
        _write_state(state_file, server_url="http://remote:9000/")

        captured_urls = []

        def mock_coral_api(base, method, path, data=None):
            captured_urls.append(base)
            return {"unread": 0}

        monkeypatch.setattr("coral.hooks.message_check.coral_api", mock_coral_api)
        monkeypatch.setattr("sys.stdin", mock.Mock(read=lambda: ""))

        main()

        assert captured_urls[0] == "http://remote:9000"

    def test_state_server_url_overrides_coral_url_env(self, board_state_dir, monkeypatch):
        """state.server_url takes priority over CORAL_URL env var."""
        _, state_file, _ = board_state_dir
        _write_state(state_file, server_url="http://state-server:8420")
        monkeypatch.setenv("CORAL_URL", "http://env-server:5000")

        captured_urls = []

        def mock_coral_api(base, method, path, data=None):
            captured_urls.append(base)
            return {"unread": 0}

        monkeypatch.setattr("coral.hooks.message_check.coral_api", mock_coral_api)
        monkeypatch.setattr("sys.stdin", mock.Mock(read=lambda: ""))

        main()

        assert captured_urls[0] == "http://state-server:8420"

    def test_empty_server_url_falls_through(self, board_state_dir, monkeypatch):
        """Empty string server_url should fall through to env/default."""
        _, state_file, _ = board_state_dir
        _write_state(state_file, server_url="")
        monkeypatch.setenv("CORAL_URL", "http://env-server:5000")

        captured_urls = []

        def mock_coral_api(base, method, path, data=None):
            captured_urls.append(base)
            return {"unread": 0}

        monkeypatch.setattr("coral.hooks.message_check.coral_api", mock_coral_api)
        monkeypatch.setattr("sys.stdin", mock.Mock(read=lambda: ""))

        main()

        assert captured_urls[0] == "http://env-server:5000"


class TestNotificationOutput:
    """Verify notification messages are printed correctly."""

    def test_prints_notification_when_unread(self, board_state_dir, monkeypatch, capsys):
        _, state_file, _ = board_state_dir
        _write_state(state_file, server_url="http://localhost:8420")

        def mock_coral_api(base, method, path, data=None):
            return {"unread": 3}

        monkeypatch.setattr("coral.hooks.message_check.coral_api", mock_coral_api)
        monkeypatch.setattr("sys.stdin", mock.Mock(read=lambda: ""))

        main()

        captured = capsys.readouterr()
        assert "3 unread messages" in captured.out
        assert "coral-board read" in captured.out

    def test_no_output_when_no_unread(self, board_state_dir, monkeypatch, capsys):
        _, state_file, _ = board_state_dir
        _write_state(state_file, server_url="http://localhost:8420")

        def mock_coral_api(base, method, path, data=None):
            return {"unread": 0}

        monkeypatch.setattr("coral.hooks.message_check.coral_api", mock_coral_api)
        monkeypatch.setattr("sys.stdin", mock.Mock(read=lambda: ""))

        main()

        captured = capsys.readouterr()
        assert captured.out == ""
