"""Gemeinsame Test-Vorrichtungen fuer den APsystems-Dienst."""

import json
import sys
from pathlib import Path

import pytest

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

from energy_node_common import appconfig


_MANIFEST_REQUIRED = {
    "apsystems": ["service_id", "poll_interval_s", "diagnostic_poll_multiplier"],
    "shelly": ["service_id", "poll_interval_s", "diagnostic_poll_multiplier", "http_timeout_s"],
    "trucki": ["service_id", "poll_interval_s", "diagnostic_poll_multiplier", "http_timeout_s"],
    "tuya": ["service_id", "poll_interval_s", "diagnostic_poll_multiplier"],
    "battery_soc": ["service_id", "poll_interval_s", "diagnostic_poll_multiplier"],
    "automation": ["service_id"],
}


def _write_manifests(config_dir):
    """Legt <config_dir>/manifests/<id>.json an -- appconfig.load liest sie dort."""
    manifests_dir = config_dir / "manifests"
    manifests_dir.mkdir(exist_ok=True)
    for service_id, required in _MANIFEST_REQUIRED.items():
        manifests_dir.joinpath(f"{service_id}.json").write_text(
            json.dumps(
                {
                    "service_id": service_id,
                    "unit": f"{service_id}.service",
                    "schema": "config.schema.json",
                    "required": required,
                }
            ),
            encoding="utf-8",
        )


def _valid_document(node=None):
    """Ein vollstaendiges Konfigurationsdokument fuer Tests."""
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
            "managed_bridges": ["apsystems"],
            "poll_interval_s": 60,
            "diagnostic_poll_multiplier": 10,
            **(node or {}),
        },
        "dashboard": {
            "node_device_id": "energy-node",
            "node_device_name": "Energy Node",
            "node_poll_interval_s": 60,
            "node_diagnostic_poll_multiplier": 10,
        },
        "services": {
            "apsystems": {"service_id": "apsystems", "poll_interval_s": 60, "diagnostic_poll_multiplier": 10},
            "shelly": {"service_id": "shelly", "poll_interval_s": 20, "diagnostic_poll_multiplier": 15, "http_timeout_s": 5},
            "trucki": {"service_id": "trucki", "poll_interval_s": 30, "diagnostic_poll_multiplier": 10, "http_timeout_s": 5},
            "tuya": {"service_id": "tuya", "poll_interval_s": 30, "diagnostic_poll_multiplier": 1},
            "battery_soc": {"service_id": "battery_soc", "poll_interval_s": 10, "diagnostic_poll_multiplier": 1},
            "automation": {"service_id": "automation"},
        },
    }


@pytest.fixture
def app_config(tmp_path):
    """Erstellt eine AppConfig-Factory fuer Tests mit Ueberrides."""
    def _make_config(node=None):
        document = _valid_document(node)
        _write_manifests(tmp_path)
        config_path = tmp_path / "config.json"
        config_path.write_text(json.dumps(document), encoding="utf-8")
        return appconfig.load(str(config_path))
    return _make_config
