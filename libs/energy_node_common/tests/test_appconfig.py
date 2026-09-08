"""Tests fuer energy_node_common.appconfig.

Ausfuehren mit `.venv/bin/pytest src/energy_node_common/tests`
(Projekt-venv, nie globales python).
"""

import json
import sys
from pathlib import Path

import pytest

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "src"))

from energy_node_common import appconfig


def valid_document() -> dict:
    return {
        "schema_version": 1,
        "mqtt": {
            "host": "127.0.0.1",
            "port": 1883,
            "username": "energynode_client",
            "password_file": "",
        },
        "paths": {
            "devices_dir": "/home/energynode/devices",
            "data_dir": "/home/energynode/dashboard/data",
        },
        "logging": {"level": "INFO"},
        "node": {
            "device_id": "energy-node",
            "device_name": "Energy Node",
            "managed_bridges": ["apsystems", "tuya", "battery_soc", "shelly"],
            "poll_interval_s": 60,
            "diagnostic_poll_multiplier": 10,
        },
        "services": {
            "apsystems": {"device_id": "apsystems", "poll_interval_s": 60, "diagnostic_poll_multiplier": 10},
            "shelly": {"device_id": "shelly", "poll_interval_s": 20, "diagnostic_poll_multiplier": 15, "http_timeout_s": 5},
            "trucki": {"device_id": "trucki", "poll_interval_s": 30, "diagnostic_poll_multiplier": 10, "http_timeout_s": 5},
            "tuya": {"device_id": "tuya", "poll_interval_s": 30, "diagnostic_poll_multiplier": 1},
            "battery_soc": {"device_id": "battery_soc", "poll_interval_s": 10, "diagnostic_poll_multiplier": 1},
            "automation": {"device_id": "automation"},
        },
    }


def write_config(tmp_path: Path, document: dict | None = None) -> str:
    path = tmp_path / "config.json"
    path.write_text(json.dumps(document if document is not None else valid_document()), encoding="utf-8")
    return str(path)


def test_load_reads_every_section(tmp_path):
    config = appconfig.load(write_config(tmp_path))

    assert config.schema_version == 1
    assert config.mqtt.host == "127.0.0.1"
    assert config.mqtt.port == 1883
    assert config.mqtt.username == "energynode_client"
    assert config.paths.devices_dir == "/home/energynode/devices"
    assert config.paths.data_dir == "/home/energynode/dashboard/data"
    assert config.log_level == "INFO"
    assert config.node.device_id == "energy-node"
    assert config.node.device_name == "Energy Node"
    assert config.node.managed_bridges == ("apsystems", "tuya", "battery_soc", "shelly")
    assert config.node.poll_interval_s == 60.0
    assert config.node.diagnostic_poll_multiplier == 10.0
    assert config.service("shelly").device_id == "shelly"
    assert config.service("shelly").poll_interval_s == 20.0
    assert config.service("shelly").http_timeout_s == 5.0
    assert config.service("automation").http_timeout_s is None


def test_missing_file_names_path_and_flag(tmp_path):
    with pytest.raises(appconfig.ConfigError) as excinfo:
        appconfig.load(str(tmp_path / "fehlt.json"))
    assert "fehlt.json" in str(excinfo.value)
    assert "--config" in str(excinfo.value)


def test_invalid_json_names_line_and_column(tmp_path):
    path = tmp_path / "config.json"
    path.write_text('{"schema_version": 1,,}', encoding="utf-8")
    with pytest.raises(appconfig.ConfigError) as excinfo:
        appconfig.load(str(path))
    assert "Zeile" in str(excinfo.value) and "Spalte" in str(excinfo.value)


def test_wrong_type_names_json_path_and_value(tmp_path):
    document = valid_document()
    document["services"]["shelly"]["poll_interval_s"] = "20"
    with pytest.raises(appconfig.ConfigError) as excinfo:
        appconfig.load(write_config(tmp_path, document))
    assert 'services.shelly.poll_interval_s: erwartet Zahl, gefunden "20"' in str(excinfo.value)


def test_schema_version_mismatch_names_both_versions(tmp_path):
    document = valid_document()
    document["schema_version"] = 2
    with pytest.raises(appconfig.ConfigError) as excinfo:
        appconfig.load(write_config(tmp_path, document))
    assert "2" in str(excinfo.value) and "1" in str(excinfo.value)


def test_missing_service_key_names_expected_key(tmp_path):
    document = valid_document()
    del document["services"]["trucki"]
    with pytest.raises(appconfig.ConfigError) as excinfo:
        appconfig.load(write_config(tmp_path, document))
    assert "services.trucki" in str(excinfo.value)


def test_managed_bridge_without_service_section_is_rejected(tmp_path):
    document = valid_document()
    document["node"]["managed_bridges"] = ["apsystems", "unbekannt"]
    with pytest.raises(appconfig.ConfigError) as excinfo:
        appconfig.load(write_config(tmp_path, document))
    assert "services.unbekannt" in str(excinfo.value)


def test_missing_password_file_names_file(tmp_path):
    document = valid_document()
    document["mqtt"]["password_file"] = str(tmp_path / "mqtt.pw")
    config = appconfig.load(write_config(tmp_path, document))
    with pytest.raises(appconfig.ConfigError) as excinfo:
        config.mqtt.password()
    assert "mqtt.pw" in str(excinfo.value)


def test_password_file_strips_trailing_newline(tmp_path):
    secret = tmp_path / "mqtt.pw"
    secret.write_text("geheim\n", encoding="utf-8")
    document = valid_document()
    document["mqtt"]["password_file"] = str(secret)
    config = appconfig.load(write_config(tmp_path, document))
    assert config.mqtt.password() == "geheim"


def test_empty_password_file_field_means_no_password(tmp_path):
    assert appconfig.load(write_config(tmp_path)).mqtt.password() == ""


@pytest.mark.parametrize("name", ["apsystems", "shelly", "trucki", "tuya", "battery_soc", "automation"])
def test_convention_paths_for_every_service(tmp_path, name):
    config = appconfig.load(write_config(tmp_path))
    assert config.devices_config(name) == Path("/home/energynode/devices") / f"{name}_devices.json"
    assert config.devices_schema(name) == Path("/home/energynode/devices") / f"{name}_devices.schema.json"


def test_rules_config_path(tmp_path):
    config = appconfig.load(write_config(tmp_path))
    assert config.rules_config() == Path("/home/energynode/devices/automation_rules.json")


def test_history_file_path(tmp_path):
    config = appconfig.load(write_config(tmp_path))
    assert config.history_file() == Path("/home/energynode/devices/automation_history.json")


def test_config_path_from_argv():
    assert appconfig.config_path_from_argv([]) == appconfig.DEFAULT_CONFIG_PATH
    assert appconfig.config_path_from_argv(["--config", "/tmp/a.json"]) == "/tmp/a.json"
    assert appconfig.config_path_from_argv(["--config=/tmp/b.json"]) == "/tmp/b.json"
    with pytest.raises(appconfig.ConfigError):
        appconfig.config_path_from_argv(["--config"])
