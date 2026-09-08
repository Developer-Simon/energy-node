"""Tests for battery_soc_core.simulation module."""
from __future__ import annotations

import pytest

from battery_soc_core.simulation import simulated_bank_voltage_v
from battery_soc_core.electrical import estimate_current
from battery_soc_core.calibration import simulated_open_circuit_v_per_cell, corrected_voltage_per_cell
from battery_soc_core.state import BankState
from battery_soc_core.params import SocParams


def make_params(**overrides):
    """SocParams with defaults for testing."""
    defaults = dict(internal_resistance_mohm_per_cell=1.2)
    defaults.update(overrides)
    return SocParams(**defaults)


def test_simulated_voltage_adds_the_load_offset_that_the_service_removes_again():
    """Klemmenspannung = Ruhespannung + Last-Offset. corrected_voltage_per_cell()
    muss daraus wieder die Ruhespannung machen, sonst kalibriert die Simulation
    an der falschen Stelle."""
    params = make_params(internal_resistance_mohm_per_cell=1.2)
    bank = BankState("bank_a", 8, 100.0)
    bank.coulomb_ah = 50.0

    voltage_v = simulated_bank_voltage_v(bank, params, 500.0)
    open_circuit = simulated_open_circuit_v_per_cell(bank.soc_pct, params)

    assert voltage_v > open_circuit * bank.cell_count   # unter Ladung hoeher
    current_a = estimate_current(500.0, voltage_v)
    corrected = corrected_voltage_per_cell(
        voltage_v, bank.cell_count, current_a, bank.capacity_ah, 1.2)
    assert corrected == pytest.approx(open_circuit, abs=0.01)


def test_simulation_voltage_starts_low_on_an_empty_bank():
    """Simulation mit leerer Bank und keiner Leistung sollte die Leer-Spannung liefern."""
    params = make_params()
    bank = BankState("bank_a", 8, 100.0)
    bank.coulomb_ah = 0.0

    voltage_v = simulated_bank_voltage_v(bank, params, 0.0)

    assert voltage_v == pytest.approx(8 * params.empty_v_per_cell)
