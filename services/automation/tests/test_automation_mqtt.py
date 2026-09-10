"""Tests fuer automation_mqtt.py.

Muster wie services/battery_soc/tests/: das Modul wird direkt importiert.
Ausfuehren mit `.venv/bin/pytest services/automation/tests` (Projekt-venv).
"""

import json
import re
import sys
import time
from pathlib import Path

import pytest
from energy_node_common.config import ReloadableConfig
from energy_node_common.settings import SlaveStatus

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

import automation_mqtt as automation


def test_module_has_no_environment_constants():
    for name in ("MQTT_HOST", "MQTT_PORT", "SERVICE_DEVICE_ID", "BASE_TOPIC",
                 "AUTOMATION_RULES_CONFIG", "TEST_COMMAND_TOPIC", "TEST_RESULT_TOPIC",
                 "AUTOMATION_HISTORY_FILE"):
        assert not hasattr(automation, name), f"{name} steht noch auf Modulebene"


def test_rules_config_path_follows_convention(app_config):
    config = app_config()
    assert config.rules_config().name == "automation_rules.json"


def test_history_file_path_follows_convention(app_config):
    config = app_config()
    assert config.history_file().name == "automation_history.json"


def test_service_topics_follow_service_id(app_config, tmp_path):
    config = app_config(services={"automation": {"service_id": "automation-x"}})
    rules_path = tmp_path / "automation_rules.json"
    rules_path.write_text(json.dumps({"version": 1, "settings": {}, "rules": []}))
    config_store = ReloadableConfig(str(rules_path), automation.load_and_validate)

    service = automation.AutomationService(config.service("automation"), config.mqtt, config_store)

    assert service.base_topic == "outstation/automation-x"
    assert service.test_command_topic == "outstation/automation-x/test/set"
    assert service.test_result_topic == "outstation/automation-x/test/result"


def test_validate_publish_topic_rejects_plus_wildcard():
    assert automation.validate_publish_topic("werkstatt/+/set") is not None


def test_validate_publish_topic_rejects_hash_wildcard():
    assert automation.validate_publish_topic("werkstatt/#") is not None


def test_validate_publish_topic_rejects_single_segment():
    assert automation.validate_publish_topic("wallbox") is not None


def test_validate_publish_topic_rejects_empty_segment():
    assert automation.validate_publish_topic("werkstatt//set") is not None


def test_validate_publish_topic_rejects_leading_slash():
    assert automation.validate_publish_topic("/werkstatt/set") is not None


def test_validate_publish_topic_rejects_trailing_slash():
    assert automation.validate_publish_topic("werkstatt/set/") is not None


def test_validate_publish_topic_rejects_homeassistant_prefix():
    assert automation.validate_publish_topic("homeassistant/switch/foo/config") is not None


def test_validate_publish_topic_rejects_sys_prefix():
    assert automation.validate_publish_topic("$SYS/broker/uptime") is not None


def test_validate_publish_topic_rejects_energy_node_balance_topic():
    assert automation.validate_publish_topic("outstation/energy_node/energy/balance") is not None


def test_validate_publish_topic_accepts_a_normal_command_topic():
    assert automation.validate_publish_topic("werkstatt/wallbox/set") is None


def minimal_settings():
    return {
        "tick_interval_s": 10, "settling_seconds": 60,
        "balance_max_age_s": 30, "publish_allowed_prefixes": [],
    }


def balance_condition(**overrides):
    cond = {"type": "balance_threshold", "field": "grid_export", "comparison": "above",
            "threshold": 800, "hysteresis": 100, "hold_seconds": 300}
    cond.update(overrides)
    return cond


def notification_action(**overrides):
    action = {"type": "notification", "severity": "info", "title": "t", "message": "m"}
    action.update(overrides)
    return action


def publish_action(**overrides):
    action = {"type": "publish", "topic": "werkstatt/wallbox/set", "retain": False,
              "payload_source": "constant", "payload": "1"}
    action.update(overrides)
    return action


def rule(**overrides):
    r = {"id": "r1", "name": "Rule 1", "enabled": True, "cooldown_seconds": 300,
         "conditions": [balance_condition()], "actions": [notification_action()]}
    r.update(overrides)
    return r


def document(rules=None, settings=None):
    return {"version": 1, "settings": settings or minimal_settings(), "rules": rules or [rule()]}


def write_doc(tmp_path, doc):
    path = tmp_path / "automation_rules.json"
    path.write_text(json.dumps(doc))
    return str(path)


def test_load_and_validate_accepts_the_minimal_valid_document(tmp_path):
    result = automation.load_and_validate(write_doc(tmp_path, document()))
    assert result.version == 1
    assert result.rules[0]["id"] == "r1"


def test_load_and_validate_rejects_duplicate_ids(tmp_path):
    doc = document(rules=[rule(id="dup"), rule(id="dup")])
    with pytest.raises(automation.RuleValidationError):
        automation.load_and_validate(write_doc(tmp_path, doc))


def test_load_and_validate_rejects_unknown_condition_type(tmp_path):
    doc = document(rules=[rule(conditions=[{"type": "nonsense"}])])
    with pytest.raises(automation.RuleValidationError):
        automation.load_and_validate(write_doc(tmp_path, doc))


def test_load_and_validate_rejects_bad_weekday(tmp_path):
    doc = document(rules=[rule(conditions=[{"type": "time_window", "start": "08:00", "end": "18:00", "weekdays": [7]}])])
    with pytest.raises(automation.RuleValidationError):
        automation.load_and_validate(write_doc(tmp_path, doc))


def test_load_and_validate_rejects_bad_time_format(tmp_path):
    doc = document(rules=[rule(conditions=[{"type": "time_window", "start": "25:00", "end": "18:00"}])])
    with pytest.raises(automation.RuleValidationError):
        automation.load_and_validate(write_doc(tmp_path, doc))


def test_load_and_validate_rejects_short_hold_seconds_on_publishing_rule(tmp_path):
    doc = document(rules=[rule(conditions=[balance_condition(hold_seconds=10)], actions=[publish_action()])])
    with pytest.raises(automation.RuleValidationError):
        automation.load_and_validate(write_doc(tmp_path, doc))


def test_load_and_validate_rejects_short_cooldown_on_publishing_rule(tmp_path):
    doc = document(rules=[rule(cooldown_seconds=10, actions=[publish_action()])])
    with pytest.raises(automation.RuleValidationError):
        automation.load_and_validate(write_doc(tmp_path, doc))


def test_load_and_validate_rejects_wildcard_publish_topic(tmp_path):
    doc = document(rules=[rule(cooldown_seconds=300,
                                conditions=[balance_condition(hold_seconds=300)],
                                actions=[publish_action(topic="werkstatt/#")])])
    with pytest.raises(automation.RuleValidationError):
        automation.load_and_validate(write_doc(tmp_path, doc))


def test_load_and_validate_rejects_homeassistant_publish_topic(tmp_path):
    doc = document(rules=[rule(cooldown_seconds=300,
                                conditions=[balance_condition(hold_seconds=300)],
                                actions=[publish_action(topic="homeassistant/switch/x/config")])])
    with pytest.raises(automation.RuleValidationError):
        automation.load_and_validate(write_doc(tmp_path, doc))


def test_load_and_validate_rejects_energy_node_balance_publish_topic(tmp_path):
    doc = document(rules=[rule(cooldown_seconds=300,
                                conditions=[balance_condition(hold_seconds=300)],
                                actions=[publish_action(topic="outstation/energy_node/energy/balance")])])
    with pytest.raises(automation.RuleValidationError):
        automation.load_and_validate(write_doc(tmp_path, doc))


def test_publish_guard_blocks_balance_and_availability_allows_settings():
    assert automation.validate_publish_topic(automation.BALANCE_TOPIC) is not None
    assert automation.validate_publish_topic("outstation/energy_node/status/online") is not None
    assert automation.validate_publish_topic("outstation/energy_node/settings/simulation_active/set") is None
    assert automation.validate_publish_topic("outstation/apsystems/settings/simulation_active/set") is None


def test_load_and_validate_rejects_too_many_rules(tmp_path):
    doc = document(rules=[rule(id=f"r{i}") for i in range(17)])
    with pytest.raises(automation.RuleValidationError):
        automation.load_and_validate(write_doc(tmp_path, doc))


def test_load_and_validate_rejects_too_many_conditions(tmp_path):
    doc = document(rules=[rule(conditions=[balance_condition() for _ in range(9)])])
    with pytest.raises(automation.RuleValidationError):
        automation.load_and_validate(write_doc(tmp_path, doc))


def test_load_and_validate_fills_in_defaults(tmp_path):
    doc = document(settings={})
    result = automation.load_and_validate(write_doc(tmp_path, doc))
    assert result.settings.tick_interval_s == 10
    assert result.settings.balance_max_age_s == 30
    assert result.settings.publish_allowed_prefixes == []


def test_load_and_validate_fills_in_history_defaults(tmp_path):
    doc = document(rules=[rule()])
    result = automation.load_and_validate(write_doc(tmp_path, doc))
    assert result.settings.history_limit == 10
    assert result.settings.history_persist is True
    assert result.rules[0]["history_enabled"] is False


def test_load_and_validate_keeps_explicit_history_settings(tmp_path):
    doc = document(rules=[rule(history_enabled=True)],
                    settings={**minimal_settings(), "history_limit": 25, "history_persist": False})
    result = automation.load_and_validate(write_doc(tmp_path, doc))
    assert result.settings.history_limit == 25
    assert result.settings.history_persist is False
    assert result.rules[0]["history_enabled"] is True


def test_load_and_validate_rejects_non_numeric_history_limit(tmp_path):
    doc = document(rules=[rule()], settings={**minimal_settings(), "history_limit": "abc"})
    with pytest.raises(automation.RuleValidationError):
        automation.load_and_validate(write_doc(tmp_path, doc))


