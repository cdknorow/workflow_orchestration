"""Tests for the --server flag and _resolve_server() priority chain in coral-board CLI."""

import json
import os
from unittest import mock

import pytest

from coral.messageboard.cli import (
    _resolve_server,
    _save_state,
    _load_state,
    build_parser,
    main,
)
import coral.messageboard.cli as cli_module


@pytest.fixture(autouse=True)
def isolate_state(tmp_path, monkeypatch):
    """Isolate each test from real state and env."""
    monkeypatch.setattr(cli_module, "_STATE_DIR", tmp_path)
    monkeypatch.setattr(cli_module, "_server_override", None)
    monkeypatch.delenv("CORAL_URL", raising=False)
    # Use a stable session ID so state files are predictable
    monkeypatch.setattr(cli_module, "_session_id", lambda: "test-session")


class TestResolveServerPriorityChain:
    """Verify: --server flag > state file server_url > CORAL_URL env > default."""

    def test_default_when_nothing_set(self):
        """Lowest priority: returns localhost:8420."""
        assert _resolve_server() == "http://localhost:8420"

    def test_coral_url_env_overrides_default(self, monkeypatch):
        """CORAL_URL env var beats the default."""
        monkeypatch.setenv("CORAL_URL", "http://env-server:9000")
        assert _resolve_server() == "http://env-server:9000"

    def test_state_file_overrides_env(self, monkeypatch, tmp_path):
        """server_url in state file beats CORAL_URL env var."""
        monkeypatch.setenv("CORAL_URL", "http://env-server:9000")
        # Simulate a previous join that saved server_url
        _save_state_with_server(tmp_path, "http://state-server:8420")
        assert _resolve_server() == "http://state-server:8420"

    def test_server_flag_overrides_state_file(self, monkeypatch, tmp_path):
        """--server flag beats state file server_url."""
        monkeypatch.setenv("CORAL_URL", "http://env-server:9000")
        _save_state_with_server(tmp_path, "http://state-server:8420")
        monkeypatch.setattr(cli_module, "_server_override", "http://flag-server:7000")
        assert _resolve_server() == "http://flag-server:7000"

    def test_server_flag_overrides_env(self, monkeypatch):
        """--server flag beats CORAL_URL env var."""
        monkeypatch.setenv("CORAL_URL", "http://env-server:9000")
        monkeypatch.setattr(cli_module, "_server_override", "http://flag-server:7000")
        assert _resolve_server() == "http://flag-server:7000"

    def test_trailing_slash_stripped_from_flag(self, monkeypatch):
        monkeypatch.setattr(cli_module, "_server_override", "http://flag-server:7000/")
        assert _resolve_server() == "http://flag-server:7000"

    def test_trailing_slash_stripped_from_env(self, monkeypatch):
        monkeypatch.setenv("CORAL_URL", "http://env-server:9000/")
        assert _resolve_server() == "http://env-server:9000"

    def test_trailing_slash_stripped_from_state(self, tmp_path):
        _save_state_with_server(tmp_path, "http://state-server:8420/")
        assert _resolve_server() == "http://state-server:8420"

    def test_empty_state_server_url_falls_through(self, monkeypatch, tmp_path):
        """An empty string server_url in state should fall through to env/default."""
        monkeypatch.setenv("CORAL_URL", "http://env-server:9000")
        _save_state_with_server(tmp_path, "")
        assert _resolve_server() == "http://env-server:9000"


class TestSaveStatePersistsServer:
    """Verify _save_state() records server_url from _resolve_server()."""

    def test_save_state_records_default_server(self, tmp_path):
        _save_state("myproject", "Dev")
        state = _load_state()
        assert state["server_url"] == "http://localhost:8420"
        assert state["project"] == "myproject"
        assert state["job_title"] == "Dev"

    def test_save_state_records_override_server(self, monkeypatch, tmp_path):
        monkeypatch.setattr(cli_module, "_server_override", "http://remote:8420")
        _save_state("myproject", "Dev")
        state = _load_state()
        assert state["server_url"] == "http://remote:8420"

    def test_save_state_records_env_server(self, monkeypatch, tmp_path):
        monkeypatch.setenv("CORAL_URL", "http://env-host:5000")
        _save_state("myproject", "Dev")
        state = _load_state()
        assert state["server_url"] == "http://env-host:5000"


class TestBuildParser:
    """Verify the argparse --server flag is wired up correctly."""

    def test_server_flag_parsed(self):
        parser = build_parser()
        args = parser.parse_args(["--server", "http://remote:8420", "projects"])
        assert args.server == "http://remote:8420"

    def test_server_flag_defaults_to_none(self):
        parser = build_parser()
        args = parser.parse_args(["projects"])
        assert args.server is None

    def test_server_flag_before_subcommand(self):
        """--server must come before the subcommand."""
        parser = build_parser()
        args = parser.parse_args(["--server", "http://x:1", "read", "--last", "5"])
        assert args.server == "http://x:1"
        assert args.last == 5

    def test_server_flag_with_join(self):
        parser = build_parser()
        args = parser.parse_args(["--server", "http://x:1", "join", "proj", "--as", "Dev"])
        assert args.server == "http://x:1"
        assert args.project == "proj"
        assert args.job_title == "Dev"


