"""Tmux pane management — discovery, send-keys, capture, kill, and terminal attach."""

from __future__ import annotations

import asyncio
import os
import platform
import shutil
from typing import Any

from coral.tools.utils import run_cmd

# Bracket paste mode escape sequences (ESC [ 200 ~ and ESC [ 201 ~)
_BRACKET_PASTE_START = ["-H", "1b", "-H", "5b", "-H", "32", "-H", "30", "-H", "30", "-H", "7e"]
_BRACKET_PASTE_END = ["-H", "1b", "-H", "5b", "-H", "32", "-H", "30", "-H", "31", "-H", "7e"]


async def _send_bracket_pasted(target: str, text: str) -> str | None:
    """Send text wrapped in bracket paste sequences. Returns error string or None."""
    rc, _, stderr = await run_cmd("tmux", "send-keys", "-t", target, *_BRACKET_PASTE_START)
    if rc != 0:
        return f"bracket paste start failed (rc={rc}): {stderr}"
    rc, _, stderr = await run_cmd("tmux", "send-keys", "-t", target, "-l", text)
    if rc != 0:
        return f"send-keys failed (rc={rc}): {stderr}"
    rc, _, stderr = await run_cmd("tmux", "send-keys", "-t", target, *_BRACKET_PASTE_END)
    if rc != 0:
        return f"bracket paste end failed (rc={rc}): {stderr}"
    return None


async def list_tmux_sessions() -> list[dict[str, str]]:
    """List all tmux panes with their titles, session names, and targets."""
    try:
        rc, stdout, _ = await run_cmd(
            "tmux", "list-panes", "-a",
            "-F", "#{pane_title}|#{session_name}|#S:#I.#P|#{pane_current_path}",
        )
        if rc != 0:
            return []

        results = []
        for line in stdout.splitlines():
            parts = line.split("|", 3)
            if len(parts) == 4:
                results.append({
                    "pane_title": parts[0],
                    "session_name": parts[1],
                    "target": parts[2],
                    "current_path": parts[3],
                })
        return results
    except (OSError, FileNotFoundError):
        return []


async def _find_pane(
    agent_name: str,
    agent_type: str | None = None,
    session_id: str | None = None,
) -> dict[str, str] | None:
    """Find the tmux pane dict for a given agent.

    When *session_id* is provided, matches by tmux session name containing
    that UUID (new format). Falls back to fuzzy agent_name matching.
    """
    sessions = await list_tmux_sessions()

    # Fast path: match by session_id in tmux session name
    if session_id:
        sid_low = session_id.lower()
        for s in sessions:
            if sid_low in s["session_name"].lower():
                return s

    # Fallback: fuzzy match by agent_name
    agent_low = agent_name.lower()
    norm_name = agent_name.replace("_", "-").lower()
    type_low = agent_type.lower() if agent_type else None

    fallback: dict[str, str] | None = None

    for s in sessions:
        title_low = s["pane_title"].lower()
        session_low = s["session_name"].lower()
        path_low = s.get("current_path", "").lower()
        path_base = os.path.basename(path_low.rstrip("/"))

        name_match = (agent_low in title_low or
                      norm_name in title_low or
                      agent_low in session_low or
                      norm_name in session_low or
                      agent_low == path_base or
                      norm_name == path_base)

        if not name_match:
            continue

        if type_low:
            if type_low in title_low or type_low in session_low:
                return s
            if fallback is None:
                fallback = s
        else:
            return s

    return fallback


async def find_pane_target(
    agent_name: str, agent_type: str | None = None, session_id: str | None = None,
) -> str | None:
    """Find the tmux pane target address for a given agent name."""
    pane = await _find_pane(agent_name, agent_type, session_id=session_id)
    return pane["target"] if pane else None


async def get_session_info(
    agent_name: str, agent_type: str | None = None, session_id: str | None = None,
) -> dict[str, Any] | None:
    """Return enriched metadata for a live session (used by the Info modal)."""
    from coral.tools.session_manager import get_agent_log_path

    pane = await _find_pane(agent_name, agent_type, session_id=session_id)
    if not pane:
        return None

    log_path = get_agent_log_path(agent_name, agent_type, session_id=session_id)
    return {
        "agent_name": agent_name,
        "agent_type": agent_type or "claude",
        "session_id": session_id,
        "tmux_session_name": pane["session_name"],
        "tmux_target": pane["target"],
        "tmux_command": f"tmux attach -t {pane['session_name']}",
        "working_directory": pane.get("current_path", ""),
        "log_path": str(log_path) if log_path else None,
        "pane_title": pane.get("pane_title", ""),
    }


