"""Einfuege-Logik des Entwurfs-Proxys (demo/draft_proxy.py)."""

import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent.parent))

import draft_proxy  # noqa: E402

PAGE = '<html><head><title>x</title></head><body><header class="masthead"></header><div class="update-bar"></div></body></html>'
FRAGMENT = '<section id="settings-system"></section><div x-ref="subnav"></div>'


def manifest(**extra):
    base = {"assets": "ob", "page": '<header class="masthead">', "head": [], "insert": [], "replace": []}
    base.update(extra)
    return base


def test_head_assets_only_on_full_pages():
    m = manifest(head=["a.css", "b.js"])
    page = draft_proxy.patch_html(PAGE, m, lambda name: "")
    assert '<link rel="stylesheet" href="/__draft/a.css">' in page
    assert '<script src="/__draft/b.js" defer></script>\n</head>' in page
    assert draft_proxy.patch_html(FRAGMENT, m, lambda name: "") == FRAGMENT


def test_insert_fragments_in_order_before_marker():
    m = manifest(insert=[{"before": "</body>", "fragment": ["one.html", "two.html"]}])
    page = draft_proxy.patch_html(PAGE, m, lambda name: f"[{name}]")
    assert page.endswith("[one.html][two.html]</body></html>")


def test_when_selects_the_response():
    rule = {"when": 'x-ref="subnav"', "before": '<section id="settings-system"', "fragment": "c.html"}
    m = manifest(insert=[rule])
    assert draft_proxy.patch_html(FRAGMENT, m, lambda name: "[c]").startswith('[c]<section id="settings-system"')
    assert draft_proxy.patch_html(PAGE, m, lambda name: "[c]") == PAGE


def test_missing_marker_warns_and_keeps_text(capsys):
    m = manifest(insert=[{"when": "<html>", "before": "<nav", "fragment": "x.html"}])
    assert draft_proxy.patch_html(PAGE, m, lambda name: "[x]") == PAGE
    assert "Einfuegemarke fehlt" in capsys.readouterr().err


def test_replace_only_matching_path(capsys):
    m = manifest(replace=[{"path": "/static/js/a.js", "find": "'users'];", "with": "'users', 'checklist'];"}])
    assert draft_proxy.patch_text("/static/js/a.js", "x = ['users'];", m) == "x = ['users', 'checklist'];"
    assert draft_proxy.patch_text("/static/js/b.js", "x = ['users'];", m) == "x = ['users'];"
    assert draft_proxy.patch_text("/static/js/a.js", "y = 1;", m) == "y = 1;"
    assert "Text zum Ersetzen fehlt" in capsys.readouterr().err