def test_load_and_validate_rejects_out_of_range_history_limit(tmp_path):
    doc = document(rules=[rule()], settings={**minimal_settings(), "history_limit": 0})
    with pytest.raises(automation.RuleValidationError):
        automation.load_and_validate(write_doc(tmp_path, doc))


def test_load_and_validate_rejects_invalid_balance_field_in_condition(tmp_path):
    doc = document(rules=[rule(conditions=[balance_condition(field="invalid_field")])])
    with pytest.raises(automation.RuleValidationError):
        automation.load_and_validate(write_doc(tmp_path, doc))


def test_load_and_validate_rejects_invalid_balance_field_in_publish_action(tmp_path):
    doc = document(rules=[rule(cooldown_seconds=300,
                                conditions=[balance_condition(hold_seconds=300)],
                                actions=[publish_action(payload_source="balance", field="invalid_field")])])
    with pytest.raises(automation.RuleValidationError):
        automation.load_and_validate(write_doc(tmp_path, doc))


def test_load_and_validate_accepts_a_toggle_publish_action(tmp_path):
    doc = document(rules=[rule(cooldown_seconds=300,
                                conditions=[balance_condition(hold_seconds=300)],
                                actions=[publish_action(payload_source="toggle", source_topic="werkstatt/wallbox/state",
                                                         payload_on="ON", payload_off="OFF")])])
    result = automation.load_and_validate(write_doc(tmp_path, doc))
    assert result.rules[0]["actions"][0]["payload_source"] == "toggle"


def test_load_and_validate_rejects_toggle_without_payload_on_off(tmp_path):
    doc = document(rules=[rule(cooldown_seconds=300,
                                conditions=[balance_condition(hold_seconds=300)],
                                actions=[publish_action(payload_source="toggle", source_topic="werkstatt/wallbox/state")])])
    with pytest.raises(automation.RuleValidationError):
        automation.load_and_validate(write_doc(tmp_path, doc))


def test_load_and_validate_rejects_toggle_with_an_invalid_source_topic(tmp_path):
    doc = document(rules=[rule(cooldown_seconds=300,
                                conditions=[balance_condition(hold_seconds=300)],
                                actions=[publish_action(payload_source="toggle", source_topic="werkstatt/#",
                                                         payload_on="ON", payload_off="OFF")])])
    with pytest.raises(automation.RuleValidationError):
        automation.load_and_validate(write_doc(tmp_path, doc))


def test_extract_json_value_reads_bare_number():
    assert automation.extract_json_value("42.5", "") == 42.5


def test_extract_json_value_reads_a_key():
    assert automation.extract_json_value('{"apower": 12.5}', "apower") == 12.5


def test_extract_json_value_returns_none_for_missing_key():
    assert automation.extract_json_value('{"apower": 12.5}', "power") is None


def test_extract_json_value_returns_none_for_invalid_json_with_key():
    assert automation.extract_json_value("not json", "apower") is None


def test_evaluate_condition_raw_balance_threshold_above():
    cond = {"type": "balance_threshold", "field": "grid_export", "comparison": "above", "threshold": 800, "hysteresis": 0}
    met, reason = automation.evaluate_condition_raw(cond, balance={"grid_export": 900}, topic_values={}, now_ts=0, weekday=0)
    assert met is True and reason == ""
    met, _ = automation.evaluate_condition_raw(cond, balance={"grid_export": 700}, topic_values={}, now_ts=0, weekday=0)
    assert met is False


def test_evaluate_condition_raw_balance_threshold_below():
    cond = {"type": "balance_threshold", "field": "autarkie", "comparison": "below", "threshold": 0.5, "hysteresis": 0}
    met, _ = automation.evaluate_condition_raw(cond, balance={"autarkie": 0.3}, topic_values={}, now_ts=0, weekday=0)
    assert met is True


def test_evaluate_condition_raw_reports_balance_stale_when_no_balance():
    cond = {"type": "balance_threshold", "field": "grid_export", "comparison": "above", "threshold": 800, "hysteresis": 0}
    met, reason = automation.evaluate_condition_raw(cond, balance=None, topic_values={}, now_ts=0, weekday=0)
    assert met is False and reason == "balance_stale"


def test_evaluate_condition_raw_topic_value_equals_text():
    cond = {"type": "topic_value", "topic": "werkstatt/mode", "json_key": "", "comparison": "equals", "text": "eco"}
    met, _ = automation.evaluate_condition_raw(cond, balance=None, topic_values={"werkstatt/mode": "eco"}, now_ts=0, weekday=0)
    assert met is True
    met, _ = automation.evaluate_condition_raw(cond, balance=None, topic_values={"werkstatt/mode": "boost"}, now_ts=0, weekday=0)
    assert met is False


def test_evaluate_condition_raw_topic_value_not_equals():
    cond = {"type": "topic_value", "topic": "werkstatt/mode", "json_key": "", "comparison": "not_equals", "text": "eco"}
    met, _ = automation.evaluate_condition_raw(cond, balance=None, topic_values={"werkstatt/mode": "boost"}, now_ts=0, weekday=0)
    assert met is True


def test_evaluate_condition_raw_missing_topic_is_not_met():
    cond = {"type": "topic_value", "topic": "werkstatt/mode", "json_key": "", "comparison": "equals", "text": "eco"}
    met, reason = automation.evaluate_condition_raw(cond, balance=None, topic_values={}, now_ts=0, weekday=0)
    assert met is False and reason == "topic_unknown"


def test_evaluate_condition_raw_time_window_within_same_day():
    cond = {"type": "time_window", "start": "08:00", "end": "18:00", "weekdays": []}
    noon = _timestamp_at(hour=12, minute=0, weekday=2)
    met, _ = automation.evaluate_condition_raw(cond, balance=None, topic_values={}, now_ts=noon, weekday=2)
    assert met is True


def test_evaluate_condition_raw_time_window_crosses_midnight():
    cond = {"type": "time_window", "start": "22:00", "end": "06:00", "weekdays": []}
    late = _timestamp_at(hour=23, minute=0, weekday=5)
    early = _timestamp_at(hour=5, minute=0, weekday=6)
    outside = _timestamp_at(hour=12, minute=0, weekday=5)
    assert automation.evaluate_condition_raw(cond, balance=None, topic_values={}, now_ts=late, weekday=5)[0] is True
    assert automation.evaluate_condition_raw(cond, balance=None, topic_values={}, now_ts=early, weekday=6)[0] is True
    assert automation.evaluate_condition_raw(cond, balance=None, topic_values={}, now_ts=outside, weekday=5)[0] is False


def test_evaluate_condition_raw_time_window_respects_weekdays():
    cond = {"type": "time_window", "start": "08:00", "end": "18:00", "weekdays": [5, 6]}
    monday_noon = _timestamp_at(hour=12, minute=0, weekday=0)
    met, _ = automation.evaluate_condition_raw(cond, balance=None, topic_values={}, now_ts=monday_noon, weekday=0)
    assert met is False


def _timestamp_at(hour, minute, weekday):
    """Hilfsfunktion: liefert nur (hour, minute, weekday) an evaluate_condition_raw,
    das now_ts selbst wird von evaluate_condition_raw fuer time_window nicht als
    echter Unix-Zeitstempel interpretiert, sondern ueber time.localtime() - siehe
    Implementierungshinweis in Schritt 3. Fuer die Tests reicht ein konstruierter
    time.struct_time-kompatibler Timestamp; einfachster Weg: einen beliebigen
    Referenztag nehmen, dessen Wochentag zum gewuenschten weekday passt, und
    hour/minute darauf setzen."""
    import datetime
    # 2024-01-01 is a Monday (weekday 0); add `weekday` days to land on the target.
    base = datetime.datetime(2024, 1, 1, hour, minute, 0)
    base += datetime.timedelta(days=weekday)
    return base.timestamp()


def test_resolve_publish_value_constant():
    action = {"type": "publish", "payload_source": "constant", "payload": "1"}
    payload, blocked = automation.resolve_publish_value(action, balance=None, topic_values={})
    assert payload == "1" and blocked is None


def test_resolve_publish_value_from_balance_with_scale_offset_clamp():
    action = {"type": "publish", "payload_source": "balance", "field": "grid_export",
              "scale": 1, "offset": -200, "min": 1380, "max": 11000, "step": 1, "decimals": 0}
    payload, blocked = automation.resolve_publish_value(action, balance={"grid_export": 6700}, topic_values={})
    assert payload == "6500" and blocked is None


def test_resolve_publish_value_clamps_to_min():
    action = {"type": "publish", "payload_source": "balance", "field": "grid_export",
              "scale": 1, "offset": -200, "min": 1380, "max": 11000}
    payload, blocked = automation.resolve_publish_value(action, balance={"grid_export": 100}, topic_values={})
    assert payload == "1380"


def test_resolve_publish_value_rounds_to_step_and_decimals():
    action = {"type": "publish", "payload_source": "balance", "field": "pv",
              "scale": 0.001, "offset": 0, "step": 0.5, "decimals": 1}
    payload, blocked = automation.resolve_publish_value(action, balance={"pv": 3260}, topic_values={})
    assert payload == "3.0"


def test_resolve_publish_value_blocked_when_balance_missing():
    action = {"type": "publish", "payload_source": "balance", "field": "grid_export"}
    payload, blocked = automation.resolve_publish_value(action, balance=None, topic_values={})
    assert payload is None and blocked == "balance_stale"


def test_resolve_publish_value_from_topic():
    action = {"type": "publish", "payload_source": "topic", "source_topic": "sensor/x", "source_json_key": "value"}
    payload, blocked = automation.resolve_publish_value(action, balance=None, topic_values={"sensor/x": '{"value": 42}'})
    assert payload == "42.0" and blocked is None


