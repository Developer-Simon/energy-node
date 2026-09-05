"""Gemeinsame Test-Vorrichtungen fuer den Tuya-Dienst."""

import json
import sys
from pathlib import Path

import pytest

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

from energy_node_common import appconfig


def _valid_document(node=None, services=None):
    """Ein vollstaendiges Konfigurationsdokument fuer Tests."""
    base_services = {
        "apsystems": {"device_id": "apsystems", "poll_interval_s": 60, "diagnostic_poll_multiplier": 10},
        "shelly": {"device_id": "shelly", "poll_interval_s": 20, "diagnostic_poll_multiplier": 15, "http_timeout_s": 5},
        "trucki": {"device_id": "trucki", "poll_interval_s": 30, "diagnostic_poll_multiplier": 10, "http_timeout_s": 5},
        "tuya": {"device_id": "tuya", "poll_interval_s": 30, "diagnostic_poll_multiplier": 1},
        "battery_soc": {"device_id": "battery_soc", "poll_interval_s": 10, "diagnostic_poll_multiplier": 1},
        "automation": {"device_id": "automation"},
    }
    if services:
        base_services.update(services)

    return {
        "schema_version": 1,
        "mqtt": {
            "host": "127.0.0.1",
            "port": 1883,
            "username": "test_client",
            "password_file": "",
        },
        "paths": {
            "devices_dir": "/tmp/devices",
            "data_dir": "/tmp/data",
        },
        "logging": {"level": "INFO"},
        "node": {
            "device_id": "energy-node",
            "device_name": "Energy Node",
            "managed_bridges": ["tuya"],
            "poll_interval_s": 60,
            "diagnostic_poll_multiplier": 10,
            **(node or {}),
        },
        "services": base_services,
    }


@pytest.fixture
def app_config(tmp_path):
    """Erstellt eine AppConfig-Factory fuer Tests mit Ueberrides."""
    def _make_config(node=None, services=None):
        document = _valid_document(node, services)
        config_path = tmp_path / "config.json"
        config_path.write_text(json.dumps(document), encoding="utf-8")
        return appconfig.load(str(config_path))
    return _make_config
