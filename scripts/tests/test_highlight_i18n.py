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


def _write(path, data):
    path.write_text(json.dumps(data, ensure_ascii=False), encoding="utf-8")


def _read(path):
    return json.loads(path.read_text(encoding="utf-8"))


def _snapshot(directory):
    return {path.name: path.read_text(encoding="utf-8") for path in directory.iterdir()}


def test_archive_is_loaded_and_per_pr_file_wins_per_language(tmp_path):
    archive = {
        "7": {"de": {"*": "alt"}, "fr": {"*": "ancien"}},
        "8": {"de": {"*": "acht"}},
    }
    (tmp_path / "archive-v0.json").write_text(json.dumps(archive), encoding="utf-8")
    (tmp_path / "7.json").write_text(json.dumps({"de": {"*": "neu"}}), encoding="utf-8")
    translations = hi.load_translations(tmp_path)
    assert translations == {
        7: {"de": {"*": "neu"}, "fr": {"*": "ancien"}},
        8: {"de": {"*": "acht"}},
    }
    assert hi.lookup(translations, 8, "dashboard", "de") == "acht"


def test_load_names_broken_archive(tmp_path):
    (tmp_path / "archive-v0.json").write_text("{broken", encoding="utf-8")
    with pytest.raises(ValueError, match="archive-v0.json"):
        hi.load_translations(tmp_path)


def test_load_rejects_non_numeric_archive_key(tmp_path):
    (tmp_path / "archive-v0.json").write_text(json.dumps({"abc": {"de": {"*": "x"}}}), encoding="utf-8")
    with pytest.raises(ValueError, match="archive-v0.json"):
        hi.load_translations(tmp_path)


def test_load_order_archive_then_release_then_pr(tmp_path):
    _write(tmp_path / "archive-v0.json", {"148": {"de": {"*": "archiv"}}})
    _write(tmp_path / "v0.10.0.json", {"148": {"de": {"*": "release"}}})
    assert hi.load_translations(tmp_path)[148] == {"de": {"*": "release"}}
    _write(tmp_path / "148.json", {"de": {"*": "pr"}})
    assert hi.load_translations(tmp_path)[148] == {"de": {"*": "pr"}}


def test_load_reads_every_major_archive(tmp_path):
    _write(tmp_path / "archive-v0.json", {"7": {"de": {"*": "null"}}})
    _write(tmp_path / "archive-v1.json", {"300": {"de": {"*": "drei"}}})
    assert set(hi.load_translations(tmp_path)) == {7, 300}


def test_load_sorts_release_files_by_version(tmp_path):
    _write(tmp_path / "v0.9.0.json", {"5": {"de": {"*": "alt"}}})
    _write(tmp_path / "v0.10.0.json", {"5": {"de": {"*": "neu"}}})
    assert hi.load_translations(tmp_path)[5] == {"de": {"*": "neu"}}


def test_archive_moves_the_previous_release_into_its_major_archive(tmp_path):
    _write(tmp_path / "archive-v0.json", {"7": {"de": {"*": "sieben"}}})
    _write(tmp_path / "v0.10.0.json", {"148": {"de": {"*": "achtundvierzig"}}})
    _write(tmp_path / "152.json", {"de": {"*": "zweiundfuenfzig"}})
    assert hi.archive_files(tmp_path, "v0.11.0") == [152]
    assert list(_read(tmp_path / "archive-v0.json")) == ["7", "148"]
    assert list(_read(tmp_path / "v0.11.0.json")) == ["152"]
    assert not (tmp_path / "v0.10.0.json").exists()
    assert not (tmp_path / "152.json").exists()


def test_archive_across_a_major_version(tmp_path):
    _write(tmp_path / "v0.12.0.json", {"160": {"de": {"*": "sechzig"}}})
    _write(tmp_path / "161.json", {"de": {"*": "einundsechzig"}})
    assert hi.archive_files(tmp_path, "v1.0.0") == [161]
    assert list(_read(tmp_path / "archive-v0.json")) == ["160"]
    assert list(_read(tmp_path / "v1.0.0.json")) == ["161"]
    assert not (tmp_path / "archive-v1.json").exists()


def test_archive_without_pr_files_changes_nothing(tmp_path):
    _write(tmp_path / "archive-v0.json", {"7": {"de": {"*": "sieben"}}})
    _write(tmp_path / "v0.10.0.json", {"148": {"de": {"*": "achtundvierzig"}}})
    before = _snapshot(tmp_path)
    assert hi.archive_files(tmp_path, "v0.11.0") == []
    assert _snapshot(tmp_path) == before


