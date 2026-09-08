import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

import apsystems_ez1_mqtt as apsystems


def test_module_has_no_environment_constants():
    for name in ("MQTT_HOST", "MQTT_PORT", "DEVICE_ID", "NODE_DEVICE_ID",
                 "DEVICES_CONFIG_PATH", "POLL_INTERVAL_SECONDS", "EXTENDED_INFO_EVERY_N_POLLS"):
        assert not hasattr(apsystems, name), f"{name} steht noch auf Modulebene"


def test_device_block_uses_configured_node_id():
    block = apsystems.device_block("ez1_1", "EZ1 Nord", node_device_id="node-x")
    assert block["via_device"] == "node-x"


def test_devices_config_path_follows_convention(app_config):
    config = app_config()
    assert config.devices_config("apsystems").name == "apsystems_devices.json"
