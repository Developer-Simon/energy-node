"""Tests fuer schlafende (batteriebetriebene) Shelly-Geraete wie den H&T.

Ein sleepy-Geraet ist die meiste Zeit nicht per HTTP erreichbar, weil es
zwischen kurzen Aufwachfenstern schlaeft. Ein einzelner fehlgeschlagener
Poll darf es deshalb nicht sofort als offline melden - erst wenn laenger
als offline_grace_s kein erfolgreicher Kontakt mehr da war.
"""

import asyncio
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

import shelly_rpc_mqtt as shelly


def ht_config(**overrides):
    defaults = dict(
        id="garten_ht",
        name="Shelly H&T Garten",
        host="192.0.2.30",
        generation=1,
        switch_channels=0,
        has_temperature=True,
        has_humidity=True,
        sleepy=True,
        offline_grace_s=3600.0,
    )
    defaults.update(overrides)
    return shelly.ShellyDeviceConfig(**defaults)


class FakeClient:
    def publish(self, topic, payload=None, qos=0, retain=True):
        pass


def _patch_online_status(monkeypatch):
    calls: list[tuple] = []
    monkeypatch.setattr(
        shelly.mqtt,
        "publish_online_status",
        lambda client, base_topic, online, reason: calls.append((online, reason)),
    )
    monkeypatch.setattr(shelly.mqtt, "publish", lambda *a, **k: None)
    return calls


def test_sleepy_device_stays_online_within_grace_window(monkeypatch):
    cfg = ht_config(offline_grace_s=3600.0)
    calls = _patch_online_status(monkeypatch)
    sleepy_state = shelly.SleepyDeviceState(last_online_ts=1_000.0)

    monkeypatch.setattr(shelly.time, "time", lambda: 1_000.0 + 1800.0)  # 30 min spaeter

    def raise_timeout(cfg, timeout_s):
        raise TimeoutError("kein Kontakt")

    monkeypatch.setattr(shelly, "fetch_gen1_status", raise_timeout)

    asyncio.run(shelly.fetch_and_publish_state(FakeClient(), cfg, 5.0, sleepy_state=sleepy_state))

    assert calls == []  # keine Offline-Meldung innerhalb der Gnadenfrist


def test_sleepy_device_goes_offline_after_grace_window_expires(monkeypatch):
    cfg = ht_config(offline_grace_s=3600.0)
    calls = _patch_online_status(monkeypatch)
    sleepy_state = shelly.SleepyDeviceState(last_online_ts=1_000.0)

    monkeypatch.setattr(shelly.time, "time", lambda: 1_000.0 + 3700.0)  # ueber der Gnadenfrist

    def raise_timeout(cfg, timeout_s):
        raise TimeoutError("kein Kontakt")

    monkeypatch.setattr(shelly, "fetch_gen1_status", raise_timeout)

    asyncio.run(shelly.fetch_and_publish_state(FakeClient(), cfg, 5.0, sleepy_state=sleepy_state))

    assert calls == [(False, "kein Kontakt")]


def test_sleepy_device_never_seen_goes_offline_immediately(monkeypatch):
    cfg = ht_config()
    calls = _patch_online_status(monkeypatch)
    sleepy_state = shelly.SleepyDeviceState(last_online_ts=None)

    def raise_timeout(cfg, timeout_s):
        raise TimeoutError("kein Kontakt")

    monkeypatch.setattr(shelly, "fetch_gen1_status", raise_timeout)

    asyncio.run(shelly.fetch_and_publish_state(FakeClient(), cfg, 5.0, sleepy_state=sleepy_state))

    assert calls == [(False, "kein Kontakt")]


def test_non_sleepy_device_goes_offline_immediately_on_failure(monkeypatch):
    cfg = ht_config(sleepy=False)
    calls = _patch_online_status(monkeypatch)
    sleepy_state = shelly.SleepyDeviceState(last_online_ts=1_000.0)

    monkeypatch.setattr(shelly.time, "time", lambda: 1_000.0 + 1.0)

    def raise_timeout(cfg, timeout_s):
        raise TimeoutError("kein Kontakt")

    monkeypatch.setattr(shelly, "fetch_gen1_status", raise_timeout)

    asyncio.run(shelly.fetch_and_publish_state(FakeClient(), cfg, 5.0, sleepy_state=sleepy_state))

    assert calls == [(False, "kein Kontakt")]


def test_successful_poll_updates_last_online_ts_and_publishes_online(monkeypatch):
    cfg = ht_config()
    calls = _patch_online_status(monkeypatch)
    sleepy_state = shelly.SleepyDeviceState(last_online_ts=None)

    monkeypatch.setattr(shelly.time, "time", lambda: 5_000.0)
    monkeypatch.setattr(shelly, "fetch_gen1_status", lambda cfg, timeout_s: {})

    asyncio.run(shelly.fetch_and_publish_state(FakeClient(), cfg, 5.0, sleepy_state=sleepy_state))

    assert calls == [(True, "connected")]
    assert sleepy_state.last_online_ts == 5_000.0


def test_default_sleepy_is_false_and_offline_grace_has_sane_default():
    cfg = shelly.ShellyDeviceConfig(id="x", name="X", host="192.0.2.1", generation=1)
    assert cfg.sleepy is False
    assert cfg.offline_grace_s > 0
