"""Speicherintervall, Flush beim Beenden und Wiederherstellung nach Absturz."""
import json
import sys
import threading
import time
import types
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

import battery_soc_mqtt as battery_soc  # noqa: E402
import pytest  # noqa: E402

import mqtt_discovery  # noqa: E402
import recovery  # noqa: E402
import state_store  # noqa: E402
from battery_soc_core.state import CalibrationEvent  # noqa: E402

SERVICE_DIR = Path(__file__).resolve().parents[1]


class FakeClient:
    def __init__(self):
        self.published, self.subscribed, self.unsubscribed = [], [], []

    def publish(self, topic, payload=None, qos=0, retain=True):
        self.published.append((topic, payload))

    def subscribe(self, topic):
        self.subscribed.append(topic)

    def unsubscribe(self, topic):
        self.unsubscribed.append(topic)

    def state_payload(self):
        for topic, payload in reversed(self.published):
            if topic.endswith("/state"):
                return json.loads(payload)
        raise AssertionError("kein /state-Payload")


class FakeMessage:
    def __init__(self, topic, payload):
        self.topic, self.payload = topic, payload.encode()


class FakeSlave:
    def handle_message(self, client, topic, payload_str):
        return False

    def simulation_active_for(self, device_id):
        return False

    def start(self, client):
        pass


def make_runtime(tmp_path, **overrides):
    values = dict(id="battery_soc", name="Batterie", topology="parallel",
                  charger_power_topic="c/power", inverter_power_topic="i/power",
                  bank_a_voltage_topic="bms/voltage", bank_b_voltage_topic="",
                  state_file=tmp_path / "state.json")
    values.update(overrides)
    return battery_soc.BatteryRuntime(battery_soc.BatteryConfig(**values))


@pytest.fixture
def saves(monkeypatch):
    calls = []
    real = state_store.save_state

    def spy(config, state, **kwargs):
        calls.append(kwargs)
        return real(config, state, **kwargs)

    monkeypatch.setattr(state_store, "save_state", spy)
    monkeypatch.setattr(battery_soc, "state_save_interval_s", 300.0)
    return calls


def fake_clock(monkeypatch, values):
    """Ersetzt nur die time-Referenz des Dienstes - logging behaelt die echte Uhr."""
    clock = iter(values)
    monkeypatch.setattr(battery_soc, "time", types.SimpleNamespace(
        time=lambda: next(clock), strftime=time.strftime, sleep=time.sleep))


def test_schema_default_matches_the_service_default():
    schema = json.loads((SERVICE_DIR / "config.schema.json").read_text())
    prop = schema["properties"]["state_save_interval_s"]
    assert prop["default"] == battery_soc.DEFAULT_STATE_SAVE_INTERVAL_S
    assert "state_save_interval_s" not in schema["required"]


def test_save_interval_falls_back_to_the_default():
    cfg = type("S", (), {"state_save_interval_s": None})()
    assert battery_soc.save_interval(cfg) == battery_soc.DEFAULT_STATE_SAVE_INTERVAL_S
    cfg.state_save_interval_s = 60.0
    assert battery_soc.save_interval(cfg) == 60.0


def test_ticks_save_at_most_once_per_interval(tmp_path, saves, monkeypatch):
    runtime = make_runtime(tmp_path)
    battery_soc.load_runtime_state(runtime, now=1000.0)
    fake_clock(monkeypatch, [1010.0, 1100.0, 1299.0, 1300.0, 1310.0])
    for _ in range(5):
        battery_soc.compute_and_publish(FakeClient(), runtime, 0.0)
    assert len(saves) == 1 and saves[0]["now"] == 1300.0


def test_a_new_calibration_event_saves_immediately(tmp_path, saves):
    runtime = make_runtime(tmp_path)
    battery_soc.load_runtime_state(runtime)
    unit = runtime.state.units[0]
    unit.events.append(CalibrationEvent(
        iso="2026-09-28T12:00:00", unit=unit.name, side="full", coulomb_before_ah=90.0,
        coulomb_after_ah=100.0, residual_ah=10.0, voltage_v=27.2, cell_count=8,
        corrected_v_per_cell=3.4, current_a=1.0, threshold_v_per_cell=3.4,
        tolerance_v_per_cell=0.0, hold_s=60.0, taper_met=True,
        charged_ah=10.0, discharged_ah=0.0))
    battery_soc.compute_and_publish(FakeClient(), runtime, 0.0)
    assert len(saves) == 1


def test_state_payload_carries_the_recovery_snapshot(tmp_path, saves):
    runtime = make_runtime(tmp_path)
    battery_soc.load_runtime_state(runtime)
    client = FakeClient()
    battery_soc.compute_and_publish(client, runtime, 0.0)
    snap = client.state_payload()["recovery"]
    assert set(snap["units"]) == {u.name for u in runtime.state.units}
    assert snap["ts"] == pytest.approx(time.time(), abs=5)


def test_flush_all_writes_a_clean_file(tmp_path, saves, monkeypatch):
    runtime = make_runtime(tmp_path)
    battery_soc.load_runtime_state(runtime)
    monkeypatch.setattr(battery_soc, "runtimes", [runtime])
    battery_soc.flush_all()
    meta = state_store.load_state(runtime.config, runtime.state)
    assert meta.clean is True


def test_sigterm_handler_raises_system_exit():
    with pytest.raises(SystemExit) as excinfo:
        battery_soc._raise_system_exit(15, None)
    assert excinfo.value.code == 0


def _crashed_file(tmp_path, coulomb_ah, saved_at, current_a):
    runtime = make_runtime(tmp_path)
    runtime.state.units[0].coulomb_ah = coulomb_ah
    name = runtime.state.units[0].name
    state_store.save_state(runtime.config, runtime.state, now=saved_at, clean=False,
                           last_current_a={name: current_a})
    return name