def test_resolve_publish_value_toggle_switches_off_when_currently_on():
    action = {"type": "publish", "payload_source": "toggle", "source_topic": "werkstatt/wallbox/state",
              "value_template": "", "payload_on": "ON", "payload_off": "OFF"}
    payload, blocked = automation.resolve_publish_value(action, balance=None, topic_values={"werkstatt/wallbox/state": "ON"})
    assert payload == "OFF" and blocked is None


def test_resolve_publish_value_toggle_switches_on_when_currently_off():
    action = {"type": "publish", "payload_source": "toggle", "source_topic": "werkstatt/wallbox/state",
              "value_template": "", "payload_on": "ON", "payload_off": "OFF"}
    payload, blocked = automation.resolve_publish_value(action, balance=None, topic_values={"werkstatt/wallbox/state": "OFF"})
    assert payload == "ON" and blocked is None


def test_resolve_publish_value_toggle_reads_the_state_through_a_value_template():
    action = {"type": "publish", "payload_source": "toggle", "source_topic": "werkstatt/wallbox/state",
              "value_template": "{{ value_json.state }}", "payload_on": "ON", "payload_off": "OFF"}
    payload, blocked = automation.resolve_publish_value(
        action, balance=None, topic_values={"werkstatt/wallbox/state": '{"state": "ON"}'})
    assert payload == "OFF" and blocked is None


def test_resolve_publish_value_toggle_blocked_when_source_topic_unknown():
    action = {"type": "publish", "payload_source": "toggle", "source_topic": "werkstatt/wallbox/state",
              "value_template": "", "payload_on": "ON", "payload_off": "OFF"}
    payload, blocked = automation.resolve_publish_value(action, balance=None, topic_values={})
    assert payload is None and blocked == "source_topic_unknown"


def test_action_executor_preview_does_not_publish():
    published = []
    executor = automation.ActionExecutor(publish_fn=lambda t, p, r: published.append((t, p, r)),
                                          publish_allowed_prefixes=["werkstatt/"])
    action = {"type": "publish", "topic": "werkstatt/wallbox/set", "retain": False,
              "payload_source": "constant", "payload": "6.5"}
    result = executor.preview(action, balance=None, topic_values={})
    assert result == {"topic": "werkstatt/wallbox/set", "payload": "6.5", "blocked": False, "reason": None}
    assert published == []


def test_action_executor_publishes_when_allowlisted():
    published = []
    executor = automation.ActionExecutor(publish_fn=lambda t, p, r: published.append((t, p, r)),
                                          publish_allowed_prefixes=["werkstatt/"])
    action = {"type": "publish", "topic": "werkstatt/wallbox/set", "retain": False,
              "payload_source": "constant", "payload": "6.5"}
    result = executor.execute(action, balance=None, topic_values={}, event_sink=lambda msg: None)
    assert result["blocked"] is False
    assert published == [("werkstatt/wallbox/set", "6.5", False)]


def test_action_executor_blocks_when_prefix_not_allowed():
    published = []
    executor = automation.ActionExecutor(publish_fn=lambda t, p, r: published.append((t, p, r)),
                                          publish_allowed_prefixes=[])
    action = {"type": "publish", "topic": "werkstatt/wallbox/set", "retain": False,
              "payload_source": "constant", "payload": "6.5"}
    result = executor.execute(action, balance=None, topic_values={}, event_sink=lambda msg: None)
    assert result["blocked"] is True
    assert published == []


def test_action_executor_blocks_on_non_matching_prefix():
    executor = automation.ActionExecutor(publish_fn=lambda t, p, r: None, publish_allowed_prefixes=["shelly/"])
    action = {"type": "publish", "topic": "werkstatt/wallbox/set", "retain": False,
              "payload_source": "constant", "payload": "1"}
    result = executor.execute(action, balance=None, topic_values={}, event_sink=lambda msg: None)
    assert result["blocked"] is True


def test_action_executor_notification_always_fires():
    executor = automation.ActionExecutor(publish_fn=lambda t, p, r: None, publish_allowed_prefixes=[])
    action = {"type": "notification", "severity": "warning", "title": "t", "message": "m"}
    events = []
    result = executor.execute(action, balance=None, topic_values={}, event_sink=events.append)
    assert result["blocked"] is False
    assert events  # a German one-liner landed in the event sink


class FakeClock:
    def __init__(self, start=1_700_000_000.0):
        self.now = start

    def advance(self, seconds):
        self.now += seconds


def engine_with(rules, settings=None, started_at=0.0, history_sink=None):
    doc = automation.RulesDocument(version=1, settings=settings or automation.Settings(settling_seconds=0), rules=rules)
    published = []
    events = []
    executor = automation.ActionExecutor(publish_fn=lambda t, p, r: published.append((t, p, r)),
                                          publish_allowed_prefixes=["werkstatt/"])
    engine = automation.Engine(executor, started_at=started_at, event_sink=events.append, history_sink=history_sink)
    return doc, engine, published, events


def test_hold_seconds_fires_only_after_the_full_duration_and_once():
    rule = rule_with_hold(hold_seconds=300, cooldown_seconds=300)
    doc, engine, published, events = engine_with([rule])
    clock = FakeClock()

    balance = {"grid_export": 900}
    for elapsed in (0, 100, 200, 299):
        result = engine.tick(doc, balance=balance, balance_age=0, topic_values={}, now_ts=clock.now, weekday=0)
        assert result["r1"]["result"] in ("hold_pending",), f"fired too early at +{elapsed}s"
        clock.advance(100 if elapsed == 0 else (100 if elapsed < 200 else 99))

    result = engine.tick(doc, balance=balance, balance_age=0, topic_values={}, now_ts=clock.now, weekday=0)
    assert result["r1"]["result"] == "fired"
    assert len(published) == 1

    # Still true one tick later: does not fire again (edge-triggered).
    clock.advance(1)
    result = engine.tick(doc, balance=balance, balance_age=0, topic_values={}, now_ts=clock.now, weekday=0)
    assert result["r1"]["result"] == "fired"
    assert len(published) == 1


def test_tick_reports_a_history_event_on_firing():
    rule_def = rule_with_hold(hold_seconds=0, cooldown_seconds=0)
    history_events = []
    doc, engine, published, events = engine_with(
        [rule_def], history_sink=lambda rule_id, event: history_events.append((rule_id, event)))
    engine.tick(doc, balance={"grid_export": 900}, balance_age=0, topic_values={}, now_ts=1000.0, weekday=0)
    assert len(history_events) == 1
    rule_id, event = history_events[0]
    assert rule_id == "r1"
    assert event["result"] == "fired"
    assert event["test"] is False
    assert event["actions"] == [{"topic": "werkstatt/wallbox/set", "payload": "1",
                                  "blocked": False, "reason": None, "type": "publish"}]


def test_tick_records_history_only_once_per_firing_episode():
    rule_def = rule_with_hold(hold_seconds=0, cooldown_seconds=0)
    history_events = []
    doc, engine, published, events = engine_with(
        [rule_def], history_sink=lambda rule_id, event: history_events.append((rule_id, event)))
    balance = {"grid_export": 900}
    engine.tick(doc, balance=balance, balance_age=0, topic_values={}, now_ts=1000.0, weekday=0)
    engine.tick(doc, balance=balance, balance_age=0, topic_values={}, now_ts=1001.0, weekday=0)
    assert len(history_events) == 1


def test_tick_reports_a_blocked_history_event():
    rule_def = rule_with_hold(hold_seconds=0, cooldown_seconds=0)
    history_events = []
    doc = automation.RulesDocument(version=1, settings=automation.Settings(settling_seconds=0), rules=[rule_def])
    # Keine erlaubten Praefixe -> die publish-Aktion wird blockiert.
    executor = automation.ActionExecutor(publish_fn=lambda *a: None, publish_allowed_prefixes=[])
    engine = automation.Engine(executor, started_at=0, event_sink=lambda m: None,
                               history_sink=lambda rule_id, event: history_events.append((rule_id, event)))
    engine.tick(doc, balance={"grid_export": 900}, balance_age=0, topic_values={}, now_ts=1000.0, weekday=0)
    assert history_events[0][1]["result"] == "blocked"
    assert history_events[0][1]["actions"][0]["blocked"] is True


def test_tick_without_a_history_sink_does_not_crash():
    rule_def = rule_with_hold(hold_seconds=0, cooldown_seconds=0)
    doc, engine, published, events = engine_with([rule_def])  # history_sink=None (Default)
    result = engine.tick(doc, balance={"grid_export": 900}, balance_age=0, topic_values={}, now_ts=1000.0, weekday=0)
    assert result["r1"]["result"] == "fired"


def test_a_dip_below_threshold_resets_the_hold_timer():
    rule = rule_with_hold(hold_seconds=300, cooldown_seconds=300)
    doc, engine, published, events = engine_with([rule])
    clock = FakeClock()

    engine.tick(doc, balance={"grid_export": 900}, balance_age=0, topic_values={}, now_ts=clock.now, weekday=0)
    clock.advance(250)
    engine.tick(doc, balance={"grid_export": 900}, balance_age=0, topic_values={}, now_ts=clock.now, weekday=0)
    clock.advance(1)
    # Dip: raw condition goes false, resetting the hold timer.
    engine.tick(doc, balance={"grid_export": 500}, balance_age=0, topic_values={}, now_ts=clock.now, weekday=0)
    clock.advance(250)
    result = engine.tick(doc, balance={"grid_export": 900}, balance_age=0, topic_values={}, now_ts=clock.now, weekday=0)
    assert result["r1"]["result"] == "hold_pending"
    assert len(published) == 0


