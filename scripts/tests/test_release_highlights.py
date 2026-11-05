import json
import subprocess
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "release"))
sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "build"))
import release_highlights as rh

e = lambda text, **kw: {"text": text, "breaking": False, "highlight": True, **kw}
DOC = {"components": [
    {"id": "dashboard", "label": "Dashboard", "version": "v0.9.2", "releases": [
        {"version": "v0.9.2", "groups": [{"type": "feat", "entries": [
            e("statistics page", pr=132, i18n={"de": "Statistikseite"}),
            {"text": "detail", "breaking": False, "highlight": False}]}]},
        {"version": "v0.9.1", "groups": [{"type": "fix", "entries": [e("old fix", pr=120)]}]},
        {"version": "v0.9.0", "groups": [{"type": "feat", "entries": [e("too old", pr=100)]}]},
    ]},
    {"id": "service:shelly", "label": "Shelly", "version": "v0.4.2", "releases": [
        {"version": "v0.4.2", "groups": [{"type": "feat", "entries": [e("drop legacy", pr=131, breaking=True)]}]},
    ]},
    {"id": "bootstrap", "label": "Bootstrap", "version": "v0.3.0", "releases": [
        {"version": "v0.3.0", "groups": [{"type": "feat", "entries": [e("unchanged", pr=90)]}]},
    ]},
]}


def test_rows_cut_the_range_per_component():
    rows = rh.highlight_rows(DOC, {"dashboard": "v0.9.0", "bootstrap": "v0.3.0"})
    assert [(label, version, [x["text"] for x in entries]) for label, version, entries in rows] == [
        ("Dashboard", "v0.9.2", ["statistics page"]),
        ("Dashboard", "v0.9.1", ["old fix"]),
        ("Shelly", "v0.4.2", ["drop legacy"]),   # new component: current release only
    ]


def test_render_links_and_both_languages_folded_with_fallback():
    out = rh.render(rh.highlight_rows(DOC, {"dashboard": "v0.9.1", "bootstrap": "v0.3.0"}), "v0.9.2")
    top, english, german = out.split("<details>")
    assert top.strip() == rh.links("v0.9.2")
    assert english.startswith("\n<summary>Highlights in English</summary>\n\n### Dashboard v0.9.2\n\n- statistics page (#132)\n")
    assert "- **⚠ Breaking** drop legacy (#131)" in english
    assert english.rstrip().endswith("</details>")
    assert "<summary>Highlights auf Deutsch</summary>" in german
    assert "### Dashboard v0.9.2" in german
    assert "- Statistikseite (#132)" in german
    assert "- **⚠ Breaking** drop legacy (#131)" in german   # no translation: English text
    assert out.rstrip().endswith("</details>")
    assert "- detail" not in out


def test_links_point_at_the_release():
    assert rh.links("v0.9.2") == (
        "[What's new in v0.9.2](https://developer-simon.github.io/energy-node/whats-new.html#v0.9.2)"
        " · [Component versions](https://developer-simon.github.io/energy-node/developing/versions.html#v0.9.2)"
        " · [Full changelog](https://github.com/Developer-Simon/energy-node/blob/v0.9.2/dashboard/CHANGELOG.md)"
    )


def test_render_without_rows_has_only_the_links():
    assert rh.render([], "v0.9.2").strip() == rh.links("v0.9.2")


def test_versions_at_reads_files_at_a_tag(tmp_path):
    def git(*args):
        subprocess.run(["git", "-C", str(tmp_path), *args], check=True, capture_output=True)
    git("init", "-q")
    git("config", "user.email", "t@example.com")
    git("config", "user.name", "t")
    (tmp_path / "dashboard").mkdir()
    (tmp_path / "dashboard/VERSION").write_text("v0.9.0\n", encoding="utf-8")
    (tmp_path / "manifest.json").write_text('{"version": "0.4.1"}', encoding="utf-8")
    git("add", ".")
    git("commit", "-q", "-m", "x")
    git("tag", "v0.9.0")
    table = {"components": [
        {"id": "dashboard", "version_file": "dashboard/VERSION"},
        {"id": "service:shelly", "version_file": "manifest.json"},
        {"id": "new", "version_file": "new/VERSION"},
    ]}
    assert rh.versions_at(tmp_path, "v0.9.0", table) == {"dashboard": "v0.9.0", "service:shelly": "v0.4.1"}
