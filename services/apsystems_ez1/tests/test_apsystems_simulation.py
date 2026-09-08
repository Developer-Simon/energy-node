import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

import apsystems_ez1_mqtt as aps


def make_device(**overrides) -> aps.APsystemsDevice:
    defaults = dict(id="ez1_test", name="EZ1 Test", host="192.0.2.30")
    defaults.update(overrides)
    cfg = aps.APsystemsDeviceConfig(**defaults)
    return aps.APsystemsDevice(cfg, node_device_id="test-node")


def test_power_is_zero_when_power_status_off():
    device = make_device()
    device._simulation_power_active = False
    data = aps._get_simulation_output_data(device)
    assert data.p1 == 0
    assert data.p2 == 0


def test_total_power_is_capped_by_power_limit(monkeypatch):
    device = make_device()
    monkeypatch.setattr(aps.time, "time", lambda: 1_000.0)
    monkeypatch.setattr(aps.math, "sin", lambda _x: 1.0)  # p1=300, p2=260 unkapped
    device._simulation_max_power_limit_w = 100

    data = aps._get_simulation_output_data(device)

    assert data.p1 + data.p2 <= 100
    assert data.p1 > 0
    assert data.p2 > 0


def test_power_is_not_capped_below_limit(monkeypatch):
    device = make_device()
    monkeypatch.setattr(aps.time, "time", lambda: 1_000.0)
    monkeypatch.setattr(aps.math, "sin", lambda _x: 1.0)  # p1=300, p2=260 unkapped
    device._simulation_max_power_limit_w = 800

    data = aps._get_simulation_output_data(device)

    assert data.p1 == 300
    assert data.p2 == 260


def test_energy_today_accumulates_from_power_over_time(monkeypatch):
    device = make_device()
    monkeypatch.setattr(aps.math, "sin", lambda _x: 1.0)  # p1=300, p2=260

    monkeypatch.setattr(aps.time, "time", lambda: 1_000.0)
    aps._get_simulation_output_data(device)  # verankert _simulation_last_poll_ts, dt=0

    monkeypatch.setattr(aps.time, "time", lambda: 1_000.0 + 3600.0)  # 1h später
    data = aps._get_simulation_output_data(device)

    assert data.e1 == 0.3  # 300W * 1h = 0.3 kWh
    assert data.e2 == 0.26


def test_energy_today_does_not_increase_while_power_off(monkeypatch):
    device = make_device()
    monkeypatch.setattr(aps.math, "sin", lambda _x: 1.0)

    monkeypatch.setattr(aps.time, "time", lambda: 1_000.0)
    aps._get_simulation_output_data(device)

    device._simulation_power_active = False
    monkeypatch.setattr(aps.time, "time", lambda: 1_000.0 + 3600.0)
    data = aps._get_simulation_output_data(device)

    assert data.e1 == 0.0
    assert data.e2 == 0.0


def test_lifetime_energy_includes_today_accumulation(monkeypatch):
    device = make_device()
    monkeypatch.setattr(aps.math, "sin", lambda _x: 1.0)

    monkeypatch.setattr(aps.time, "time", lambda: 1_000.0)
    aps._get_simulation_output_data(device)
    monkeypatch.setattr(aps.time, "time", lambda: 1_000.0 + 3600.0)
    data = aps._get_simulation_output_data(device)

    assert data.te1 == round(device._simulation_lifetime_base_e1 + 0.3, 3)
    assert data.te2 == round(device._simulation_lifetime_base_e2 + 0.26, 3)