def rule_with_hold(hold_seconds, cooldown_seconds, hysteresis=0):
    return {
        "id": "r1", "name": "Test", "enabled": True, "cooldown_seconds": cooldown_seconds,
        "conditions": [{"type": "balance_threshold", "field": "grid_export", "comparison": "above",
                         "threshold": 800, "hysteresis": hysteresis, "hold_seconds": hold_seconds}],
        "actions": [{"type": "publish", "topic": "werkstatt/wallbox/set", "retain": False,
                     "payload_source": "constant", "payload": "1"}],
    }


def test_hysteresis_holds_until_below_threshold_minus_hysteresis():
    rule = rule_with_hold(hold_seconds=0, cooldown_seconds=30, hysteresis=150)
    doc, engine, published, events = engine_with([rule])
    clock = FakeClock()

    # Cross above 800 to latch active.
    engine.tick(doc, balance={"grid_export": 850}, balance_age=0, topic_values={}, now_ts=clock.now, weekday=0)
    assert len(published) == 1
    clock.advance(60)  # clear cooldown so a state change would be visible

    # 780 W is below 800 but still above 800-150=650: stays latched active.
    result = engine.tick(doc, balance={"grid_export": 780}, balance_age=0, topic_values={}, now_ts=clock.now, weekday=0)
    assert result["r1"]["result"] == "fired"  # still true, already fired this episode

    clock.advance(60)
    # 640 W is below 650: latch releases, condition goes raw-false.
    result = engine.tick(doc, balance={"grid_export": 640}, balance_age=0, topic_values={}, now_ts=clock.now, weekday=0)
    assert result["r1"]["result"] == "conditions_not_met"


def test_no_refire_during_cooldown_even_if_still_true():
    rule = rule_with_hold(hold_seconds=0, cooldown_seconds=300)
    doc, engine, published, events = engine_with([rule])
    clock = FakeClock()
    engine.tick(doc, balance={"grid_export": 900}, balance_age=0, topic_values={}, now_ts=clock.now, weekday=0)
    assert len(published) == 1
    clock.advance(299)
    result = engine.tick(doc, balance={"grid_export": 900}, balance_age=0, topic_values={}, now_ts=clock.now, weekday=0)
    assert result["r1"]["result"] in ("cooldown", "fired")
    assert len(published) == 1


def test_fresh_engine_instance_starts_the_hold_timer_at_zero():
    rule = rule_with_hold(hold_seconds=300, cooldown_seconds=300)
    doc, engine, published, events = engine_with([rule])
    result = engine.tick(doc, balance={"grid_export": 900}, balance_age=0, topic_values={}, now_ts=1_700_000_000.0, weekday=0)
    assert result["r1"]["conditions"][0]["since"] == 1_700_000_000.0
    assert result["r1"]["result"] == "hold_pending"


def test_settling_window_suppresses_firing_right_after_start():
    rule = rule_with_hold(hold_seconds=0, cooldown_seconds=30)
    settings = automation.Settings(settling_seconds=60)
    doc, engine, published, events = engine_with([rule], settings=settings, started_at=1_700_000_000.0)
    result = engine.tick(doc, balance={"grid_export": 900}, balance_age=0, topic_values={}, now_ts=1_700_000_030.0, weekday=0)
    assert result["r1"]["result"] == "settling"
    assert len(published) == 0
    result = engine.tick(doc, balance={"grid_export": 900}, balance_age=0, topic_values={}, now_ts=1_700_000_061.0, weekday=0)
    assert result["r1"]["result"] == "fired"


def test_publish_failure_still_starts_the_cooldown():
    def failing_publish(topic, payload, retain):
        raise RuntimeError("broker unreachable")

    rule = rule_with_hold(hold_seconds=0, cooldown_seconds=300)
    doc = automation.RulesDocument(version=1, settings=automation.Settings(settling_seconds=0), rules=[rule])
    executor = automation.ActionExecutor(publish_fn=failing_publish, publish_allowed_prefixes=["werkstatt/"])
    events = []
    engine = automation.Engine(executor, started_at=0.0, event_sink=events.append)

    first = engine.tick(doc, balance={"grid_export": 900}, balance_age=0, topic_values={}, now_ts=1000.0, weekday=0)
    assert first["r1"]["result"] == "error"
    second = engine.tick(doc, balance={"grid_export": 900}, balance_age=0, topic_values={}, now_ts=1001.0, weekday=0)
    assert second["r1"]["cooldown_remaining"] > 0 or second["r1"]["result"] in ("cooldown", "error")


def test_balance_gate_forces_balance_threshold_false_when_stale():
    rule = rule_with_hold(hold_seconds=0, cooldown_seconds=30)
    doc, engine, published, events = engine_with([rule])
    result = engine.tick(doc, balance={"grid_export": 900}, balance_age=999, topic_values={}, now_ts=1000.0, weekday=0)
    assert result["r1"]["result"] == "balance_stale"
    assert len(published) == 0


def test_balance_gate_does_not_affect_time_window_rules():
    rule = {
        "id": "r2", "name": "Time", "enabled": True, "cooldown_seconds": 30,
        "conditions": [{"type": "time_window", "start": "00:00", "end": "23:59", "weekdays": []}],
        "actions": [{"type": "notification", "severity": "info", "title": "t", "message": "m"}],
    }
    doc, engine, published, events = engine_with([rule])
    result = engine.tick(doc, balance=None, balance_age=999, topic_values={}, now_ts=1000.0, weekday=0)
    assert result["r2"]["result"] == "fired"


def test_event_ring_keeps_only_the_last_ten_newest_first():
    ring = automation.EventRing(maxlen=10)
    for i in range(15):
        ring.push(f"event {i}", at=float(i))
    items = ring.as_list()
    assert len(items) == 10
    assert items[0]["message"] == "event 14"
    assert items[-1]["message"] == "event 5"


def test_rule_history_caps_entries_per_rule():
    history = automation.RuleHistory(None)
    for i in range(15):
        history.record("r1", {"at": float(i)}, limit=10)
    items = history.as_list("r1")
    assert len(items) == 10
    assert items[0]["at"] == 14.0
    assert items[-1]["at"] == 5.0


def test_rule_history_round_trips_through_a_restart(tmp_path):
    path = str(tmp_path / "automation_history.json")
    history = automation.RuleHistory(path)
    history.record("r1", {"at": 1.0, "result": "fired"}, limit=10)
    history.record("r2", {"at": 2.0, "result": "blocked"}, limit=10)

    restarted = automation.RuleHistory(path)
    assert restarted.as_list("r1") == [{"at": 1.0, "result": "fired"}]
    assert restarted.as_list("r2") == [{"at": 2.0, "result": "blocked"}]


def test_rule_history_starts_empty_on_missing_file(tmp_path):
    history = automation.RuleHistory(str(tmp_path / "does_not_exist.json"))
    assert history.as_list("r1") == []


def test_rule_history_starts_empty_on_corrupt_file(tmp_path):
    path = tmp_path / "automation_history.json"
    path.write_text("not json")
    history = automation.RuleHistory(str(path))
    assert history.as_list("r1") == []


def test_rule_history_write_is_atomic(tmp_path):
    path = tmp_path / "automation_history.json"
    history = automation.RuleHistory(str(path))
    history.record("r1", {"at": 1.0}, limit=10)
    leftovers = [p for p in tmp_path.iterdir() if p.name.startswith(".automation_history-")]
    assert leftovers == []
    assert path.exists()


def test_build_history_document_wraps_the_per_rule_history_with_a_count():
    doc = automation.build_history_document({"r1": [{"at": 1.0, "result": "fired"}]}, at=5.0)
    assert doc == {"at": 5.0, "rules": {"r1": [{"at": 1.0, "result": "fired"}]}, "rule_count": 1}


def test_build_history_document_is_empty_for_no_history_enabled_rules():
    doc = automation.build_history_document({}, at=5.0)
    assert doc == {"at": 5.0, "rules": {}, "rule_count": 0}


def test_build_state_document_shape():
    doc = automation.build_state_document({"r1": {"enabled": True, "fire_count": 0, "result": "fired"}},
                                           [{"at": 1.0, "message": "x"}], at=2.0, online=True)
    assert doc == {
        "at": 2.0, "online": True,
        "rules": {"r1": {"enabled": True, "fire_count": 0, "result": "fired"}},
        "events": [{"at": 1.0, "message": "x"}],
        "rules_total": 1, "rules_enabled": 1, "rules_fired_total": 0,
    }


def test_build_state_document_counts_enabled_rules_and_sums_fire_count():
    rule_results = {
        "r1": {"enabled": True, "fire_count": 3},
        "r2": {"enabled": False, "fire_count": 0},
        "r3": {"enabled": True, "fire_count": 2},
    }
    doc = automation.build_state_document(rule_results, [], at=1.0, online=True)
    assert doc["rules_total"] == 3
    assert doc["rules_enabled"] == 2
    assert doc["rules_fired_total"] == 5


def test_change_gated_publisher_publishes_on_first_call():
    calls = []
    publisher = automation.ChangeGatedPublisher(publish_fn=lambda t, p: calls.append((t, p)), heartbeat_seconds=60)
    published = publisher.maybe_publish("t", {"a": 1}, now_ts=0)
    assert published is True
    assert len(calls) == 1


def test_change_gated_publisher_skips_unchanged_payload():
    calls = []
    publisher = automation.ChangeGatedPublisher(publish_fn=lambda t, p: calls.append((t, p)), heartbeat_seconds=60)
    publisher.maybe_publish("t", {"a": 1}, now_ts=0)
    published = publisher.maybe_publish("t", {"a": 1}, now_ts=1)
    assert published is False
    assert len(calls) == 1


def test_change_gated_publisher_publishes_on_change():
    calls = []
    publisher = automation.ChangeGatedPublisher(publish_fn=lambda t, p: calls.append((t, p)), heartbeat_seconds=60)
    publisher.maybe_publish("t", {"a": 1}, now_ts=0)
    published = publisher.maybe_publish("t", {"a": 2}, now_ts=1)
    assert published is True
    assert len(calls) == 2


