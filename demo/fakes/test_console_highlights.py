import pytest

from console_highlights import HighlightsTab
from fake_console import Log

REPO = __import__("pathlib").Path(__file__).resolve().parents[2]


@pytest.fixture
def tab():
    return HighlightsTab(Log(), REPO)


def test_default_since_is_minor_start_of_current_changelog(tab):
    assert tab.since.startswith("v0.") and tab.since.endswith(".0")


def test_page_is_served_only_after_render(tab):
    assert tab.serve("/highlights/") is None
    assert tab.summary()["level"] == "off"

    state = tab.act("render", {"since": "v0.9.8"})

    ctype, body = tab.serve("/highlights/")
    assert state["rendered"] and ctype.startswith("text/html")
    assert b"pv-page" in body
    assert tab.summary()["level"] == "ok"


def test_invalid_version_is_value_error(tab):
    with pytest.raises(ValueError):
        tab.act("render", {"since": "latest"})


def test_images_are_served_from_docs_images_only(tab):
    name = next((REPO / "docs" / "images").glob("*.png")).name

    assert tab.serve(f"/docs/images/{name}")[0] == "image/png"
    assert tab.serve("/docs/images/../../README.md") is None
    assert tab.serve("/docs/images/nope.png") is None


def test_reset_clears_page(tab):
    tab.act("render", {"since": "v0.9.8"})
    tab.reset()
    assert tab.serve("/highlights/") is None


def test_render_writes_changelog_into_installer_state(tmp_path):
    import json
    import shutil
    state = tmp_path / "state"
    shutil.copytree(REPO / "demo" / "fixtures" / "installed-state", state)
    tab = HighlightsTab(Log(), REPO, state)

    tab.act("render", {"since": "v0.9.8"})

    document = json.loads((state / "changelog.json").read_text())
    assert any(c["id"] == "dashboard" for c in document["components"])


def test_render_without_installer_manifest_is_value_error(tmp_path):
    tab = HighlightsTab(Log(), REPO, tmp_path)

    with pytest.raises(ValueError):
        tab.act("render", {"since": "v0.9.8"})
