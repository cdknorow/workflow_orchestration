"""Background poller that queries git state for live corral agents."""

from __future__ import annotations

import asyncio
import logging
import os
from pathlib import Path
from typing import Any

from corral.tools.session_manager import discover_corral_agents
from corral.tools.tmux_manager import _find_pane
from corral.store import CorralStore
from corral.tools.utils import run_cmd, HISTORY_PATH

log = logging.getLogger(__name__)


class GitPoller:
    """Periodically polls git branch/commit info for live agents and stores snapshots."""

    def __init__(self, store: CorralStore) -> None:
        self._store = store

    async def run_forever(self, interval: float = 30) -> None:
        while True:
            try:
                await self.poll_once()
            except Exception:
                log.exception("GitPoller error")
            await asyncio.sleep(interval)

    async def poll_once(self) -> dict[str, int]:
        agents = await discover_corral_agents()
        polled = 0

        # Group agents by working directory so we query git once per directory
        # but store a snapshot for every session in that directory.
        dir_to_agents: dict[str, list[dict[str, Any]]] = {}
        for agent in agents:
            session_id = agent.get("session_id")
            pane = await _find_pane(
                agent["agent_name"], agent["agent_type"], session_id=session_id,
            )
            if not pane:
                continue
            workdir = pane.get("current_path", "")
            if not workdir:
                continue
            dir_to_agents.setdefault(workdir, []).append(agent)

        for workdir, dir_agents in dir_to_agents.items():
            try:
                git_info = await self._query_git(workdir)
                if not git_info:
                    continue
                changed_files = await self._query_changed_files(workdir)
                # Store a snapshot for each session in this directory
                for agent in dir_agents:
                    await self._store.upsert_git_snapshot(
                        agent_name=agent["agent_name"],
                        agent_type=agent["agent_type"],
                        working_directory=workdir,
                        branch=git_info["branch"],
                        commit_hash=git_info["commit_hash"],
                        commit_subject=git_info["commit_subject"],
                        commit_timestamp=git_info["commit_timestamp"],
                        session_id=agent.get("session_id"),
                        remote_url=git_info.get("remote_url"),
                    )
                    await self._store.replace_changed_files(
                        agent_name=agent["agent_name"],
                        working_directory=workdir,
                        files=changed_files,
                        session_id=agent.get("session_id"),
                    )
                    polled += 1
            except Exception:
                log.exception("GitPoller error for dir %s", workdir)
        return {"polled": polled}

    async def _query_git(self, workdir: str) -> dict[str, str] | None:
        """Query git for current branch and latest commit in a working directory."""
        try:
            # Get branch name
            rc, stdout, _ = await run_cmd(
                "git", "-C", workdir, "rev-parse", "--abbrev-ref", "HEAD", timeout=5.0
            )
            if rc != 0:
                return None
            branch = stdout

            # Get latest commit: hash|subject|timestamp
            rc, stdout, _ = await run_cmd(
                "git", "-C", workdir, "log", "-1", "--format=%H|%s|%aI", timeout=5.0
            )
            if rc != 0:
                return None
            parts = stdout.split("|", 2)
            if len(parts) < 3:
                return None

            # Get remote URL (best-effort)
            remote_url = None
            try:
                rc, stdout, _ = await run_cmd(
                    "git", "-C", workdir, "remote", "get-url", "origin", timeout=5.0
                )
                if rc == 0:
                    remote_url = stdout or None
            except Exception:
                pass

            return {
                "branch": branch,
                "commit_hash": parts[0],
                "commit_subject": parts[1],
                "commit_timestamp": parts[2],
                "remote_url": remote_url,
            }
        except (asyncio.TimeoutError, OSError, FileNotFoundError):
            return None

    async def _get_diff_base(self, workdir: str) -> str:
        """Return the base ref to diff against.

        On a feature branch: merge-base with main/master (shows all branch work).
        On the default branch (or merge-base fails): HEAD (shows changes since last commit).
        """
        # Detect current branch
        rc, branch, _ = await run_cmd(
            "git", "-C", workdir, "rev-parse", "--abbrev-ref", "HEAD", timeout=5.0,
        )
        current_branch = branch.strip() if rc == 0 else ""

        # If we're not on a default branch, try to find the merge-base
        if current_branch not in ("main", "master", "HEAD", ""):
            for base_branch in ("main", "master"):
                rc, stdout, _ = await run_cmd(
                    "git", "-C", workdir, "merge-base", base_branch, "HEAD", timeout=5.0,
                )
                if rc == 0 and stdout:
                    return stdout.strip()

        # On the default branch or merge-base unavailable — diff against HEAD
        # (shows only uncommitted changes since the last commit)
        return "HEAD"

    async def _get_base_timestamp(self, workdir: str, base_ref: str) -> float:
        """Get the unix timestamp of the base ref commit.

        Used to filter out untracked files that existed before the base point.
        """
        rc, stdout, _ = await run_cmd(
            "git", "-C", workdir, "log", "-1", "--format=%ct", base_ref, timeout=5.0,
        )
        if rc == 0 and stdout:
            try:
                return float(stdout.strip())
            except ValueError:
                pass
        return 0.0

    async def _query_changed_files(self, workdir: str) -> list[dict[str, Any]]:
        """Query git for files changed relative to a smart base ref.

        On a feature branch: shows all changes since the branch diverged from main.
        On main/master: shows only changes since the last commit.
        Untracked files are only included if they were created after the base
        commit timestamp, so pre-existing untracked files don't pollute the list.
        """
        file_map: dict[str, dict[str, Any]] = {}

        try:
            base = await self._get_diff_base(workdir)
            base_ts = await self._get_base_timestamp(workdir, base)

            # Diff from base to working tree — captures committed (on branch) +
            # staged + unstaged changes in one shot.
            rc, stdout, _ = await run_cmd(
                "git", "-C", workdir, "diff", base, "--numstat", timeout=5.0,
            )
            if rc == 0 and stdout:
                for line in stdout.strip().split("\n"):
                    if not line.strip():
                        continue
                    parts = line.split("\t", 2)
                    if len(parts) < 3:
                        continue
                    added, removed, filepath = parts
                    a = int(added) if added != "-" else 0
                    d = int(removed) if removed != "-" else 0
                    file_map[filepath] = {"filepath": filepath, "additions": a, "deletions": d, "status": "M"}

            # Untracked files don't appear in git diff, so pick up '??' entries
            # from git status — but only if they were created after the base commit.
            rc, stdout, _ = await run_cmd(
                "git", "-C", workdir, "status", "--porcelain", "--untracked-files=all", timeout=5.0,
            )
            if rc == 0 and stdout:
                for line in stdout.strip().split("\n"):
                    if len(line) < 4:
                        continue
                    status_code = line[:2].strip()
                    filepath = line[3:]
                    if " -> " in filepath:
                        filepath = filepath.split(" -> ", 1)[1]
                    if status_code == "??":
                        if filepath not in file_map:
                            # Only include untracked files created after the base commit
                            full_path = os.path.join(workdir, filepath)
                            try:
                                mtime = os.path.getmtime(full_path)
                            except OSError:
                                continue
                            if mtime < base_ts:
                                continue
                            file_map[filepath] = {"filepath": filepath, "additions": 0, "deletions": 0, "status": "??"}
                    elif filepath in file_map:
                        file_map[filepath]["status"] = status_code

        except (asyncio.TimeoutError, OSError, FileNotFoundError):
            pass

        return list(file_map.values())
