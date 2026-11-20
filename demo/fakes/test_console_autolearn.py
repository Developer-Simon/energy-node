import gzip
import http.server
import json
import shutil
import socket
import threading
from datetime import date, timedelta
from pathlib import Path

import pytest

from console_autolearn import ACTIONS, AutolearnTab, SCENARIOS
from fake_console import Log

REPO = Path(__file__).resolve().parents[2]
FIXTURE = REPO / "demo" / "fixtures" / "devices" / "demo" / "automation_rules.json"
STATE_TOPIC = "outstation/automation/state"
STATIC_STATE = {"rules_enabled": 3, "online": True}


@pytest.fixture
def broker():
    """Stand-in for the control server of the fake broker: records every POST body."""
    posts = []

    class Handler(http.server.BaseHTTPRequestHandler):
        def log_message(self, *args):
            pass

        def do_POST(self):
            length = int(self.headers.get("Content-Length", 0))
            posts.append((self.path, json.loads(self.rfile.read(length).decode("utf-8"))))
            body = b"{}"
            self.send_response(200)
            self.send_header("Content-Type", "application/json")
            self.send_header("Content-Length", str(len(body)))
            self.end_headers()
            self.wfile.write(body)

    server = http.server.ThreadingHTTPServer(("127.0.0.1", 0), Handler)
    threading.Thread(target=server.serve_forever, daemon=True).start()
    yield f"http://127.0.0.1:{server.server_address[1]}", posts
    server.shutdown()
    server.server_close()


def _states(posts):
    """Decoded state payloads in publish order, checks the topic and the retain flag."""
    for path, body in posts:
        assert path == "/publish"
        assert body["topic"] == STATE_TOPIC
        assert body["retain"] is True
    return [json.loads(body["payload"]) for _, body in posts]


@pytest.fixture
def rules_path(tmp_path):
    path = tmp_path / "automation_rules.json"
    shutil.copyfile(FIXTURE, path)
    return path


@pytest.fixture
def tab(rules_path):
    return AutolearnTab(Log(), rules_path)


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
    tab = AutolearnTab(Log(), tmp_path / "fehlt.json")

    with pytest.raises(ValueError, match="Regeldatei nicht lesbar"):
        tab.state()
    with pytest.raises(ValueError, match="Regeldatei nicht lesbar"):
        tab.act("insert", {"scenario": "duplicate"})


def test_state_flags_inserted_scenarios(tab):
    state = tab.state()
    assert [s["id"] for s in state] == [s["id"] for s in SCENARIOS]
    assert all(set(s) == {"id", "title", "expect", "note", "inserted", "triggerable"} for s in state)
    assert _scenario(state, "frequent")["triggerable"] is True
    assert not any(s["triggerable"] for s in state if s["id"] != "frequent")
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


def test_every_example_rule_meets_the_minimum_publish_interval():
    """Die Oberflaeche blockiert das Speichern unter 30 s Abkuehl- und Haltezeit."""
    from console_autolearn import SCENARIOS

    for scenario in SCENARIOS:
        for rule in scenario["rules"]:
            assert rule["cooldown_seconds"] >= 30, rule["id"]
            for condition in rule["conditions"]:
                if "hold_seconds" in condition:
                    assert condition["hold_seconds"] >= 30, rule["id"]


def test_insert_publishes_zero_counts_for_every_rule(rules_path, broker):
    url, posts = broker
    tab = AutolearnTab(Log(), rules_path, url)

    tab.act("insert", {"scenario": "duplicate"})

    doc = _document(rules_path)
    states = _states(posts)
    assert len(states) == 1
    state = states[0]
    assert state["online"] is True
    assert state["rules_enabled"] == sum(1 for r in doc["rules"] if r["enabled"])
    assert state["rules"] == {r["id"]: {"trigger_count": 0} for r in doc["rules"]}
    assert "regel_ueberschuss_wallbox" in state["rules"]
    assert "chk_dup_a" in state["rules"]


def test_trigger_adds_four_to_the_frequent_rule_only(rules_path, broker):
    url, posts = broker
    tab = AutolearnTab(Log(), rules_path, url)
    tab.act("insert", {"scenario": "duplicate"})
    tab.act("insert", {"scenario": "frequent"})

    tab.act("trigger", {"scenario": "frequent"})

    counts = _states(posts)[-1]["rules"]
    assert counts["chk_pendeln"] == {"trigger_count": 4}
    assert all(v == {"trigger_count": 0} for k, v in counts.items() if k != "chk_pendeln")

    tab.act("trigger", {"scenario": "frequent"})

    assert _states(posts)[-1]["rules"]["chk_pendeln"] == {"trigger_count": 8}


