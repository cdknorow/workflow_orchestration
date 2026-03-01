"""Tests for PULSE protocol tag parsing when lines are split by terminal wrapping.

tmux pipe-pane captures output with hard line wraps at the terminal width,
which can split a single ||PULSE:...|| tag across multiple log lines.
These tests verify that _rejoin_pulse_lines, get_log_status, get_log_snapshot,
and PULSE_EVENT_RE all handle wrapped tags correctly.
"""

import os
import tempfile

import pytest

from corral.session_manager import (
    _rejoin_pulse_lines,
    get_log_status,
    STATUS_RE,
    SUMMARY_RE,
    clean_match,
)
from corral.log_streamer import get_log_snapshot
from corral.task_detector import PULSE_EVENT_RE


# ---------------------------------------------------------------------------
# _rejoin_pulse_lines unit tests
# ---------------------------------------------------------------------------

class TestRejoinPulseLines:

    def test_complete_tags_unchanged(self):
        lines = [
            "normal output",
            "||PULSE:STATUS Working on it||",
            "||PULSE:SUMMARY Implementing auth||",
            "more output",
        ]
        result = _rejoin_pulse_lines(lines)
        assert result == lines

    def test_split_summary_two_lines(self):
        lines = [
            "||PULSE:SUMMARY Moving Settings button to top gear icon and creating",
            "persistent settings store in database||",
        ]
        result = _rejoin_pulse_lines(lines)
        assert len(result) == 1
        assert "Moving Settings button" in result[0]
        assert "persistent settings store in database||" in result[0]

    def test_split_status_two_lines(self):
        lines = [
            "||PULSE:STATUS Reading current codebase to understand settings",
            "implementation||",
        ]
        result = _rejoin_pulse_lines(lines)
        assert len(result) == 1
        assert result[0].startswith("||PULSE:STATUS")
        assert result[0].endswith("implementation||")

    def test_split_across_three_lines(self):
        lines = [
            "||PULSE:SUMMARY This is a very long goal description that spans",
            "multiple terminal lines because the terminal is narrow and the",
            "message is quite verbose||",
        ]
        result = _rejoin_pulse_lines(lines)
        assert len(result) == 1
        assert "very long goal" in result[0]
        assert "quite verbose||" in result[0]

    def test_mixed_complete_and_split(self):
        lines = [
            "normal output",
            "||PULSE:STATUS Short status||",
            "||PULSE:SUMMARY Long summary that wraps across",
            "the terminal width boundary||",
            "more normal output",
        ]
        result = _rejoin_pulse_lines(lines)
        assert len(result) == 4
        assert result[0] == "normal output"
        assert result[1] == "||PULSE:STATUS Short status||"
        assert "Long summary that wraps across" in result[2]
        assert "terminal width boundary||" in result[2]
        assert result[3] == "more normal output"

    def test_no_pulse_lines_unchanged(self):
        lines = ["line one", "line two", "line three"]
        result = _rejoin_pulse_lines(lines)
        assert result == lines

    def test_empty_input(self):
        assert _rejoin_pulse_lines([]) == []

    def test_incomplete_tag_at_end_flushed(self):
        """An incomplete tag at the end of input is emitted as-is."""
        lines = [
            "||PULSE:STATUS This tag never closes",
        ]
        result = _rejoin_pulse_lines(lines)
        assert len(result) == 1
        assert "never closes" in result[0]

    def test_max_join_limit(self):
        """Safety limit prevents runaway joining (default MAX_JOIN=5)."""
        lines = ["||PULSE:SUMMARY Start of a very long tag"]
        # Add 10 continuation lines without closing ||
        for i in range(10):
            lines.append(f"continuation line {i}")
        result = _rejoin_pulse_lines(lines)
        # Should have flushed after MAX_JOIN lines, not accumulated all 11
        assert len(result) >= 2

    def test_spinner_prefix_preserved(self):
        """Lines with spinner prefixes (like Claude's bullet) are handled."""
        lines = [
            "  ||PULSE:SUMMARY Moving settings button to top gear icon and creating",
            "persistent settings store in database||",
        ]
        result = _rejoin_pulse_lines(lines)
        assert len(result) == 1
        assert "||PULSE:SUMMARY" in result[0]
        assert "database||" in result[0]

    def test_multiple_split_tags(self):
        """Multiple split tags in sequence are each rejoined correctly."""
        lines = [
            "||PULSE:SUMMARY First long summary that wraps",
            "across the line||",
            "||PULSE:STATUS Also a long status that wraps",
            "to the next line||",
        ]
        result = _rejoin_pulse_lines(lines)
        assert len(result) == 2
        assert SUMMARY_RE.search(result[0])
        assert STATUS_RE.search(result[1])


# ---------------------------------------------------------------------------
# Regex matching on rejoined lines
# ---------------------------------------------------------------------------

