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
