import json
import sys
from pathlib import Path

import pytest

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "build"))
import highlight_i18n as hi

TARGETS = {"dashboard", "installer", "common"}

BODY = """## Squash-merge commit message

```text
Body.

Highlights:
- The battery state survives a crash.
- dashboard: The version page opens on the highlights.
```

## Highlight translations

```text
Highlights (de):
- Der Akkustand übersteht einen Absturz.
- dashboard: Die Versionsseite öffnet mit den Highlights.
- service:battery_soc: Nur für Battery SoC.
- unknown: bleibt ein Text für alle
```
"""


def test_parse_blocks_reads_default_and_targeted_lines():
    blocks = hi.parse_blocks(BODY, TARGETS)
    assert blocks == {"de": {
        "*": "Der Akkustand übersteht einen Absturz.",
        "dashboard": "Die Versionsseite öffnet mit den Highlights.",
        "service:battery_soc": "Nur für Battery SoC.",
    }}


def test_parse_blocks_first_line_wins_and_english_block_is_ignored():
    body = "Highlights:\n- english\n\nHighlights (de):\n- erste\n- zweite\n"
    assert hi.parse_blocks(body, TARGETS) == {"de": {"*": "erste"}}


def test_parse_blocks_without_block_or_with_empty_block():
    assert hi.parse_blocks("no block here", TARGETS) == {}
    assert hi.parse_blocks("Highlights (de):\nNot a list line\n", TARGETS) == {}


def test_load_and_lookup_fall_back_to_default(tmp_path):
    (tmp_path / "130.json").write_text(json.dumps({"de": {"*": "alle", "dashboard": "nur dashboard"}}), encoding="utf-8")
    (tmp_path / "README.md").write_text("ignored", encoding="utf-8")
    translations = hi.load_translations(tmp_path)
    assert list(translations) == [130]
    assert hi.lookup(translations, 130, "dashboard", "de") == "nur dashboard"
    assert hi.lookup(translations, 130, "installer", "de") == "alle"
    assert hi.lookup(translations, 130, "installer", "fr") is None
    assert hi.lookup(translations, 131, "installer", "de") is None
    assert hi.entry_i18n(translations, 130, "installer") == {"de": "alle"}
    assert hi.entry_i18n(translations, 131, "installer") == {}


def test_load_missing_directory_is_empty(tmp_path):
    assert hi.load_translations(tmp_path / "nope") == {}


def test_load_names_broken_file(tmp_path):
    (tmp_path / "7.json").write_text("{broken", encoding="utf-8")
    with pytest.raises(ValueError, match="7.json"):
        hi.load_translations(tmp_path)


def test_write_pr_file_merges_languages_and_reports_change(tmp_path):
    assert hi.write_pr_file(tmp_path, 5, {"de": {"*": "eins"}}) is True
    assert hi.write_pr_file(tmp_path, 5, {"de": {"*": "eins"}}) is False
    (tmp_path / "5.json").write_text(json.dumps({"fr": {"*": "un"}, "de": {"*": "alt", "dashboard": "x"}}), encoding="utf-8")
    assert hi.write_pr_file(tmp_path, 5, {"de": {"*": "neu"}}) is True
    data = json.loads((tmp_path / "5.json").read_text(encoding="utf-8"))
    assert data == {"de": {"*": "neu"}, "fr": {"*": "un"}}
    assert (tmp_path / "5.json").read_text(encoding="utf-8").endswith("}\n")


def test_entry_i18n_ignores_the_details_key():
    translations = {5: {"de": {"*": "x"}, "details": {"*": {"en": "e", "de": "d"}}}}
    assert hi.entry_i18n(translations, 5, "installer") == {"de": "x"}


def test_targets_from_table():
    table = {"components": [{"target": "dashboard"}, {"target": "service:shelly"}]}
    assert hi.targets_from_table(table) == {"dashboard", "service:shelly"}


def test_archive_is_loaded_and_per_pr_file_wins_per_language(tmp_path):
    archive = {
        "7": {"de": {"*": "alt"}, "fr": {"*": "ancien"}},
        "8": {"de": {"*": "acht"}},
    }
    (tmp_path / "archive.json").write_text(json.dumps(archive), encoding="utf-8")
    (tmp_path / "7.json").write_text(json.dumps({"de": {"*": "neu"}}), encoding="utf-8")
    translations = hi.load_translations(tmp_path)
    assert translations == {
        7: {"de": {"*": "neu"}, "fr": {"*": "ancien"}},
        8: {"de": {"*": "acht"}},
    }
    assert hi.lookup(translations, 8, "dashboard", "de") == "acht"


def test_load_names_broken_archive(tmp_path):
    (tmp_path / "archive.json").write_text("{broken", encoding="utf-8")
    with pytest.raises(ValueError, match="archive.json"):
        hi.load_translations(tmp_path)


def test_load_rejects_non_numeric_archive_key(tmp_path):
    (tmp_path / "archive.json").write_text(json.dumps({"abc": {"de": {"*": "x"}}}), encoding="utf-8")
    with pytest.raises(ValueError, match="archive.json"):
        hi.load_translations(tmp_path)


def test_archive_files_folds_per_pr_files_and_deletes_them(tmp_path):
    (tmp_path / "archive.json").write_text(
        json.dumps({"7": {"de": {"*": "alt"}}}), encoding="utf-8")
    (tmp_path / "7.json").write_text(json.dumps({"de": {"*": "neu"}}), encoding="utf-8")
    (tmp_path / "5.json").write_text(json.dumps({"de": {"*": "fuenf"}}), encoding="utf-8")
    (tmp_path / "README.md").write_text("keep", encoding="utf-8")

    assert hi.archive_files(tmp_path) == [5, 7]
    assert not (tmp_path / "5.json").exists()
    assert not (tmp_path / "7.json").exists()
    assert (tmp_path / "README.md").exists()
    assert hi.load_translations(tmp_path) == {
        5: {"de": {"*": "fuenf"}},
        7: {"de": {"*": "neu"}},
    }
    keys = list(json.loads((tmp_path / "archive.json").read_text(encoding="utf-8")))
    assert keys == ["5", "7"]


def test_archive_files_without_files_writes_nothing(tmp_path):
    assert hi.archive_files(tmp_path) == []
    assert not (tmp_path / "archive.json").exists()


def test_archive_files_broken_file_changes_nothing(tmp_path):
    (tmp_path / "3.json").write_text(json.dumps({"de": {"*": "ok"}}), encoding="utf-8")
    (tmp_path / "4.json").write_text("{broken", encoding="utf-8")
    with pytest.raises(ValueError, match="4.json"):
        hi.archive_files(tmp_path)
    assert (tmp_path / "3.json").exists()
    assert not (tmp_path / "archive.json").exists()
