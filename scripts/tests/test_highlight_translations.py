import json
import sys
from pathlib import Path

import pytest

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "build"))
import highlight_translations as ht

CHANGELOG = """# Changelog

## v0.9.11 (2026-10-14)

### Features

- **dashboard:** show the translated highlights (#140)

### Chores

- tidy things (#141)
- **dashboard:** an inline commit (abc1234)
"""

INSTALLER = """# Changelog

## v0.4.1 (2026-10-14)

### Features

- show the translated highlights (#140)
"""


def make_repo(tmp_path):
    (tmp_path / "scripts/version").mkdir(parents=True)
    table = {"components": [
        {"id": "dashboard", "label": "Dashboard", "kind": "app", "target": "dashboard",
         "version_file": "dashboard/VERSION", "changelog": "dashboard/CHANGELOG.md", "bundle": True},
        {"id": "installer", "label": "Installer", "kind": "app", "target": "installer",
         "version_file": "installer/VERSION", "changelog": "installer/CHANGELOG.md", "bundle": False},
    ]}
    (tmp_path / "scripts/version/components.json").write_text(json.dumps(table), encoding="utf-8")
    for name, text in (("dashboard", CHANGELOG), ("installer", INSTALLER)):
        (tmp_path / name).mkdir()
        (tmp_path / name / "CHANGELOG.md").write_text(text, encoding="utf-8")
    return tmp_path


def write_translation(repo, pr, data):
    directory = repo / "changelog/highlights"
    directory.mkdir(parents=True, exist_ok=True)
    (directory / f"{pr}.json").write_text(json.dumps(data), encoding="utf-8")


def test_check_passes_with_default_text(tmp_path, capsys):
    repo = make_repo(tmp_path)
    write_translation(repo, 140, {"de": {"*": "zeigt die übersetzten Highlights"}})
    assert ht.main(["check", "--repo", str(repo), "--pr", "140"]) == 0


def test_check_fails_without_file_and_names_every_component(tmp_path, capsys):
    repo = make_repo(tmp_path)
    assert ht.main(["check", "--repo", str(repo), "--pr", "140"]) == 1
    out = capsys.readouterr().out
    assert "Dashboard" in out and "Installer" in out
    assert "show the translated highlights" in out
    assert "Highlights (de):" in out


def test_check_reports_component_without_own_line(tmp_path, capsys):
    repo = make_repo(tmp_path)
    write_translation(repo, 140, {"de": {"dashboard": "nur Dashboard"}})
    assert ht.main(["check", "--repo", str(repo), "--pr", "140"]) == 1
    out = capsys.readouterr().out
    assert "Installer" in out and "Dashboard" not in out


def test_check_ignores_non_highlight_pr(tmp_path):
    repo = make_repo(tmp_path)
    assert ht.main(["check", "--repo", str(repo), "--pr", "141"]) == 0
    assert ht.main(["check", "--repo", str(repo), "--pr", "999"]) == 0


def test_sync_writes_file_from_body(tmp_path, capsys):
    repo = make_repo(tmp_path)
    body = tmp_path / "body.txt"
    body.write_text("Text\n\n```text\nHighlights (de):\n- alle\n- installer: nur Installer\n```\n", encoding="utf-8")
    assert ht.main(["sync", "--repo", str(repo), "--pr", "140", "--body-file", str(body)]) == 0
    data = json.loads((repo / "changelog/highlights/140.json").read_text(encoding="utf-8"))
    assert data == {"de": {"*": "alle", "installer": "nur Installer"}}
    assert ht.main(["sync", "--repo", str(repo), "--pr", "140", "--body-file", str(body)]) == 0
    assert "unchanged" in capsys.readouterr().out


def test_sync_without_block_keeps_file(tmp_path):
    repo = make_repo(tmp_path)
    write_translation(repo, 140, {"de": {"*": "von Hand"}})
    body = tmp_path / "body.txt"
    body.write_text("Not needed. Only feat, fix, perf and breaking PRs are highlights.\n", encoding="utf-8")
    assert ht.main(["sync", "--repo", str(repo), "--pr", "140", "--body-file", str(body)]) == 0
    assert json.loads((repo / "changelog/highlights/140.json").read_text(encoding="utf-8")) == {"de": {"*": "von Hand"}}
    assert not (repo / "changelog/highlights/141.json").exists()


def test_sync_replaces_language_wholesale(tmp_path):
    repo = make_repo(tmp_path)
    write_translation(repo, 140, {"de": {"*": "alt", "dashboard": "veraltet"}})
    body = tmp_path / "body.txt"
    body.write_text("Highlights (de):\n- neu\n", encoding="utf-8")
    ht.main(["sync", "--repo", str(repo), "--pr", "140", "--body-file", str(body)])
    assert json.loads((repo / "changelog/highlights/140.json").read_text(encoding="utf-8")) == {"de": {"*": "neu"}}


def test_archive_command_needs_the_tag(tmp_path):
    repo = make_repo(tmp_path)
    with pytest.raises(SystemExit) as exit_info:
        ht.main(["archive", "--repo", str(repo)])
    assert exit_info.value.code == 2


def test_archive_command_folds_into_the_tag_file(tmp_path, capsys):
    repo = make_repo(tmp_path)
    write_translation(repo, 140, {"de": {"*": "zeigt die übersetzten Highlights"}})
    assert ht.main(["archive", "--repo", str(repo), "--tag", "v0.11.0"]) == 0
    assert "archived 1 files" in capsys.readouterr().out
    assert not (repo / "changelog/highlights/140.json").exists()
    release = json.loads((repo / "changelog/highlights/v0.11.0.json").read_text(encoding="utf-8"))
    assert "140" in release


def test_archive_command_with_nothing_to_do(tmp_path, capsys):
    repo = make_repo(tmp_path)
    assert ht.main(["archive", "--repo", str(repo), "--tag", "v0.11.0"]) == 0
    assert "nothing to archive" in capsys.readouterr().out


def test_validate_passes_without_details(tmp_path, capsys):
    repo = make_repo(tmp_path)
    write_translation(repo, 140, {"de": {"*": "zeigt die übersetzten Highlights"}})
    assert ht.main(["validate", "--repo", str(repo)]) == 0
    assert capsys.readouterr().out == ""


def test_validate_fails_with_a_missing_image(tmp_path, capsys):
    repo = make_repo(tmp_path)
    details = {"en": "Switch channels", "de": "Schaltkanäle", "image": "fehlt.png"}
    write_translation(repo, 148, {"de": {"*": "Schaltkanäle"}, "details": {"*": details}})
    assert ht.main(["validate", "--repo", str(repo)]) == 1
    out = capsys.readouterr().out
    assert "#148" in out
    assert "fehlt.png" in out


def test_validate_checks_every_file_not_only_the_pr(tmp_path, capsys):
    repo = make_repo(tmp_path)
    details = {"en": "Switch channels", "de": "Schaltkanäle", "image": "fehlt.png"}
    write_translation(repo, 141, {"de": {"*": "Text"}, "details": {"*": details}})
    assert ht.main(["validate", "--repo", str(repo)]) == 1
    assert "#141" in capsys.readouterr().out
