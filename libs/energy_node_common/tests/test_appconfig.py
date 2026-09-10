"""Tests fuer energy_node_common.appconfig.

Ausfuehren mit `.venv/bin/pytest libs/energy_node_common/tests`
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
        "schema_version": 2,
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
        "dashboard": {
            "node_device_id": "energy_node",
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


_MANIFESTS = {
    "apsystems": ["service_id", "poll_interval_s", "diagnostic_poll_multiplier"],
    "shelly": ["service_id", "poll_interval_s", "diagnostic_poll_multiplier", "http_timeout_s"],
    "trucki": ["service_id", "poll_interval_s", "diagnostic_poll_multiplier", "http_timeout_s"],
    "tuya": ["service_id", "poll_interval_s", "diagnostic_poll_multiplier"],
    "battery_soc": ["service_id", "poll_interval_s", "diagnostic_poll_multiplier"],
    "automation": ["service_id"],
}


def write_manifests(tmp_path: Path, service_ids=None) -> None:
    """Legt <tmp_path>/manifests/<id>.json fuer die gewuenschten Dienste an."""
    manifests_dir = tmp_path / "manifests"
    manifests_dir.mkdir(exist_ok=True)
    for service_id in _MANIFESTS if service_ids is None else service_ids:
        manifests_dir.joinpath(f"{service_id}.json").write_text(
            json.dumps(
                {
                    "service_id": service_id,
                    "unit": f"{service_id}.service",
                    "schema": "config.schema.json",
                    "required": _MANIFESTS[service_id],
                }
            ),
            encoding="utf-8",
        )


def write_config(tmp_path: Path, document: dict | None = None) -> str:
    write_manifests(tmp_path)
    path = tmp_path / "config.json"
    path.write_text(json.dumps(document if document is not None else valid_document()), encoding="utf-8")
    return str(path)


def test_load_reads_every_section(tmp_path):
    config = appconfig.load(write_config(tmp_path))

    assert config.schema_version == 2
    assert config.mqtt.host == "127.0.0.1"
    assert config.mqtt.port == 1883
    assert config.mqtt.username == "energynode_client"
    assert config.paths.devices_dir == "/home/energynode/devices"
    assert config.paths.data_dir == "/home/energynode/dashboard/data"
    assert config.log_level == "INFO"
    assert config.service("shelly").service_id == "shelly"
    assert config.service("shelly").poll_interval_s == 20.0
    assert config.service("shelly").http_timeout_s == 5.0
    assert config.service("automation").http_timeout_s is None


def test_load_reads_dashboard_node_fields(tmp_path):
    config = appconfig.load(write_config(tmp_path))
    assert config.dashboard.node_device_id == "energy_node"
    assert config.dashboard.node_device_name == "Energy Node"
    assert config.dashboard.node_poll_interval_s == 60.0
    assert config.dashboard.node_diagnostic_poll_multiplier == 10.0


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


def test_schema_version_one_is_rejected_with_hint(tmp_path):
    document = valid_document()
    document["schema_version"] = 1
    with pytest.raises(appconfig.ConfigError) as excinfo:
        appconfig.load(write_config(tmp_path, document))
    assert "schema_version 1" in str(excinfo.value)
    assert "dashboard.node_" in str(excinfo.value)


def test_schema_version_mismatch_names_expected_version(tmp_path):
    document = valid_document()
    document["schema_version"] = 99
    with pytest.raises(appconfig.ConfigError) as excinfo:
        appconfig.load(write_config(tmp_path, document))
    assert "99" in str(excinfo.value) and "2" in str(excinfo.value)


def test_missing_service_key_names_expected_key(tmp_path):
    document = valid_document()
    del document["services"]["trucki"]
    with pytest.raises(appconfig.ConfigError) as excinfo:
        appconfig.load(write_config(tmp_path, document))
    assert "services.trucki" in str(excinfo.value)


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


def test_services_entry_without_manifest_is_rejected(tmp_path):
    document = valid_document()
    document["services"]["extra"] = {"service_id": "extra"}
    with pytest.raises(appconfig.ConfigError) as excinfo:
        appconfig.load(write_config(tmp_path, document))
    assert "services.extra" in str(excinfo.value) and "Manifest" in str(excinfo.value)


def test_manifested_service_without_services_entry_is_rejected(tmp_path):
    document = valid_document()
    del document["services"]["trucki"]
    with pytest.raises(appconfig.ConfigError) as excinfo:
        appconfig.load(write_config(tmp_path, document))
    assert "services.trucki" in str(excinfo.value)


def test_missing_manifests_dir_is_rejected(tmp_path):
    path = tmp_path / "config.json"
    path.write_text(json.dumps(valid_document()), encoding="utf-8")  # kein write_manifests
    with pytest.raises(appconfig.ConfigError) as excinfo:
        appconfig.load(str(path))
    assert "manifests" in str(excinfo.value)
