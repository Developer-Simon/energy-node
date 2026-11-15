"""Tests for scripts/dev/cache_bust.py (Referenzscan, Änderungserkennung, report)."""
import importlib.util
import subprocess
import sys
from pathlib import Path

import pytest

ROOT = Path(__file__).resolve().parents[2]
SCRIPT = ROOT / "scripts" / "dev" / "cache_bust.py"
spec = importlib.util.spec_from_file_location("cache_bust", SCRIPT)
cache_bust = importlib.util.module_from_spec(spec)
spec.loader.exec_module(cache_bust)

WEBUI = cache_bust.WEBUI
scan_refs = cache_bust.scan_refs
versions = cache_bust.versions
compare = cache_bust.compare
main = cache_bust.main


def git(root, *args):
    subprocess.run(["git", "-C", str(root), *args], check=True, capture_output=True, text=True)


def write(root, path, text):
    target = root / path
    target.parent.mkdir(parents=True, exist_ok=True)
    target.write_text(text, encoding="utf-8")


def edit(root, path, text):
    write(root, f"{WEBUI}/{path}", text)


@pytest.fixture
def repo(tmp_path):
    root = tmp_path / "repo"
    root.mkdir()
    git(root, "init", "-q")
    git(root, "config", "user.email", "test@example.com")
    git(root, "config", "user.name", "test")
    write(root, "dashboard/VERSION", "v0.11.8\n")
    write(root, f"{WEBUI}/webui.go", '"/static/js/card.js?v=2"\n')
    write(
        root,
        f"{WEBUI}/templates/base.html",
        '{{.BasePath}}/static/css/manager.css?v=5\n'
        '{{.BasePath}}/static/css/manager.css?v=5\n'
        '/static/js/app.js?v=3\n'
        '/static/js-deps/lib.min.js\n',
    )
    write(root, f"{WEBUI}/static/js/app.js", "console.log(1);\n")
    write(root, f"{WEBUI}/static/js/card.js", "console.log(2);\n")
    write(root, f"{WEBUI}/static/js-deps/lib.min.js", "var lib;\n")
    write(root, f"{WEBUI}/static/css/manager.css", "body {}\n")
    git(root, "add", "-A")
    git(root, "commit", "-q", "-m", "base")
    base = subprocess.run(
        ["git", "-C", str(root), "rev-parse", "HEAD"],
        check=True, capture_output=True, text=True,
    ).stdout.strip()
    return root, base


def row_for(rows, asset):
    matches = [r for r in rows if r.asset == asset]
    assert len(matches) == 1, rows
    return matches[0]


def test_scan_reads_templates_and_webui_go(repo):
    root, _ = repo
    assert versions(scan_refs(root, None)) == {
        "css/manager.css": {5},
        "js/app.js": {3},
        "js-deps/lib.min.js": {None},
        "js/card.js": {2},
    }


def test_scan_skips_non_integer_marks(repo):
    root, _ = repo
    edit(root, "templates/base.html",
         '/static/css/manager.css?v=5\n/static/js/app.js?v=3\n/static/js/x.js?v={{.Foo}}\n')
    assert "js/x.js" not in versions(scan_refs(root, None))


def test_unchanged_repo_reports_nothing(repo):
    root, base = repo
    assert compare(root, base) == []


def test_changed_asset_without_bump(repo):
    root, base = repo
    edit(root, "static/js/app.js", "console.log(3);\n")
    row = row_for(compare(root, base), "js/app.js")
    assert (row.base, row.head, row.changed, row.expected, row.status) == (3, {3}, True, 4, "bump")


def test_changed_asset_with_bump_is_ok(repo):
    root, base = repo
    edit(root, "static/js/app.js", "console.log(3);\n")
    edit(root, "templates/base.html",
         '{{.BasePath}}/static/css/manager.css?v=5\n'
         '{{.BasePath}}/static/css/manager.css?v=5\n'
         '/static/js/app.js?v=4\n'
         '/static/js-deps/lib.min.js\n')
    row = row_for(compare(root, base), "js/app.js")
    assert row.status == "ok"
    assert row.expected == 4


def test_unversioned_asset_needs_v1(repo):
    root, base = repo
    edit(root, "static/js-deps/lib.min.js", "var lib = 2;\n")
    row = row_for(compare(root, base), "js-deps/lib.min.js")
    assert row.status == "bump"
    assert row.expected == 1


def test_changed_unreferenced_asset(repo):
    root, base = repo
    write(root, f"{WEBUI}/static/js/orphan.js", "console.log(0);\n")
    git(root, "add", f"{WEBUI}/static/js/orphan.js")
    row = row_for(compare(root, base), "js/orphan.js")
    assert row.status == "unreferenced"
    assert row.expected is None


def test_base_inconsistency_repaired_by_lifting(repo):
    root, _ = repo
    # base: app.js is 3 in the template but 2 in webui.go
    edit(root, "webui.go", '"/static/js/card.js?v=2"\n"/static/js/app.js?v=2"\n')
    git(root, "add", "-A")
    git(root, "commit", "-q", "-m", "inconsistent base")
    base = subprocess.run(
        ["git", "-C", str(root), "rev-parse", "HEAD"],
        check=True, capture_output=True, text=True,
    ).stdout.strip()
    # head: webui.go lifted to 3, app.js itself untouched
    edit(root, "webui.go", '"/static/js/card.js?v=2"\n"/static/js/app.js?v=3"\n')
    rows = compare(root, base)
    assert all(r.status == "ok" for r in rows), rows
    assert all(r.expected == 3 for r in rows if r.asset == "js/app.js")