def test_clean_file_does_not_subscribe_the_state_topic(tmp_path, monkeypatch):
    runtime = make_runtime(tmp_path)
    state_store.save_state(runtime.config, runtime.state, now=1.0, clean=True)
    battery_soc.load_runtime_state(runtime)
    assert runtime.recovery_pending is False
    monkeypatch.setattr(battery_soc, "runtimes", [runtime])
    monkeypatch.setattr(battery_soc, "slave", FakeSlave())
    client = FakeClient()
    monkeypatch.setattr(mqtt_discovery, "publish_discovery", lambda *a: None)
    monkeypatch.setattr(mqtt_discovery, "publish_simulation_discovery", lambda *a: None)
    battery_soc.on_connect(client, None, None, 0)
    assert battery_soc.state_topic(runtime.config) not in client.subscribed


def test_retained_snapshot_restores_the_counter(tmp_path, monkeypatch):
    now = time.time()
    _crashed_file(tmp_path, 40.0, now - 200, -10.0)
    runtime = make_runtime(tmp_path)
    battery_soc.load_runtime_state(runtime, now=now)
    assert runtime.recovery_pending is True
    monkeypatch.setattr(battery_soc, "runtimes", [runtime])
    monkeypatch.setattr(battery_soc, "slave", FakeSlave())
    crashed = make_runtime(tmp_path).state
    crashed.units[0].coulomb_ah = 38.0
    payload = json.dumps({"recovery": recovery.snapshot(crashed, now - 50)})
    client = FakeClient()
    battery_soc.on_message(client, None,
                           FakeMessage(battery_soc.state_topic(runtime.config), payload))
    assert runtime.state.units[0].coulomb_ah == pytest.approx(38.0)
    assert runtime.recovery_pending is False
    assert client.unsubscribed == [battery_soc.state_topic(runtime.config)]


def test_old_retained_payload_falls_back_to_extrapolation(tmp_path, monkeypatch):
    now = time.time()
    _crashed_file(tmp_path, 40.0, now - 1000, -36.0)
    runtime = make_runtime(tmp_path)
    battery_soc.load_runtime_state(runtime, now=now)
    monkeypatch.setattr(battery_soc, "runtimes", [runtime])
    monkeypatch.setattr(battery_soc, "slave", FakeSlave())
    monkeypatch.setattr(battery_soc, "state_save_interval_s", 300.0)
    battery_soc.on_message(FakeClient(), None, FakeMessage(
        battery_soc.state_topic(runtime.config), json.dumps({"soc_combined_pct": 40})))
    assert runtime.state.units[0].coulomb_ah == pytest.approx(40.0 - 36.0 * 150 / 3600)


def test_without_retained_state_the_counter_is_extrapolated_after_the_wait(
        tmp_path, monkeypatch):
    start = 10_000.0
    _crashed_file(tmp_path, 40.0, start - 1000, -36.0)
    runtime = make_runtime(tmp_path)
    monkeypatch.setattr(battery_soc, "state_save_interval_s", 300.0)
    battery_soc.load_runtime_state(runtime, now=start)
    fake_clock(monkeypatch, [start + 1, start + battery_soc.RECOVERY_WAIT_S + 1, start + 20])
    battery_soc.compute_and_publish(FakeClient(), runtime, 0.0)
    assert runtime.recovery_pending is True
    battery_soc.compute_and_publish(FakeClient(), runtime, 0.0)
    battery_soc.compute_and_publish(FakeClient(), runtime, 0.0)
    assert runtime.recovery_pending is False
    assert runtime.state.units[0].coulomb_ah == pytest.approx(40.0 - 36.0 * 150 / 3600)


def test_manual_soc_cancels_a_pending_recovery(tmp_path, monkeypatch):
    now = time.time()
    _crashed_file(tmp_path, 40.0, now - 200, -10.0)
    runtime = make_runtime(tmp_path)
    battery_soc.load_runtime_state(runtime, now=now)
    monkeypatch.setattr(battery_soc, "runtimes", [runtime])
    monkeypatch.setattr(battery_soc, "slave", FakeSlave())
    topic = f"{runtime.config.base_topic}/{mqtt_discovery.MANUAL_SOC_COMMAND_SUFFIX}"
    battery_soc.on_message(FakeClient(), None, FakeMessage(topic, "80"))
    crashed = make_runtime(tmp_path).state
    crashed.units[0].coulomb_ah = 10.0
    battery_soc.on_message(FakeClient(), None, FakeMessage(
        battery_soc.state_topic(runtime.config),
        json.dumps({"recovery": recovery.snapshot(crashed, now - 50)})))
    assert runtime.state.units[0].soc_pct == 80.0


def test_stored_events_do_not_cancel_a_pending_recovery(tmp_path, saves):
    runtime = make_runtime(tmp_path)
    unit = runtime.state.units[0]
    unit.events.append(CalibrationEvent(
        iso="2026-09-28T12:00:00", unit=unit.name, side="full", coulomb_before_ah=90.0,
        coulomb_after_ah=100.0, residual_ah=10.0, voltage_v=27.2, cell_count=8,
        corrected_v_per_cell=3.4, current_a=1.0, threshold_v_per_cell=3.4,
        tolerance_v_per_cell=0.0, hold_s=60.0, taper_met=True,
        charged_ah=10.0, discharged_ah=0.0))
    state_store.save_state(runtime.config, runtime.state, now=time.time() - 200, clean=False)
    restarted = make_runtime(tmp_path)
    battery_soc.load_runtime_state(restarted)
    saves.clear()
    battery_soc.compute_and_publish(FakeClient(), restarted, 0.0)
    assert restarted.recovery_pending is True
    assert saves == []
