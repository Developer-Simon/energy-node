import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

import shelly_rpc_mqtt as shelly


def test_module_has_no_environment_constants():
    for name in ("MQTT_HOST", "MQTT_PORT", "DEVICE_ID", "NODE_DEVICE_ID", "BASE_TOPIC",
                 "DEVICES_CONFIG_PATH", "DEFAULT_POLL_INTERVAL_S",
                 "DEFAULT_DIAGNOSTIC_MULTIPLIER", "HTTP_TIMEOUT_S"):
        assert not hasattr(shelly, name), f"{name} steht noch auf Modulebene"


def test_http_timeout_is_passed_through(monkeypatch):
    gesehen = {}

    class FakeResponse:
        def raise_for_status(self):
            pass

        def json(self):
            return {}

    def fake_get(url, auth=None, timeout=None):
        gesehen["timeout"] = timeout
        return FakeResponse()

    monkeypatch.setattr(shelly._SESSION, "get", fake_get)
    shelly._http_get("http://192.0.2.1/status", timeout_s=7.5)

    assert gesehen["timeout"] == 7.5


def test_service_derives_base_topic_from_service_config(app_config):
    config = app_config(services={"shelly": {"device_id": "shelly-x", "poll_interval_s": 20,
                                             "diagnostic_poll_multiplier": 15, "http_timeout_s": 5}})
    service = shelly.ShellyService([], None, config, "shelly")

    assert service.base_topic == "outstation/shelly-x"
    assert service.service_config.http_timeout_s == 5.0


def test_device_block_uses_configured_node_id():
    cfg = shelly.ShellyDeviceConfig(id="a", name="A", host="192.0.2.1", generation=1)
    assert shelly.device_block(cfg, node_device_id="node-x")["via_device"] == "node-x"
