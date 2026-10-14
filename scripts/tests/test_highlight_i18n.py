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


def test_targets_from_table():
    table = {"components": [{"target": "dashboard"}, {"target": "service:shelly"}]}
    assert hi.targets_from_table(table) == {"dashboard", "service:shelly"}
