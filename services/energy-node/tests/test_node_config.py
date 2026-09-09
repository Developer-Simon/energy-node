"""Tests fuer den Node-Dienst gegen die zentrale config.json.

Ausfuehren mit `.venv/bin/pytest services/energy-node/tests`.
"""

import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

import energy_node_mqtt as node


def test_managed_bridges_are_derived_from_services(app_config):
    config = app_config()
    bridges = node.managed_bridges(config)

    assert [bridge_id for bridge_id, _, _ in bridges] == sorted(config.services)
    intervals = {bridge_id: interval for bridge_id, _, interval in bridges}
    assert intervals["battery_soc"] == 10.0
    assert intervals["shelly"] == 20.0
    names = {bridge_id: name for bridge_id, name, _ in bridges}
    assert names["battery_soc"] == "Batterie-Ladezustand"


def test_device_block_uses_dashboard_node_fields(app_config):
    config = app_config(dashboard={"node_device_id": "node-x", "node_device_name": "Werkstatt X"})
    block = node.device_block(config.dashboard)

    assert block["identifiers"] == ["node-x"]
    assert block["name"] == "Werkstatt X"


def test_module_import_has_no_env_constants():
    # Der Import darf auf jedem System ohne /etc/energy-node laufen.
    assert not hasattr(node, "MQTT_HOST")
    assert not hasattr(node, "MANAGED_BRIDGES")
