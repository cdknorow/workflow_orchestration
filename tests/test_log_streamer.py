import pytest
from corral.log_streamer import _is_noise_line

def test_is_noise_line():
    # Empty or whitespace
    assert _is_noise_line("") is True
    assert _is_noise_line("   ") is True
    
    # Box drawing
    assert _is_noise_line("──────────") is True
    assert _is_noise_line("─── ───") is True
    
    # Status bar fragments
    assert _is_noise_line("worktree: main") is True
    assert _is_noise_line("model: claude-3") is True
    
    # Prompts
    assert _is_noise_line(">") is True
    assert _is_noise_line("❯ ") is True
    
    # Bare numbers
    assert _is_noise_line("1") is True
    assert _is_noise_line("  2  ") is True
    assert _is_noise_line("· 3") is True
    
    # TUI Noise
    assert _is_noise_line("Real-time Output Streaming") is True
    
    # Valid lines (Not noise)
    assert _is_noise_line("This is an actual log message") is False
    assert _is_noise_line("||PULSE:STATUS Working on it||") is False
    assert _is_noise_line("def foo():") is False


def test_pulse_regex_with_spinner_prefix():
    """PULSE regexes must match lines with spinner/indent prefixes from terminal output."""
    from corral.session_manager import STATUS_RE, SUMMARY_RE

    # Bare (no prefix)
    assert STATUS_RE.search("||PULSE:STATUS Waiting||")
    assert SUMMARY_RE.search("||PULSE:SUMMARY Implementing feature||")

    # Spinner prefix (⏺ from Claude Code)
    assert STATUS_RE.search("⏺ ||PULSE:STATUS Reading user request||")
    assert SUMMARY_RE.search("⏺ ||PULSE:SUMMARY Waiting for instructions||")

    # Indented (follows a spinner-prefixed line)
    assert STATUS_RE.search("  ||PULSE:STATUS Waiting for instructions||")
    assert SUMMARY_RE.search("  ||PULSE:SUMMARY Building feature||")

    # Multiline: both should be found
    log_text = (
        "⏺ ||PULSE:SUMMARY Waiting for instructions||\n"
        "  ||PULSE:STATUS Idle||\n"
    )
    assert len(STATUS_RE.findall(log_text)) == 1
    assert len(SUMMARY_RE.findall(log_text)) == 1


def test_pulse_event_regex_with_spinner_prefix():
    """PULSE_EVENT_RE in task_detector must match spinner-prefixed lines."""
    from corral.task_detector import PULSE_EVENT_RE

    # Bare
    m = PULSE_EVENT_RE.search("||PULSE:CONFIDENCE Low Unfamiliar with library||")
    assert m and m.group(1) == "CONFIDENCE"

    # Spinner prefix
    m = PULSE_EVENT_RE.search("⏺ ||PULSE:CONFIDENCE High Matches existing pattern||")
    assert m and m.group(1) == "CONFIDENCE"

    # Indented
    m = PULSE_EVENT_RE.search("  ||PULSE:STATUS Reading codebase||")
    assert m and m.group(1) == "STATUS"
