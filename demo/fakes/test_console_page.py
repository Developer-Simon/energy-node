import re
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))
from console_page import PAGE_HTML  # noqa: E402


def visible_texts(html):
    """Text zwischen > und < ausserhalb von <style> und <script>."""
    stripped = re.sub(r"<(style|script)\b.*?</\1>", "", html, flags=re.S)
    return " ".join(re.findall(r">([^<]*)<", stripped))


def test_page_has_shell_regions():
    for marker in ('id="sidebar"', 'id="panels"', 'id="log"', "data-log-toggle",
                   'data-log-filter', "Alles auf Start zurücksetzen", "Dieser Bereich", "Nur Fehler"):
        assert marker in PAGE_HTML


def test_page_defines_shared_building_blocks():
    for cls in (".grp", ".list", ".row", ".note", ".grp-err", ".btn-primary", ".btn-danger",
                ".switch", ".seg", ".pill", ".lvl-ok", ".lvl-changed", ".lvl-error", ".lvl-off"):
        assert cls in PAGE_HTML


def test_page_exposes_konsole_helpers_and_polls_status():
    for name in ("window.konsole", "setUnlessFocused", "setSeg", "/api/status", "/api/reset", "/api/log"):
        assert name in PAGE_HTML


def test_page_respects_user_preferences_and_has_no_external_resources():
    assert "prefers-color-scheme: dark" in PAGE_HTML
    assert "prefers-reduced-motion" in PAGE_HTML
    assert "prefers-contrast" in PAGE_HTML
    assert "http://" not in PAGE_HTML and "https://" not in PAGE_HTML


def test_local_storage_is_guarded():
    assert PAGE_HTML.count("localStorage") <= PAGE_HTML.count("try")


def test_page_texts_follow_copy_rules():
    assert ";" not in visible_texts(PAGE_HTML)
    assert " – " not in PAGE_HTML and " — " not in PAGE_HTML
