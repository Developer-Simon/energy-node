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


def publish_action(payload):
    return {"type": "publish", "topic": "werkstatt/x/set", "payload_source": "constant", "payload": payload}


def threshold_condition(comparison="above", threshold=500, field_name="grid_export", hold_seconds=0):
    return {"type": "balance_threshold", "field": field_name, "comparison": comparison,
            "threshold": threshold, "hysteresis": 0, "hold_seconds": hold_seconds}


def scenario_def(**overrides):
    raw = {
        "id": "x",
        "name": "X",
        "enabled": True,
        "when": {"conditions": [threshold_condition()]},
        "on_start": {"actions": [publish_action("start")]},
        "on_end": {"actions": [publish_action("end")]},
        "while": {"mode": "none"},
    }
    raw.update(overrides)
    return raw


def normalized(raw):
    errors = []
    result = automation._validate_scenario(raw, 0, errors, set(), 30)
    assert not errors
    return result


def scenario_engine_with(scenarios, settings=None, started_at=0.0, history_sink=None):
    doc = automation.RulesDocument(version=1, settings=settings or automation.Settings(settling_seconds=0),
                                   rules=[], scenarios=scenarios)
    published = []
    events = []
    executor = automation.ActionExecutor(publish_fn=lambda t, p, r: published.append((t, p, r)),
                                          publish_allowed_prefixes=["werkstatt/"])
    engine = automation.ScenarioEngine(executor, started_at=started_at, event_sink=events.append,
                                       history_sink=history_sink)
    return doc, engine, published, events


def payloads(published):
    return [payload for _topic, payload, _retain in published]


def tick(engine, doc, now_ts, grid_export=900, balance_age=0):
    return engine.tick(doc, balance={"grid_export": grid_export}, balance_age=balance_age,
                       topic_values={}, now_ts=now_ts)


def test_scenario_starts_once_and_runs_on_start():
    doc, engine, published, _ = scenario_engine_with([normalized(scenario_def())])
    tick(engine, doc, 1000)
    report = tick(engine, doc, 1010)
    assert payloads(published) == ["start"]
    assert report["x"]["phase"] == "active"


def test_scenario_ends_when_a_when_condition_drops():
    doc, engine, published, _ = scenario_engine_with([normalized(scenario_def())])
    tick(engine, doc, 1000)
    report = tick(engine, doc, 1010, grid_export=0)
    assert payloads(published) == ["start", "end"]
    assert report["x"]["phase"] == "idle"


def test_until_keeps_the_scenario_running_until_it_holds():
    until = {"conditions": [threshold_condition(comparison="below", threshold=100, field_name="grid_export",
                                                hold_seconds=300)]}
    doc, engine, published, _ = scenario_engine_with([normalized(scenario_def(until=until))])
    tick(engine, doc, 1000, grid_export=900)
    tick(engine, doc, 1010, grid_export=300)
    assert payloads(published) == ["start"]
    tick(engine, doc, 1020, grid_export=50)
    tick(engine, doc, 1319, grid_export=50)
    assert payloads(published) == ["start"]
    report = tick(engine, doc, 1320, grid_export=50)
    assert payloads(published) == ["start", "end"]
    assert report["x"]["phase"] == "idle"


def test_settling_delays_the_start():
    settings = automation.Settings(settling_seconds=60)
    doc, engine, published, _ = scenario_engine_with([normalized(scenario_def())], settings=settings,
                                                     started_at=1000)
    report = tick(engine, doc, 1030)
    assert report["x"]["phase"] == "idle"
    assert report["x"]["reason"] == "settling"
    report = tick(engine, doc, 1061)
    assert report["x"]["phase"] == "active"
    assert payloads(published) == ["start"]


def test_stale_balance_neither_starts_nor_ends():
    doc, engine, published, _ = scenario_engine_with([normalized(scenario_def())])
    tick(engine, doc, 1000)
    report = tick(engine, doc, 1010, balance_age=999)
    assert report["x"]["phase"] == "active"
    assert payloads(published) == ["start"]


def test_disabling_an_active_scenario_runs_on_end():
    doc, engine, published, _ = scenario_engine_with([normalized(scenario_def())])
    tick(engine, doc, 1000)
    doc.scenarios = [normalized(scenario_def(enabled=False))]
    report = tick(engine, doc, 1010)
    assert payloads(published) == ["start", "end"]
    assert report["x"]["phase"] == "disabled"


def test_removing_an_active_scenario_runs_on_end_once():
    doc, engine, published, _ = scenario_engine_with([normalized(scenario_def())])
    tick(engine, doc, 1000)
    doc.scenarios = []
    tick(engine, doc, 1010)
    tick(engine, doc, 1020)
    assert payloads(published) == ["start", "end"]


def test_hold_repeats_its_actions():
    hold = {"mode": "hold", "actions": [publish_action("on")], "repeat_seconds": 60}
    doc, engine, published, _ = scenario_engine_with([normalized(scenario_def(**{"while": hold}))])
    tick(engine, doc, 1000)
    assert payloads(published) == ["start", "on"]
    tick(engine, doc, 1030)
    assert payloads(published) == ["start", "on"]
    tick(engine, doc, 1060)
    assert payloads(published) == ["start", "on", "on"]


def test_history_events_for_start_and_end():
    history = []
    doc, engine, _, _ = scenario_engine_with([normalized(scenario_def())],
                                             history_sink=lambda sid, event: history.append((sid, event)))
    tick(engine, doc, 1000)
    tick(engine, doc, 1010, grid_export=0)
    assert [(sid, event["result"]) for sid, event in history] == [("x", "started"), ("x", "ended")]
    started = history[0][1]
    assert started["reason"] is None
    assert started["test"] is False
    assert started["actions"][0]["type"] == "publish"
    assert started["actions"][0]["topic"] == "werkstatt/x/set"


def test_engine_condition_reports_are_unchanged():
    executor = automation.ActionExecutor(publish_fn=lambda *args: None, publish_allowed_prefixes=[])
    engine = automation.Engine(executor, started_at=0, event_sink=lambda message: None)
    doc = automation.RulesDocument(
        version=1,
        settings=automation.Settings(settling_seconds=0, balance_max_age_s=30),
        rules=[{"id": "r1", "name": "R", "enabled": True, "cooldown_seconds": 300,
                "conditions": [threshold_condition()],
                "actions": [{"type": "notification", "severity": "info", "title": "t", "message": "m"}]}])
    results = engine.tick(doc, balance={"grid_export": 620}, balance_age=1, topic_values={}, now_ts=1000)
    assert set(results["r1"]["conditions"][0]) == {"met", "raw_met", "value", "target", "since", "hold_remaining"}
