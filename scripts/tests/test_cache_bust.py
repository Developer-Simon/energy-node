"""Tests for scripts/dev/cache_bust.py (Referenzscan, Änderungserkennung, report)."""
import importlib.util
import subprocess
import sys
from datetime import date
from pathlib import Path

import pytest

ROOT = Path(__file__).resolve().parents[2]
SCRIPT = ROOT / "scripts" / "dev" / "cache_bust.py"
spec = importlib.util.spec_from_file_location("cache_bust", SCRIPT)
cache_bust = importlib.util.module_from_spec(spec)
sys.modules["cache_bust"] = cache_bust
spec.loader.exec_module(cache_bust)

WEBUI = cache_bust.WEBUI
HISTORY = cache_bust.HISTORY
HEADER_LINE = "version,date,asset,v,note\n"
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
    write(root, HISTORY, HEADER_LINE)
    git(root, "add", "-A")
    git(root, "commit", "-q", "-m", "base")
    base = subprocess.run(
        ["git", "-C", str(root), "rev-parse", "HEAD"],
        check=True, capture_output=True, text=True,
    ).stdout.strip()
    return root, base


def read_history(root):
    return (root / HISTORY).read_text(encoding="utf-8")


def write_history(root, text):
    write(root, HISTORY, text)


def with_history(root, base, today="2026-11-09"):
    assert main(["history", "--base", base, "--root", str(root), "--today", today]) == 0


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
    with_history(root, base)
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
    with_history(root, base)
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


Entry = cache_bust.Entry
parse_history = cache_bust.parse_history
format_history = cache_bust.format_history
expected_version = cache_bust.expected_version
bumps = cache_bust.bumps
update_history = cache_bust.update_history
check_history = cache_bust.check_history
Row = cache_bust.Row
TODAY = date(2026, 11, 9)
BASE_ROW = Entry("v0.11.8", "2026-10-30", "css/manager.css", 5, "")
HISTORY_NAME = "cache-bust-history.csv"


def test_parse_and_format_round_trip():
    text = (
        HEADER_LINE
        + "v0.3.19,2026-10-01,css/manager.css,5,\n"
        + 'v0.11.7,2026-11-01,js/app.js,3,"fix, again"\n'
        + "v0.11.10,2026-11-08,js/app.js,4,\n"
    )
    entries = parse_history(text)
    assert [e.version for e in entries] == ["v0.3.19", "v0.11.7", "v0.11.10"]
    out = format_history(entries)
    assert out == (
        HEADER_LINE
        + "v0.11.10,2026-11-08,js/app.js,4,\n"
        + 'v0.11.7,2026-11-01,js/app.js,3,"fix, again"\n'
        + "v0.3.19,2026-10-01,css/manager.css,5,\n"
    )
    assert format_history(parse_history(out)) == out


def test_parse_empty_and_format_empty():
    assert parse_history(None) == []
    assert parse_history("") == []
    assert format_history([]) == HEADER_LINE


def test_parse_collapses_duplicate_rows():
    text = HEADER_LINE + "v0.11.9,2026-11-09,js/app.js,4,\n" * 2
    assert parse_history(text) == [Entry("v0.11.9", "2026-11-09", "js/app.js", 4, "")]


def test_parse_rejects_bad_header():
    with pytest.raises(ValueError):
        parse_history("version,date,asset,v\n")


def test_parse_rejects_non_int_v():
    with pytest.raises(ValueError, match="line 2"):
        parse_history(HEADER_LINE + "v0.11.9,2026-11-09,js/app.js,four,\n")


@pytest.mark.parametrize(
    "base, head, expected",
    [
        ("v0.11.8", "v0.11.8", "v0.11.9"),
        ("v0.11.8", "v0.11.9", "v0.11.9"),
        ("v0.4.6-pre.2", "v0.4.6-pre.2", "v0.4.6-pre.3"),
        ("v0.11.8", "v0.12.0", "v0.12.0"),
    ],
)
def test_expected_version(base, head, expected):
    assert expected_version(base, head) == expected


def test_bumps_maps_changed_int_values():
    rows = [
        Row("js/app.js", 3, {4}, True, 4, "ok"),
        Row("js/a.js", 2, {1}, True, 3, "too-low"),
        Row("js/b.js", 2, {5}, True, 3, "too-high"),
        Row("css/x.css", 1, {1}, False, 1, "ok"),
        Row("js-deps/lib.min.js", None, {None}, True, 1, "bump"),
        Row("js/c.js", 2, {2, 3}, True, 3, "inconsistent"),
    ]
    assert bumps(rows) == {"js/app.js": 4, "js/a.js": 1, "js/b.js": 5}


