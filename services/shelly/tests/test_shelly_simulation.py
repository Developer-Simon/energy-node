import asyncio
from types import SimpleNamespace
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

import shelly_rpc_mqtt as shelly


def plug_config(**overrides):
    defaults = dict(
        id="netz_meanwell",
        name="Shelly Plug S – MeanWell-Ladegerät",
        host="192.0.2.10",
        generation=1,
        switch_channels=1,
        has_power=True,
        has_energy=True,
    )
    defaults.update(overrides)
    return shelly.ShellyDeviceConfig(**defaults)


def meter_config(**overrides):
    defaults = dict(
        id="hauptzaehler",
        name="Shelly 3EM Hauptzähler",
        host="192.0.2.20",
        generation=1,
        switch_channels=0,
        has_power=True,
        has_energy=True,
        has_3phase=True,
    )
    defaults.update(overrides)
    return shelly.ShellyDeviceConfig(**defaults)


def test_plug_power_is_zero_while_relay_off():
    cfg = plug_config()
    state = shelly.new_simulated_state(cfg)
    data = shelly.simulated_state(cfg, state)
    assert data["power_w"] == [0.0]
    assert data["energy_wh"] == [0.0]


def test_plug_power_is_nonzero_while_relay_on():
    cfg = plug_config()
    state = shelly.new_simulated_state(cfg)
    state.relays[0] = True
    data = shelly.simulated_state(cfg, state)
    assert data["power_w"][0] > 0.0


def test_plug_energy_accumulates_only_while_on(monkeypatch):
    cfg = plug_config()
    state = shelly.new_simulated_state(cfg)
    monkeypatch.setattr(shelly, "_simulated_channel_power_w", lambda cfg, ch, now: 60.0)

    monkeypatch.setattr(shelly.time, "time", lambda: 1_000.0)
    state.relays[0] = True
    shelly.simulated_state(cfg, state)  # verankert last_update_ts, dt=0

    monkeypatch.setattr(shelly.time, "time", lambda: 1_000.0 + 3600.0)  # 1h später
    data = shelly.simulated_state(cfg, state)
    assert data["energy_wh"][0] == 60.0

    state.relays[0] = False
    before = state.energy_wh[0]
    monkeypatch.setattr(shelly.time, "time", lambda: 1_000.0 + 7200.0)  # nochmal 1h später
    data = shelly.simulated_state(cfg, state)
    assert data["power_w"][0] == 0.0
    assert data["energy_wh"][0] == before


def test_meter_power_is_always_present_without_relay():
    cfg = meter_config()
    state = shelly.new_simulated_state(cfg)
    data = shelly.simulated_state(cfg, state)
    assert len(data["power_w"]) == 3
    assert all(isinstance(w, float) for w in data["power_w"])
    assert "relays" not in data


def test_meter_energy_accumulates_over_time():
    cfg = meter_config()
    state = shelly.new_simulated_state(cfg)
    shelly.simulated_state(cfg, state)
    state.last_update_ts -= 3600.0
    data = shelly.simulated_state(cfg, state)
    # Je Phase wächst entweder der Bezugs- oder der Einspeisezähler, je nach Vorzeichen.
    assert all(
        bought + returned > 0.0
        for bought, returned in zip(data["energy_wh"], data["energy_returned_wh"])
    )


def test_apply_relay_command_zeros_power_when_switched_off(monkeypatch):
    cfg = plug_config()
    state = shelly.new_simulated_state(cfg)
    state.relays[0] = True

    class FakeClient:
        def publish(self, topic, payload=None, qos=0, retain=True):
            pass

    published: dict[str, object] = {}
    monkeypatch.setattr(
        shelly.mqtt, "publish", lambda client, topic, value: published.__setitem__(topic, value)
    )
    monkeypatch.setattr(shelly.mqtt, "publish_online_status", lambda *a, **k: None)

    asyncio.run(shelly.apply_relay_command(FakeClient(), cfg, 0, False, 5.0, True, state))

    assert state.relays[0] is False
    assert published[f"{cfg.base_topic}/power/0"] == 0.0


def test_meter_power_goes_negative_at_some_time():
    cfg = meter_config()
    totals = [
        sum(shelly._simulated_channel_power_w(cfg, ch, float(t)) for ch in range(3))
        for t in range(0, 3601, 60)
    ]
    assert min(totals) < -1500.0


def test_meter_phases_run_in_step():
    cfg = meter_config()
    for t in range(0, 3601, 60):
        values = [shelly._simulated_channel_power_w(cfg, ch, float(t)) for ch in range(3)]
        assert values[0] == values[1] == values[2]


def test_meter_counters_never_decrease(monkeypatch):
    clock = {"now": 0.0}
    monkeypatch.setattr(shelly, "time", SimpleNamespace(time=lambda: clock["now"]))
    cfg = meter_config()
    state = shelly.new_simulated_state(cfg)
    previous = None
    for t in range(0, 3601, 60):
        clock["now"] = float(t)
        data = shelly.simulated_state(cfg, state)
        current = (list(data["energy_wh"]), list(data["energy_returned_wh"]))
        if previous is not None:
            for before, after in zip(previous[0] + previous[1], current[0] + current[1]):
                assert after >= before
        previous = current
    assert all(e > 0.0 for e in data["energy_returned_wh"])


def test_plug_simulation_unchanged():
    cfg = plug_config()
    for t in range(0, 3601, 60):
        watt = shelly._simulated_channel_power_w(cfg, 0, float(t))
        assert 20.0 <= watt <= 70.0
