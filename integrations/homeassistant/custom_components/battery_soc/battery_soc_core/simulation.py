from __future__ import annotations

from .calibration import simulated_open_circuit_v_per_cell, corrected_voltage_per_cell
from .electrical import estimate_current


def simulated_bank_voltage_v(bank, params, net_bank_power_w):
    """Klemmenspannung im Simulationsmodus: Ruhespannung aus dem SoC, dann der
    Last-Offset ADDIERT - genau die Groesse, die corrected_voltage_per_cell()
    im Betrieb wieder herausrechnet. Der Offset wird nicht neu implementiert,
    sondern aus derselben Funktion zurueckgewonnen; ein zweiter Codepfad wuerde
    sonst frueher oder spaeter auseinanderlaufen.

    Der Strom haengt selbst von der Spannung ab. Ein Iterationsschritt ueber die
    Ruhespannung genuegt: die Rueckkopplung liegt im Promillebereich."""
    ocv_per_cell = simulated_open_circuit_v_per_cell(bank.soc_pct, params)
    if ocv_per_cell is None or bank.cell_count <= 0:
        return None
    open_circuit_v = ocv_per_cell * bank.cell_count
    current_a = estimate_current(net_bank_power_w, open_circuit_v) or 0.0
    corrected = corrected_voltage_per_cell(
        open_circuit_v, bank.cell_count, current_a, bank.capacity_ah,
        params.internal_resistance_mohm_per_cell)
    offset_v_per_cell = ocv_per_cell - corrected
    return (ocv_per_cell + offset_v_per_cell) * bank.cell_count
