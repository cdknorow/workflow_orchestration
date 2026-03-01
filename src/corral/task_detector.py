"""Scan agent log files for ||PULSE:*|| events and store as activities."""

from __future__ import annotations

import re
from pathlib import Path

from corral.session_manager import strip_ansi, clean_match

# Only match known PULSE event types to avoid matching protocol documentation examples
KNOWN_PULSE_TYPES = ("STATUS", "SUMMARY", "CONFIDENCE")
PULSE_EVENT_RE = re.compile(r"^[\s●⏺]*\|\|PULSE:(" + "|".join(KNOWN_PULSE_TYPES) + r")\s+(.*?)\|\|", re.MULTILINE)

# Track file positions to avoid re-scanning the same content
_file_positions: dict[str, int] = {}


async def scan_log_for_pulse_events(
    store, agent_name: str, log_path: str, session_id: str | None = None,
) -> None:
    """Scan new content in a log file for all PULSE events.

    - All pulse events are stored as activities in agent_events.
    - STATUS and SUMMARY are skipped here (handled by _track_status_summary_events
      in web_server.py which deduplicates on change).
    - *session_id* is passed from discovery (no DB lookup needed).
    """
    path = Path(log_path)
    if not path.exists():
        return

    try:
        file_size = path.stat().st_size
    except OSError:
        return

    last_pos = _file_positions.get(log_path, 0)
    if file_size <= last_pos:
        if file_size < last_pos:
            # File was truncated (e.g. restart), reset
            _file_positions[log_path] = 0
            last_pos = 0
        else:
            return

    try:
        with open(path, "r", errors="replace") as f:
            f.seek(last_pos)
            new_content = f.read()
            _file_positions[log_path] = f.tell()
    except OSError:
        return

    clean = strip_ansi(new_content)

    # Process all pulse events
    for match in PULSE_EVENT_RE.finditer(clean):
        event_type = match.group(1)
        payload = clean_match(match.group(2))
        if not payload:
            continue

        # STATUS and SUMMARY are tracked with deduplication in web_server.py
        if event_type in ("STATUS", "SUMMARY"):
            continue

        # Store as activity
        await store.insert_agent_event(
            agent_name, event_type.lower(), payload, session_id=session_id,
        )
