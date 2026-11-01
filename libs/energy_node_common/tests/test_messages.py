import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "src"))

from energy_node_common.messages import render

CATALOGS = {
    "de": {"a": "{n} Stück"},
    "en": {"a": "{n} pieces"},
}


def test_renders_in_requested_language():
    assert render(CATALOGS, "de", "a", {"n": 3}) == "3 Stück"


def test_regional_tag_uses_primary_language():
    assert render(CATALOGS, "de-AT", "a", {"n": 3}) == "3 Stück"
    assert render(CATALOGS, "DE", "a", {"n": 3}) == "3 Stück"


def test_unknown_language_falls_back_to_english():
    assert render(CATALOGS, "fr", "a", {"n": 3}) == "3 pieces"
    assert render(CATALOGS, None, "a", {"n": 3}) == "3 pieces"


def test_unknown_key_returns_none():
    assert render(CATALOGS, "de", "missing", {"n": 3}) is None


def test_missing_param_returns_none():
    assert render(CATALOGS, "de", "a", None) is None


def test_braces_without_placeholder_do_not_raise():
    catalogs = {"en": {"a": '{"a": 1}'}}
    assert render(catalogs, "en", "a") is None