def test_archive_keeps_details(tmp_path):
    details = {"*": {"en": "Switch channels", "de": "Schaltkanäle", "image": "switch.png"}}
    _write(tmp_path / "152.json", {"de": {"*": "Schaltkanäle"}, "details": details})
    assert hi.archive_files(tmp_path, "v0.11.0") == [152]
    assert _read(tmp_path / "v0.11.0.json")["152"]["details"] == details
    assert hi.load_translations(tmp_path)[152]["details"] == details


def test_archive_rejects_a_prerelease_tag(tmp_path):
    _write(tmp_path / "152.json", {"de": {"*": "Text"}})
    with pytest.raises(ValueError):
        hi.archive_files(tmp_path, "v0.11.0-rc1")
    assert (tmp_path / "152.json").exists()


def test_archive_aborts_before_writing_on_a_broken_file(tmp_path):
    _write(tmp_path / "archive-v0.json", {"7": {"de": {"*": "sieben"}}})
    (tmp_path / "v0.10.0.json").write_text("{broken", encoding="utf-8")
    _write(tmp_path / "152.json", {"de": {"*": "zweiundfuenfzig"}})
    before = _snapshot(tmp_path)
    with pytest.raises(ValueError, match="v0.10.0.json"):
        hi.archive_files(tmp_path, "v0.11.0")
    assert _snapshot(tmp_path) == before


def test_archive_files_without_files_writes_nothing(tmp_path):
    assert hi.archive_files(tmp_path, "v0.11.0") == []
    assert not (tmp_path / "v0.11.0.json").exists()


def test_archive_files_broken_file_changes_nothing(tmp_path):
    (tmp_path / "3.json").write_text(json.dumps({"de": {"*": "ok"}}), encoding="utf-8")
    (tmp_path / "4.json").write_text("{broken", encoding="utf-8")
    with pytest.raises(ValueError, match="4.json"):
        hi.archive_files(tmp_path, "v0.11.0")
    assert (tmp_path / "3.json").exists()
    assert not (tmp_path / "v0.11.0.json").exists()


def test_archive_files_folds_per_pr_files_and_deletes_them(tmp_path):
    _write(tmp_path / "archive-v0.json", {"7": {"de": {"*": "alt"}}})
    _write(tmp_path / "7.json", {"de": {"*": "neu"}})
    _write(tmp_path / "5.json", {"de": {"*": "fuenf"}})
    (tmp_path / "README.md").write_text("keep", encoding="utf-8")

    assert hi.archive_files(tmp_path, "v0.11.0") == [5, 7]
    assert not (tmp_path / "5.json").exists()
    assert not (tmp_path / "7.json").exists()
    assert (tmp_path / "README.md").exists()
    assert hi.load_translations(tmp_path) == {
        5: {"de": {"*": "fuenf"}},
        7: {"de": {"*": "neu"}},
    }
    assert list(_read(tmp_path / "v0.11.0.json")) == ["5", "7"]


def test_archive_files_second_run_folds_into_the_existing_tag_file(tmp_path):
    _write(tmp_path / "5.json", {"de": {"*": "fuenf"}})
    assert hi.archive_files(tmp_path, "v0.11.0") == [5]
    _write(tmp_path / "9.json", {"de": {"*": "neun"}})
    assert hi.archive_files(tmp_path, "v0.11.0") == [9]
    assert list(_read(tmp_path / "v0.11.0.json")) == ["5", "9"]


def test_load_rejects_non_object_pr_file(tmp_path):
    (tmp_path / "7.json").write_text("[]", encoding="utf-8")
    with pytest.raises(ValueError, match="7.json"):
        hi.load_translations(tmp_path)


def test_load_rejects_non_object_release_file(tmp_path):
    (tmp_path / "v0.10.0.json").write_text("[]", encoding="utf-8")
    with pytest.raises(ValueError, match="v0.10.0.json"):
        hi.load_translations(tmp_path)


def test_load_rejects_non_object_archive_file(tmp_path):
    (tmp_path / "archive-v0.json").write_text('"text"', encoding="utf-8")
    with pytest.raises(ValueError, match="archive-v0.json"):
        hi.load_translations(tmp_path)