def test_change_gated_publisher_heartbeats_even_without_a_change():
    calls = []
    publisher = automation.ChangeGatedPublisher(publish_fn=lambda t, p: calls.append((t, p)), heartbeat_seconds=60)
    publisher.maybe_publish("t", {"a": 1}, now_ts=0)
    published = publisher.maybe_publish("t", {"a": 1}, now_ts=61)
    assert published is True
    assert len(calls) == 2


def test_discovery_object_ids_covers_state_and_last_event():
    entries = automation.discovery_object_ids("automation", "outstation/automation", {"identifiers": ["automation"]})
    object_ids = [object_id for _component, object_id, _config in entries]
    assert object_ids == ["state", "last_event", "status", "test_result", "history"]
    state_config = next(config for _c, object_id, config in entries if object_id == "state")
    assert state_config.get("entity_category") == "diagnostic"
    last_event_config = next(config for _c, object_id, config in entries if object_id == "last_event")
    assert "entity_category" not in last_event_config
    status_config = next(config for _c, object_id, config in entries if object_id == "status")
    assert status_config.get("entity_category") == "diagnostic"
    assert status_config.get("state_topic") == "outstation/automation/settings/status"


def test_discovery_includes_a_history_entity_scoped_to_diagnostics():
    entries = automation.discovery_object_ids("automation", "outstation/automation", {"identifiers": ["automation"]})
    history_config = next(config for _c, object_id, config in entries if object_id == "history")
    assert history_config.get("entity_category") == "diagnostic"
    assert history_config.get("state_topic") == "outstation/automation/history"
    assert history_config.get("json_attributes_topic") == "outstation/automation/history"


# Dieselbe Teilmenge, die dashboard/internal/registry/registry.go:1311 parst -
# Dashboard und Home Assistant muessen denselben Wert aus dem Template ziehen.
_DASHBOARD_VALUE_TEMPLATE_RE = re.compile(
    r"^\{\{\s*value_json\.([A-Za-z0-9_]+)"
    r"(?:\[['\"]([^'\"]+)['\"]\])?"
    r"(?:\s*\|\s*default\(0\))?(?:\s*\|\s*int)?(?:\s*\|\s*timestamp_local)?\s*\}\}$"
)


def test_discovery_entities_have_value_templates_matching_dashboard_subset():
    entries = automation.discovery_object_ids("automation", "outstation/automation", {"identifiers": ["automation"]})
    for _component, object_id, config in entries:
        assert _DASHBOARD_VALUE_TEMPLATE_RE.match(config.get("value_template", "")), object_id


def test_discovery_entities_expose_full_payload_via_json_attributes_topic():
    entries = automation.discovery_object_ids("automation", "outstation/automation", {"identifiers": ["automation"]})
    for _component, object_id, config in entries:
        assert config.get("json_attributes_topic") == config.get("state_topic"), object_id


def test_state_entity_carries_measurement_metadata():
    entries = automation.discovery_object_ids("automation", "outstation/automation", {"identifiers": ["automation"]})
    state_config = next(config for _c, object_id, config in entries if object_id == "state")
    assert state_config.get("unit_of_measurement") == "Regeln"
    assert state_config.get("state_class") == "measurement"


def test_discovery_value_template_keys_exist_in_their_own_payload():
    """Ein Tippfehler zwischen value_template und dem tatsaechlich publizierten
    Payload ist in Home Assistant sonst nur eine stille 'unbekannt'-Entity."""
    entries = automation.discovery_object_ids("automation", "outstation/automation", {"identifiers": ["automation"]})
    payloads = {
        "state": automation.build_state_document({"r1": {"enabled": True, "fire_count": 1}}, [], at=1.0, online=True),
        "last_event": {"at": 1.0, "message": "x"},
        "status": SlaveStatus(poll_interval_s=1, diagnostic_poll_multiplier=1, actual_poll_interval_s=1).to_dict(),
        "test_result": {"at": 1.0, "rule_id": "r1", "action_index": 0, "status": "published",
                         "topic": "werkstatt/x/set", "payload": "on", "reason": None},
        "history": automation.build_history_document({"r1": [{"at": 1.0, "result": "fired"}]}, at=1.0),
    }
    for _component, object_id, config in entries:
        referenced = re.findall(r"value_json\.(\w+)", config.get("value_template", ""))
        assert referenced, object_id
        assert set(referenced) <= set(payloads[object_id]), (object_id, referenced)


class FakeClient:
    def __init__(self):
        self.published = []
        self.subscribed = []
        self.unsubscribed = []

    def publish(self, topic, payload=None, qos=0, retain=False):
        self.published.append((topic, payload, retain))

    def subscribe(self, topic, qos=0):
        self.subscribed.append(topic)

    def unsubscribe(self, topic):
        self.unsubscribed.append(topic)

    def state_payload(self, suffix="/state"):
        for topic, payload, _retain in reversed(self.published):
            if topic.endswith(suffix):
                return json.loads(payload)
        raise AssertionError(f"no payload published on a topic ending in {suffix!r}")


def doc_with_topics(*, conditions=None, source_topic=None):
    """Baut ein minimal-gueltiges RulesDocument, dessen Regel genau die
    uebergebenen (Bedingungs-Topic, Aktions-Quelltopic)-Kombination nutzt,
    fuer die required_topics()-Tests."""
    conditions = conditions or []
    if not conditions and not source_topic:
        return automation.RulesDocument(version=1, settings=automation.Settings(), rules=[])
    actions = (
        [{"type": "publish", "topic": "werkstatt/x/set", "retain": False, "payload_source": "topic",
          "source_topic": source_topic, "source_json_key": ""}]
        if source_topic else
        [{"type": "notification", "severity": "info", "title": "t", "message": "m"}]
    )
    rule_conditions = [
        {"type": "topic_value", "topic": t, "json_key": "", "comparison": "equals", "text": "x", "hold_seconds": 0}
        for t in conditions
    ] or [{"type": "time_window", "start": "00:00", "end": "23:59", "weekdays": []}]
    return automation.RulesDocument(
        version=1, settings=automation.Settings(),
        rules=[{"id": "r1", "name": "n", "enabled": True, "cooldown_seconds": 30,
                "conditions": rule_conditions, "actions": actions}],
    )


def test_required_topics_always_includes_the_balance_topic():
    doc = automation.RulesDocument(version=1, settings=automation.Settings(), rules=[])
    service = automation.AutomationService.__new__(automation.AutomationService)  # no MQTT needed for this pure method
    service.test_command_topic = "outstation/automation/test/set"
    assert automation.BALANCE_TOPIC in service.required_topics(doc)


def test_required_topics_collects_condition_and_action_source_topics():
    doc = doc_with_topics(conditions=["bms/state"])
    service = automation.AutomationService.__new__(automation.AutomationService)
    service.test_command_topic = "outstation/automation/test/set"
    topics = service.required_topics(doc)
    assert "bms/state" in topics
    assert automation.BALANCE_TOPIC in topics


def test_required_topics_includes_the_toggle_source_topic():
    doc = automation.RulesDocument(
        version=1, settings=automation.Settings(),
        rules=[{"id": "r1", "name": "n", "enabled": True, "cooldown_seconds": 30,
                "conditions": [{"type": "time_window", "start": "00:00", "end": "23:59", "weekdays": []}],
                "actions": [{"type": "publish", "topic": "werkstatt/wallbox/set", "retain": False,
                             "payload_source": "toggle", "source_topic": "werkstatt/wallbox/state",
                             "payload_on": "ON", "payload_off": "OFF"}]}],
    )
    service = automation.AutomationService.__new__(automation.AutomationService)
    service.test_command_topic = "outstation/automation/test/set"
    assert "werkstatt/wallbox/state" in service.required_topics(doc)


def test_apply_subscriptions_diffs_against_the_previous_set():
    service = automation.AutomationService.__new__(automation.AutomationService)
    service.subscribed_topics = {"old/topic", automation.BALANCE_TOPIC}
    service.client = FakeClient()
    service.test_command_topic = "outstation/automation/test/set"
    new_doc = doc_with_topics(conditions=["new/topic"])
    service.apply_subscriptions(new_doc)
    assert "new/topic" in service.client.subscribed
    assert "old/topic" in service.client.unsubscribed
    assert service.subscribed_topics == {"new/topic", automation.BALANCE_TOPIC, service.test_command_topic}


class FakeSlave:
    def start(self, client):
        pass

    def note_update(self, client):
        pass


def test_on_connect_resubscribes_after_a_reconnect_with_an_unchanged_document():
    """Nach einem MQTT-Broker-Neustart feuert paho on_connect erneut, aber
    (bei clean_session=True) ohne Erinnerung an frueher gesetzte
    Subscriptions. apply_subscriptions() darf sich beim zweiten on_connect
    also nicht auf den alten subscribed_topics-Cache verlassen, sonst bleibt
    der Dienst nach einem Broker-Neustart auf keinem Topic mehr abonniert."""
    service = automation.AutomationService.__new__(automation.AutomationService)
    service.doc = doc_with_topics(conditions=["bms/state"])
    service.subscribed_topics = set()
    service.client = FakeClient()
    service.slave = FakeSlave()
    service.startup_error = None
    service.base_topic = "outstation/automation"
    service.test_command_topic = "outstation/automation/test/set"
    service.service_config = type('obj', (object,), {'service_id': 'automation'})()

    service.on_connect(service.client, None, {}, 0)
    service.client.subscribed.clear()  # nur den zweiten (Reconnect-)Aufruf pruefen

    service.on_connect(service.client, None, {}, 0)

    assert "bms/state" in service.client.subscribed
    assert automation.BALANCE_TOPIC in service.client.subscribed


