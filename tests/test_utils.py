import pytest
from corral.tools.session_manager import strip_ansi, clean_match

def test_strip_ansi():
    # Test basic string
    assert strip_ansi("hello world") == "hello world"
    
    # Test with ansi color codes
    assert strip_ansi("\x1b[31mred text\x1b[0m") == " red text "
    
    # Test with other control chars
    assert strip_ansi("text\x07") == "text"

def test_clean_match():
    # Test standard string
    assert clean_match("  hello   world  ") == "hello world"
    
    # Test with newlines and tabs
    assert clean_match("hello\n\tworld") == "hello world"
    
    # Test empty
    assert clean_match("") == ""
    assert clean_match("   ") == ""
