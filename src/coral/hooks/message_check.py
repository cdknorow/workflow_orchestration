"""PostToolUse hook that checks for unread message board messages.

Runs after each tool use. If there are unread messages, prints a
notification so the agent sees it naturally during its workflow.
Does NOT advance the read cursor — the agent must run
``coral-board read`` to consume the messages.
"""

import json
import os
import sys

from coral.hooks.utils import coral_api, debug_log


def _load_board_state() -> dict | None:
    """Load the agent's active message board state."""
    if os.environ.get("TMUX"):
        import subprocess
        try:
            result = subprocess.run(
                ["tmux", "display-message", "-p", "#S"],
                capture_output=True, text=True, timeout=2,
            )
            session_name = result.stdout.strip()
        except Exception:
            session_name = ""
    else:
        import platform
        session_name = platform.node()

    if not session_name:
        return None

    safe_name = session_name.replace("/", "_").replace("\\", "_")
    state_path = os.path.join(os.path.expanduser("~"), ".coral", f"board_state_{safe_name}.json")
    try:
        with open(state_path) as f:
            return json.load(f)
    except (OSError, json.JSONDecodeError):
        return None


def main():
    """Check for unread messages and print a notification if any exist."""
    try:
        # Read and discard stdin (hook protocol requires it)
        sys.stdin.read()

        state = _load_board_state()
        if not state:
            return

        project = state.get("project")
        session_id = state.get("session_id")
        if not project or not session_id:
            return

        # Server resolution: state file > CORAL_URL env > localhost fallback
        base = (
            state.get("server_url")
            or os.environ.get("CORAL_URL")
            or f"http://localhost:{os.environ.get('CORAL_PORT', '8420')}"
        ).rstrip("/")

        result = coral_api(base, "GET", f"/api/board/{project}/messages/check?session_id={session_id}")
        if not result:
            return

        count = result.get("unread", 0)
        debug_log(f"message_check: project={project} unread={count}")

        if count > 0:
            plural = "s" if count != 1 else ""
            print(f"\n📬 You have {count} unread message{plural} on the board. Run 'coral-board read' to see them.\n")

    except Exception:
        pass  # Never block the agent


if __name__ == "__main__":
    main()
