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

_VECTOR_FILE = json.loads((Path(__file__).parent / "learned_vectors.json").read_text(encoding="utf-8"))
VECTORS = _VECTOR_FILE["resolve"]
BOUNDS = _VECTOR_FILE["bounds"]


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


@pytest.mark.parametrize("vector", BOUNDS, ids=[v["name"] for v in BOUNDS])
def test_bounds_vector(vector):
    errors = []
    learned.validate_ref(vector["ref"], vector["kind"], "rules[0].start", errors)
    # Jede Meldung beginnt mit dem Pfad, die Go-Seite prueft denselben Text.
    assert [e.split(": ", 1)[1] for e in errors] == vector["errors"]


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


NOW = 1_800_000_000.0


def _store(key, value, age_s=0):
    return {learned.learned_topic(key): json.dumps({"value": value, "n": 1, "at": NOW - age_s})}


def _lv(**extra):
    cond = {"type": "learned_value", "learned": "site/pv_reserve_w", "comparison": "above",
            "threshold": 1500, "fallback": 0, "max_age_s": 1800, "hold_seconds": 0}
    cond.update(extra)
    return cond


def test_learned_value_above_threshold_is_met():
    assert learned.evaluate_learned_value(_lv(), _store("site/pv_reserve_w", 1600), NOW) is True


def test_learned_value_missing_uses_fallback_zero_and_is_not_met():
    assert learned.evaluate_learned_value(_lv(), {}, NOW) is False


def test_learned_value_older_than_max_age_uses_fallback():
    store = _store("site/pv_reserve_w", 1600, age_s=2000)
    assert learned.evaluate_learned_value(_lv(), store, NOW) is False
    assert learned.learned_number(_lv(), store, NOW) == (0, "fallback")


def test_learned_value_fallback_above_threshold_counts_as_met():
    assert learned.evaluate_learned_value(_lv(fallback=2000), {}, NOW) is True


def test_learned_value_hysteresis_keeps_met_until_value_drops_below_band():
    cond = _lv(hysteresis=100)
    met = learned.evaluate_learned_value(cond, _store("site/pv_reserve_w", 1600), NOW)
    assert met is True
    still = learned.evaluate_learned_value(cond, _store("site/pv_reserve_w", 1450), NOW, latched=met)
    assert still is True
    dropped = learned.evaluate_learned_value(cond, _store("site/pv_reserve_w", 1390), NOW, latched=still)
    assert dropped is False


def test_learned_value_without_hysteresis_ignores_latched_state():
    cond = _lv(hysteresis=0)
    assert learned.evaluate_learned_value(cond, _store("site/pv_reserve_w", 1450), NOW, latched=True) is False


def test_learned_value_below_comparison_with_hysteresis():
    cond = _lv(comparison="below", threshold=500, hysteresis=50)
    assert learned.evaluate_learned_value(cond, _store("site/pv_reserve_w", 540), NOW, latched=True) is True
    assert learned.evaluate_learned_value(cond, _store("site/pv_reserve_w", 560), NOW, latched=True) is False


def test_learned_value_threshold_as_reference_resolves_on_cycle_wh():
    ref = {"learned": "device/wallbox_1/cycle_wh", "min": 500, "max": 3000, "fallback": 1000}
    cond = _lv(threshold=ref)
    store = _store("device/wallbox_1/cycle_wh", 1200)
    assert learned.evaluate_learned_value(cond, {**store, **_store("site/pv_reserve_w", 1250)}, NOW) is True
    assert learned.evaluate_learned_value(cond, {**store, **_store("site/pv_reserve_w", 1100)}, NOW) is False
    assert "device/wallbox_1/cycle_wh" in learned.cond_learned_keys(cond)
    assert "site/pv_reserve_w" in learned.cond_learned_keys(cond)


def test_learned_value_invalid_key_is_a_validation_error():
    import automation_mqtt
    errors = []
    automation_mqtt._validate_condition(_lv(learned="site/../pv"), 0, errors)
    assert any("learned must be a key" in e for e in errors)


def test_learned_value_validation_accepts_the_documented_form():
    import automation_mqtt
    errors = []
    automation_mqtt._validate_condition(_lv(hysteresis=100), 0, errors)
    assert errors == []