async def send_to_tmux(
    agent_name: str, command: str, agent_type: str | None = None, session_id: str | None = None,
) -> str | None:
    """Send a command to the tmux pane for the given agent. Returns error string or None."""
    target = await find_pane_target(agent_name, agent_type, session_id=session_id)
    if not target:
        return f"Pane '{agent_name}' not found in any tmux session"

    try:
        # Multi-line: wrap in bracket paste sequences
        if "\n" in command:
            err = await _send_bracket_pasted(target, command)
            if err:
                return err
        else:
            # Single-line: send as literal text
            rc, _, stderr = await run_cmd(
                "tmux", "send-keys", "-t", target, "-l", command,
            )
            if rc != 0:
                return f"send-keys failed (rc={rc}): {stderr}"

        # Pause to let tmux deliver keystrokes to the pane
        await asyncio.sleep(0.3)

        # Send Enter
        rc, _, stderr = await run_cmd(
            "tmux", "send-keys", "-t", target, "Enter"
        )
        if rc != 0:
            return f"send Enter failed (rc={rc}): {stderr}"

        return None
    except Exception as e:
        return str(e)


async def send_raw_keys(
    agent_name: str, keys: list[str], agent_type: str | None = None, session_id: str | None = None,
) -> str | None:
    """Send raw tmux key names (e.g. BTab, Escape) to a pane. Returns error string or None."""
    target = await find_pane_target(agent_name, agent_type, session_id=session_id)
    if not target:
        return f"Pane '{agent_name}' not found in any tmux session"

    try:
        for key in keys:
            rc, _, stderr = await run_cmd(
                "tmux", "send-keys", "-t", target, key
            )
            if rc != 0:
                return f"send-keys '{key}' failed (rc={rc}): {stderr}"
            await asyncio.sleep(0.1)
        return None
    except Exception as e:
        return str(e)


async def send_terminal_input(
    agent_name: str, data: str, agent_type: str | None = None, session_id: str | None = None,
) -> str | None:
    """Send raw terminal input data to a tmux pane (resolves target automatically)."""
    target = await find_pane_target(agent_name, agent_type, session_id=session_id)
    if not target:
        return f"Pane '{agent_name}' not found in any tmux session"
    return await send_terminal_input_to_target(target, data)


async def send_terminal_input_to_target(target: str, data: str) -> str | None:
    """Send raw terminal input data to a resolved tmux pane target.

    Handles both literal text and control sequences from xterm.js onData.
    Returns error string or None.
    """
    try:
        # Map common control characters to tmux key names
        _CTRL_MAP = {
            "\r": "Enter",
            "\x7f": "BSpace",
            "\x1b": "Escape",
            "\t": "Tab",
        }

        # Check for single control character
        if len(data) == 1 and data in _CTRL_MAP:
            rc, _, stderr = await run_cmd(
                "tmux", "send-keys", "-t", target, _CTRL_MAP[data],
            )
            if rc != 0:
                return f"send-keys failed (rc={rc}): {stderr}"
            return None

        # Check for Ctrl+<letter> (0x01-0x1a)
        if len(data) == 1 and 1 <= ord(data) <= 26:
            key_name = f"C-{chr(ord(data) + 96)}"  # 0x01 -> C-a, etc.
            rc, _, stderr = await run_cmd(
                "tmux", "send-keys", "-t", target, key_name,
            )
            if rc != 0:
                return f"send-keys failed (rc={rc}): {stderr}"
            return None

        # Escape sequences (arrow keys, function keys, etc.)
        if data.startswith("\x1b"):
            # Send as hex bytes so tmux passes them through verbatim
            hex_args = []
            for byte in data.encode("utf-8"):
                hex_args.extend(["-H", f"{byte:02x}"])
            rc, _, stderr = await run_cmd(
                "tmux", "send-keys", "-t", target, *hex_args,
            )
            if rc != 0:
                return f"send-keys hex failed (rc={rc}): {stderr}"
            return None

        # Multi-line text: wrap in bracket paste sequences so the terminal
        # treats it as pasted content rather than executing each newline.
        if "\n" in data or "\r\n" in data:
            return await _send_bracket_pasted(target, data)

        # Single-line literal text
        rc, _, stderr = await run_cmd(
            "tmux", "send-keys", "-t", target, "-l", data,
        )
        if rc != 0:
            return f"send-keys -l failed (rc={rc}): {stderr}"
        return None
    except Exception as e:
        return str(e)


async def capture_pane(
    agent_name: str, lines: int = 200, agent_type: str | None = None, session_id: str | None = None,
) -> str | None:
    """Capture the current content of a tmux pane. Returns text or None on error."""
    target = await find_pane_target(agent_name, agent_type, session_id=session_id)
    if not target:
        return None

    try:
        rc, stdout, _ = await run_cmd(
            "tmux", "capture-pane", "-t", target, "-p", f"-S-{lines}"
        )
        if rc != 0:
            return None
        return stdout
    except (OSError, FileNotFoundError):
        return None


