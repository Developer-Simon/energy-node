import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

import shelly_rpc_mqtt as shelly


class FakeClient:
    def __init__(self):
        self.published = []

    def publish(self, topic, payload=None, qos=0, retain=True):
        self.published.append((topic, payload))


def ht_config(**overrides):
    defaults = dict(
        id="ht_1",
        name="Shelly Plus H&T",
        host="192.0.2.10",
        generation=2,
        switch_channels=0,
        has_temperature=True,
        has_humidity=True,
    )
    defaults.update(overrides)
    return shelly.ShellyDeviceConfig(**defaults)


def test_normalize_gen2_reads_temperature_and_humidity_without_switch():
    cfg = ht_config()
    raw = {
        "temperature:0": {"tC": 21.5},
        "humidity:0": {"rh": 47.2},
        "wifi": {"rssi": -60},
    }
    out = shelly.normalize_gen2(cfg, raw)
    assert out["temperature_c"] == 21.5
    assert out["humidity_pct"] == 47.2


def test_normalize_gen2_ignores_humidity_when_not_configured():
    cfg = ht_config(has_humidity=False)
    raw = {"temperature:0": {"tC": 19.0}, "humidity:0": {"rh": 50.0}}
    out = shelly.normalize_gen2(cfg, raw)
    assert "temperature_c" in out
    assert "humidity_pct" not in out


def test_normalize_gen2_still_reads_temperature_from_switch_channel():
    cfg = ht_config(switch_channels=1, has_humidity=False)
    raw = {"switch:0": {"temperature": {"tC": 33.0}}}
    out = shelly.normalize_gen2(cfg, raw)
    assert out["temperature_c"] == 33.0


def test_simulated_state_includes_humidity():
    cfg = ht_config()
    data = shelly.simulated_state(cfg)
    assert data["humidity_pct"] == 45.0
    assert data["temperature_c"] == 20.0


def test_publish_state_publishes_humidity_topic():
    cfg = ht_config()
    client = FakeClient()
    shelly._publish_state(client, cfg, {"temperature_c": 21.5, "humidity_pct": 47.23})
    topics = dict(client.published)
    assert topics[f"{cfg.base_topic}/humidity"] == "47.2"
    assert topics[f"{cfg.base_topic}/temperature"] == "21.5"


def test_publish_device_discovery_announces_humidity_sensor():
    cfg = ht_config()
    client = FakeClient()
    shelly.publish_device_discovery(client, cfg, "energy-node")
    discovery_topics = [topic for topic, _ in client.published if "/humidity/" in topic]
    assert discovery_topics, "expected a humidity discovery entity to be published"


def test_base_topic_has_no_service_prefix():
    cfg = ht_config()
    assert cfg.base_topic == "outstation/ht_1"


def test_unique_id_equals_configured_id():
    cfg = ht_config()
    assert cfg.unique_id == "ht_1"
