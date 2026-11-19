import json
import shutil
from pathlib import Path

import pytest

from console_rulecheck import RuleCheckTab, SCENARIOS
from fake_console import Log

REPO = Path(__file__).resolve().parents[2]
FIXTURE = REPO / "demo" / "fixtures" / "devices" / "demo" / "automation_rules.json"


@pytest.fixture
def rules_path(tmp_path):
    path = tmp_path / "automation_rules.json"
    shutil.copyfile(FIXTURE, path)
    return path


@pytest.fixture
def tab(rules_path):
    return RuleCheckTab(Log(), rules_path)


def _document(path):
    return json.loads(path.read_text(encoding="utf-8"))


def _scenario(state, scenario_id):
    return next(s for s in state if s["id"] == scenario_id)


def _rule_ids(scenario_id):
    return [rule["id"] for rule in next(s for s in SCENARIOS if s["id"] == scenario_id)["rules"]]


def test_insert_adds_exactly_the_scenario_rules(tab, rules_path):
    original = _document(FIXTURE)

    tab.act("insert", {"scenario": "duplicate"})

    doc = _document(rules_path)
    assert [r["id"] for r in doc["rules"]] == [r["id"] for r in original["rules"]] + _rule_ids("duplicate")
    assert doc["rules"][: len(original["rules"])] == original["rules"]
    assert doc["settings"] == original["settings"]
    assert doc["scenarios"] == original["scenarios"]
    assert doc["version"] == original["version"]


def test_insert_twice_is_idempotent(tab, rules_path):
    tab.act("insert", {"scenario": "contradiction"})
    once = _document(rules_path)

    tab.act("insert", {"scenario": "contradiction"})

    assert _document(rules_path) == once


def test_remove_restores_the_original_document(tab, rules_path):
    original = _document(FIXTURE)
    tab.act("insert", {"scenario": "orphan"})
    tab.act("insert", {"scenario": "frequent"})

    tab.act("remove", {"scenario": "orphan"})
    tab.act("remove", {"scenario": "frequent"})

    assert _document(rules_path) == original


def test_insert_all_and_remove_all(tab, rules_path):
    original = _document(FIXTURE)

    state = tab.act("insert_all", {})
    doc = _document(rules_path)
    inserted_ids = [r["id"] for r in doc["rules"] if r["id"].startswith("chk_")]
    expected_ids = [rid for s in SCENARIOS for rid in (r["id"] for r in s["rules"])]
    assert sorted(inserted_ids) == sorted(expected_ids)
    assert all(s["inserted"] for s in state)

    state = tab.act("remove_all", {})
    assert _document(rules_path) == original
    assert not any(s["inserted"] for s in state)


def test_reset_removes_only_chk_rules(tab, rules_path):
    original = _document(FIXTURE)
    tab.act("insert_all", {})

    tab.reset()

    assert _document(rules_path) == original


def test_unknown_scenario_is_value_error(tab):
    with pytest.raises(ValueError):
        tab.act("insert", {"scenario": "gibt_es_nicht"})
    with pytest.raises(ValueError):
        tab.act("remove", {})


def test_unknown_action_is_value_error(tab):
    with pytest.raises(ValueError):
        tab.act("explode", {})


def test_missing_file_is_value_error(tmp_path):
    tab = RuleCheckTab(Log(), tmp_path / "fehlt.json")

    with pytest.raises(ValueError, match="Regeldatei nicht lesbar"):
        tab.state()
    with pytest.raises(ValueError, match="Regeldatei nicht lesbar"):
        tab.act("insert", {"scenario": "duplicate"})


def test_state_flags_inserted_scenarios(tab):
    state = tab.state()
    assert [s["id"] for s in state] == [s["id"] for s in SCENARIOS]
    assert all(set(s) == {"id", "title", "expect", "note", "inserted"} for s in state)
    assert not any(s["inserted"] for s in state)

    state = tab.act("insert", {"scenario": "unsatisfiable"})

    assert _scenario(state, "unsatisfiable")["inserted"] is True
    assert _scenario(state, "duplicate")["inserted"] is False


def test_every_scenario_rule_has_the_document_shape(tab):
    ids = set()
    for scenario in SCENARIOS:
        assert scenario["expect"] and scenario["note"] is not None
        for rule in scenario["rules"]:
            assert rule["id"].startswith("chk_")
            assert rule["id"] not in ids
            ids.add(rule["id"])
            for key in ("id", "name", "enabled", "conditions", "actions"):
                assert key in rule
            assert rule["enabled"] is True
            assert rule["history_enabled"] is True
            assert rule["conditions"]
            assert rule["actions"][0]["type"] == "publish"
            assert rule["actions"][0]["payload_source"] == "constant"


def test_texts_follow_the_style_rules():
    for scenario in SCENARIOS:
        for text in (scenario["title"], scenario["expect"], scenario["note"]):
            assert ";" not in text
            assert "–" not in text and "—" not in text
