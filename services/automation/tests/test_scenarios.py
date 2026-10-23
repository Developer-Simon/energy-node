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


def regulator_settings(**overrides):
    values = {"settling_seconds": 0, "tick_interval_s": 10, "regulator_interval_s": 10}
    values.update(overrides)
    return automation.Settings(**values)


def regulate_scenario(**reg_overrides):
    """Nulleinspeisung mit Zeitfenster statt Sonnenfenster, damit der Test ohne Standort laeuft."""
    raw = zero_feed(when={"conditions": [{"type": "time_window", "start": "00:00", "end": "23:59"}]},
                    **{"while": with_regulator(**reg_overrides)})
    return normalized(raw)


def regulate_tick(engine, doc, now_ts, grid_import=0, grid_export=470, balance_age=0, topic_values=None):
    values = {"werkstatt/bkw/state": '{"power": 800}'} if topic_values is None else topic_values
    return engine.tick(doc, balance={"grid_import": grid_import, "grid_export": grid_export},
                       balance_age=balance_age, topic_values=values, now_ts=now_ts)


def history_results(history):
    return [event["result"] for _sid, event in history]


def test_regulator_sends_the_first_step_on_activation():
    doc, engine, published, _ = scenario_engine_with([regulate_scenario()], settings=regulator_settings())
    report = regulate_tick(engine, doc, 1000)
    assert published == [("werkstatt/bkw/limit/set", "400", False)]
    assert report["nulleinspeisung"]["output"] == 400
    assert report["nulleinspeisung"]["net"] == -470


def test_regulator_waits_for_its_interval():
    doc, engine, published, _ = scenario_engine_with([regulate_scenario(interval_s=30)],
                                                     settings=regulator_settings())
    regulate_tick(engine, doc, 1000)
    regulate_tick(engine, doc, 1010)
    regulate_tick(engine, doc, 1020)
    assert payloads(published) == ["400"]
    regulate_tick(engine, doc, 1030, grid_export=270)
    assert payloads(published) == ["400", "560"]


def test_regulator_interval_never_beats_the_tick():
    doc, engine, published, _ = scenario_engine_with([regulate_scenario(interval_s=5)],
                                                     settings=regulator_settings(tick_interval_s=20))
    regulate_tick(engine, doc, 1000)
    regulate_tick(engine, doc, 1010, grid_export=270)
    assert payloads(published) == ["400"]


def test_stale_balance_sends_the_fallback_once():
    history = []
    doc, engine, published, _ = scenario_engine_with([regulate_scenario()], settings=regulator_settings(),
                                                     history_sink=lambda sid, event: history.append((sid, event)))
    regulate_tick(engine, doc, 1000)
    regulate_tick(engine, doc, 1010, balance_age=999)
    report = regulate_tick(engine, doc, 1020, balance_age=999)
    assert payloads(published) == ["400", "800"]
    assert report["nulleinspeisung"]["phase"] == "fallback"
    fallbacks = [event for _sid, event in history if event["result"] == "fallback"]
    assert len(fallbacks) == 1
    assert fallbacks[0]["reason"] == "balance_stale"


def test_balance_based_when_stays_active_through_a_stale_balance():
    history = []
    scenario = normalized(zero_feed(
        when={"conditions": [threshold_condition(comparison="above", threshold=500)]},
        on_end={"actions": [publish_action("end")]},
    ))
    doc, engine, published, _ = scenario_engine_with([scenario], settings=regulator_settings(),
                                                     history_sink=lambda sid, event: history.append((sid, event)))
    regulate_tick(engine, doc, 1000, grid_export=900)
    report = regulate_tick(engine, doc, 1010, grid_export=900, balance_age=999)
    assert report["nulleinspeisung"]["phase"] == "fallback"
    report = regulate_tick(engine, doc, 1020, grid_export=900, balance_age=999)
    assert report["nulleinspeisung"]["phase"] == "fallback"
    assert payloads(published) == ["50", "800"]
    report = regulate_tick(engine, doc, 1030, grid_export=900)
    assert report["nulleinspeisung"]["phase"] == "active"
    assert payloads(published) == ["50", "800", "50"]
    results = history_results(history)
    assert results.count("started") == 1
    assert results.count("fallback") == 1
    assert "ended" not in results
    assert "end" not in payloads(published)