class TestMainSetsOverride:
    """Verify main() sets _server_override from args."""

    def test_main_sets_server_override(self, monkeypatch):
        """When --server is passed, main() sets the module-level override."""
        monkeypatch.setattr(
            "sys.argv", ["coral-board", "--server", "http://remote:9000", "projects"]
        )
        # Mock _api to prevent actual HTTP call
        monkeypatch.setattr(cli_module, "_api", lambda *a, **kw: [])
        main()
        assert cli_module._server_override == "http://remote:9000"

    def test_main_no_server_leaves_override_none(self, monkeypatch):
        """Without --server, _server_override stays None."""
        monkeypatch.setattr("sys.argv", ["coral-board", "projects"])
        monkeypatch.setattr(cli_module, "_api", lambda *a, **kw: [])
        main()
        assert cli_module._server_override is None


class TestResolveServerUsedByApi:
    """Verify _api() and error messages reference _resolve_server()."""

    def test_api_uses_resolve_server_in_url(self, monkeypatch):
        """_api() builds URL from _resolve_server()."""
        monkeypatch.setattr(cli_module, "_server_override", "http://custom:1234")
        captured_urls = []

        def mock_urlopen(req, **kwargs):
            captured_urls.append(req.full_url)

            class FakeResp:
                def read(self):
                    return b"[]"
                def __enter__(self):
                    return self
                def __exit__(self, *a):
                    pass

            return FakeResp()

        monkeypatch.setattr(cli_module, "urlopen", mock_urlopen)
        _result = cli_module._api("GET", "/projects")
        assert captured_urls[0] == "http://custom:1234/api/board/projects"


class TestRemoteJoinRegistration:
    """Verify that joining a remote board registers with local Coral."""

    def test_is_remote_join_localhost(self):
        """localhost is not a remote join."""
        assert cli_module._is_remote_join() is False

    def test_is_remote_join_remote_server(self, monkeypatch):
        """A non-localhost server is a remote join."""
        monkeypatch.setattr(cli_module, "_server_override", "http://remote-host:8420")
        assert cli_module._is_remote_join() is True

    def test_is_remote_join_127(self, monkeypatch):
        """127.0.0.1 is not a remote join."""
        monkeypatch.setattr(cli_module, "_server_override", "http://127.0.0.1:8420")
        assert cli_module._is_remote_join() is False

    def test_join_remote_registers_with_local(self, monkeypatch, tmp_path):
        """When joining a remote board, CLI should POST to local Coral."""
        monkeypatch.setattr(cli_module, "_server_override", "http://remote:9420")

        # Track calls to _api (for the remote join)
        monkeypatch.setattr(cli_module, "_api", lambda *a, **kw: {"session_id": "test-session"})

        # Track calls to _register_remote_with_local
        register_calls = []
        original_register = cli_module._register_remote_with_local

        def mock_register(*args, **kwargs):
            register_calls.append(args)

        monkeypatch.setattr(cli_module, "_register_remote_with_local", mock_register)

        parser = build_parser()
        args = parser.parse_args(["--server", "http://remote:9420", "join", "proj1", "--as", "Dev"])
        cli_module._server_override = "http://remote:9420"
        args.func(args)

        assert len(register_calls) == 1
        assert register_calls[0] == ("test-session", "http://remote:9420", "proj1", "Dev")

    def test_join_local_does_not_register(self, monkeypatch, tmp_path):
        """Joining a local board should NOT register a remote subscription."""
        monkeypatch.setattr(cli_module, "_api", lambda *a, **kw: {"session_id": "test-session"})

        register_calls = []
        monkeypatch.setattr(cli_module, "_register_remote_with_local",
                            lambda *a, **kw: register_calls.append(a))

        parser = build_parser()
        args = parser.parse_args(["join", "proj1", "--as", "Dev"])
        args.func(args)

        assert len(register_calls) == 0

    def test_leave_remote_unregisters(self, monkeypatch, tmp_path):
        """Leaving a remote board should DELETE from local Coral."""
        monkeypatch.setattr(cli_module, "_server_override", "http://remote:9420")

        # Set up state as if joined to a remote board
        cli_module._save_state("proj1", "Dev")

        monkeypatch.setattr(cli_module, "_api", lambda *a, **kw: {"ok": True})

        unregister_calls = []
        monkeypatch.setattr(cli_module, "_unregister_remote_from_local",
                            lambda *a, **kw: unregister_calls.append(a))

        parser = build_parser()
        args = parser.parse_args(["leave"])
        args.func(args)

        assert len(unregister_calls) == 1
        assert unregister_calls[0] == ("test-session",)


# ── Helpers ─────────────────────────────────────────────────────────────────


def _save_state_with_server(tmp_path, server_url: str):
    """Write a state file directly with a given server_url."""
    state = {
        "project": "testproj",
        "job_title": "Dev",
        "session_id": "test-session",
        "server_url": server_url,
    }
    state_file = tmp_path / "board_state_test-session.json"
    state_file.write_text(json.dumps(state))
