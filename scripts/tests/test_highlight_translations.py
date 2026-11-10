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

EN = {"*": "Show the translated highlights"}


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
    write_translation(repo, 140, {"de": {"*": "zeigt die übersetzten Highlights"}, "en": EN})
    assert ht.main(["check", "--repo", str(repo), "--pr", "140"]) == 0
    assert "every highlight has a text in en, de" in capsys.readouterr().out


def test_check_fails_without_file_and_names_every_component(tmp_path, capsys):
    repo = make_repo(tmp_path)
    assert ht.main(["check", "--repo", str(repo), "--pr", "140"]) == 1
    out = capsys.readouterr().out
    assert "Dashboard" in out and "Installer" in out
    assert "show the translated highlights" in out
    assert "Highlights (de):" in out


def test_check_reports_component_without_own_line(tmp_path, capsys):
    repo = make_repo(tmp_path)
    write_translation(repo, 140, {"de": {"dashboard": "nur Dashboard"}, "en": EN})
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
    assert data == {"de": {"*": "alle", "installer": "nur Installer"}, "en": EN}
    assert ht.main(["sync", "--repo", str(repo), "--pr", "140", "--body-file", str(body)]) == 0
    assert "unchanged" in capsys.readouterr().out


def test_sync_without_block_adds_only_english(tmp_path):
    repo = make_repo(tmp_path)
    write_translation(repo, 140, {"de": {"*": "von Hand"}})
    body = tmp_path / "body.txt"
    body.write_text("Not needed. Only feat, fix, perf and breaking PRs are highlights.\n", encoding="utf-8")
    assert ht.main(["sync", "--repo", str(repo), "--pr", "140", "--body-file", str(body)]) == 0
    assert json.loads((repo / "changelog/highlights/140.json").read_text(encoding="utf-8")) == {
        "de": {"*": "von Hand"}, "en": EN}
    assert not (repo / "changelog/highlights/141.json").exists()


def test_sync_replaces_language_wholesale(tmp_path):
    repo = make_repo(tmp_path)
    write_translation(repo, 140, {"de": {"*": "alt", "dashboard": "veraltet"}})
    body = tmp_path / "body.txt"
    body.write_text("Highlights (de):\n- neu\n", encoding="utf-8")
    ht.main(["sync", "--repo", str(repo), "--pr", "140", "--body-file", str(body)])
    assert json.loads((repo / "changelog/highlights/140.json").read_text(encoding="utf-8")) == {
        "de": {"*": "neu"}, "en": EN}


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
    write_translation(repo, 140, {"de": {"*": "zeigt die übersetzten Highlights"}, "en": EN})
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


def test_validate_fails_with_a_broken_tour(tmp_path, capsys):
    repo = make_repo(tmp_path)
    write_translation(repo, 190, {"de": {"*": "Touren"}, "tour": {"*": {"steps": []}}})
    assert ht.main(["validate", "--repo", str(repo)]) == 1
    assert "#190 tour *: steps must be a non-empty list" in capsys.readouterr().out


TOUR_BODY = """Highlights (de):
- Text

## Tour

```json
{"*": {"exempt": "Nothing to show"}}
```
"""


def test_sync_copies_the_tour_from_the_body(tmp_path):
    repo = make_repo(tmp_path)
    body = tmp_path / "body.txt"
    body.write_text(TOUR_BODY, encoding="utf-8")
    assert ht.main(["sync", "--repo", str(repo), "--pr", "140", "--body-file", str(body)]) == 0
    written = json.loads((repo / "changelog/highlights/140.json").read_text(encoding="utf-8"))
    assert written["tour"] == {"*": {"exempt": "Nothing to show"}}
    assert written["de"] == {"*": "Text"}
    assert written["en"] == EN


def test_sync_without_a_tour_keeps_the_one_in_the_file(tmp_path):
    repo = make_repo(tmp_path)
    path = repo / "changelog/highlights/140.json"
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(json.dumps({"tour": {"*": {"exempt": "By hand"}}}), encoding="utf-8")
    body = tmp_path / "body.txt"
    body.write_text(TOUR_BODY.replace('{"*": {"exempt": "Nothing to show"}}', "none"), encoding="utf-8")
    assert ht.main(["sync", "--repo", str(repo), "--pr", "140", "--body-file", str(body)]) == 0
    written = json.loads(path.read_text(encoding="utf-8"))
    assert written["tour"] == {"*": {"exempt": "By hand"}}
    assert written["de"] == {"*": "Text"}
    assert written["en"] == EN


