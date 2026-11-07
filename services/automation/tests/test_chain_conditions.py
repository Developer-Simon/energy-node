"""Tests fuer die Kettenbedingungen chain_finding und chain_value (Validierung, Topics)."""

import json

import automation_mqtt as automation


def chain_finding(**overrides):
    cond = {"type": "chain_finding", "chain_id": "balkon", "kind": "frozen"}
    cond.update(overrides)
    return cond


def chain_value(**overrides):
    cond = {"type": "chain_value", "chain_id": "balkon", "field": "standby_w",
            "comparison": "above", "value": 10}
    cond.update(overrides)
    return cond


def validate(cond):
    errors = []
    result = automation._validate_condition(cond, 0, errors)
    return result, errors


def test_validate_chain_finding_defaults_and_errors():
    result, errors = validate(chain_finding())
    assert errors == []
    assert result["points"] == []
    assert result["min_age_s"] == 0
    assert result["once"] is False
    assert "hold_seconds" not in result

    bad_conditions = [
        {"type": "chain_finding", "kind": "frozen"},
        chain_finding(chain_id="Balkon"),
        chain_finding(chain_id="bal-kon"),
        chain_finding(kind="unknown"),
        chain_finding(points="sensor.dc"),
        chain_finding(points=[1]),
        chain_finding(min_age_s=-1),
        chain_finding(min_age_s=86401),
        chain_finding(min_age_s=True),
        chain_finding(min_age_s="900"),
        chain_finding(once="yes"),
    ]
    for cond in bad_conditions:
        _, errors = validate(cond)
        assert errors, f"expected an error for {cond}"


def test_validate_chain_value_defaults_and_errors():
    result, errors = validate(chain_value())
    assert errors == []
    assert result["hold_seconds"] == 0

    bad_conditions = [
        {"type": "chain_value", "field": "standby_w", "comparison": "above", "value": 10},
        chain_value(field="pv"),
        chain_value(comparison="equals"),
        chain_value(value="10"),
        chain_value(value=True),
    ]
    for cond in bad_conditions:
        _, errors = validate(cond)
        assert errors, f"expected an error for {cond}"


def _write_rules(tmp_path, rules):
    doc = {
        "version": 1,
        "settings": {"tick_interval_s": 10, "settling_seconds": 60,
                     "balance_max_age_s": 30, "publish_allowed_prefixes": []},
        "rules": rules,
    }
    path = tmp_path / "automation_rules.json"
    path.write_text(json.dumps(doc))
    return str(path)


def test_publish_rule_with_chain_finding_needs_no_hold(tmp_path):
    rule = {
        "id": "r1", "name": "Rule 1", "enabled": True, "cooldown_seconds": 300,
        "conditions": [chain_finding()],
        "actions": [{"type": "publish", "topic": "werkstatt/wallbox/set", "retain": False,
                     "payload_source": "constant", "payload": "1"}],
    }
    result = automation.load_and_validate(_write_rules(tmp_path, [rule]))
    assert result.settings.publish_min_interval_s == 30
    assert result.rules[0]["conditions"][0]["type"] == "chain_finding"


def test_chains_topic_is_protected():
    assert automation.validate_publish_topic(automation.CHAINS_TOPIC) is not None


def _service():
    service = automation.AutomationService.__new__(automation.AutomationService)  # no MQTT needed for this pure method
    service.test_command_topic = "outstation/automation/test/set"
    return service


def _rule(conditions):
    return {"id": "r1", "name": "n", "enabled": True, "cooldown_seconds": 300,
            "conditions": conditions, "actions": [{"type": "notification", "severity": "info",
                                                   "title": "t", "message": "m"}]}


def _scenario_with_until(conditions):
    return {
        "id": "s1", "name": "S", "enabled": True,
        "when": {"conditions": [{"type": "time_window", "start": "00:00", "end": "23:59", "weekdays": []}]},
        "until": {"conditions": conditions},
        "on_start": {"actions": []}, "on_end": {"actions": []},
        "while": {"mode": "none"},
    }


def test_required_topics_include_chains_only_when_used():
    plain = automation.RulesDocument(version=1, settings=automation.Settings(),
                                     rules=[_rule([{"type": "time_window", "start": "00:00", "end": "23:59"}])])
    assert automation.CHAINS_TOPIC not in _service().required_topics(plain)

    in_until = automation.RulesDocument(
        version=1, settings=automation.Settings(), rules=[],
        scenarios=[_scenario_with_until([chain_value()])],
    )
    assert automation.CHAINS_TOPIC in _service().required_topics(in_until)

    in_rule = automation.RulesDocument(version=1, settings=automation.Settings(),
                                       rules=[_rule([chain_finding()])])
    assert automation.CHAINS_TOPIC in _service().required_topics(in_rule)


def chains_msg(at, chains):
    return json.dumps({"at": at, "chains": chains})


def balkon(**overrides):
    chain = {"label": "Balkon", "flow": "down", "loss_w": 3.0, "efficiency_pct": 95.0,
             "standby_w": 12.0, "findings": []}
    chain.update(overrides)
    return chain


def finding_cond(**overrides):
    cond = {"type": "chain_finding", "chain_id": "balkon", "kind": "any", "points": [], "min_age_s": 0}
    cond.update(overrides)
    return cond