def test_report_cli_prints_rows(repo, capsys):
    root, base = repo
    edit(root, "static/js/app.js", "console.log(3);\n")
    assert main(["report", "--base", base, "--root", str(root)]) == 0
    out = capsys.readouterr().out
    assert "js/app.js" in out
    assert "bump" in out
    assert "1 assets to bump" in out


def _commit(root, message):
    git(root, "add", "-A")
    git(root, "commit", "-q", "-m", message)
    return subprocess.run(
        ["git", "-C", str(root), "rev-parse", "HEAD"],
        check=True, capture_output=True, text=True,
    ).stdout.strip()


def test_unchanged_asset_with_disagreeing_references_is_inconsistent(repo):
    root, base = repo  # app.js is 3 everywhere at base
    edit(root, "webui.go", '"/static/js/card.js?v=2"\n"/static/js/app.js?v=2"\n')
    row = row_for(compare(root, base), "js/app.js")
    assert row.status == "inconsistent"
    assert row.expected == 3
    assert row.base == 3
    assert row.head == {2, 3}
    assert row.changed is False


def test_too_high_when_changed_and_jump_is_more_than_one(repo):
    root, base = repo
    edit(root, "static/js/app.js", "console.log(9);\n")
    edit(root, "templates/base.html",
         '{{.BasePath}}/static/css/manager.css?v=5\n'
         '{{.BasePath}}/static/css/manager.css?v=5\n'
         '/static/js/app.js?v=5\n'
         '/static/js-deps/lib.min.js\n')
    row = row_for(compare(root, base), "js/app.js")
    assert row.status == "too-high"
    assert row.expected == 4


def test_too_low_when_head_is_below_base(repo):
    root, base = repo
    edit(root, "templates/base.html",
         '{{.BasePath}}/static/css/manager.css?v=5\n'
         '{{.BasePath}}/static/css/manager.css?v=5\n'
         '/static/js/app.js?v=2\n'
         '/static/js-deps/lib.min.js\n')
    row = row_for(compare(root, base), "js/app.js")
    assert row.status == "too-low"
    assert row.expected == 3


def test_no_change_when_unchanged_asset_has_higher_mark(repo):
    root, base = repo
    edit(root, "templates/base.html",
         '{{.BasePath}}/static/css/manager.css?v=5\n'
         '{{.BasePath}}/static/css/manager.css?v=5\n'
         '/static/js/app.js?v=4\n'
         '/static/js-deps/lib.min.js\n')
    row = row_for(compare(root, base), "js/app.js")
    assert row.status == "no-change"
    assert row.expected == 3


def write_marks(root, css=(5, 5), app=3):
    edit(root, "templates/base.html",
         f'{{{{.BasePath}}}}/static/css/manager.css?v={css[0]}\n'
         f'{{{{.BasePath}}}}/static/css/manager.css?v={css[1]}\n'
         f'/static/js/app.js?v={app}\n'
         '/static/js-deps/lib.min.js\n')


def run_check(root, base, *extra):
    return main(["check", "--base", base, "--root", str(root), *extra])


def test_check_passes_on_clean_bump(repo):
    root, base = repo
    edit(root, "static/js/app.js", "console.log(3);\n")
    write_marks(root, app=4)
    assert run_check(root, base) == 0


def test_check_fails_missing_bump(repo, capsys):
    root, base = repo
    edit(root, "static/js/app.js", "console.log(3);\n")
    assert run_check(root, base) == 1
    assert "expected 4" in capsys.readouterr().out


def test_check_fails_partial_multi_reference_bump(repo, capsys):
    root, base = repo
    edit(root, "static/css/manager.css", "body { color: red; }\n")
    write_marks(root, css=(6, 5))
    assert run_check(root, base) == 1
    assert "references disagree" in capsys.readouterr().out


def test_check_fails_when_base_already_bumped(repo, capsys):
    root, _ = repo
    edit(root, "static/css/manager.css", "body { color: red; }\n")
    write_marks(root, css=(6, 6))
    base = _commit(root, "base already bumped")
    edit(root, "static/css/manager.css", "body { color: blue; }\n")
    assert run_check(root, base) == 1
    assert "expected 7" in capsys.readouterr().out


def test_check_fails_double_bump_strict(repo, capsys):
    root, base = repo
    edit(root, "static/js/app.js", "console.log(3);\n")
    write_marks(root, app=5)
    assert run_check(root, base) == 1
    out = capsys.readouterr().out
    assert "went from 3 to 5" in out
    assert "(expected 4)" in out


def test_check_lenient_accepts_double_bump(repo):
    root, base = repo
    edit(root, "static/js/app.js", "console.log(3);\n")
    write_marks(root, app=5)
    assert run_check(root, base, "--lenient") == 0


def test_check_lenient_catches_unchanged_v(repo):
    root, base = repo
    edit(root, "static/js/app.js", "console.log(3);\n")
    assert run_check(root, base, "--lenient") == 1


def test_check_fails_bump_without_change(repo, capsys):
    root, base = repo
    write_marks(root, app=4)
    assert run_check(root, base) == 1
    assert "but the file did not" in capsys.readouterr().out


def test_check_warns_unreferenced(repo, capsys):
    root, base = repo
    write(root, f"{WEBUI}/static/js/orphan.js", "console.log(0);\n")
    git(root, "add", f"{WEBUI}/static/js/orphan.js")
    assert run_check(root, base) == 0
    assert "warning:" in capsys.readouterr().out