def test_recovery_continues_without_on_start():
    history = []
    doc, engine, published, _ = scenario_engine_with([regulate_scenario()], settings=regulator_settings(),
                                                     history_sink=lambda sid, event: history.append((sid, event)))
    regulate_tick(engine, doc, 1000)
    regulate_tick(engine, doc, 1010, balance_age=999)
    report = regulate_tick(engine, doc, 1020)
    assert report["nulleinspeisung"]["phase"] == "active"
    assert [r for r in history_results(history) if r != "jump"] == ["started", "fallback", "recovered"]
    assert payloads(published) == ["400", "800", "400"]


def test_missing_entity_reading_uses_the_fallback_not_last():
    doc, engine, published, _ = scenario_engine_with([regulate_scenario()], settings=regulator_settings())
    report = regulate_tick(engine, doc, 1000, topic_values={})
    assert payloads(published) == ["800"]
    assert report["nulleinspeisung"]["reason"] == "base_missing"


def test_base_from_balance_and_from_last():
    balance_reg = with_regulator(base={"source": "balance", "field": "grid_export"})["regulator"]
    assert automation.regulator_base_value(balance_reg, balance={"grid_export": 470}, topic_values={},
                                           last_value=None) == 470
    assert automation.regulator_base_value(balance_reg, balance=None, topic_values={},
                                           last_value=None) is None

    last_reg = with_regulator(base={"source": "last"})["regulator"]
    assert automation.regulator_base_value(last_reg, balance=None, topic_values={}, last_value=None) == 800
    assert automation.regulator_base_value(last_reg, balance=None, topic_values={}, last_value=300) == 300

    consumer_reg = with_regulator(base={"source": "last"}, direction="consumer", fallback=None)["regulator"]
    assert automation.regulator_base_value(consumer_reg, balance=None, topic_values={}, last_value=None) == 50


def test_blocked_publish_does_not_move_last_value():
    doc, engine, published, _ = scenario_engine_with([regulate_scenario()], settings=regulator_settings())
    engine.executor.publish_allowed_prefixes = ["andere/"]
    report = regulate_tick(engine, doc, 1000)
    assert published == []
    assert engine._runtime["nulleinspeisung"].last_value is None
    assert report["nulleinspeisung"]["reason"] == "topic does not match any allowed prefix"


def test_large_error_records_a_jump():
    history = []
    doc, engine, _, _ = scenario_engine_with([regulate_scenario()], settings=regulator_settings(),
                                             history_sink=lambda sid, event: history.append((sid, event)))
    regulate_tick(engine, doc, 1000)
    assert "jump" in history_results(history)


def test_zero_deadband_records_no_jump():
    history = []
    doc, engine, published, _ = scenario_engine_with([regulate_scenario(deadband_w=0)],
                                                     settings=regulator_settings(),
                                                     history_sink=lambda sid, event: history.append((sid, event)))
    regulate_tick(engine, doc, 1000)
    assert payloads(published) == ["400"]
    assert "jump" not in history_results(history)


# --- Task 7: Dienst-Verdrahtung (Topics, Zustand, Verlauf, Discovery) ---

import time
from pathlib import Path
from unittest.mock import Mock

from energy_node_common.config import ReloadableConfig

import ha_template
from test_automation_mqtt import FakeClient, FakeSlave


def zero_feed_all_day():
    """Nulleinspeisung mit Zeitfenster, damit sie ohne Standort aktiv wird."""
    return zero_feed(when={"conditions": [{"type": "time_window", "start": "00:00", "end": "23:59"}]})