def test_chain_finding_matches_kind_and_points():
    now = 10000.0
    chain = balkon(findings=[
        {"kind": "frozen", "from": "sensor.dc", "since": now - 1000},
        {"kind": "drift", "from": "sensor.plug", "to": "sensor.ac", "since": now - 1000},
    ])
    assert len(automation.matching_findings(finding_cond(kind="frozen"), chain, now)) == 1
    assert automation.matching_findings(finding_cond(kind="drift"), chain, now)[0][:3] == (
        "drift", "sensor.plug", "sensor.ac")
    assert automation.matching_findings(finding_cond(kind="drift"), chain, now)
    assert len(automation.matching_findings(finding_cond(kind="any"), chain, now)) == 2
    assert len(automation.matching_findings(finding_cond(kind="frozen", points=["sensor.dc"]), chain, now)) == 1
    assert automation.matching_findings(finding_cond(kind="frozen", points=["sensor.plug"]), chain, now) == []
    assert len(automation.matching_findings(finding_cond(kind="any", points=["sensor.ac"]), chain, now)) == 1


def test_chain_finding_min_age_counts_from_since():
    now = 10000.0
    young = balkon(findings=[{"kind": "frozen", "from": "sensor.dc", "since": now - 600}])
    old = balkon(findings=[{"kind": "frozen", "from": "sensor.dc", "since": now - 900}])
    cond = finding_cond(kind="frozen", min_age_s=900)
    assert automation.matching_findings(cond, young, now) == []
    assert len(automation.matching_findings(cond, old, now)) == 1


def test_chain_conditions_unknown_stale_missing():
    now = 10000.0
    cond = finding_cond(kind="frozen")
    value_cond = {"type": "chain_value", "chain_id": "balkon", "field": "standby_w",
                  "comparison": "above", "value": 10, "hold_seconds": 0}

    assert automation.evaluate_condition_raw(cond, balance=None, topic_values={}, now_ts=now) == (False, "chains_unknown")

    stale = {automation.CHAINS_TOPIC: chains_msg(now - 61, {"balkon": balkon()})}
    assert automation.evaluate_condition_raw(cond, balance=None, topic_values=stale, now_ts=now) == (False, "chains_stale")

    fresh = {automation.CHAINS_TOPIC: chains_msg(now - 60, {"balkon": balkon()})}
    assert automation.evaluate_condition_raw(cond, balance=None, topic_values=fresh, now_ts=now) == (False, "")

    broken = {automation.CHAINS_TOPIC: "{nicht json"}
    assert automation.evaluate_condition_raw(cond, balance=None, topic_values=broken, now_ts=now) == (False, "chains_unknown")

    missing = {automation.CHAINS_TOPIC: chains_msg(now, {"other": balkon()})}
    assert automation.evaluate_condition_raw(cond, balance=None, topic_values=missing, now_ts=now) == (False, "chain_missing")

    # Keiner dieser Gruende setzt saw_stale.
    for topic_values in ({}, stale, broken, missing):
        result = automation.evaluate_conditions([value_cond], [automation.ConditionRuntime()], balance=None,
                                                topic_values=topic_values, now_ts=now, location=None)
        assert result.saw_stale is False


def test_chain_value_compares_and_handles_null():
    now = 10000.0
    cond = {"type": "chain_value", "chain_id": "balkon", "field": "standby_w",
            "comparison": "above", "value": 10, "hold_seconds": 0}
    msg = {automation.CHAINS_TOPIC: chains_msg(now, {"balkon": balkon(standby_w=12)})}
    assert automation.evaluate_condition_raw(cond, balance=None, topic_values=msg, now_ts=now) == (True, "")
    below = dict(cond, comparison="below")
    assert automation.evaluate_condition_raw(below, balance=None, topic_values=msg, now_ts=now) == (False, "")

    null_msg = {automation.CHAINS_TOPIC: chains_msg(now, {"balkon": balkon(standby_w=None)})}
    assert automation.evaluate_condition_raw(cond, balance=None, topic_values=null_msg, now_ts=now) == (False, "value_missing")


def test_chain_value_hold_seconds():
    t0 = 10000.0
    cond = {"type": "chain_value", "chain_id": "balkon", "field": "standby_w",
            "comparison": "above", "value": 10, "hold_seconds": 7200}
    runtime = automation.ConditionRuntime()

    def tick(now, standby):
        topic_values = {automation.CHAINS_TOPIC: chains_msg(now, {"balkon": balkon(standby_w=standby)})}
        return automation._evaluate_one(cond, runtime, balance=None, topic_values=topic_values,
                                        now_ts=now, location=None)

    assert tick(t0, 12) == (True, False, "")
    assert tick(t0 + 3600, 12) == (True, False, "")
    assert tick(t0 + 7200, 12) == (True, True, "")
    # Ein Null-Wert setzt die Haltezeit zurueck.
    assert tick(t0 + 7300, None)[1] is False
    assert tick(t0 + 7400, 12) == (True, False, "")
    assert tick(t0 + 7400 + 7200, 12)[1] is True


def test_condition_value_for_chain_conditions():
    now = 10000.0
    topic_values = {automation.CHAINS_TOPIC: chains_msg(now, {"balkon": balkon(
        standby_w=12, findings=[{"kind": "frozen", "from": "sensor.dc", "since": now - 1000}])})}
    value_cond = {"type": "chain_value", "chain_id": "balkon", "field": "standby_w",
                  "comparison": "above", "value": 10, "hold_seconds": 0}
    assert automation.condition_value(value_cond, balance=None, topic_values=topic_values, now_ts=now) == (12.0, 10)
    assert automation.condition_value(finding_cond(kind="frozen"), balance=None,
                                      topic_values=topic_values, now_ts=now) == (1, None)

    stale = {automation.CHAINS_TOPIC: chains_msg(now - 61, {"balkon": balkon()})}
    assert automation.condition_value(finding_cond(kind="frozen"), balance=None,
                                      topic_values=stale, now_ts=now) == (None, None)
