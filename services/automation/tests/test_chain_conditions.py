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