def test_load_initial_document_returns_the_loaded_doc_when_valid(tmp_path):
    path = write_doc(tmp_path, document())
    config_store = automation.ReloadableConfig(path, automation.load_and_validate)
    doc, error = automation.load_initial_document(config_store)
    assert error is None
    assert doc.rules[0]["id"] == "r1"


def test_load_initial_document_falls_back_to_empty_rules_when_invalid(tmp_path):
    path = tmp_path / "automation_rules.json"
    path.write_text("{not valid json")
    config_store = automation.ReloadableConfig(str(path), automation.load_and_validate)
    doc, error = automation.load_initial_document(config_store)
    assert error is not None
    assert doc.rules == []


def test_publish_startup_rejection_publishes_a_rejected_slave_status():
    client = FakeClient()
    service = automation.AutomationService.__new__(automation.AutomationService)
    service.client = client
    service.doc = automation.RulesDocument(version=1, settings=automation.Settings(), rules=[])
    service.service_config = type('obj', (object,), {'service_id': 'automation'})()
    service._publish_startup_rejection(client, "bad json")
    payload = client.state_payload(suffix="/settings/status")
    assert payload["runtime_status"] == "rejected"
    assert payload["error"] == "bad json"


def test_reload_config_swaps_the_document_on_success(tmp_path):
    path = write_doc(tmp_path, document())
    config_store = automation.ReloadableConfig(path, automation.load_and_validate)
    doc, _ = automation.load_initial_document(config_store)
    service = automation.AutomationService.__new__(automation.AutomationService)
    service.config_store = config_store
    service.doc = doc
    service.client = FakeClient()
    service.subscribed_topics = set()
    service.executor = automation.ActionExecutor(publish_fn=lambda *a: None, publish_allowed_prefixes=[])
    service.base_topic = "outstation/automation"
    service.test_command_topic = "outstation/automation/test/set"
    service._history_path = None
    service.history = automation.RuleHistory(None)

    new_rules = document(rules=[rule(id="r2")])
    Path(path).write_text(json.dumps(new_rules))
    service.reload_config()
    assert service.doc.rules[0]["id"] == "r2"


def test_reload_config_leaves_old_rules_active_on_invalid_reload(tmp_path):
    path = write_doc(tmp_path, document())
    config_store = automation.ReloadableConfig(path, automation.load_and_validate)
    doc, _ = automation.load_initial_document(config_store)
    service = automation.AutomationService.__new__(automation.AutomationService)
    service.config_store = config_store
    service.doc = doc
    service.client = FakeClient()
    service.subscribed_topics = set()
    service.executor = automation.ActionExecutor(publish_fn=lambda *a: None, publish_allowed_prefixes=[])
    service._history_path = None
    service.history = automation.RuleHistory(None)

    Path(path).write_text("{not valid json")
    with pytest.raises(Exception):
        service.reload_config()
    assert service.doc.rules[0]["id"] == "r1"  # unchanged


def test_condition_value_reads_the_balance_field_and_threshold():
    cond = balance_condition(threshold=800)
    value, target = automation.condition_value(
        cond, balance={"grid_export": 620}, topic_values={}, now_ts=0)
    assert value == 620
    assert target == 800


def test_condition_value_is_none_when_the_balance_is_stale():
    cond = {"type": "balance_threshold", "field": "grid_export", "comparison": "above", "threshold": 800}
    value, target = automation.condition_value(cond, balance=None, topic_values={}, now_ts=0)
    assert value is None
    assert target == 800


def test_condition_value_reads_a_text_topic_value():
    cond = {"type": "topic_value", "topic": "werkstatt/mode", "json_key": "",
            "comparison": "equals", "text": "auto"}
    value, target = automation.condition_value(
        cond, balance=None, topic_values={"werkstatt/mode": "auto"}, now_ts=0)
    assert value == "auto"
    assert target == "auto"


def test_condition_value_reports_the_local_time_for_a_time_window():
    cond = {"type": "time_window", "start": "08:00", "end": "18:00", "weekdays": []}
    value, target = automation.condition_value(
        cond, balance=None, topic_values={}, now_ts=_timestamp_at(hour=14, minute=5, weekday=2))
    assert value == "14:05"
    assert target is None


def test_tick_reports_value_target_and_raw_met_per_condition():
    executor = automation.ActionExecutor(publish_fn=lambda *args: None, publish_allowed_prefixes=[])
    engine = automation.Engine(executor, started_at=0, event_sink=lambda message: None)
    doc = automation.RulesDocument(
        version=1,
        settings=automation.Settings(settling_seconds=0, balance_max_age_s=30),
        rules=[{"id": "r1", "name": "R", "enabled": True, "cooldown_seconds": 300,
                "conditions": [{"type": "balance_threshold", "field": "grid_export",
                                "comparison": "above", "threshold": 500, "hysteresis": 0,
                                "hold_seconds": 50}],
                "actions": [{"type": "notification", "severity": "info", "title": "t", "message": "m"}]}])
    results = engine.tick(doc, balance={"grid_export": 620}, balance_age=1,
                          topic_values={}, now_ts=1000, weekday=2)
    report = results["r1"]["conditions"][0]
    assert report["value"] == 620
    assert report["target"] == 500
    assert report["raw_met"] is True
    assert report["met"] is False  # Haltedauer laeuft noch
    assert report["hold_remaining"] == 50


def test_tick_keeps_the_existing_condition_report_fields():
    executor = automation.ActionExecutor(publish_fn=lambda *args: None, publish_allowed_prefixes=[])
    engine = automation.Engine(executor, started_at=0, event_sink=lambda message: None)
    doc = automation.RulesDocument(
        version=1,
        settings=automation.Settings(settling_seconds=0, balance_max_age_s=30),
        rules=[{"id": "r1", "name": "R", "enabled": True, "cooldown_seconds": 300,
                "conditions": [{"type": "balance_threshold", "field": "grid_export",
                                "comparison": "above", "threshold": 500, "hysteresis": 0,
                                "hold_seconds": 0}],
                "actions": [{"type": "notification", "severity": "info", "title": "t", "message": "m"}]}])
    results = engine.tick(doc, balance={"grid_export": 620}, balance_age=1,
                          topic_values={}, now_ts=1000, weekday=2)
    report = results["r1"]["conditions"][0]
    assert set(report) == {"met", "raw_met", "value", "target", "since", "hold_remaining"}


def _service_with(rules, *, prefixes=None, published=None, history_path=None, history_persist=True):
    """Baut einen AutomationService ohne MQTT-Client. published sammelt
    (topic, payload, retain)-Tupel aus _publish_action. history_path=None
    (Default) haelt die Historie im RAM, ohne die Platte zu beruehren."""
    from unittest.mock import Mock

    doc = automation.RulesDocument(
        version=1,
        settings=automation.Settings(settling_seconds=0, balance_max_age_s=30,
                                      publish_allowed_prefixes=prefixes or [],
                                      history_persist=history_persist),
        rules=rules)

    # Mock config objects for testing
    service_config = Mock()
    service_config.service_id = "automation"
    mqtt_config = Mock()

    # Mock config_store that returns the test doc
    config_store = Mock()
    config_store.load.return_value = doc

    service = automation.AutomationService(service_config, mqtt_config, config_store, history_path=history_path, started_at=0.0)
    sink = published if published is not None else []
    service._publish_action = lambda topic, payload, retain: sink.append((topic, payload, retain))
    service.executor.publish_fn = service._publish_action
    return service


def test_run_test_action_publishes_the_addressed_action():
    published = []
    service = _service_with(
        [{"id": "r1", "name": "R", "enabled": True, "cooldown_seconds": 300,
          "conditions": [{"type": "balance_threshold", "field": "grid_export",
                          "comparison": "above", "threshold": 500, "hold_seconds": 30}],
          "actions": [{"type": "notification", "severity": "info", "title": "t", "message": "m"},
                      {"type": "publish", "topic": "werkstatt/heizstab/set", "retain": False,
                       "payload_source": "constant", "payload": "on"}]}],
        prefixes=["werkstatt/"], published=published)
    result = service.run_test_action('{"rule_id": "r1", "action_index": 1}', now_ts=1000)
    assert result["status"] == "published"
    assert result["topic"] == "werkstatt/heizstab/set"
    assert result["payload"] == "on"
    assert published == [("werkstatt/heizstab/set", "on", False)]


def test_run_test_action_does_not_touch_the_rule_state_machine():
    service = _service_with(
        [{"id": "r1", "name": "R", "enabled": True, "cooldown_seconds": 300,
          "conditions": [{"type": "balance_threshold", "field": "grid_export",
                          "comparison": "above", "threshold": 500, "hold_seconds": 30}],
          "actions": [{"type": "publish", "topic": "werkstatt/heizstab/set", "retain": False,
                       "payload_source": "constant", "payload": "on"}]}],
        prefixes=["werkstatt/"])
    service.run_test_action('{"rule_id": "r1", "action_index": 0}', now_ts=1000)
    runtime = service.engine._runtime.get("r1")
    assert runtime is None or (runtime.fire_count == 0
                               and runtime.last_fired_at is None
                               and runtime.fired_this_episode is False)


def test_run_test_action_works_on_a_disabled_rule():
    published = []
    service = _service_with(
        [{"id": "r1", "name": "R", "enabled": False, "cooldown_seconds": 300,
          "conditions": [{"type": "balance_threshold", "field": "grid_export",
                          "comparison": "above", "threshold": 500, "hold_seconds": 30}],
          "actions": [{"type": "publish", "topic": "werkstatt/heizstab/set", "retain": False,
                       "payload_source": "constant", "payload": "on"}]}],
        prefixes=["werkstatt/"], published=published)
    result = service.run_test_action('{"rule_id": "r1", "action_index": 0}', now_ts=1000)
    assert result["status"] == "published"
    assert len(published) == 1


