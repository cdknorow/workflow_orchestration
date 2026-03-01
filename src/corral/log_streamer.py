"""Async log file tailing for WebSocket streaming."""

from __future__ import annotations

import asyncio
import re
import time
from pathlib import Path
from typing import Any, AsyncGenerator

from corral.session_manager import (
    STATUS_RE,
    SUMMARY_RE,
    strip_ansi,
    clean_match,
    _rejoin_pulse_lines,
)

# Lines that are purely TUI chrome / noise after ANSI stripping
_BOX_LINE_RE = re.compile(r"^[\s─━═╌╍┄┅┈┉╴╶╸╺─]+$")
_SPINNER_RE = re.compile(
    r"^[\s✶✷✸✹✺✻✼✽✾✿⏺⏵⏴⏹⏏⚡●○◉◎◌◐◑◒◓▪▫▸▹►▻\u2800-\u28FF·•]*$"
)
_STATUS_BAR_RE = re.compile(
    r"(worktree:|branch:|model:|ctx:|in:\d|out:\d|cache:\d|shift\+tab|accept edits)"
)
_PROMPT_RE = re.compile(r"^\s*[❯›>$#%]\s*$")

# OSC title sequence fragments that survive ANSI stripping
# (e.g., "0;⠐ Real-time Output Streaming" from split \x1b]0;...\x07)
_OSC_TITLE_RE = re.compile(r"^\d+;")

# Bare numbers with optional decorators (progress counters / step numbers)
# Matches: "2", "·  3", "  5 ", "· 12"
_BARE_NUMBER_RE = re.compile(r"^[·•.\s]*\d+[·•.\s]*$")

# Known TUI chrome / status labels that leak from terminal UI
_TUI_NOISE_RE = re.compile(
    r"Real-time Output Streaming|Streaming response",
    re.IGNORECASE,
)


def _is_noise_line(line: str) -> bool:
    """Return True if a line is TUI rendering noise that should be filtered."""
    stripped = line.strip()

    # Empty or whitespace-only
    if not stripped:
        return True

    # Box-drawing / horizontal rules
    if _BOX_LINE_RE.match(stripped):
        return True

    # Spinner-only lines
    if _SPINNER_RE.match(stripped):
        return True

    # Status bar fragments
    if _STATUS_BAR_RE.search(stripped):
        return True

    # Bare prompt characters
    if _PROMPT_RE.match(stripped):
        return True

    # Very short lines that are just punctuation/symbols (single stray chars)
    if len(stripped) <= 2 and not stripped.isalnum():
        return True

    # OSC title sequence fragments (e.g., "0;⠐ Real-time Output Streaming")
    if _OSC_TITLE_RE.match(stripped):
        return True

    # Bare numbers / progress counters (e.g., "2", "·  3")
    if _BARE_NUMBER_RE.match(stripped):
        return True

    # Known TUI chrome text that leaks from terminal title / status bar
    if _TUI_NOISE_RE.search(stripped):
        return True

    return False


def get_log_snapshot(log_path: str | Path, max_lines: int = 200, chunk_size: int = 8192) -> dict[str, Any]:
    """Return a snapshot of the current log state, reading backwards for efficiency.

    Returns dict with: status, summary, recent_lines, staleness_seconds.
    """
    log_path = Path(log_path)
    result: dict[str, Any] = {
        "status": None,
        "summary": None,
        "recent_lines": [],
        "staleness_seconds": None,
    }

    if not log_path.exists():
        return result

    try:
        result["staleness_seconds"] = time.time() - log_path.stat().st_mtime

        with open(log_path, "rb") as f:
            f.seek(0, 2)
            file_size = f.tell()
            pos = file_size
            lines = []
            leftover = b""

            max_chunks = 1000  # Up to ~8MB backwards
            chunks_read = 0

            # Read backwards in chunks
            while pos > 0 and (len(lines) < max_lines or result["status"] is None or result["summary"] is None):
                if chunks_read >= max_chunks:
                    break

                read_size = min(chunk_size, pos)
                pos -= read_size
                f.seek(pos)
                chunk = f.read(read_size) + leftover

                parts = chunk.split(b"\n")
                # The first part might be incomplete, carry it over to the next loop
                if pos > 0:
                    leftover = parts.pop(0)

                # Decode, strip ANSI, and rejoin split PULSE tags
                clean_parts = []
                for p in parts:
                    try:
                        clean_parts.append(strip_ansi(p.decode("utf-8", errors="replace")))
                    except Exception:
                        clean_parts.append("")
                clean_parts = _rejoin_pulse_lines(clean_parts)

                for clean_line in reversed(clean_parts):
                    need_status = result["status"] is None
                    need_summary = result["summary"] is None
                    need_lines = len(lines) < max_lines

                    if not (need_status or need_summary or need_lines):
                        break

                    if need_status:
                        status_matches = STATUS_RE.findall(clean_line)
                        if status_matches:
                            result["status"] = clean_match(status_matches[-1])

                    if need_summary:
                        summary_matches = SUMMARY_RE.findall(clean_line)
                        if summary_matches:
                            result["summary"] = clean_match(summary_matches[-1])

                    if need_lines and not _is_noise_line(clean_line):
                        lines.insert(0, clean_line)
                
                chunks_read += 1
                
            # Fallback for summary: if not found in the tail, it might be at the very top
            if result["summary"] is None:
                f.seek(0)
                head_chunk = f.read(16384).decode("utf-8", errors="replace")
                head_matches = SUMMARY_RE.findall(strip_ansi(head_chunk))
                if head_matches:
                    result["summary"] = clean_match(head_matches[-1])
                        
            result["recent_lines"] = lines
    except OSError:
        pass

    return result