class TestRegexAfterRejoin:

    def test_status_regex_matches_rejoined(self):
        lines = [
            "||PULSE:STATUS Reading current codebase to understand settings",
            "implementation||",
        ]
        rejoined = _rejoin_pulse_lines(lines)
        matches = STATUS_RE.findall(rejoined[0])
        assert len(matches) == 1
        assert clean_match(matches[0]) == "Reading current codebase to understand settings implementation"

    def test_summary_regex_matches_rejoined(self):
        lines = [
            "||PULSE:SUMMARY Moving Settings button to top gear icon and creating",
            "persistent settings store in database||",
        ]
        rejoined = _rejoin_pulse_lines(lines)
        matches = SUMMARY_RE.findall(rejoined[0])
        assert len(matches) == 1
        assert "persistent settings store" in clean_match(matches[0])


# ---------------------------------------------------------------------------
# PULSE_EVENT_RE (task_detector) multiline matching
# ---------------------------------------------------------------------------

class TestPulseEventRegexMultiline:

    def test_matches_across_newlines(self):
        content = (
            "some output\n"
            "||PULSE:CONFIDENCE Low Unfamiliar with this auth library\n"
            "guessing at the API||\n"
            "more output\n"
        )
        matches = list(PULSE_EVENT_RE.finditer(content))
        assert len(matches) == 1
        assert matches[0].group(1) == "CONFIDENCE"
        assert "guessing at the API" in clean_match(matches[0].group(2))

    def test_single_line_still_works(self):
        content = "||PULSE:STATUS Short status||\n"
        matches = list(PULSE_EVENT_RE.finditer(content))
        assert len(matches) == 1
        assert matches[0].group(1) == "STATUS"
        assert clean_match(matches[0].group(2)) == "Short status"

    def test_multiple_events_mixed(self):
        content = (
            "||PULSE:STATUS Working||\n"
            "||PULSE:CONFIDENCE Low Not sure about\n"
            "this approach||\n"
            "||PULSE:SUMMARY Goal here||\n"
        )
        matches = list(PULSE_EVENT_RE.finditer(content))
        assert len(matches) == 3
        types = [m.group(1) for m in matches]
        assert types == ["STATUS", "CONFIDENCE", "SUMMARY"]


# ---------------------------------------------------------------------------
# End-to-end: get_log_status with wrapped PULSE tags
# ---------------------------------------------------------------------------

class TestGetLogStatusWrapped:

    def _write_log(self, content: str) -> str:
        f = tempfile.NamedTemporaryFile(mode="w", suffix=".log", delete=False)
        f.write(content)
        f.close()
        return f.name

    def test_wrapped_status_and_summary(self):
        path = self._write_log(
            "Normal output line 1\n"
            "||PULSE:SUMMARY Moving Settings button to top gear icon and creating\n"
            "persistent settings store in database||\n"
            "Some more output\n"
            "||PULSE:STATUS Reading current codebase to understand settings\n"
            "implementation||\n"
            "Final output line\n"
        )
        try:
            result = get_log_status(path)
            assert result["status"] == "Reading current codebase to understand settings implementation"
            assert result["summary"] == "Moving Settings button to top gear icon and creating persistent settings store in database"
        finally:
            os.unlink(path)

    def test_no_split_fragments_in_recent_lines(self):
        """Continuation fragments should not appear as separate recent_lines."""
        path = self._write_log(
            "||PULSE:STATUS Long status that wraps across\n"
            "the terminal line||\n"
        )
        try:
            result = get_log_status(path)
            # The rejoined tag should be a single line, not two
            pulse_lines = [l for l in result["recent_lines"] if "PULSE:" in l]
            assert len(pulse_lines) == 1
            # No orphan "the terminal line||" fragment
            fragment_lines = [l for l in result["recent_lines"] if l.strip() == "the terminal line||"]
            assert len(fragment_lines) == 0
        finally:
            os.unlink(path)

    def test_complete_tags_still_work(self):
        path = self._write_log(
            "||PULSE:SUMMARY Short goal||\n"
            "||PULSE:STATUS Short status||\n"
        )
        try:
            result = get_log_status(path)
            assert result["status"] == "Short status"
            assert result["summary"] == "Short goal"
        finally:
            os.unlink(path)


# ---------------------------------------------------------------------------
# End-to-end: get_log_snapshot with wrapped PULSE tags
# ---------------------------------------------------------------------------

class TestGetLogSnapshotWrapped:

    def _write_log(self, content: str) -> str:
        f = tempfile.NamedTemporaryFile(mode="w", suffix=".log", delete=False)
        f.write(content)
        f.close()
        return f.name

    def test_wrapped_status_and_summary(self):
        path = self._write_log(
            "Normal output\n"
            "||PULSE:SUMMARY Long summary that wraps\n"
            "across the line||\n"
            "||PULSE:STATUS Long status that wraps\n"
            "to next line||\n"
        )
        try:
            result = get_log_snapshot(path)
            assert result["status"] == "Long status that wraps to next line"
            assert result["summary"] == "Long summary that wraps across the line"
        finally:
            os.unlink(path)

    def test_no_fragment_lines_in_output(self):
        """Continuation fragments should be merged, not shown as separate lines."""
        path = self._write_log(
            "||PULSE:SUMMARY Goal that wraps\n"
            "to next line||\n"
            "Real output here\n"
        )
        try:
            result = get_log_snapshot(path)
            # Should not have an orphan "to next line||" entry
            for line in result["recent_lines"]:
                assert line.strip() != "to next line||"
        finally:
            os.unlink(path)
