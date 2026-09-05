import asyncio
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
    assert all(w > 0.0 for w in data["power_w"])
    assert "relays" not in data


def test_meter_energy_accumulates_over_time():
    cfg = meter_config()
    state = shelly.new_simulated_state(cfg)
    shelly.simulated_state(cfg, state)
    state.last_update_ts -= 3600.0
    data = shelly.simulated_state(cfg, state)
    assert all(e > 0.0 for e in data["energy_wh"])


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
