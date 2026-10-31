import json
import sys
from pathlib import Path

import pytest

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "build"))
import highlight_i18n as hi
import highlight_tours as ht

STEP = {
    "panel": "devices-panel", "page": "users", "target": "favorites",
    "device": "phone", "need": "view_devices",
    "title": {"en": "Favourites", "de": "Favoriten"},
    "text": {"en": "Star a device", "de": "Markiere ein Gerät"},
}


def tours(entry, key="*"):
    return {190: {"tour": {key: entry}}}


def test_full_step_is_valid():
    assert ht.validate(tours({"steps": [STEP]})) == []


def test_english_is_required():
    step = {**STEP, "text": {"de": "Markiere ein Gerät"}}
    assert ht.validate(tours({"steps": [step]})) == ["#190 tour *: step 1: text for en is missing"]


def test_every_language_of_highlight_i18n_is_required(monkeypatch):
    monkeypatch.setattr(hi, "LANGUAGES", ("de", "fr"))
    assert ht.validate(tours({"steps": [STEP]})) == [
        "#190 tour *: step 1: title for fr is missing",
        "#190 tour *: step 1: text for fr is missing",
    ]


def test_empty_text_is_reported():
    step = {**STEP, "title": {"en": " ", "de": "Favoriten"}}
    assert ht.validate(tours({"steps": [step]})) == ["#190 tour *: step 1: title for en is empty"]


def test_unknown_device_is_reported():
    step = {**STEP, "device": "tablet"}
    assert ht.validate(tours({"steps": [step]})) == [
        "#190 tour *: step 1: device must be one of any, phone, desktop"]


def test_missing_target_is_reported():
    step = {k: v for k, v in STEP.items() if k != "target"}
    assert ht.validate(tours({"steps": [step]})) == ["#190 tour *: step 1: target must be a data-tour id"]


def test_unknown_step_key_is_reported():
    step = {**STEP, "selector": ".x"}
    assert ht.validate(tours({"steps": [step]})) == ["#190 tour *: step 1: unknown key selector"]


def test_empty_steps_without_exempt_are_reported():
    assert ht.validate(tours({"steps": []})) == [
        "#190 tour *: steps must be a non-empty list, or set exempt with a reason"]


def test_exempt_needs_a_reason():
    assert ht.validate(tours({"exempt": "  "})) == ["#190 tour *: exempt needs a reason"]


def test_exempt_with_reason_is_valid():
    assert ht.validate(tours({"exempt": "Only the backend changed"})) == []


def test_exempt_stands_alone():
    assert ht.validate(tours({"exempt": "Backend", "steps": [STEP]})) == [
        "#190 tour *: exempt stands alone, without steps"]


def test_tour_must_be_an_object_keyed_by_component():
    assert ht.validate({190: {"tour": [STEP]}}) == ["#190 tour: must be an object keyed by component"]


def test_entries_without_tour_are_skipped():
    assert ht.validate({190: {"de": {"*": "Text"}}}) == []


def test_resolve_fills_the_default_device():
    step = {k: v for k, v in STEP.items() if k not in ("device", "panel", "page", "need")}
    assert ht.resolve(tours({"steps": [step]}), 190, "dashboard") == {"steps": [{
        "target": "favorites", "device": "any",
        "title": {"en": "Favourites", "de": "Favoriten"},
        "text": {"en": "Star a device", "de": "Markiere ein Gerät"},
    }]}


def test_resolve_keeps_panel_page_and_need():
    assert ht.resolve(tours({"steps": [STEP]}), 190, "dashboard") == {"steps": [STEP]}


def test_resolve_star_applies_to_every_component():
    data = tours({"steps": [STEP]})
    assert ht.resolve(data, 190, "installer") == {"steps": [STEP]}


def test_resolve_target_key_replaces_star_wholesale():
    data = {190: {"tour": {"*": {"steps": [STEP]}, "installer": {"exempt": "No web UI"}}}}
    assert ht.resolve(data, 190, "installer") == {"steps": [], "exempt": "No web UI"}
    assert ht.resolve(data, 190, "dashboard") == {"steps": [STEP]}


