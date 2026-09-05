"""Voltage correction, calibration, and plausibility functions for LiFePO4 batteries.

Pure functions for computing load-corrected voltages, calibration tolerance windows,
applying calibration when voltage thresholds are stable, and validating coulomb
counter against voltage-based estimates.
"""
from __future__ import annotations

import time
from typing import Optional

from .curves import (
    CALIBRATION_BULK_C_RATE, CALIBRATION_TAPER_C_RATE, load_offset_mv, soc_curve_for,
)


def corrected_voltage_per_cell(voltage_v, cell_count, current_a, capacity_ah,
                               resistance_mohm_per_cell=None):
    """Rechnet die gemessene Packspannung auf eine ruhespannungs-aequivalente
    Spannung pro Zelle um. Ist resistance_mohm_per_cell gesetzt, wird der
    Offset linear als I*R gerechnet, sonst kommt die Bin-Tabelle zum Einsatz
    (Standardpfad). current_a > 0 = Laden (Korrektur nach unten),
    current_a < 0 = Entladen (nach oben)."""
    if voltage_v is None or cell_count <= 0:
        return None
    current_a = current_a or 0.0
    if resistance_mohm_per_cell is not None:
        # Vorzeichen faellt hier aus der Arithmetik.
        offset_v_per_cell = current_a * resistance_mohm_per_cell / 1000.0
    elif current_a == 0:
        # Die Tabelle beginnt zwar bei 5 mV, aber ohne Strom gibt es nichts zu
        # korrigieren - und beide Zweige muessen bei 0 A denselben Wert liefern.
        offset_v_per_cell = 0.0
    else:
        c_rate = abs(current_a) / capacity_ah if capacity_ah > 0 else 0
        offset_v_per_cell = load_offset_mv(c_rate) / 1000.0
        if current_a < 0:
            offset_v_per_cell = -offset_v_per_cell
    return voltage_v / cell_count - offset_v_per_cell


def calibration_tolerance(params, current_a, capacity_ah):
    """Wie weit die Kalibrierschwellen bei diesem Strom aufgeweicht werden
    duerfen (V/Zelle, immer >= 0). Volle Toleranz im Ruhezustand und in der
    CV-Endphase, keine bei Bulk-Strom - siehe CALIBRATION_TAPER_C_RATE."""
    max_tolerance = params.calibration_tolerance_v_per_cell
    if max_tolerance <= 0 or capacity_ah <= 0:
        return 0.0
    c_rate = abs(current_a or 0.0) / capacity_ah
    if c_rate <= CALIBRATION_TAPER_C_RATE:
        return max_tolerance
    if c_rate >= CALIBRATION_BULK_C_RATE:
        return 0.0
    span = CALIBRATION_BULK_C_RATE - CALIBRATION_TAPER_C_RATE
    return max_tolerance * (CALIBRATION_BULK_C_RATE - c_rate) / span


def simulated_open_circuit_v_per_cell(soc_pct, params):
    """Ruhespannung je Zelle fuer einen Ladezustand - die Umkehrung dessen,
    was apply_calibration() an den Enden auswertet."""
    if soc_pct is None:
        return None
    fraction = max(0.0, min(1.0, soc_pct / 100.0))
    span = params.full_v_per_cell - params.empty_v_per_cell
    curve = soc_curve_for(params.soc_curve)
    for (x0, y0), (x1, y1) in zip(curve, curve[1:]):
        if fraction <= x1:
            share = y0 if x1 == x0 else y0 + (y1 - y0) * (fraction - x0) / (x1 - x0)
            return params.empty_v_per_cell + span * share
    return params.full_v_per_cell


def voltage_based_soc_pct(corrected_v_per_cell, params):
    """Schaetzt die SoC allein aus der (lastkorrigierten) Zellspannung, als
    Umkehrung von simulated_open_circuit_v_per_cell(). Nur eine grobe
    Plausibilitaetspruefung gegen den Coulomb-Zaehler - im flachen
    LiFePO4-Mittelbereich (ca. 15-85 %) ist diese Schaetzung praktisch
    nutzlos, siehe Modulkommentar Punkt 2)."""
    if corrected_v_per_cell is None:
        return None
    span = params.full_v_per_cell - params.empty_v_per_cell
    if span <= 0:
        return None
    y = (corrected_v_per_cell - params.empty_v_per_cell) / span
    y = max(0.0, min(1.0, y))
    curve = soc_curve_for(params.soc_curve)
    for (x0, y0), (x1, y1) in zip(curve, curve[1:]):
        if y <= y1:
            share = x0 if y1 == y0 else x0 + (x1 - x0) * (y - y0) / (y1 - y0)
            return round(share * 100, 1)
    return 100.0


def apply_calibration(params, bank, corrected_v_per_cell, now, current_a=0.0):
    """Prueft, ob die (lastkorrigierte) Spannung stabil genug ausserhalb der
    Schwellen liegt, um den Coulomb-Zaehler auf 0%/100% zurueckzusetzen.

    Die Schwellen sind nicht hart: bei kleinem Strom weicht sie
    calibration_tolerance() um bis zu calibration_tolerance_v_per_cell auf,
    damit ein Ladegeraet mit zu tiefer CV-Schwelle bzw. ein vor der
    Leerspannung abschaltender Wechselrichter die Kalibrierung nicht dauerhaft
    verhindert. Die Toleranz gilt weiterhin nur zusammen mit
    calibration_hold_s - ein einzelner Messausreisser kalibriert nichts."""
    if corrected_v_per_cell is None:
        bank.pending_low_since = None
        bank.pending_high_since = None
        return

    tolerance = calibration_tolerance(params, current_a, bank.capacity_ah)
    if corrected_v_per_cell <= params.empty_v_per_cell + tolerance:
        bank.pending_high_since = None
        bank.pending_low_since = bank.pending_low_since or now
        if now - bank.pending_low_since >= params.calibration_hold_s:
            bank.coulomb_ah = 0.0
            bank.last_calibration_iso = time.strftime("%Y-%m-%dT%H:%M:%S%z")
    elif corrected_v_per_cell >= params.full_v_per_cell - tolerance:
        bank.pending_low_since = None
        bank.pending_high_since = bank.pending_high_since or now
        if now - bank.pending_high_since >= params.calibration_hold_s:
            bank.coulomb_ah = bank.capacity_ah
            bank.last_calibration_iso = time.strftime("%Y-%m-%dT%H:%M:%S%z")
    else:
        bank.pending_low_since = None
        bank.pending_high_since = None


def apply_voltage_plausibility(params, bank, voltage_soc_pct, now):
    """Vergleicht die spannungsbasierte SoC-Schaetzung mit dem Coulomb-Zaehler
    und meldet eine Abweichung erst, nachdem sie voltage_mismatch_hold_s lang
    stabil ueber der Schwelle lag - Einzelausreisser (Rauschen, kurzer
    Lastwechsel) sollen nicht sofort anschlagen, analog zu apply_calibration()."""
    if voltage_soc_pct is None:
        bank.pending_mismatch_since = None
        bank.voltage_mismatch = False
        return
    deviation = abs(voltage_soc_pct - bank.soc_pct)
    if deviation > params.voltage_soc_mismatch_warn_pct:
        bank.pending_mismatch_since = bank.pending_mismatch_since or now
        bank.voltage_mismatch = (now - bank.pending_mismatch_since) >= params.voltage_mismatch_hold_s
    else:
        bank.pending_mismatch_since = None
        bank.voltage_mismatch = False
