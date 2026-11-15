import json
from types import SimpleNamespace

import pytest

from console_performance import PRESET_LABELS, PRESETS, PerformanceTab
from fake_console import Log

# Die Namen der Szenarien aus .superpowers/sdd/2026-11-09-performance-analyse/task-7-contract.md
CONTRACT_PRESETS = ("measuring", "good", "tight", "over", "throttled", "memory", "lonely", "many-browsers")


@pytest.fixture
def paths(tmp_path):
    return SimpleNamespace(dir=tmp_path, state=tmp_path / "fake-performance.json")


@pytest.fixture
def log():
    return Log()


@pytest.fixture
def tab(paths, log):
    return PerformanceTab(log, paths.state)


def read(paths):
    return json.loads(paths.state.read_text(encoding="utf-8"))


def entries(log):
    return [(e["source"], e["text"], e["level"]) for e in log.since(0)[1]]


def test_preset_names_match_contract():
    assert list(PRESETS) == list(CONTRACT_PRESETS)
    assert list(PRESET_LABELS) == list(CONTRACT_PRESETS)


def test_apply_preset_writes_expected_file(tab, paths):
    tab.act("preset", {"name": "over"})

    data = read(paths)
    assert data["preset"] == "over"
    assert data["cpu_pct"] == 78
    assert data["units"]["shelly"]["cpu_pct"] == 14
    assert data["units"]["trucki"]["cpu_pct"] == 9
    assert data == PRESETS["over"]


def test_missing_file_is_preset_good(tab, paths):
    assert not paths.state.exists()
    state = tab.state()

    assert state["preset"] == "good"
    assert state["preset_label"] == "Alles gut"
    assert tab.summary() == {"level": "ok", "text": "Alles gut"}


def test_writes_are_atomic(tab, paths):
    tab.act("preset", {"name": "memory"})
    tab.act("global", {"cpu_pct": 50})

    assert list(paths.dir.glob("*.tmp")) == []


def test_global_change_makes_custom_values(tab, paths, log):
    tab.act("global", {"cpu_pct": 50, "throttled": True})

    data = read(paths)
    assert data["cpu_pct"] == 50
    assert data["throttled"] is True
    assert data["preset"] == "custom"
    assert tab.summary() == {"level": "changed", "text": "eigene Werte"}
    assert entries(log) == [("performance", "Leistungswerte geändert", "info")]


def test_temp_unknown_is_null(tab, paths):
    tab.act("global", {"temp_c": None})

    assert read(paths)["temp_c"] is None
    assert tab.state()["values"]["temp_c"] is None


def test_hour_adds_one_hour(tab, paths):
    tab.act("preset", {"name": "measuring"})

    tab.act("hour", {})

    assert read(paths)["measured_s"] == 1800 + 3600
    assert read(paths)["preset"] == "custom"


def test_external_none_and_back(tab, paths):
    tab.act("external", {"none": True})
    assert read(paths)["external"] is None
    assert tab.state()["values"]["external"] is None

    tab.act("external", {"none": False, "name": "Heizstab", "every_s": 5})
    assert read(paths)["external"] == {"name": "Heizstab", "every_s": 5.0}


def test_unit_toggled_on_uses_good_values(tab, paths):
    tab.act("preset", {"name": "lonely"})

    tab.act("unit", {"id": "shelly", "present": True})

    unit = read(paths)["units"]["shelly"]
    assert unit == {"present": True, "cpu_pct": 5.0, "ram_mb": 22.0, "round_s": 0.4, "msg_per_min": 126.0}


def test_fixed_unit_has_no_round_or_messages(tab, paths):
    tab.act("unit", {"id": "caddy.service", "cpu_pct": 1.5})

    unit = read(paths)["units"]["caddy.service"]
    assert set(unit) == {"present", "cpu_pct", "ram_mb"}
    assert unit["cpu_pct"] == 1.5


def test_state_lists_every_unit(tab):
    state = tab.state()

    assert [u["id"] for u in state["units"]] == [
        "shelly", "trucki", "battery_soc", "apsystems", "tuya",
        "automation.service", "mosquitto.service", "tailscaled.service", "caddy.service",
        "energy-node-dashboard.service",
    ]
    absent = [u for u in state["units"] if not u["present"]]
    assert len(absent) == 0
    tab.act("preset", {"name": "lonely"})
    present = [u["id"] for u in tab.state()["units"] if u["present"]]
    assert present == ["automation.service", "mosquitto.service", "energy-node-dashboard.service"]


def test_invalid_inputs_are_value_errors(tab, paths):
    bad = [
        ("preset", {"name": "quatsch"}),
        ("preset", {}),
        ("global", {}),
        ("global", {"quatsch": 1}),
        ("global", {"cpu_pct": 101}),
        ("global", {"cpu_pct": -1}),
        ("global", {"cpu_pct": "32"}),
        ("global", {"cpu_pct": True}),
        ("global", {"cores": 0}),
        ("global", {"cores": 65}),
        ("global", {"cores": 2.5}),
        ("global", {"temp_c": 121}),
        ("global", {"temp_c": -21}),
        ("global", {"measured_s": -1}),
        ("global", {"throttled": "ja"}),
        ("global", {"ram_free_pct": 101}),
        ("global", {"clients_max": 0}),
        ("external", {}),
        ("external", {"none": "ja"}),
        ("external", {"name": "  "}),
        ("external", {"every_s": -1}),
        ("unit", {"id": "quatsch", "present": True}),
        ("unit", {"id": "shelly"}),
        ("unit", {"id": "shelly", "cpu_pct": 150}),
        ("unit", {"id": "caddy.service", "round_s": 1.0}),
        ("unit", {"id": "caddy.service", "msg_per_min": 1}),
        ("unit", {"id": "shelly", "quatsch": 1}),
    ]
    for action, body in bad:
        with pytest.raises(ValueError):
            tab.act(action, body)
    assert not paths.state.exists()


def test_unwritable_state_is_value_error(tmp_path, log):
    tab = PerformanceTab(log, tmp_path / "missing" / "fake-performance.json")

    with pytest.raises(ValueError):
        tab.act("preset", {"name": "good"})


def test_reset_writes_good(tab, paths):
    tab.act("preset", {"name": "throttled"})

    tab.reset()

    assert read(paths) == PRESETS["good"]


def test_unknown_action_is_value_error(tab):
    with pytest.raises(ValueError):
        tab.act("quatsch", {})


def test_fragment_renders_buttons_and_markers():
    html = PerformanceTab.html
    for marker in ('id="tab-performance"', "data-global", "data-hours", "data-external",
                   "data-field", "Messdauer um 1 Stunde erhöhen"):
        assert marker in html
    for label in ("Messung läuft", "Alles gut", "Knapp", "Über Budget", "Gedrosselt und heiß",
                  "Speicher knapp", "Allein ohne Dienste", "Viele Browser"):
        assert label in html
    assert 'data-unit="apsystems"' in html
    assert 'data-unit="energy-node-dashboard.service"' in html