def test_run_test_action_respects_publish_allowed_prefixes():
    published = []
    service = _service_with(
        [{"id": "r1", "name": "R", "enabled": True, "cooldown_seconds": 300,
          "conditions": [{"type": "balance_threshold", "field": "grid_export",
                          "comparison": "above", "threshold": 500, "hold_seconds": 30}],
          "actions": [{"type": "publish", "topic": "fremd/topic/set", "retain": False,
                       "payload_source": "constant", "payload": "on"}]}],
        prefixes=["werkstatt/"], published=published)
    result = service.run_test_action('{"rule_id": "r1", "action_index": 0}', now_ts=1000)
    assert result["status"] == "blocked"
    assert result["reason"] == "topic does not match any allowed prefix"
    assert published == []


def test_run_test_action_rejects_an_unknown_rule_id():
    service = _service_with([])
    result = service.run_test_action('{"rule_id": "gibtsnicht", "action_index": 0}', now_ts=1000)
    assert result["status"] == "error"
    assert "gibtsnicht" in result["reason"]


def test_run_test_action_rejects_an_out_of_range_action_index():
    service = _service_with(
        [{"id": "r1", "name": "R", "enabled": True, "cooldown_seconds": 300,
          "conditions": [{"type": "time_window", "start": "00:00", "end": "23:59", "weekdays": []}],
          "actions": [{"type": "notification", "severity": "info", "title": "t", "message": "m"}]}])
    result = service.run_test_action('{"rule_id": "r1", "action_index": 7}', now_ts=1000)
    assert result["status"] == "error"
    assert "out of range" in result["reason"]


def test_run_test_action_rejects_a_malformed_payload():
    service = _service_with([])
    assert service.run_test_action("kein json", now_ts=1000)["status"] == "error"
    assert service.run_test_action('{"rule_id": 5, "action_index": "x"}', now_ts=1000)["status"] == "error"
    assert service.run_test_action('[]', now_ts=1000)["status"] == "error"


def test_run_test_action_records_an_event_marked_as_test():
    service = _service_with(
        [{"id": "r1", "name": "R", "enabled": True, "cooldown_seconds": 300,
          "conditions": [{"type": "time_window", "start": "00:00", "end": "23:59", "weekdays": []}],
          "actions": [{"type": "notification", "severity": "info", "title": "Titel", "message": "Text"}]}])
    service.run_test_action('{"rule_id": "r1", "action_index": 0}', now_ts=1000)
    messages = [entry["message"] for entry in service.events.as_list()]
    assert any(message.startswith("Test: ") for message in messages)


def test_engine_tick_records_history_for_a_history_enabled_rule(tmp_path):
    history_path = str(tmp_path / "automation_history.json")
    service = _service_with(
        [{"id": "r1", "name": "R", "enabled": True, "cooldown_seconds": 0, "history_enabled": True,
          "conditions": [{"type": "balance_threshold", "field": "grid_export", "comparison": "above",
                          "threshold": 500, "hysteresis": 0, "hold_seconds": 0}],
          "actions": [{"type": "notification", "severity": "info", "title": "t", "message": "m"}]}],
        history_path=history_path)
    service.engine.tick(service.doc, balance={"grid_export": 900}, balance_age=0, topic_values={}, now_ts=1000.0, weekday=0)
    events = service.history.as_list("r1")
    assert len(events) == 1
    assert events[0]["result"] == "fired"

    # ueberlebt einen Neustart: eine frische RuleHistory liest dieselbe Datei.
    restarted_history = automation.RuleHistory(history_path)
    assert restarted_history.as_list("r1")[0]["result"] == "fired"


def test_engine_tick_does_not_record_history_when_disabled():
    service = _service_with(
        [{"id": "r1", "name": "R", "enabled": True, "cooldown_seconds": 30,
          "conditions": [{"type": "time_window", "start": "00:00", "end": "23:59", "weekdays": []}],
          "actions": [{"type": "notification", "severity": "info", "title": "t", "message": "m"}]}])
    service.engine.tick(service.doc, balance=None, balance_age=None, topic_values={}, now_ts=1000.0, weekday=0)
    assert service.history.as_list("r1") == []


def test_run_test_action_records_history_marked_as_test(tmp_path):
    history_path = str(tmp_path / "automation_history.json")
    service = _service_with(
        [{"id": "r1", "name": "R", "enabled": True, "cooldown_seconds": 300, "history_enabled": True,
          "conditions": [{"type": "time_window", "start": "00:00", "end": "23:59", "weekdays": []}],
          "actions": [{"type": "notification", "severity": "info", "title": "Titel", "message": "Text"}]}],
        history_path=history_path)
    service.run_test_action('{"rule_id": "r1", "action_index": 0}', now_ts=1000.0)
    events = service.history.as_list("r1")
    assert len(events) == 1
    assert events[0]["test"] is True
    action = events[0]["actions"][0]
    assert action["type"] == "notification"
    assert action["severity"] == "info"
    assert action["title"] == "Titel"
    assert action["message"] == "Text"


def test_run_test_action_does_not_record_history_when_disabled():
    service = _service_with(
        [{"id": "r1", "name": "R", "enabled": True, "cooldown_seconds": 300,
          "conditions": [{"type": "time_window", "start": "00:00", "end": "23:59", "weekdays": []}],
          "actions": [{"type": "notification", "severity": "info", "title": "t", "message": "m"}]}])
    service.run_test_action('{"rule_id": "r1", "action_index": 0}', now_ts=1000.0)
    assert service.history.as_list("r1") == []


def test_history_persist_false_keeps_events_in_ram_without_touching_disk(tmp_path):
    """settings.history_persist=False: die Regel zeichnet weiter auf (im RAM
    fuer die Oberflaeche), aber es entsteht keine Datei - selbst wenn ein
    history_path uebergeben wurde (der Fall im echten Dienst, wo der Pfad
    aus AppConfig.history_file() kommt und die persist-Entscheidung erst
    aus settings.history_persist folgt)."""
    history_path = str(tmp_path / "automation_history.json")
    service = _service_with(
        [{"id": "r1", "name": "R", "enabled": True, "cooldown_seconds": 0, "history_enabled": True,
          "conditions": [{"type": "balance_threshold", "field": "grid_export", "comparison": "above",
                          "threshold": 500, "hysteresis": 0, "hold_seconds": 0}],
          "actions": [{"type": "notification", "severity": "info", "title": "t", "message": "m"}]}],
        history_path=history_path, history_persist=False)
    service.engine.tick(service.doc, balance={"grid_export": 900}, balance_age=0, topic_values={}, now_ts=1000.0, weekday=0)
    assert service.history.as_list("r1")[0]["result"] == "fired"
    assert not Path(history_path).exists()


def test_poll_core_publishes_live_history_even_when_persist_is_off(tmp_path):
    """Der eigentliche gemeldete Bug: history_persist=False heisst nur 'keine
    Datei', nicht 'kein Verlauf im Dashboard'. poll_core() muss die
    RAM-Historie live ueber das history-Topic tragen, unabhaengig von der
    Datei."""
    history_path = str(tmp_path / "automation_history.json")
    service = _service_with(
        [{"id": "r1", "name": "R", "enabled": True, "cooldown_seconds": 0, "history_enabled": True,
          "conditions": [{"type": "balance_threshold", "field": "grid_export", "comparison": "above",
                          "threshold": 500, "hysteresis": 0, "hold_seconds": 0}],
          "actions": [{"type": "notification", "severity": "info", "title": "t", "message": "m"}]},
         {"id": "r2", "name": "Ohne Verlauf", "enabled": True, "cooldown_seconds": 0,
          "conditions": [{"type": "balance_threshold", "field": "grid_export", "comparison": "above",
                          "threshold": 500, "hysteresis": 0, "hold_seconds": 0}],
          "actions": [{"type": "notification", "severity": "info", "title": "t", "message": "m"}]}],
        history_path=history_path, history_persist=False)
    service.client = FakeClient()
    service.slave = FakeSlave()
    service.last_balance = {"grid_export": 900}
    service.last_balance_at = time.time()

    service.poll_core()

    payload = service.client.state_payload(suffix="/history")
    assert payload["rules"]["r1"][0]["result"] == "fired"
    assert payload["rule_count"] == 1
    assert "r2" not in payload["rules"]  # history_enabled=False -> nicht im Payload
    assert not Path(history_path).exists()  # history_persist=False -> weiterhin keine Datei


def test_reload_config_disables_disk_persistence_live_when_history_persist_turns_off(tmp_path):
    rules_path = write_doc(tmp_path, document(rules=[rule(history_enabled=True)]))
    history_path = str(tmp_path / "automation_history.json")
    config_store = automation.ReloadableConfig(rules_path, automation.load_and_validate)
    doc, _ = automation.load_initial_document(config_store)
    service = automation.AutomationService.__new__(automation.AutomationService)
    service.config_store = config_store
    service.doc = doc
    service.client = FakeClient()
    service.subscribed_topics = set()
    service.executor = automation.ActionExecutor(publish_fn=lambda *a: None, publish_allowed_prefixes=[])
    service.base_topic = "outstation/automation"
    service.test_command_topic = "outstation/automation/test/set"
    service._history_path = history_path
    service.history = automation.RuleHistory(history_path)
    service.history.record("r1", {"at": 1.0, "result": "fired"}, limit=10)
    assert Path(history_path).exists()

    Path(rules_path).write_text(json.dumps(document(rules=[rule(history_enabled=True)],
                                                      settings={**minimal_settings(), "history_persist": False})))
    service.reload_config()
    assert service.history.path is None
    # bereits geschriebene Datei bleibt liegen (kein aktives Aufraeumen), aber
    # neue Eintraege landen nur noch im RAM.
    service.history.record("r1", {"at": 2.0, "result": "fired"}, limit=10)
    assert automation.RuleHistory(history_path).as_list("r1") == [{"at": 1.0, "result": "fired"}]
    assert service.history.as_list("r1")[0] == {"at": 2.0, "result": "fired"}