def test_trigger_defaults_to_four_and_accepts_times(rules_path, broker):
    url, posts = broker
    tab = AutolearnTab(Log(), rules_path, url)
    tab.act("insert", {"scenario": "frequent"})

    tab.act("trigger", {"scenario": "frequent", "times": 2})
    assert _states(posts)[-1]["rules"]["chk_pendeln"] == {"trigger_count": 2}

    tab.act("trigger", {"scenario": "frequent", "times": 50})
    assert _states(posts)[-1]["rules"]["chk_pendeln"] == {"trigger_count": 52}


def test_trigger_rejects_other_scenarios_and_bad_times(rules_path, broker):
    url, posts = broker
    tab = AutolearnTab(Log(), rules_path, url)
    tab.act("insert", {"scenario": "duplicate"})
    published = len(posts)

    with pytest.raises(ValueError):
        tab.act("trigger", {"scenario": "duplicate"})
    with pytest.raises(ValueError):
        tab.act("trigger", {"scenario": "gibt_es_nicht"})
    with pytest.raises(ValueError):
        tab.act("trigger", {})
    for times in (0, 51, -1, True, "4", 2.5):
        with pytest.raises(ValueError):
            tab.act("trigger", {"scenario": "frequent", "times": times})

    assert len(posts) == published


def test_trigger_is_an_action(tab):
    assert "trigger" in ACTIONS


def test_reset_publishes_the_static_payload_and_clears_counts(rules_path, broker):
    url, posts = broker
    tab = AutolearnTab(Log(), rules_path, url)
    tab.act("insert", {"scenario": "frequent"})
    tab.act("trigger", {"scenario": "frequent"})

    tab.reset()

    assert _states(posts)[-1] == STATIC_STATE
    tab.act("insert", {"scenario": "frequent"})
    assert _states(posts)[-1]["rules"]["chk_pendeln"] == {"trigger_count": 0}


def test_works_without_broker_control(rules_path):
    tab = AutolearnTab(Log(), rules_path)

    tab.act("insert", {"scenario": "frequent"})
    tab.act("trigger", {"scenario": "frequent"})
    tab.reset()

    assert _document(rules_path) == _document(FIXTURE)


def test_dead_broker_does_not_raise_and_logs_an_error(rules_path):
    with socket.socket() as probe:
        probe.bind(("127.0.0.1", 0))
        dead_port = probe.getsockname()[1]
    log = Log()
    tab = AutolearnTab(log, rules_path, f"http://127.0.0.1:{dead_port}")

    state = tab.act("insert", {"scenario": "frequent"})
    tab.act("trigger", {"scenario": "frequent"})
    tab.reset()

    assert _scenario(state, "frequent")["inserted"] is True
    assert any(e["level"] == "error" for e in log.since(0)[1])


DEVICES = ("demo_heater", "demo_boiler", "demo_washer", "demo_pump")
DEVICE_KEYS = ("seen", "reach", "on_min", "avg_w", "wh", "pn", "hand", "ext", "rule")
SITE_KEYS = ("seen", "load_total", "grid_import", "grid_export", "pv", "battery_charge", "battery_discharge")
LEARNING_ACTIONS = ("day_create", "day_remove", "ack_reset")


@pytest.fixture
def data_dir(tmp_path):
    folder = tmp_path / "data"
    folder.mkdir()
    return folder


@pytest.fixture
def log():
    return Log()


@pytest.fixture
def learning(rules_path, data_dir, log):
    return AutolearnTab(log, rules_path, "", data_dir)


def _days(data_dir):
    folder = data_dir / "autolearn" / "days"
    return sorted(folder.glob("*.json.gz")) if folder.is_dir() else []


def _read_gz(path):
    return json.loads(gzip.decompress(path.read_bytes()).decode("utf-8"))


def _texts(log):
    return [entry["text"] for entry in log.since(0)[1]]


def _write_settings(data_dir, data):
    (data_dir / "settings.json").write_text(json.dumps(data, indent=2), encoding="utf-8")


def test_learning_actions_are_actions():
    assert set(LEARNING_ACTIONS) <= set(ACTIONS)


def test_day_create_writes_one_valid_file_per_past_day(learning, data_dir):
    learning.act("day_create", {"days": 3})

    today = date.today()
    expected = sorted(f"{(today - timedelta(days=k)).isoformat()}.json.gz" for k in (1, 2, 3))
    assert [p.name for p in _days(data_dir)] == expected
    for path in _days(data_dir):
        data = _read_gz(path)
        assert data["fake"] is True
        assert data["v"] == 1
        assert data["day"] == path.name[: -len(".json.gz")]
        assert set(data["devices"]) == set(DEVICES)
        for device in DEVICES:
            entry = data["devices"][device]
            assert set(entry) == set(DEVICE_KEYS)
            for key in DEVICE_KEYS:
                assert len(entry[key]) == 96, (device, key)
            assert all(v == 15 for v in entry["seen"])
            assert all(0 <= v <= 15 for v in entry["on_min"])
        assert set(data["site"]) == set(SITE_KEYS)
        for key in SITE_KEYS:
            assert len(data["site"][key]) == 96, key
        assert data["site"]["seen"] == [15] * 96
        assert data["events"], "a day has switching events"
        assert {e["o"] for e in data["events"]} <= {"hand", "ext", "rule"}
        assert all(len(e) == 8 for e in data["events"])
        assert {a["r"] for a in data["actions"]} <= {"regel_ueberschuss_wallbox", "nulleinspeisung"}


