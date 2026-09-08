"""Tests fuer soc_config.py.

Modul wird direkt importiert. Ausfuehren mit
`.venv/bin/pytest services/battery_soc/tests` (Projekt-venv, nie globales python).
"""

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