def test_update_adds_row_for_bump():
    new, removed = update_history([BASE_ROW], [BASE_ROW], {"js/app.js": 4}, "v0.11.9", TODAY)
    assert removed == []
    assert len(new) == 2
    assert BASE_ROW in new
    assert Entry("v0.11.9", "2026-11-09", "js/app.js", 4, "") in new


def test_update_rerun_after_version_move_keeps_note():
    branch = Entry("v0.11.9", "2026-11-09", "js/app.js", 4, "my note")
    new, removed = update_history(
        [BASE_ROW], [BASE_ROW, branch], {"js/app.js": 4}, "v0.11.10", date(2026, 11, 10)
    )
    assert removed == []
    assert len(new) == 2
    assert BASE_ROW in new
    assert Entry("v0.11.10", "2026-11-09", "js/app.js", 4, "my note") in new


def test_update_removes_stale_branch_row():
    stale = Entry("v0.11.9", "2026-11-09", "js/app.js", 3, "old")
    new, removed = update_history([BASE_ROW], [BASE_ROW, stale], {}, "v0.11.9", TODAY)
    assert removed == [stale]
    assert new == [BASE_ROW]


def test_check_names_missing_row():
    expected = [
        "cache-bust-history.csv has no row for js/app.js ?v=4, run scripts/dev/cache_bust.py history"
    ]
    assert check_history([BASE_ROW], [BASE_ROW], {"js/app.js": 4}, "v0.11.9", False) == expected
    assert check_history([BASE_ROW], [BASE_ROW], {"js/app.js": 4}, "v0.11.9", True) == expected


def test_check_accepts_empty_note():
    head = [BASE_ROW, Entry("v0.11.9", "2026-11-09", "js/app.js", 4, "")]
    assert check_history([BASE_ROW], head, {"js/app.js": 4}, "v0.11.9", False) == []


def test_check_wrong_version_strict_only():
    head = [BASE_ROW, Entry("v0.11.8", "2026-11-09", "js/app.js", 4, "")]
    assert check_history([BASE_ROW], head, {"js/app.js": 4}, "v0.11.9", False) == [
        "cache-bust-history.csv row js/app.js ?v=4 says v0.11.8, expected v0.11.9"
    ]
    assert check_history([BASE_ROW], head, {"js/app.js": 4}, "v0.11.9", True) == []


def test_check_strict_flags_branch_row_without_bump():
    head = [BASE_ROW, Entry("v0.11.9", "2026-11-09", "js/app.js", 3, "")]
    assert check_history([BASE_ROW], head, {}, "v0.11.9", False) == [
        "cache-bust-history.csv row js/app.js ?v=3 has no matching bump, "
        "run scripts/dev/cache_bust.py history"
    ]
    assert check_history([BASE_ROW], head, {}, "v0.11.9", True) == []


def test_check_accepts_reversed_and_duplicated_rows():
    text = (
        HEADER_LINE
        + "v0.11.9,2026-11-09,js/app.js,4,\n"
        + "v0.11.8,2026-10-30,css/manager.css,5,\n"
        + "v0.11.9,2026-11-09,css/manager.css,6,\n"
        + "v0.11.9,2026-11-09,js/app.js,4,\n"
    )
    head = parse_history(text)
    assert check_history(
        [BASE_ROW], head, {"js/app.js": 4, "css/manager.css": 6}, "v0.11.9", False
    ) == []


def test_history_adds_rows_for_bumps(repo, capsys):
    root, base = repo
    edit(root, "static/js/app.js", "console.log(3);\n")
    write_marks(root, app=4)
    assert main(["history", "--base", base, "--root", str(root), "--today", "2026-11-09"]) == 0
    assert "v0.11.9,2026-11-09,js/app.js,4," in read_history(root)
    assert "added v0.11.9 js/app.js ?v=4" in capsys.readouterr().out


def test_history_rerun_after_version_move_keeps_note(repo):
    root, _ = repo
    write(root, "dashboard/VERSION", "v0.11.9\n")
    write_history(root, HEADER_LINE + "v0.11.8,2026-10-30,css/manager.css,5,\n")
    base = _commit(root, "base at v0.11.9")
    edit(root, "static/js/app.js", "console.log(3);\n")
    write_marks(root, app=4)
    write_history(
        root,
        HEADER_LINE
        + "v0.11.8,2026-10-30,css/manager.css,5,\n"
        + "v0.11.9,2026-11-09,js/app.js,4,my note\n",
    )
    write(root, "dashboard/VERSION", "v0.11.10\n")
    assert main(["history", "--base", base, "--root", str(root), "--today", "2026-11-10"]) == 0
    text = read_history(root)
    assert "v0.11.10,2026-11-09,js/app.js,4,my note\n" in text
    assert "v0.11.9" not in text
    assert "v0.11.8,2026-10-30,css/manager.css,5,\n" in text


