import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

import tuya_mqtt


def test_module_has_no_environment_constants():
    for name in ("MQTT_HOST", "MQTT_PORT", "SERVICE_DEVICE_ID", "NODE_DEVICE_ID",
                 "DEVICES_CONFIG_PATH", "POLL_INTERVAL_SECONDS", "DIAGNOSTIC_POLL_MULTIPLIER"):
        assert not hasattr(tuya_mqtt, name), f"{name} steht noch auf Modulebene"


def test_service_uses_config_values(app_config):
    config = app_config(services={"tuya": {"device_id": "tuya-x", "poll_interval_s": 45, "diagnostic_poll_multiplier": 2}})
    service = tuya_mqtt.TuyaService([], None, config, "tuya")

    assert service.base_topic == "outstation/tuya-x"
    assert service.service_config.poll_interval_s == 45.0
