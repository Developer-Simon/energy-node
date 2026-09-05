"""Tests fuer die Konfigurationsaufloesung des Node-Dienstes.

Ausfuehren mit `.venv/bin/pytest src/energy-node/tests`.
"""

import sys
from pathlib import Path

import pytest

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

import energy_node_mqtt as node
from energy_node_common import appconfig


def test_managed_bridges_take_interval_from_services(app_config):
    config = app_config()
    bridges = node.managed_bridges(config)

    assert [device_id for device_id, _, _ in bridges] == ["apsystems", "tuya", "battery_soc", "shelly"]
    assert dict((device_id, interval) for device_id, _, interval in bridges) == {
        "apsystems": 60.0,
        "tuya": 30.0,
        "battery_soc": 10.0,
        "shelly": 20.0,
    }
    assert dict((device_id, name) for device_id, name, _ in bridges)["battery_soc"] == "Batterie-Ladezustand"


def test_managed_bridge_without_service_section_is_rejected(app_config, tmp_path):
    with pytest.raises(appconfig.ConfigError):
        app_config(node={"managed_bridges": ["apsystems", "gibtsnicht"]})


def test_device_block_uses_config_values(app_config):
    config = app_config(node={"device_id": "node-x", "device_name": "Werkstatt X"})
    block = node.device_block(config.node)

    assert block["identifiers"] == ["node-x"]
    assert block["name"] == "Werkstatt X"


def test_module_import_does_not_touch_etc():
    # Der Modulkopf darf keine Konfiguration mehr lesen: sonst scheitert
    # schon der Import auf jedem System ohne /etc/energy-node.
    assert not hasattr(node, "MQTT_HOST")
    assert not hasattr(node, "MANAGED_BRIDGES")
    assert not hasattr(node, "parse_managed_bridges")