def test_required_topics_always_include_the_test_command_topic():
    service = _service_with([])
    assert service.test_command_topic in service.required_topics(service.doc)


def test_discovery_includes_a_test_result_entity():
    entries = automation.discovery_object_ids("automation", "outstation/automation", {"identifiers": ["automation"]})
    object_ids = [object_id for _component, object_id, _config in entries]
    assert "test_result" in object_ids


def test_balance_threshold_accepts_battery_soc():
    cond = {"type": "balance_threshold", "field": "battery_soc", "comparison": "below",
            "threshold": 20, "hysteresis": 0}
    errors = []
    automation._validate_condition(cond, 0, errors)
    assert errors == []

    met, reason = automation.evaluate_condition_raw(
        cond, balance={"battery_soc": 18.5}, topic_values={}, now_ts=0, weekday=0)
    assert met is True and reason == ""

    met, reason = automation.evaluate_condition_raw(
        cond, balance={"battery_soc": 42.0}, topic_values={}, now_ts=0, weekday=0)
    assert met is False and reason == ""


def test_balance_threshold_accepts_battery_energy_kwh():
    cond = {"type": "balance_threshold", "field": "battery_energy_kwh", "comparison": "above",
            "threshold": 5, "hysteresis": 0}
    errors = []
    automation._validate_condition(cond, 0, errors)
    assert errors == []

    met, _ = automation.evaluate_condition_raw(
        cond, balance={"battery_energy_kwh": 23.0}, topic_values={}, now_ts=0, weekday=0)
    assert met is True


def test_publish_action_accepts_battery_capacity_kwh_as_balance_field():
    action = {"type": "publish", "topic": "outstation/x/set", "payload_source": "balance",
              "field": "battery_capacity_kwh"}
    errors = []
    automation._validate_action(action, 0, errors)
    assert errors == []


def _entity_cond(**overrides):
    cond = {
        "type": "entity_value", "entity_id": "bank_a_soc",
        "topic": "outstation/battery_soc/state",
        "value_template": "{{ value_json.soc_a }}",
        "comparison": "below", "value": 20,
    }
    cond.update(overrides)
    return cond


def test_entity_value_validates_a_complete_condition():
    errors = []
    result = automation._validate_condition(_entity_cond(), 0, errors)
    assert errors == []
    assert result["hold_seconds"] == 0


def test_entity_value_rejects_a_wildcard_topic():
    errors = []
    automation._validate_condition(_entity_cond(topic="outstation/+/state"), 0, errors)
    assert any("topic" in e or "wildcard" in e for e in errors)


def test_entity_value_rejects_an_empty_entity_id():
    errors = []
    automation._validate_condition(_entity_cond(entity_id=""), 0, errors)
    assert any("entity_id" in e for e in errors)


def test_entity_value_rejects_an_unreadable_template():
    errors = []
    automation._validate_condition(_entity_cond(value_template="{{ value_json.a.b.c }}"), 0, errors)
    assert any("value_template" in e for e in errors)


def test_entity_value_accepts_an_empty_template():
    errors = []
    automation._validate_condition(_entity_cond(value_template=""), 0, errors)
    assert errors == []


def test_entity_value_rejects_a_bad_comparison():
    errors = []
    automation._validate_condition(_entity_cond(comparison="near"), 0, errors)
    assert any("comparison" in e for e in errors)


def test_entity_value_requires_a_number_for_above_below():
    errors = []
    automation._validate_condition(_entity_cond(comparison="above", value="viel"), 0, errors)
    assert any("number" in e for e in errors)


def test_entity_value_requires_text_or_value_for_equals():
    cond = _entity_cond(comparison="equals")
    del cond["value"]
    errors = []
    automation._validate_condition(cond, 0, errors)
    assert any("text or value" in e for e in errors)


def test_entity_value_evaluates_below():
    cond = _entity_cond(comparison="below", value=20)
    payloads = {"outstation/battery_soc/state": '{"soc_a": 18.5}'}
    met, reason = automation.evaluate_condition_raw(
        cond, balance={}, topic_values=payloads, now_ts=0, weekday=0)
    assert met is True and reason == ""


def test_entity_value_evaluates_above():
    cond = _entity_cond(comparison="above", value=20)
    payloads = {"outstation/battery_soc/state": '{"soc_a": 18.5}'}
    met, _ = automation.evaluate_condition_raw(
        cond, balance={}, topic_values=payloads, now_ts=0, weekday=0)
    assert met is False


def test_entity_value_evaluates_equals_on_text():
    cond = _entity_cond(value_template="{{ value_json.mode }}", comparison="equals", text="ON")
    del cond["value"]
    payloads = {"outstation/battery_soc/state": '{"mode": "ON"}'}
    met, _ = automation.evaluate_condition_raw(
        cond, balance={}, topic_values=payloads, now_ts=0, weekday=0)
    assert met is True


def test_entity_value_equals_compares_against_gos_number_formatting():
    # Go gibt 80.0 als "80" aus, nicht als "80.0" - str(80.0) waere hier falsch.
    cond = _entity_cond(comparison="equals", text="80")
    del cond["value"]
    cond["value_template"] = "{{ value_json.soc_a }}"
    payloads = {"outstation/battery_soc/state": '{"soc_a": 80.0}'}
    met, _ = automation.evaluate_condition_raw(
        cond, balance={}, topic_values=payloads, now_ts=0, weekday=0)
    assert met is True


def test_entity_value_evaluates_not_equals():
    cond = _entity_cond(value_template="{{ value_json.mode }}", comparison="not_equals", text="ON")
    del cond["value"]
    payloads = {"outstation/battery_soc/state": '{"mode": "OFF"}'}
    met, _ = automation.evaluate_condition_raw(
        cond, balance={}, topic_values=payloads, now_ts=0, weekday=0)
    assert met is True


def test_entity_value_reports_topic_unknown():
    met, reason = automation.evaluate_condition_raw(
        _entity_cond(), balance={}, topic_values={}, now_ts=0, weekday=0)
    assert met is False and reason == "topic_unknown"


def test_entity_value_reports_value_unparseable_for_a_missing_key():
    payloads = {"outstation/battery_soc/state": '{"other": 1}'}
    met, reason = automation.evaluate_condition_raw(
        _entity_cond(), balance={}, topic_values=payloads, now_ts=0, weekday=0)
    assert met is False and reason == "value_unparseable"


def test_entity_value_reports_value_unparseable_for_a_non_numeric_value():
    cond = _entity_cond(value_template="{{ value_json.mode }}", comparison="below", value=20)
    payloads = {"outstation/battery_soc/state": '{"mode": "ON"}'}
    met, reason = automation.evaluate_condition_raw(
        cond, balance={}, topic_values=payloads, now_ts=0, weekday=0)
    assert met is False and reason == "value_unparseable"


def test_entity_value_condition_value_returns_value_and_target():
    payloads = {"outstation/battery_soc/state": '{"soc_a": 18.5}'}
    value, target = automation.condition_value(
        _entity_cond(), balance={}, topic_values=payloads, now_ts=0)
    assert value == 18.5
    assert target == 20


def test_entity_value_condition_value_returns_text_for_equals():
    cond = _entity_cond(value_template="{{ value_json.mode }}", comparison="equals", text="ON")
    del cond["value"]
    payloads = {"outstation/battery_soc/state": '{"mode": "OFF"}'}
    value, target = automation.condition_value(
        cond, balance={}, topic_values=payloads, now_ts=0)
    assert value == "OFF"
    assert target == "ON"


def test_entity_value_condition_value_is_none_without_a_payload():
    value, target = automation.condition_value(
        _entity_cond(), balance={}, topic_values={}, now_ts=0)
    assert value is None
    assert target == 20


def test_required_topics_includes_entity_value_topics():
    doc = automation.RulesDocument(
        version=1, settings=automation.Settings(),
        rules=[{
            "id": "r1", "name": "n", "enabled": True, "cooldown_seconds": 60,
            "conditions": [_entity_cond()],
            "actions": [{"type": "notification", "severity": "info", "title": "t", "message": "m"}],
        }],
    )
    # Dasselbe Muster wie test_required_topics_collects_condition_and_action_source_topics
    # (Zeile 725): __new__ ohne __init__, weil required_topics eine reine
    # Funktion ist und kein MQTT braucht.
    service = automation.AutomationService.__new__(automation.AutomationService)
    service.test_command_topic = "outstation/automation/test/set"
    assert "outstation/battery_soc/state" in service.required_topics(doc)


def test_entity_value_inherits_the_hold_seconds_guard_for_publish_rules():
    rule = {
        "id": "r1", "enabled": True, "cooldown_seconds": 60,
        "conditions": [_entity_cond(hold_seconds=5)],
        "actions": [{"type": "publish", "topic": "outstation/x/set",
                     "payload_source": "constant", "payload": "1"}],
    }
    errors = []
    automation._validate_rule(rule, 0, errors, set())
    assert any("hold_seconds must be >= 30" in e for e in errors)


def test_extract_value_returns_none_for_a_non_scalar_result():
    # Nicht in der geteilten Fixture: Go druckt hier seine eigene
    # Map-Darstellung, die Python bewusst nicht nachbaut.
    import ha_template
    assert ha_template.extract_value('{"a": {"b": 1}}', "{{ value_json.a }}") is None


def test_battery_soc_condition_type_is_rejected():
    cond = {"type": "battery_soc", "topic": "werkstatt/bms/state",
            "json_key": "soc_percent", "comparison": "above", "value": 80}
    errors = []
    automation._validate_condition(cond, 0, errors)
    assert any("unknown type" in e for e in errors)