async def capture_pane_raw(
    agent_name: str, lines: int = 200, agent_type: str | None = None, session_id: str | None = None,
) -> str | None:
    """Capture pane content WITH ANSI escape sequences preserved."""
    target = await find_pane_target(agent_name, agent_type, session_id=session_id)
    if not target:
        return None
    return await capture_pane_raw_target(target, lines)


async def capture_pane_raw_target(target: str, lines: int = 200, visible_only: bool = False) -> str | None:
    """Capture pane content by target address (skips pane lookup).

    If visible_only is True, capture only the visible viewport (no scrollback).
    This is needed for TUI apps like vim that control the full screen.
    """
    try:
        cmd = ["tmux", "capture-pane", "-t", target, "-p", "-e"]
        if not visible_only:
            cmd.append(f"-S-{lines}")
        rc, stdout, _ = await run_cmd(*cmd)
        if rc != 0:
            return None
        return stdout
    except (OSError, FileNotFoundError):
        return None


async def kill_session(
    agent_name: str, agent_type: str | None = None, session_id: str | None = None,
) -> str | None:
    """Kill the tmux session for a given agent and remove its log file.

    Returns error string or None.
    """
    from coral.tools.session_manager import get_agent_log_path

    pane = await _find_pane(agent_name, agent_type, session_id=session_id)
    if not pane:
        return f"Pane '{agent_name}' not found in any tmux session"

    session_name = pane["session_name"]
    try:
        rc, _, stderr = await run_cmd(
            "tmux", "kill-session", "-t", session_name
        )
        if rc != 0:
            return f"kill-session failed: {stderr}"

        # Remove the log file so the agent disappears from discover_coral_agents
        log_path = get_agent_log_path(agent_name, agent_type, session_id=session_id)
        if log_path:
            try:
                log_path.unlink()
            except OSError:
                pass

        # Clean up settings temp file written by build_launch_command
        if session_id:
            from pathlib import Path
            settings_file = Path(f"/tmp/coral_settings_{session_id}.json")
            try:
                settings_file.unlink(missing_ok=True)
            except OSError:
                pass

        # Unregister from persistent live sessions
        if session_id:
            try:
                from coral.store.registry import get_store
                _store = get_store()
                await _store.unregister_live_session(session_id)
            except Exception:
                pass  # Non-critical

        return None
    except Exception as e:
        return str(e)


async def resize_pane(
    agent_name: str, columns: int, agent_type: str | None = None, session_id: str | None = None,
) -> str | None:
    """Resize the tmux pane width to the given number of columns. Returns error string or None."""
    target = await find_pane_target(agent_name, agent_type, session_id=session_id)
    if not target:
        return f"Pane '{agent_name}' not found in any tmux session"

    try:
        rc, _, stderr = await run_cmd(
            "tmux", "resize-window", "-t", target, "-x", str(columns)
        )
        if rc != 0:
            return f"resize-window failed (rc={rc}): {stderr}"
        return None
    except Exception as e:
        return str(e)


async def resize_pane_target(target: str, columns: int) -> str | None:
    """Resize the tmux pane width by target address (skips pane lookup). Returns error string or None."""
    try:
        rc, _, stderr = await run_cmd(
            "tmux", "resize-window", "-t", target, "-x", str(columns)
        )
        if rc != 0:
            return f"resize-window failed (rc={rc}): {stderr}"
        return None
    except Exception as e:
        return str(e)


async def open_terminal_attached(
    agent_name: str, agent_type: str | None = None, session_id: str | None = None,
) -> str | None:
    """Open a local terminal window attached to the agent's tmux session.

    Returns an error string on failure, or None on success.
    Uses osascript on macOS, or falls back to common terminal emulators on Linux.
    """
    pane = await _find_pane(agent_name, agent_type, session_id=session_id)
    if not pane:
        return f"Pane '{agent_name}' not found in any tmux session"

    session_name = pane["session_name"]
    attach_cmd = f"tmux attach -t {session_name}"

    try:
        if platform.system() == "Darwin":
            # macOS: use osascript to open Terminal.app
            script = (
                f'tell application "Terminal"\n'
                f'    activate\n'
                f'    do script "{attach_cmd}"\n'
                f'end tell'
            )
            rc, _, stderr = await run_cmd(
                "osascript", "-e", script
            )
            if rc != 0:
                return f"osascript failed: {stderr}"
        else:
            # Linux: try common terminal emulators
            for term in ("gnome-terminal", "xfce4-terminal", "konsole", "xterm"):
                if shutil.which(term):
                    if term == "gnome-terminal":
                        args = [term, "--", "bash", "-c", attach_cmd]
                    elif term == "konsole":
                        args = [term, "-e", "bash", "-c", attach_cmd]
                    else:
                        args = [term, "-e", attach_cmd]
                    asyncio.create_task(run_cmd(*args))
                    # Don't wait — terminal runs independently
                    return None
            return "No supported terminal emulator found"

        return None
    except Exception as e:
        return str(e)


