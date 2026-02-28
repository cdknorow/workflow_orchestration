"""Generic utilities and configuration for Corral."""

from __future__ import annotations

import asyncio
import json
import os
import subprocess
from pathlib import Path
from typing import Tuple

# Configuration Constants
LOG_DIR = os.environ.get("TMPDIR", "/tmp").rstrip("/")
LOG_PATTERN = f"{LOG_DIR}/*_corral_*.log"

HISTORY_PATH = Path(os.environ.get("CLAUDE_PROJECTS_DIR", Path.home() / ".claude" / "projects"))
GEMINI_HISTORY_BASE = Path(os.environ.get("GEMINI_TMP_DIR", Path.home() / ".gemini" / "tmp"))


def _hook_entry_exists(matcher_groups: list, command: str) -> bool:
    """Check if a hook command already exists in a list of matcher groups."""
    for group in matcher_groups:
        for hook in group.get("hooks", []):
            if hook.get("command") == command:
                return True
    return False


def install_hooks(target_dir: Path | str | None = None):
    """Ensure Claude hooks for Corral are installed in settings.local.json.

    When *target_dir* is provided, hooks are written to
    ``<target_dir>/.claude/settings.local.json`` (per-worktree).
    When omitted, falls back to ``~/.claude/settings.local.json`` (global).

    Merges corral hooks into existing user hooks without overriding them.
    Skips any hook that is already present (matched by command name).
    """
    if target_dir is not None:
        target = Path(target_dir)
        # Only install into git worktree roots (.git file or directory)
        if not (target / ".git").exists():
            return
        settings_path = target / ".claude" / "settings.local.json"
    else:
        settings_path = Path.home() / ".claude" / "settings.local.json"
    settings_path.parent.mkdir(parents=True, exist_ok=True)

    if settings_path.exists():
        settings = json.loads(settings_path.read_text())
    else:
        settings = {}

    # Clean up any old-format keys from previous versions
    for old_key in ("agenticStateHook", "taskStateHook"):
        settings.pop(old_key, None)

    hooks = settings.setdefault("hooks", {})

    # Hooks we want to ensure exist: (event, matcher_group_to_add)
    desired = [
        ("PostToolUse", {
            "matcher": "TaskCreate|TaskUpdate",
            "hooks": [{"type": "command", "command": "corral-hook-task-sync"}],
        }),
        ("PostToolUse", {
            "hooks": [{"type": "command", "command": "corral-hook-agentic-state"}],
        }),
        ("Stop", {
            "hooks": [{"type": "command", "command": "corral-hook-agentic-state"}],
        }),
        ("Notification", {
            "hooks": [{"type": "command", "command": "corral-hook-agentic-state"}],
        }),
    ]

    modified = False
    for event, group in desired:
        event_list = hooks.setdefault(event, [])
        command = group["hooks"][0]["command"]
        if not _hook_entry_exists(event_list, command):
            event_list.append(group)
            modified = True

    if modified:
        settings_path.write_text(json.dumps(settings, indent=2) + "\n")


async def run_cmd(*args: str, timeout: float | None = None) -> Tuple[int, str, str]:
    """Execute a subprocess command asynchronously.

    Returns:
        Tuple of (returncode, stdout, stderr).
    """
    try:
        proc = await asyncio.create_subprocess_exec(
            *args,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
        )
        
        if timeout is not None:
            stdout, stderr = await asyncio.wait_for(proc.communicate(), timeout=timeout)
        else:
            stdout, stderr = await proc.communicate()
            
        return proc.returncode or 0, stdout.decode().strip(), stderr.decode().strip()
    except asyncio.TimeoutError:
        # If timeout, try to terminate the process
        if proc:
            try:
                proc.terminate()
                await asyncio.wait_for(proc.wait(), timeout=1.0)
            except Exception:
                pass
        return -1, "", "Command timed out"
    except Exception as e:
        return -1, "", str(e)
