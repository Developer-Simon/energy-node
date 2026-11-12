"""Tests fuer soc_config.py.

Modul wird direkt importiert. Ausfuehren mit
`.venv/bin/pytest services/battery_soc/tests` (Projekt-venv, nie globales python).
"""

import json
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

import soc_config


def test_soc_params_projection_carries_physics_fields():
    """Projiziert BatteryConfig zu SocParams und traegt die Physik-Felder korrekt ueber."""
    cfg = soc_config.BatteryConfig(id="b", name="B", topology="series",
                                   bank_a_voltage_topic="a", bank_b_voltage_topic="b",
                                   bank_a_capacity_ah=100, bank_b_capacity_ah=100)
    p = cfg.soc_params()
    assert p.topology == "series"
    assert p.bank_a_capacity_ah == 100
    assert not hasattr(p, "bank_a_voltage_topic")


def test_legacy_default_state_file_moves_to_runtime_dir(tmp_path):
    entries = [
        {"id": "b1", "name": "B1", "charger_power_topic": "b1/c", "inverter_power_topic": "b1/i",
         "bank_a_voltage_topic": "b1/v"},
        {"id": "b2", "name": "B2", "charger_power_topic": "b2/c", "inverter_power_topic": "b2/i",
         "bank_a_voltage_topic": "b2/v", "state_file": "/home/energynode/battery_soc/state.json"},
    ]
    path = tmp_path / "battery_soc_devices.json"
    path.write_text(json.dumps(entries))
    runtime = tmp_path / "runtime"
    configs = soc_config.load_configs(path, runtime_dir=str(runtime))
    assert [c.state_file for c in configs] == [
        runtime / "battery_soc" / "b1.json",
        runtime / "battery_soc" / "b2.json",
    ]


def test_custom_state_file_stays(tmp_path):
    entries = [{"id": "b1", "name": "B1", "charger_power_topic": "b1/c", "inverter_power_topic": "b1/i",
                "bank_a_voltage_topic": "b1/v", "state_file": "/var/lib/custom/b1.json"}]
    path = tmp_path / "battery_soc_devices.json"
    path.write_text(json.dumps(entries))
    [config] = soc_config.load_configs(path, runtime_dir=str(tmp_path / "runtime"))
    assert config.state_file == Path("/var/lib/custom/b1.json")