def test_resolve_without_tour_is_none():
    assert ht.resolve({190: {"de": {"*": "Text"}}}, 190, "dashboard") is None
    assert ht.resolve({190: {"tour": {"installer": {"exempt": "x"}}}}, 190, "dashboard") is None


def test_resolve_raises_on_a_broken_tour():
    with pytest.raises(ValueError, match=r"#190 tour \*: steps must be"):
        ht.resolve(tours({"steps": []}), 190, "dashboard")


CHANGELOG = """# Changelog

## v0.12.0 (2026-11-01)

### Features

- **onboarding:** tours in the pipeline (#192)
- **dashboard:** an older highlight (#150)

### Chores

- **dashboard:** not a highlight (#193)
"""

INSTALLER_CHANGELOG = """# Changelog

## v0.5.0 (2026-11-01)

### Features

- installer only (#194)
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
    for name, text in (("dashboard", CHANGELOG), ("installer", INSTALLER_CHANGELOG)):
        (tmp_path / name).mkdir()
        (tmp_path / name / "CHANGELOG.md").write_text(text, encoding="utf-8")
    (tmp_path / "changelog/highlights").mkdir(parents=True)
    return tmp_path


def write_file(repo, name, data):
    (repo / "changelog/highlights" / name).write_text(json.dumps(data), encoding="utf-8")


def test_check_fails_for_a_web_ui_highlight_without_tour(tmp_path, capsys):
    repo = make_repo(tmp_path)
    write_file(repo, "192.json", {"de": {"*": "Touren in der Pipeline"}})
    assert ht.main(["check", "--repo", str(repo), "--pr", "192"]) == 1
    out = capsys.readouterr().out
    assert 'PR #192, Dashboard v0.12.0: no tour for "tours in the pipeline"' in out
    assert "exempt" in out


def test_check_fails_without_any_file(tmp_path):
    repo = make_repo(tmp_path)
    assert ht.main(["check", "--repo", str(repo), "--pr", "192"]) == 1


def test_check_passes_with_a_tour(tmp_path):
    repo = make_repo(tmp_path)
    write_file(repo, "192.json", {"tour": {"*": {"steps": [STEP]}}})
    assert ht.main(["check", "--repo", str(repo), "--pr", "192"]) == 0


def test_check_passes_with_exempt(tmp_path):
    repo = make_repo(tmp_path)
    write_file(repo, "192.json", {"tour": {"dashboard": {"exempt": "Build scripts only"}}})
    assert ht.main(["check", "--repo", str(repo), "--pr", "192"]) == 0


def test_check_tour_for_another_component_does_not_count(tmp_path):
    repo = make_repo(tmp_path)
    write_file(repo, "192.json", {"tour": {"installer": {"exempt": "No web UI"}}})
    assert ht.main(["check", "--repo", str(repo), "--pr", "192"]) == 1


def test_check_ignores_components_without_web_ui(tmp_path):
    repo = make_repo(tmp_path)
    assert ht.main(["check", "--repo", str(repo), "--pr", "194"]) == 0


def test_check_ignores_entries_that_are_no_highlight(tmp_path):
    repo = make_repo(tmp_path)
    assert ht.main(["check", "--repo", str(repo), "--pr", "193"]) == 0


def test_check_skips_prs_before_the_cutoff(tmp_path, capsys):
    repo = make_repo(tmp_path)
    assert ht.TOUR_REQUIRED_FROM_PR == 186
    assert ht.main(["check", "--repo", str(repo), "--pr", "150"]) == 0
    assert "no tour required" in capsys.readouterr().out


def test_check_ignores_archived_highlights(tmp_path):
    repo = make_repo(tmp_path)
    write_file(repo, "192.json", {"tour": {"*": {"steps": [STEP]}}})
    write_file(repo, "v0.11.0.json", {"190": {"de": {"*": "ohne Tour"}}})
    write_file(repo, "archive-v0.json", {"189": {"de": {"*": "ohne Tour"}}})
    assert ht.main(["check", "--repo", str(repo), "--pr", "192"]) == 0
