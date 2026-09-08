import asyncio
import json
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

import shelly_rpc_mqtt as shelly


class FakeClient:
    def __init__(self):
        self.published = []

    def publish(self, topic, payload=None, qos=0, retain=True):
        self.published.append((topic, payload))

    def subscribe(self, topic):
        pass


class FakeSlave:
    def __init__(self, simulated=()):
        self._simulated = set(simulated)

    def simulation_active_for(self, unique_id):
        return unique_id in self._simulated

    def note_update(self, client):
        pass


def gen2_info_raw(**overrides):
    raw = {
        "name": "Plug",
        "id": "shellyplusplugs-abc",
        "mac": "A0B1C2D3E4F5",
        "model": "SNPL-00112EU",
        "gen": 2,
        "fw_id": "20230913-...",
        "ver": "1.4.4",
        "app": "PlusPlugS",
    }
    raw.update(overrides)
    return raw


def gen1_settings_raw(**overrides):
    raw = {
        "device": {"type": "SHSW-21", "mac": "16324CAABBCC", "hostname": "shelly1-B929CC"},
        "fw": "20230913-112003/v1.14.0-gcb84623",
        "name": "shellyMODEL",
    }
    raw.update(overrides)
    return raw


# --- Parser -----------------------------------------------------------------

def test_parse_gen2_device_info_maps_fields():
    info = shelly.parse_gen2_device_info(gen2_info_raw())
    assert info.model == "SNPL-00112EU"
    assert info.sw_version == "1.4.4"
    assert info.generation == 2
    assert info.mac == "A0B1C2D3E4F5"


def test_parse_gen2_device_info_falls_back_to_app_when_model_missing():
    info = shelly.parse_gen2_device_info(gen2_info_raw(model=""))
    assert info.model == "PlusPlugS"


def test_parse_gen1_settings_maps_type_mac_and_cleans_firmware():
    info = shelly.parse_gen1_settings(gen1_settings_raw())
    assert info.model == "Shelly 2.5"
    assert info.sw_version == "1.14.0"
    assert info.generation == 1
    assert info.mac == "16324CAABBCC"


def test_parse_gen1_settings_passes_unknown_model_code_through():
    info = shelly.parse_gen1_settings(gen1_settings_raw(device={"type": "SHXYZ-9", "mac": "00"}))
    assert info.model == "SHXYZ-9"


def test_clean_gen1_version_keeps_raw_string_without_semver():
    assert shelly._clean_gen1_version("20170427-114337/master@79dbb397") == \
        "20170427-114337/master@79dbb397"


# --- device_block ---------------------------------------------------------

def test_device_block_without_info_has_no_stammdaten_keys():
    cfg = shelly.ShellyDeviceConfig(id="a", name="A", host="192.0.2.1", generation=1)
    block = shelly.device_block(cfg, node_device_id="node-x")
    for key in ("model", "sw_version", "hw_version", "serial_number"):
        assert key not in block


def test_device_block_merges_info_fields():
    cfg = shelly.ShellyDeviceConfig(id="a", name="A", host="192.0.2.1", generation=2)
    info = shelly.ShellyDeviceInfo(model="SNPL-00112EU", sw_version="1.4.4",
                                   generation=2, mac="A0B1C2D3E4F5")
    block = shelly.device_block(cfg, node_device_id="node-x", info=info)
    assert block["model"] == "SNPL-00112EU"
    assert block["sw_version"] == "1.4.4"
    assert block["hw_version"] == "Gen 2"
    assert block["serial_number"] == "A0B1C2D3E4F5"
    assert block["via_device"] == "node-x"


# --- poll_diagnostics ---------------------------------------------------

def _service(app_config, devices, simulated=()):
    config = app_config()
    service = shelly.ShellyService(devices, None, config, "shelly")
    service.client = FakeClient()
    service.slave = FakeSlave(simulated=simulated)
    # Basis-Discovery gilt als bereits veroeffentlicht.
    for cfg in devices:
        service._published_block_hash[cfg.unique_id] = service._block_hash(
            service._current_device_block(cfg)
        )
    return service


def test_poll_diagnostics_populates_device_info_and_republishes(app_config, monkeypatch):
    gen1 = shelly.ShellyDeviceConfig(id="g1", name="Gen1", host="192.0.2.1", generation=1)
    gen2 = shelly.ShellyDeviceConfig(id="g2", name="Gen2", host="192.0.2.2", generation=2)
    service = _service(app_config, [gen1, gen2])

    def fake_fetch(cfg, timeout_s):
        if cfg.generation == 1:
            return shelly.parse_gen1_settings(gen1_settings_raw())
        return shelly.parse_gen2_device_info(gen2_info_raw())

    monkeypatch.setattr(shelly, "fetch_device_info", fake_fetch)
    asyncio.run(service.poll_diagnostics())

    assert service.device_info["g1"].model == "Shelly 2.5"
    assert service.device_info["g2"].sw_version == "1.4.4"

    discovery_payloads = [
        json.loads(payload)
        for topic, payload in service.client.published
        if topic.startswith("homeassistant/") and topic.endswith("/config")
    ]
    assert discovery_payloads, "erwartet erneute Discovery nach Blockaenderung"
    assert any(p["device"].get("sw_version") == "1.4.4" for p in discovery_payloads)


def test_poll_diagnostics_skips_simulated_devices(app_config, monkeypatch):
    gen2 = shelly.ShellyDeviceConfig(id="g2", name="Gen2", host="192.0.2.2", generation=2)
    service = _service(app_config, [gen2], simulated=["g2"])

    calls = []
    monkeypatch.setattr(shelly, "fetch_device_info",
                        lambda cfg, timeout_s: calls.append(cfg.id))
    asyncio.run(service.poll_diagnostics())

    assert calls == []
    assert "g2" not in service.device_info


def test_poll_diagnostics_swallows_fetch_errors(app_config, monkeypatch):
    gen2 = shelly.ShellyDeviceConfig(id="g2", name="Gen2", host="192.0.2.2", generation=2)
    service = _service(app_config, [gen2])

    def boom(cfg, timeout_s):
        raise RuntimeError("offline")

    monkeypatch.setattr(shelly, "fetch_device_info", boom)
    asyncio.run(service.poll_diagnostics())  # darf nicht werfen

    assert "g2" not in service.device_info


def test_sync_device_block_does_not_republish_when_unchanged(app_config, monkeypatch):
    gen2 = shelly.ShellyDeviceConfig(id="g2", name="Gen2", host="192.0.2.2", generation=2)
    service = _service(app_config, [gen2])

    monkeypatch.setattr(shelly, "fetch_device_info",
                        lambda cfg, timeout_s: shelly.parse_gen2_device_info(gen2_info_raw()))
    asyncio.run(service.poll_diagnostics())
    first_count = len(service.client.published)

    asyncio.run(service.poll_diagnostics())
    assert len(service.client.published) == first_count, \
        "unveraenderter Geraeteblock darf keine erneute Discovery ausloesen"