def test_history_removes_stale_branch_row(repo, capsys):
    root, base = repo
    write_history(root, HEADER_LINE + "v0.11.9,2026-11-09,js/app.js,3,old note\n")
    assert main(["history", "--base", base, "--root", str(root), "--today", "2026-11-09"]) == 0
    assert "old note" not in read_history(root)
    out = capsys.readouterr().out
    assert "removed v0.11.9 js/app.js ?v=3" in out
    assert "old note" in out


def test_check_fails_without_history_row(repo, capsys):
    root, base = repo
    edit(root, "static/js/app.js", "console.log(3);\n")
    write_marks(root, app=4)
    assert run_check(root, base) == 1
    assert "has no row for js/app.js ?v=4" in capsys.readouterr().out


def test_check_cli_accepts_empty_note(repo):
    root, base = repo
    edit(root, "static/js/app.js", "console.log(3);\n")
    write_marks(root, app=4)
    with_history(root, base)
    assert "v0.11.9,2026-11-09,js/app.js,4,\n" in read_history(root)
    assert run_check(root, base) == 0


def test_check_fails_wrong_version_strict_only(repo, capsys):
    root, base = repo
    edit(root, "static/js/app.js", "console.log(3);\n")
    write_marks(root, app=4)
    write_history(root, HEADER_LINE + "v0.11.8,2026-11-09,js/app.js,4,\n")
    assert run_check(root, base) == 1
    assert "row js/app.js ?v=4 says v0.11.8, expected v0.11.9" in capsys.readouterr().out
    assert run_check(root, base, "--lenient") == 0


def test_check_accepts_union_merged_csv(repo):
    root, base = repo
    edit(root, "static/css/manager.css", "body { color: red; }\n")
    edit(root, "static/js/app.js", "console.log(3);\n")
    write_marks(root, css=(6, 6), app=4)
    # reversed order (js before css) and one row duplicated, as a union merge can leave it
    write_history(
        root,
        HEADER_LINE
        + "v0.11.9,2026-11-09,js/app.js,4,\n"
        + "v0.11.9,2026-11-09,css/manager.css,6,\n"
        + "v0.11.9,2026-11-09,js/app.js,4,\n",
    )
    assert run_check(root, base) == 0


OLD_HISTORY_ROWS = (
    "v0.3.19,2026-10-01,css/manager.css,5,\n"
    'v0.11.7,2026-11-01,js/app.js,3,"fix, again"\n'
)


def base_without_history(root):
    (root / HISTORY).unlink()
    return _commit(root, "base without history")


def test_bootstrap_csv_without_bumps_passes_check(repo):
    root, _ = repo
    base = base_without_history(root)
    write_history(root, HEADER_LINE + OLD_HISTORY_ROWS)
    assert run_check(root, base) == 0


def test_bootstrap_bump_without_row_fails_check(repo, capsys):
    root, _ = repo
    base = base_without_history(root)
    write_history(root, HEADER_LINE + OLD_HISTORY_ROWS)
    edit(root, "static/js/app.js", "console.log(3);\n")
    write_marks(root, app=4)
    assert run_check(root, base) == 1
    assert "has no row for js/app.js ?v=4" in capsys.readouterr().out


def test_bootstrap_history_keeps_old_rows(repo, capsys):
    root, _ = repo
    base = base_without_history(root)
    write_history(root, HEADER_LINE + OLD_HISTORY_ROWS)
    edit(root, "static/js/app.js", "console.log(3);\n")
    write_marks(root, app=4)
    assert main(["history", "--base", base, "--root", str(root), "--today", "2026-11-09"]) == 0
    text = read_history(root)
    assert "v0.3.19,2026-10-01,css/manager.css,5,\n" in text
    assert "v0.11.7,2026-11-01,js/app.js,3,\"fix, again\"\n" in text
    assert "v0.11.9,2026-11-09,js/app.js,4,\n" in text
    out = capsys.readouterr().out
    assert "removed" not in out
    assert "added v0.11.9 js/app.js ?v=4" in out