def test_sync_stops_on_a_broken_tour(tmp_path, capsys):
    repo = make_repo(tmp_path)
    body = tmp_path / "body.txt"
    body.write_text(TOUR_BODY.replace('{"*": {"exempt": "Nothing to show"}}', "{oops"), encoding="utf-8")
    assert ht.main(["sync", "--repo", str(repo), "--pr", "140", "--body-file", str(body)]) == 1
    assert "Tour section" in capsys.readouterr().out
    assert not (repo / "changelog/highlights/140.json").exists()


def test_check_fails_without_english_and_explains_the_copy(tmp_path, capsys):
    repo = make_repo(tmp_path)
    write_translation(repo, 140, {"de": {"*": "zeigt die übersetzten Highlights"}})
    assert ht.main(["check", "--repo", str(repo), "--pr", "140"]) == 1
    out = capsys.readouterr().out
    assert "no 'en' text" in out
    assert "copies the English text" in out
    assert "Highlights (de):" not in out


def test_english_texts_use_one_default_line(tmp_path):
    repo = make_repo(tmp_path)
    highlights = ht._highlights(repo, ht._table(repo))
    assert ht.english_texts(highlights, 140) == EN
    assert ht.english_texts(highlights, 141) == {}


def test_english_texts_keep_a_component_with_its_own_text(tmp_path):
    repo = make_repo(tmp_path)
    (repo / "installer/CHANGELOG.md").write_text(
        INSTALLER.replace("show the translated highlights", "installer only."), encoding="utf-8")
    highlights = ht._highlights(repo, ht._table(repo))
    assert ht.english_texts(highlights, 140) == {"*": "Show the translated highlights", "installer": "Installer only"}


def test_english_texts_refuse_two_texts_in_one_component(tmp_path):
    repo = make_repo(tmp_path)
    twice = INSTALLER + "\n## v0.4.0 (2026-10-01)\n\n### Fixes\n\n- another wording (#140)\n"
    (repo / "installer/CHANGELOG.md").write_text(twice, encoding="utf-8")
    with pytest.raises(ValueError, match="#140 installer"):
        ht.english_texts(ht._highlights(repo, ht._table(repo)), 140)


def test_sync_keeps_an_existing_english_text(tmp_path):
    repo = make_repo(tmp_path)
    write_translation(repo, 140, {"de": {"*": "x"}, "en": {"*": "Shortened by hand"}})
    body = tmp_path / "body.txt"
    body.write_text("Highlights (de):\n- x\n", encoding="utf-8")
    ht.main(["sync", "--repo", str(repo), "--pr", "140", "--body-file", str(body)])
    assert json.loads((repo / "changelog/highlights/140.json").read_text(encoding="utf-8")) == {
        "de": {"*": "x"}, "en": {"*": "Shortened by hand"}}


def test_sync_takes_english_from_the_block(tmp_path):
    repo = make_repo(tmp_path)
    write_translation(repo, 140, {"de": {"*": "x"}, "en": {"*": "Shortened by hand"}})
    body = tmp_path / "body.txt"
    body.write_text("Highlights (en):\n- from a block.\n- installer: just the installer\n", encoding="utf-8")
    ht.main(["sync", "--repo", str(repo), "--pr", "140", "--body-file", str(body)])
    assert json.loads((repo / "changelog/highlights/140.json").read_text(encoding="utf-8")) == {
        "de": {"*": "x"}, "en": {"*": "From a block", "installer": "Just the installer"}}


def test_sync_without_highlight_and_block_writes_nothing(tmp_path, capsys):
    repo = make_repo(tmp_path)
    body = tmp_path / "body.txt"
    body.write_text("Just a chore.\n", encoding="utf-8")
    assert ht.main(["sync", "--repo", str(repo), "--pr", "141", "--body-file", str(body)]) == 0
    assert not (repo / "changelog/highlights/141.json").exists()
    assert "file left as it is" in capsys.readouterr().out
