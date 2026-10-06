"""Tests for scripts/docs/check_site.py."""
import importlib.util
import json
from pathlib import Path

spec = importlib.util.spec_from_file_location(
    "check_site", Path(__file__).resolve().parents[1] / "docs" / "check_site.py")
check_site = importlib.util.module_from_spec(spec)
spec.loader.exec_module(check_site)


def page(body: str, moves: dict | None = None) -> str:
    extra = ""
    if moves is not None:
        extra = f'<script type="application/json" id="anchor-moves">{json.dumps(moves)}</script>'
    return f"<!doctype html><html><body>{body}{extra}</body></html>"


def make(tmp: Path, files: dict[str, str], nav: str, sources: list[str]):
    site, docs = tmp / "_site", tmp / "docs"
    for rel, html in files.items():
        p = site / rel
        p.parent.mkdir(parents=True, exist_ok=True)
        p.write_text(html)
    (docs / "_data").mkdir(parents=True)
    (docs / "_data" / "nav.yml").write_text(nav)
    for rel in sources:
        p = docs / rel
        p.parent.mkdir(parents=True, exist_ok=True)
        p.write_text("---\ntitle: x\n---\n")
    return site, docs


NAV = """- title: G
  items:
    - label: "Home"
      url: "index.md"
    - label: "Dash"
      url: "dashboard/index.md"
      children:
        - { label: "Settings", url: "dashboard/settings.md" }
"""
SOURCES = ["index.md", "dashboard/index.md", "dashboard/settings.md"]


def test_clean_site_passes(tmp_path):
    site, docs = make(tmp_path, {
        "index.html": page('<a href="/energy-node/dashboard/">d</a><a href="dashboard/settings.html#mqtt">m</a>'),
        "dashboard/index.html": page('<h2 id="x">x</h2>', {"settings": "settings.html#top"}),
        "dashboard/settings.html": page('<h1 id="top">s</h1><h2 id="mqtt">m</h2>'),
    }, NAV, SOURCES)
    assert check_site.check(site, docs) == []


def test_missing_target_is_reported(tmp_path):
    site, docs = make(tmp_path, {
        "index.html": page('<a href="nope.html">x</a>'),
        "dashboard/index.html": page(""),
        "dashboard/settings.html": page(""),
    }, NAV, SOURCES)
    assert any("nope.html" in f for f in check_site.check(site, docs))


def test_missing_anchor_is_reported(tmp_path):
    site, docs = make(tmp_path, {
        "index.html": page('<a href="dashboard/settings.html#gone">x</a>'),
        "dashboard/index.html": page(""),
        "dashboard/settings.html": page('<h2 id="mqtt">m</h2>'),
    }, NAV, SOURCES)
    assert any("#gone" in f for f in check_site.check(site, docs))


def test_anchor_listed_in_anchor_moves_is_accepted(tmp_path):
    site, docs = make(tmp_path, {
        "index.html": page('<a href="dashboard/#settings">x</a>'),
        "dashboard/index.html": page("", {"settings": "settings.html"}),
        "dashboard/settings.html": page(""),
    }, NAV, SOURCES)
    assert check_site.check(site, docs) == []


def test_broken_anchor_move_target_is_reported(tmp_path):
    site, docs = make(tmp_path, {
        "index.html": page(""),
        "dashboard/index.html": page("", {"mqtt": "settings.html#mqtt"}),
        "dashboard/settings.html": page(""),
    }, NAV, SOURCES)
    assert any("anchor_moves" in f and "#mqtt" in f for f in check_site.check(site, docs))


def test_nav_entry_without_source_is_reported(tmp_path):
    site, docs = make(tmp_path, {"index.html": page("")}, NAV, ["index.md", "dashboard/index.md"])
    assert any("dashboard/settings.md" in f and "nav" in f for f in check_site.check(site, docs))


def test_orphan_page_is_reported_but_internal_and_redirects_are_not(tmp_path):
    site, docs = make(tmp_path, {
        "index.html": page(""), "dashboard/index.html": page(""), "dashboard/settings.html": page(""),
    }, NAV, SOURCES + ["orphan.md", "_internal/x.md", "redirects/old.md"])
    (docs / "moved.md").write_text("---\nredirect_to: /x\n---\n")
    findings = check_site.check(site, docs)
    assert any("orphan.md" in f for f in findings)
    assert not any("_internal" in f or "redirects/" in f or "moved.md" in f for f in findings)


DEEP_NAV = """- title: G
  items:
    - label: "Home"
      url: "index.md"
    - label: "Dash"
      url: "dashboard/index.md"
      children:
        - label: "Settings"
          url: "dashboard/settings.md"
          children:
            - label: "Deep"
              url: "dashboard/deep.md"
              children:
                - { label: "Too deep", url: "dashboard/deeper.md" }
"""


def test_nav_deeper_than_three_levels_is_reported(tmp_path):
    site, docs = make(tmp_path, {"index.html": page("")}, DEEP_NAV,
                      SOURCES + ["dashboard/deep.md", "dashboard/deeper.md"])
    assert any("deeper than 3" in f for f in check_site.check(site, docs))


def test_external_and_mailto_links_are_ignored(tmp_path):
    site, docs = make(tmp_path, {
        "index.html": page('<a href="https://example.com/x">x</a><a href="mailto:a@b">m</a><a href="#top">t</a><h1 id="top"></h1>'),
        "dashboard/index.html": page(""), "dashboard/settings.html": page(""),
    }, NAV, SOURCES)
    assert check_site.check(site, docs) == []
