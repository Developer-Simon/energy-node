"""Tests fuer die Validierung der Szenarien (Spec 1.3)."""

import json

import pytest

import automation_mqtt as automation


def zero_feed(**overrides):
    scenario = {
        "id": "nulleinspeisung",
        "name": "Nulleinspeisung",
        "enabled": True,
        "template": "zero_feed",
        "history_enabled": True,
        "when": {"conditions": [{"type": "sun_window", "from": "sunrise", "to": "sunset"}]},
        "on_start": {"actions": [
            {"type": "notification", "severity": "info", "title": "Nulleinspeisung", "message": "startet"},
        ]},
        "while": {
            "mode": "regulate",
            "regulator": {
                "output": {"topic": "werkstatt/bkw/limit/set", "entity_id": "bkw_limit",
                           "min": 50, "max": 800, "step": 10, "decimals": 0},
                "direction": "producer",
                "target_w": 30,
                "deadband_w": 20,
                "gain": 0.8,
                "base": {"source": "entity", "entity_id": "bkw_power",
                         "topic": "werkstatt/bkw/state", "value_template": "{{ value_json.power }}"},
                "fallback": {"payload": "800"},
            },
        },
        "on_end": {"actions": [
            {"type": "publish", "topic": "werkstatt/bkw/limit/set", "payload_source": "constant", "payload": "800"},
        ]},
    }
    scenario.update(overrides)
    return scenario


def with_regulator(**overrides):
    """Ein while-Block im Regler-Modus; ein Schluessel mit None entfernt die Angabe."""
    regulator = zero_feed()["while"]["regulator"]
    for key, value in overrides.items():
        if value is None:
            regulator.pop(key, None)
        else:
            regulator[key] = value
    return {"mode": "regulate", "regulator": regulator}


def rule(**overrides):
    r = {
        "id": "r1",
        "name": "Rule 1",
        "enabled": True,
        "conditions": [{"type": "balance_threshold", "field": "grid_export", "comparison": "above",
                        "threshold": 800, "hysteresis": 100, "hold_seconds": 300}],
        "actions": [{"type": "notification", "severity": "info", "title": "t", "message": "m"}],
    }
    r.update(overrides)
    return r


def doc_with(rules=None, scenarios=None, settings=None):
    doc = {
        "version": 1,
        "settings": settings or {"tick_interval_s": 10, "settling_seconds": 60,
                                 "balance_max_age_s": 30, "publish_allowed_prefixes": []},
        "rules": rules or [],
    }
    if scenarios is not None:
        doc["scenarios"] = scenarios
    return doc


def load(tmp_path, doc):
    path = tmp_path / "automation_rules.json"
    path.write_text(json.dumps(doc), encoding="utf-8")
    return automation.load_and_validate(str(path))


def test_the_spec_example_validates_and_gets_defaults(tmp_path):
    doc = load(tmp_path, doc_with(scenarios=[zero_feed()]))
    scenario = doc.scenarios[0]
    regulator = scenario["while"]["regulator"]
    assert regulator["max_change_w"] is None
    assert regulator["interval_s"] is None
    assert scenario["until"] is None
    assert regulator["gain"] == 0.8
    assert scenario["history_enabled"] is True


def test_scenario_ids_share_the_namespace_with_rules(tmp_path):
    doc = doc_with(rules=[rule(id="r1")], scenarios=[zero_feed(id="r1")])
    with pytest.raises(automation.RuleValidationError, match="duplicate id"):
        load(tmp_path, doc)


def test_at_most_8_scenarios(tmp_path):
    scenarios = [zero_feed(id=f"s{i}", **{"while": {"mode": "none"}}) for i in range(9)]
    with pytest.raises(automation.RuleValidationError, match="at most 8 scenarios are allowed"):
        load(tmp_path, doc_with(scenarios=scenarios))


def test_producer_without_fallback_is_rejected(tmp_path):
    doc = doc_with(scenarios=[zero_feed(**{"while": with_regulator(fallback=None)})])
    with pytest.raises(automation.RuleValidationError, match="fallback.payload is required for a producer"):
        load(tmp_path, doc)


def test_consumer_without_fallback_is_accepted(tmp_path):
    scenario = zero_feed(**{"while": with_regulator(direction="consumer", fallback=None)})
    doc = load(tmp_path, doc_with(scenarios=[scenario]))
    assert doc.scenarios[0]["while"]["regulator"]["fallback"] is None


def test_output_min_must_be_below_max(tmp_path):
    output = {"topic": "werkstatt/bkw/limit/set", "entity_id": "bkw_limit", "min": 800, "max": 50, "step": 10}
    scenario = zero_feed(**{"while": with_regulator(output=output)})
    with pytest.raises(automation.RuleValidationError, match="output.min must be below output.max"):
        load(tmp_path, doc_with(scenarios=[scenario]))


def test_two_regulators_on_the_same_output_topic_are_rejected(tmp_path):
    doc = doc_with(scenarios=[zero_feed(id="a"), zero_feed(id="b")])
    with pytest.raises(automation.RuleValidationError,
                       match="output.topic is already regulated by scenario 'a'"):
        load(tmp_path, doc)


def test_hold_rejects_a_toggle_action(tmp_path):
    toggle = {"type": "publish", "topic": "werkstatt/wallbox/set", "payload_source": "toggle",
              "source_topic": "werkstatt/wallbox/state", "payload_on": "1", "payload_off": "0"}
    scenario = zero_feed(**{"while": {"mode": "hold", "actions": [toggle], "repeat_seconds": 60}})
    with pytest.raises(automation.RuleValidationError, match="toggle"):
        load(tmp_path, doc_with(scenarios=[scenario]))


def test_hold_repeat_below_the_publish_minimum_is_rejected(tmp_path):
    publish = {"type": "publish", "topic": "werkstatt/wallbox/set", "payload_source": "constant", "payload": "1"}
    scenario = zero_feed(**{"while": {"mode": "hold", "actions": [publish], "repeat_seconds": 10}})
    with pytest.raises(automation.RuleValidationError, match="repeat_seconds must be >= 30"):
        load(tmp_path, doc_with(scenarios=[scenario]))


def test_on_end_may_toggle(tmp_path):
    toggle = {"type": "publish", "topic": "werkstatt/wallbox/set", "payload_source": "toggle",
              "source_topic": "werkstatt/wallbox/state", "payload_on": "1", "payload_off": "0"}
    scenario = zero_feed(on_end={"actions": [toggle]})
    doc = load(tmp_path, doc_with(scenarios=[scenario]))
    assert doc.scenarios[0]["on_end"]["actions"][0]["payload_source"] == "toggle"


def test_regulate_with_entity_base_needs_a_valid_topic(tmp_path):
    base = {"source": "entity", "entity_id": "bkw_power", "topic": "werkstatt/#", "value_template": ""}
    scenario = zero_feed(**{"while": with_regulator(base=base)})
    with pytest.raises(automation.RuleValidationError, match="'#' wildcard"):
        load(tmp_path, doc_with(scenarios=[scenario]))


def test_a_document_without_scenarios_still_loads(tmp_path):
    doc = load(tmp_path, doc_with(rules=[rule()]))
    assert doc.scenarios == []
