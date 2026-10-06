"""Tests for scripts/docs/check_tokens.py."""
import importlib.util
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
spec = importlib.util.spec_from_file_location("check_tokens", ROOT / "scripts" / "docs" / "check_tokens.py")
check_tokens = importlib.util.module_from_spec(spec)
spec.loader.exec_module(check_tokens)

BASE = """
:root { --bg: #111418; --panel: #171a20; --text: #e6e6e6; --text-muted: #8b949e; --accent: #9fd; }
:root[data-theme="tageslicht"] { --bg: #f3f1ec; --panel: #ffffff; --text: #1b2027; --text-muted: #5b6570; --accent: #0f9e7a; --accent-line: #0b7a5e; }
"""
SITE_OK = """
:root { --bg: #f3f1ec; --panel: #ffffff; --text: #1b2027; --text-muted: #5b6570; --accent: #0f9e7a; --accent-line: #0b7a5e; --link: var(--accent-line); }
:root[data-theme="dark"] { --bg: #111418; --panel: #171a20; --text: #e6e6e6; --text-muted: #8b949e; --accent: #9fd; --link: var(--accent); }
"""


def test_matching_tokens_pass():
    assert check_tokens.check(SITE_OK, BASE) == []


def test_drift_is_reported():
    drifted = SITE_OK.replace("--panel: #ffffff", "--panel: #fefefe", 1)
    assert any("--panel" in f and "light" in f for f in check_tokens.check(drifted, BASE))


def test_low_contrast_is_reported():
    base = BASE.replace("--text-muted: #5b6570", "--text-muted: #c0c0c0")
    site = SITE_OK.replace("--text-muted: #5b6570", "--text-muted: #c0c0c0")
    assert any("contrast" in f and "--text-muted" in f for f in check_tokens.check(site, base))


def test_real_files_pass():
    site = (ROOT / "docs" / "assets" / "css" / "site.css").read_text()
    base = (ROOT / "dashboard" / "internal" / "webui" / "static" / "css" / "base.css").read_text()
    assert check_tokens.check(site, base) == []