def test_day_create_default_is_fourteen_days(learning, data_dir):
    learning.act("day_create", {})

    assert len(_days(data_dir)) == 14


def test_day_create_rejects_bad_day_counts(learning, data_dir):
    for days in (0, 57, "3", True, 2.5):
        with pytest.raises(ValueError, match="Anzahl der Tage"):
            learning.act("day_create", {"days": days})
    assert _days(data_dir) == []


def test_day_create_does_not_overwrite_a_foreign_file(learning, data_dir, log):
    folder = data_dir / "autolearn" / "days"
    folder.mkdir(parents=True)
    yesterday = (date.today() - timedelta(days=1)).isoformat()
    foreign = folder / f"{yesterday}.json.gz"
    raw = gzip.compress(json.dumps({"v": 1, "day": yesterday, "devices": {}}).encode("utf-8"))
    foreign.write_bytes(raw)

    learning.act("day_create", {"days": 2})

    assert foreign.read_bytes() == raw
    assert len(_days(data_dir)) == 2
    assert any("übersprungen" in text for text in _texts(log))


def test_day_remove_deletes_only_marked_files(learning, data_dir, log):
    learning.act("day_create", {"days": 3})
    folder = data_dir / "autolearn" / "days"
    foreign = folder / "2020-01-01.json.gz"
    foreign.write_bytes(gzip.compress(b'{"v": 1, "day": "2020-01-01"}'))
    broken = folder / "2020-01-02.json.gz"
    broken.write_bytes(b"kein gzip")
    unrelated = folder / "notizen.txt"
    unrelated.write_text("x", encoding="utf-8")

    learning.act("day_remove", {})

    assert sorted(p.name for p in folder.iterdir()) == sorted(
        ["2020-01-01.json.gz", "2020-01-02.json.gz", "notizen.txt"])
    assert foreign.read_bytes() == gzip.compress(b'{"v": 1, "day": "2020-01-01"}')
    assert any("3 Tagesprofile entfernt" in text for text in _texts(log))


def test_day_remove_without_folder_is_quiet(learning, data_dir):
    state = learning.act("day_remove", {})

    assert _days(data_dir) == []
    assert _scenario(state, "duplicate")["inserted"] is False


def test_ack_reset_keeps_every_other_field(learning, data_dir):
    settings = {
        "learning_acknowledged": ["automation_patterns", "chain_learning"],
        "learning_profile_days": 21,
        "learning_excluded_devices": ["demo_pump"],
        "theme": "mint",
        "nested": {"a": [1, 2]},
        "unknown_key": "bleibt",
    }
    _write_settings(data_dir, settings)

    learning.act("ack_reset", {})

    stored = json.loads((data_dir / "settings.json").read_text(encoding="utf-8"))
    assert stored == {**settings, "learning_acknowledged": []}


def test_ack_reset_tells_the_user_about_the_restart(learning, data_dir, log):
    _write_settings(data_dir, {"learning_acknowledged": ["x"]})

    learning.act("ack_reset", {})

    assert any("Neustart" in text for text in _texts(log))


def test_ack_reset_without_settings_file_is_value_error(learning):
    with pytest.raises(ValueError, match="Einstellungen nicht lesbar"):
        learning.act("ack_reset", {})


def test_missing_data_dir_is_value_error(rules_path, tmp_path):
    tab = AutolearnTab(Log(), rules_path)

    for action, body in (("day_create", {"days": 1}), ("day_remove", {}), ("ack_reset", {})):
        with pytest.raises(ValueError, match="Datenordner nicht angegeben"):
            tab.act(action, body)


def test_unreadable_data_dir_is_value_error(rules_path, tmp_path):
    tab = AutolearnTab(Log(), rules_path, "", tmp_path / "fehlt")

    with pytest.raises(ValueError, match="Datenordner nicht lesbar"):
        tab.act("day_create", {"days": 1})


def test_learning_actions_work_without_rules_file(data_dir, log):
    tab = AutolearnTab(log, None, "", data_dir)

    state = tab.act("day_create", {"days": 1})

    assert len(_days(data_dir)) == 1
    assert not any(s["inserted"] for s in state)
    with pytest.raises(ValueError, match="Regeldatei nicht angegeben"):
        tab.state()


def test_learning_texts_follow_the_style_rules(learning, data_dir, log):
    _write_settings(data_dir, {"learning_acknowledged": ["x"]})
    learning.act("day_create", {"days": 1})
    learning.act("day_remove", {})
    learning.act("ack_reset", {})

    for text in _texts(log):
        assert ";" not in text
        assert "–" not in text and "—" not in text