def scenario_service(scenarios=(), rules=(), *, config_store=None):
    """AutomationService ohne echten MQTT-Client. Der Client sammelt Publishes
    in FakeClient, damit die Tests die Topics direkt pruefen koennen."""
    doc = automation.RulesDocument(
        version=1,
        settings=automation.Settings(settling_seconds=0, balance_max_age_s=30,
                                      publish_allowed_prefixes=["werkstatt/"]),
        rules=list(rules), scenarios=list(scenarios))
    service_config = Mock()
    service_config.service_id = "automation"
    if config_store is None:
        config_store = Mock()
        config_store.load_or.return_value = (doc, None)
    service = automation.AutomationService(service_config, Mock(), config_store, history_path=None, started_at=0.0)
    service.doc = doc
    service.client = FakeClient()
    service.slave = FakeSlave()
    return service


def fresh_balance(service, **values):
    service.last_balance = {"grid_import": 0, "grid_export": 470, **values}
    service.last_balance_at = time.time()


def test_required_topics_include_scenario_inputs():
    scenario = normalized(zero_feed(
        when={"conditions": [{"type": "topic_value", "topic": "werkstatt/when/state", "json_key": "v",
                              "comparison": "above", "value": 1, "hold_seconds": 0}]},
        until={"conditions": [{"type": "entity_value", "topic": "werkstatt/until/state", "entity_id": "u",
                               "json_key": "v", "comparison": "above", "value": 1, "hold_seconds": 0}]},
        on_start={"actions": [{"type": "publish", "topic": "werkstatt/out/set", "payload_source": "topic",
                               "source_topic": "werkstatt/start/state", "retain": False}]},
    ))
    service = scenario_service([scenario])
    topics = service.required_topics(service.doc)
    assert {"werkstatt/when/state", "werkstatt/until/state", "werkstatt/start/state",
            "werkstatt/bkw/state"} <= topics


def test_state_document_carries_scenarios():
    scenarios = {"x": {"phase": "active", "output": 120}}
    doc = automation.build_state_document({}, [], at=1.0, online=True, scenario_results=scenarios)
    assert doc["scenarios"] == scenarios
    assert automation.build_state_document({}, [], at=1.0, online=True)["scenarios"] == {}


def test_poll_core_publishes_the_scenario_state_topic():
    service = scenario_service([regulate_scenario()])
    service.topic_values["werkstatt/bkw/state"] = '{"power": 800}'
    fresh_balance(service)
    service.poll_core()
    state = service.client.state_payload("/scenario/nulleinspeisung/state")
    assert state["phase"] == "active"
    assert "output" in state
    assert state["at"] > 0


def test_idle_scenario_state_has_no_output_key():
    scenario = normalized(zero_feed(when={"conditions": [threshold_condition(threshold=5000)]}))
    service = scenario_service([scenario])
    fresh_balance(service)
    service.poll_core()
    state = service.client.state_payload("/scenario/nulleinspeisung/state")
    assert state["phase"] == "idle"
    assert "output" not in state


def test_discovery_has_output_only_for_regulating_scenarios():
    regulating = normalized(zero_feed())
    plain = normalized(zero_feed(id="nur_phase", **{"while": {"mode": "none"}}))
    block = automation.device_block("automation", "energy_node")
    reg_entries = automation.scenario_discovery("automation", "outstation/automation", block, regulating)
    plain_entries = automation.scenario_discovery("automation", "outstation/automation", block, plain)
    assert [object_id for _c, object_id, _cfg in plain_entries] == ["scenario_nur_phase_phase"]
    assert [object_id for _c, object_id, _cfg in reg_entries] == [
        "scenario_nulleinspeisung_phase", "scenario_nulleinspeisung_output"]
    output = reg_entries[1][2]
    assert output["unit_of_measurement"] == "W"
    assert output["state_class"] == "measurement"
    assert output["device_class"] == "power"
    assert output["value_template"] == "{{ value_json.output }}"
    assert output["name"] == "Nulleinspeisung Stellwert"
    assert output["state_topic"] == "outstation/automation/scenario/nulleinspeisung/state"
    phase = reg_entries[0][2]
    assert phase["entity_category"] == "diagnostic"
    assert phase["value_template"] == "{{ value_json.phase }}"
    assert phase["name"] == "Nulleinspeisung Phase"


def test_scenario_discovery_value_templates_are_readable_by_the_dashboard():
    block = automation.device_block("automation", "energy_node")
    entries = automation.scenario_discovery("automation", "outstation/automation", block, normalized(zero_feed()))
    for _component, _object_id, config in entries:
        assert ha_template.parse_value_template(config["value_template"]) is not None


