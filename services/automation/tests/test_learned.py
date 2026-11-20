"""Tests fuer learned.py: Bezugsangaben auf Lernwerte, Klemmen und Ersatzwerte.

Die Vektoren in learned_vectors.json liest auch die Dashboard-Seite, daher
bleiben die Erwartungen dort die Quelle der Wahrheit.
Ausfuehren mit `.venv/bin/pytest services/automation/tests` (Projekt-venv).
"""

import json
import sys
from pathlib import Path

import pytest

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

import learned

VECTORS = json.loads((Path(__file__).parent / "learned_vectors.json").read_text(encoding="utf-8"))


@pytest.mark.parametrize("vector", VECTORS, ids=[v["name"] for v in VECTORS])
def test_vector(vector):
    ref = vector["ref"]
    now = vector["now"]
    if vector["kind"] == "state":
        cond = {"type": "learned_state", "equals": "quiet", **ref}
        topic_values = {}
        if vector["payload"] is not None:
            topic_values[learned.learned_topic(ref["learned"])] = vector["payload"]
        result = learned.state_value(cond, topic_values, now)
    else:
        result = learned.resolve_ref(ref, vector["kind"], vector["payload"], now)
    assert result == (vector["expect"]["value"], vector["expect"]["source"])


def _errors(ref, kind="time"):
    errors = []
    learned.validate_ref(ref, kind, "rules[0].start", errors)
    return errors


def test_validate_accepts_midnight_arc():
    assert _errors({"learned": "site/day_start", "min": "23:00", "max": "01:00", "fallback": "00:00"}) == []


def test_validate_rejects_missing_min():
    assert _errors({"learned": "site/day_start", "max": "01:00", "fallback": "00:00"})


def test_validate_rejects_time_fallback_outside_arc():
    assert _errors({"learned": "site/day_start", "min": "23:00", "max": "01:00", "fallback": "12:00"})


def test_validate_rejects_number_min_above_max():
    ref = {"learned": "device/pump_1/usual_runtime_s", "min": 100, "max": 0, "fallback": 50}
    assert _errors(ref, kind="number")


def test_validate_rejects_number_fallback_outside_range():
    ref = {"learned": "device/pump_1/usual_runtime_s", "min": 0, "max": 100, "fallback": 150}
    assert _errors(ref, kind="number")


def test_validate_rejects_unknown_key():
    ref = {"learned": "site/day_start", "min": "23:00", "max": "01:00", "fallback": "00:00", "extra": 1}
    assert _errors(ref)


def test_validate_rejects_path_traversal_key():
    ref = {"learned": "../x", "min": "23:00", "max": "01:00", "fallback": "00:00"}
    assert _errors(ref)


def test_validate_rejects_fractional_time_offset():
    ref = {"learned": "site/day_start", "min": "23:00", "max": "01:00", "fallback": "00:00", "offset": 1.5}
    assert _errors(ref)


def test_validate_rejects_time_offset_beyond_half_day():
    ref = {"learned": "site/day_start", "min": "23:00", "max": "01:00", "fallback": "00:00", "offset": 721}
    assert _errors(ref)


def test_validate_rejects_non_positive_max_age():
    ref = {"learned": "site/day_start", "min": "23:00", "max": "01:00", "fallback": "00:00", "max_age_s": 0}
    assert _errors(ref)


def test_is_ref_detects_learned_key():
    assert learned.is_ref({"learned": "site/day_start"})
    assert not learned.is_ref("23:00")
    assert not learned.is_ref({"min": 0})


def test_field_kind_splits_time_and_number_fields():
    assert learned.field_kind("start") == "time"
    assert learned.field_kind("end") == "time"
    assert learned.field_kind("threshold") == "number"
    assert learned.field_kind("hold_seconds") == "number"


def test_resolve_condition_replaces_only_learned_fields():
    cond = {
        "type": "time_window",
        "start": {"learned": "site/day_start", "min": "23:00", "max": "01:00", "fallback": "00:00", "offset": 5},
        "end": "22:00",
    }
    payload = "{\"value\":\"23:58\",\"n\":4,\"at\":1794000000}"
    resolved, sources = learned.resolve_condition(
        cond, {learned.learned_topic("site/day_start"): payload}, 1794000060)
    assert resolved["start"] == "00:03"
    assert resolved["end"] == "22:00"
    assert sources == {"start": "learned"}
    assert cond["start"]["learned"] == "site/day_start"


def test_cond_learned_keys_collects_state_and_field_refs():
    cond = {
        "type": "learned_state",
        "learned": "site/house_state",
        "equals": "quiet",
        "fallback": "active",
        "hold_seconds": {"learned": "device/pump_1/usual_on", "min": 0, "max": 600, "fallback": 60},
    }
    assert learned.cond_learned_keys(cond) == {"site/house_state", "device/pump_1/usual_on"}
    assert learned.cond_learned_keys({"type": "chain_value", "field": "x"}) == set()


def test_resolve_ref_survives_deeply_nested_payload():
    ref = {"learned": "device/pump_1/usual_runtime_s", "min": 0, "max": 100, "fallback": 50}
    payload = "[" * 100000 + "]" * 100000
    assert learned.resolve_ref(ref, "number", payload, 1794000060) == (50, "fallback")


def test_resolve_ref_survives_integer_too_large_for_float():
    ref = {"learned": "device/pump_1/usual_runtime_s", "min": 0, "max": 100, "fallback": 50}
    payload = '{"value":' + "9" * 400 + ',"at":1794000000}'
    assert learned.resolve_ref(ref, "number", payload, 1794000060) == (50, "fallback")


def test_state_value_survives_pathological_payloads():
    cond = {"type": "learned_state", "learned": "site/house_state", "equals": "quiet", "fallback": "active"}
    topic = learned.learned_topic("site/house_state")
    for payload in ("[" * 100000 + "]" * 100000, '{"value":"quiet","n":1,"at":' + "9" * 400 + "}"):
        assert learned.state_value(cond, {topic: payload}, 1794000060) == ("active", "fallback")


@pytest.mark.parametrize("value", ['5', '""'])
def test_state_value_fresh_payload_with_non_text_or_empty_value_is_fallback(value):
    cond = {"type": "learned_state", "learned": "site/house_state", "equals": "quiet", "fallback": "active"}
    payload = '{"value":' + value + ',"n":3,"at":1794000000}'
    topic = learned.learned_topic("site/house_state")
    assert learned.state_value(cond, {topic: payload}, 1794000060) == ("active", "fallback")


def test_hold_seconds_may_be_learned_on_time_window_and_sun_window():
    hold = {"learned": "site/hold_s", "min": 60, "max": 600, "fallback": 300}
    assert learned.cond_learned_keys({"type": "time_window", "hold_seconds": hold}) == {"site/hold_s"}
    assert learned.cond_learned_keys({"type": "sun_window", "hold_seconds": hold}) == {"site/hold_s"}
    assert learned.cond_learned_keys({"type": "chain_finding", "hold_seconds": hold}) == set()
