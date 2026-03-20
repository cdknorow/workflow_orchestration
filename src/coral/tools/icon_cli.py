"""CLI for setting agent emoji icons.

Usage:
    coral-agent-icon set <emoji>
    coral-agent-icon clear

Session name is resolved from the tmux session name, with hostname as fallback.
Server URL is resolved from CORAL_URL env var or defaults to http://localhost:8420.
"""

from __future__ import annotations

import json
import os
import platform
import subprocess
import sys
from urllib.error import HTTPError, URLError
from urllib.parse import quote as urlquote
from urllib.request import Request, urlopen


def _session_name() -> str:
    """Resolve session name from tmux session name, with hostname fallback.

    Tmux session names have the format 'claude-<uuid>' or 'gemini-<uuid>'.
    The DB stores just the UUID as session_id, so strip the agent prefix.
    """
    if os.environ.get("TMUX"):
        try:
            result = subprocess.run(
                ["tmux", "display-message", "-p", "#S"],
                capture_output=True, text=True, timeout=5,
            )
            name = result.stdout.strip()
            if name:
                # Strip agent type prefix (e.g. 'claude-', 'gemini-') to get the UUID
                for prefix in ("claude-", "gemini-"):
                    if name.startswith(prefix):
                        return name[len(prefix):]
                return name
        except (FileNotFoundError, subprocess.TimeoutExpired):
            pass
    return platform.node()


def _resolve_server() -> str:
    """Resolve the Coral server URL."""
    url = os.environ.get("CORAL_URL", "http://localhost:8420")
    return url.rstrip("/")


def _api_put(url: str, data: dict) -> dict:
    """Make a PUT request to the Coral API."""
    body = json.dumps(data).encode()
    req = Request(url, data=body, method="PUT")
    req.add_header("Content-Type", "application/json")
    try:
        with urlopen(req, timeout=10) as resp:
            return json.loads(resp.read())
    except HTTPError as e:
        try:
            err_body = json.loads(e.read())
            return {"error": err_body.get("detail", str(e))}
        except Exception:
            return {"error": str(e)}
    except URLError as e:
        return {"error": f"Cannot connect to Coral server: {e.reason}"}


def main() -> None:
    if len(sys.argv) < 2 or sys.argv[1] in ("-h", "--help"):
        print("Usage:")
        print("  coral-agent-icon set <emoji>   Set your agent icon")
        print("  coral-agent-icon clear          Clear your agent icon")
        sys.exit(0)

    command = sys.argv[1]
    session = _session_name()
    server = _resolve_server()
    url = f"{server}/api/sessions/live/{urlquote(session, safe='')}/icon"

    if command == "set":
        if len(sys.argv) < 3:
            print("Error: emoji argument required", file=sys.stderr)
            print("Usage: coral-agent-icon set <emoji>", file=sys.stderr)
            sys.exit(1)
        icon = sys.argv[2]
        result = _api_put(url, {"session_id": session, "icon": icon})
        if result.get("error"):
            print(f"Error: {result['error']}", file=sys.stderr)
            sys.exit(1)
        print(f"Icon set to {icon}")

    elif command == "clear":
        result = _api_put(url, {"session_id": session, "icon": ""})
        if result.get("error"):
            print(f"Error: {result['error']}", file=sys.stderr)
            sys.exit(1)
        print("Icon cleared")

    else:
        print(f"Unknown command: {command}", file=sys.stderr)
        print("Usage: coral-agent-icon set <emoji> | coral-agent-icon clear", file=sys.stderr)
        sys.exit(1)


if __name__ == "__main__":
    main()