def test_reload_clears_discovery_of_a_removed_scenario(tmp_path):
    path = tmp_path / "automation_rules.json"
    path.write_text(json.dumps(doc_with(scenarios=[zero_feed_all_day()])))
    config_store = ReloadableConfig(str(path), automation.load_and_validate)
    service = scenario_service(config_store=config_store)
    service.doc, _ = automation.load_initial_document(config_store)
    service.publish_scenario_discovery(service.doc)

    path.write_text(json.dumps(doc_with()))
    service.reload_config()

    published = service.client.published
    assert ("homeassistant/sensor/automation/scenario_nulleinspeisung_output/config", "", True) in published
    assert ("outstation/automation/scenario/nulleinspeisung/state", "", True) in published


def test_reload_with_the_same_file_does_not_restart_an_active_scenario(tmp_path):
    path = tmp_path / "automation_rules.json"
    path.write_text(json.dumps(doc_with(scenarios=[zero_feed_all_day()])))
    config_store = ReloadableConfig(str(path), automation.load_and_validate)
    service = scenario_service(config_store=config_store)
    service.doc, _ = automation.load_initial_document(config_store)
    service.topic_values["werkstatt/bkw/state"] = '{"power": 800}'
    fresh_balance(service)
    service.poll_core()
    runtime = service.scenario_engine._runtime["nulleinspeisung"]
    assert runtime.phase == "active"
    since, last_value = runtime.since, runtime.last_value

    service.reload_config()
    fresh_balance(service)
    service.poll_core()

    assert runtime is service.scenario_engine._runtime["nulleinspeisung"]
    assert runtime.since == since
    assert runtime.last_value == last_value
    assert [e["result"] for e in service.history.as_list("nulleinspeisung")].count("started") == 1


def test_scenario_history_lands_in_the_history_document():
    service = scenario_service([regulate_scenario()])
    service.topic_values["werkstatt/bkw/state"] = '{"power": 800}'
    fresh_balance(service)
    service.poll_core()
    results = [e["result"] for e in service.history.as_list("nulleinspeisung")]
    assert "started" in results
    history = service.client.state_payload("/history")
    assert "started" in [e["result"] for e in history["rules"]["nulleinspeisung"]]


def test_removing_an_active_regulator_by_reload_ends_once_and_clears_its_topics(tmp_path):
    path = tmp_path / "automation_rules.json"
    path.write_text(json.dumps(doc_with(scenarios=[zero_feed_all_day()])))
    config_store = ReloadableConfig(str(path), automation.load_and_validate)
    service = scenario_service(config_store=config_store)
    service.doc, _ = automation.load_initial_document(config_store)
    service.topic_values["werkstatt/bkw/state"] = '{"power": 800}'
    fresh_balance(service)
    service.poll_core()
    assert service.scenario_engine._runtime["nulleinspeisung"].phase == "active"
    service.publish_scenario_discovery(service.doc)
    before_removal = len(service.client.published)

    path.write_text(json.dumps(doc_with(settings={"tick_interval_s": 10, "settling_seconds": 0,
                                                  "balance_max_age_s": 30,
                                                  "publish_allowed_prefixes": ["werkstatt/"]})))
    service.reload_config()
    service.poll_core()
    service.poll_core()

    after = service.client.published[before_removal:]
    limit_topic = "werkstatt/bkw/limit/set"
    assert [(t, p) for t, p, _r in after if t == limit_topic] == [(limit_topic, "800")]
    state_topic = "outstation/automation/scenario/nulleinspeisung/state"
    assert [(t, p, r) for t, p, r in after if t == state_topic] == [(state_topic, "", True)]
    assert ("homeassistant/sensor/automation/scenario_nulleinspeisung_output/config", "", True) in after
    assert any(t.endswith("scenario_nulleinspeisung_phase/config") and p == "" for t, p, _r in after)
    assert "nulleinspeisung" not in service.scenario_engine._runtime
    assert "nulleinspeisung" not in service.scenario_publishers
