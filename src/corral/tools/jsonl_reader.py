"""Generic incremental JSONL reader for live agent session transcripts.

The reader is agent-agnostic: it resolves the correct agent implementation
at read time based on the ``agent_type`` parameter. Each agent implements
``resolve_transcript_path`` and ``parse_transcript_entry`` (see
``agents/base.py`` for the interface and expected return formats).
"""

from __future__ import annotations

import json
import logging
from dataclasses import dataclass, field
from pathlib import Path
from typing import Any

from corral.agents import get_agent

log = logging.getLogger(__name__)


@dataclass
class _SessionCache:
    path: Path | None = None
    offset: int = 0
    messages: list[dict[str, Any]] = field(default_factory=list)
    # Map tool_use_id → tool_name for labeling results (populated by agent)
    tool_use_names: dict[str, str] = field(default_factory=dict)


class JsonlSessionReader:
    """Incrementally reads JSONL session files for live chat display."""

    def __init__(self) -> None:
        self._cache: dict[str, _SessionCache] = {}

    def read_new_messages(
        self, session_id: str, working_directory: str = "", agent_type: str = "claude"
    ) -> tuple[list[dict[str, Any]], int]:
        """Read new messages since last call.

        Parameters
        ----------
        session_id : str
            The session to read.
        working_directory : str
            Optional hint for fast transcript file lookup.
        agent_type : str
            Agent type used to select the correct parser (default: ``"claude"``).

        Returns (new_messages, total_count).
        """
        agent = get_agent(agent_type)

        cache = self._cache.get(session_id)
        if cache is None:
            cache = _SessionCache()
            self._cache[session_id] = cache

        # Resolve path on first call or if not found yet
        if cache.path is None:
            cache.path = agent.resolve_transcript_path(session_id, working_directory)
            if cache.path is None:
                return [], 0

        # Read new data from file
        try:
            with open(cache.path, "r", errors="replace") as f:
                f.seek(cache.offset)
                new_data = f.read()
                cache.offset = f.tell()
        except OSError:
            return [], len(cache.messages)

        if not new_data:
            return [], len(cache.messages)

        # Parse new lines
        new_messages: list[dict[str, Any]] = []
        for line in new_data.splitlines():
            line = line.strip()
            if not line:
                continue
            try:
                entry = json.loads(line)
            except json.JSONDecodeError:
                continue
            parsed = agent.parse_transcript_entry(entry, cache.tool_use_names)
            if parsed is None:
                continue
            items = parsed if isinstance(parsed, list) else [parsed]
            for item in items:
                new_messages.append(item)

        cache.messages.extend(new_messages)
        return new_messages, len(cache.messages)

    def clear_session(self, session_id: str) -> None:
        """Remove cached state for a session (e.g. on restart)."""
        self._cache.pop(session_id, None)
