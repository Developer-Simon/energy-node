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


def calibration_tolerance(params, current_a, capacity_ah, side="full"):
    """Wie weit die Kalibrierschwelle dieser Seite bei diesem Strom
    aufgeweicht werden darf (V/Zelle, immer >= 0). Volle Toleranz im
    Ruhezustand und in der CV-Endphase, keine bei Bulk-Strom - siehe
    CALIBRATION_TAPER_C_RATE.

    `side` waehlt zwischen der Leer- und der Voll-Schwelle; ohne
    seitenweise Uebersteuerung liefern beide denselben Wert."""
    max_tolerance = params.tolerance_for(side)
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


def full_taper_satisfied(params, current_a, capacity_ah):
    """Nimmt das Pack bei Vollspannung noch nennenswert Ladung auf - oder
    gibt es welche ab? Nur wenn beides verneint ist, ist 'voll' mehr als
    eine Spannungsablesung.

    Richtungsblind: 27,9 V unter 30 A Entladung sind genauso wenig ein
    volles Pack wie 27,9 V unter 30 A Ladung. Ohne konfigurierte C-Rate
    ist das Gate offen (Bestandsverhalten)."""
    if params.full_taper_c_rate is None:
        return True
    if capacity_ah <= 0:
        return False
    return abs(current_a or 0.0) / capacity_ah <= params.full_taper_c_rate


def _hold_timer(bank, side, now, grace_s, active):
    """Fuehrt den Haltezeit-Timer einer Seite und liefert seinen Startpunkt
    zurueck (None = laeuft nicht).

    `active` sagt, ob die Bedingung in diesem Tick erfuellt ist. Ein
    Aussetzer loescht den Timer nicht sofort, sondern erst wenn er laenger
    als grace_s dauert - siehe SocParams.calibration_grace_s."""
    since_attr = f"pending_{side}_since"
    broken_attr = f"pending_{side}_broken_since"
    if active:
        setattr(bank, broken_attr, None)
        if getattr(bank, since_attr) is None:
            setattr(bank, since_attr, now)
        return getattr(bank, since_attr)
    if getattr(bank, since_attr) is None:
        return None
    broken_since = getattr(bank, broken_attr)
    if broken_since is None:
        broken_since = now
        setattr(bank, broken_attr, broken_since)
    if now - broken_since >= grace_s:
        setattr(bank, since_attr, None)
        setattr(bank, broken_attr, None)
    return None


def _clear_hold_timers(bank):
    bank.pending_low_since = None
    bank.pending_high_since = None
    bank.pending_low_broken_since = None
    bank.pending_high_broken_since = None


def _record_calibration(bank, side, target_ah, now, since, corrected_v_per_cell,
                        current_a, raw_voltage_v, threshold, tolerance, taper_met):
    from .state import CalibrationEvent  # lokal: haelt state.py importfrei von calibration.py

    before = bank.coulomb_ah
    bank.coulomb_ah = target_ah
    bank.last_calibration_iso = time.strftime("%Y-%m-%dT%H:%M:%S%z")
    bank.append_event(CalibrationEvent(
        iso=bank.last_calibration_iso, unit=bank.name, side=side,
        coulomb_before_ah=round(before, 3),
        coulomb_after_ah=round(target_ah, 3),
        residual_ah=round(target_ah - before, 3),
        voltage_v=None if raw_voltage_v is None else round(raw_voltage_v, 3),
        cell_count=bank.cell_count,
        corrected_v_per_cell=round(corrected_v_per_cell, 4),
        current_a=round(current_a or 0.0, 3),
        threshold_v_per_cell=round(threshold, 4),
        tolerance_v_per_cell=round(tolerance, 4),
        hold_s=round(now - since, 1),
        taper_met=taper_met,
        charged_ah=round(bank.charged_ah, 3),
        discharged_ah=round(bank.discharged_ah, 3),
    ))
    bank.reset_balance()
    # Ruling 6: Reset the hold timer so the next event on the same side
    # needs a fresh calibration_hold_s to accumulate.
    setattr(bank, f"pending_{'low' if side == 'empty' else 'high'}_since", None)
    setattr(bank, f"pending_{'low' if side == 'empty' else 'high'}_broken_since", None)


def apply_calibration(params, bank, corrected_v_per_cell, now, current_a=0.0,
                      raw_voltage_v=None):
    """Prueft, ob die (lastkorrigierte) Spannung stabil genug ausserhalb der
    Schwellen liegt, um den Coulomb-Zaehler auf 0%/100% zurueckzusetzen.

    Drei Bedingungen muessen zusammenkommen:
      1. die Spannung liegt jenseits der Schwelle, aufgeweicht um die
         seitenweise Toleranz (calibration_tolerance),
      2. oben zusaetzlich: das Pack nimmt keine Ladung mehr auf und gibt
         auch keine ab (full_taper_satisfied) - in einer Anlage mit
         Ueberschussladen ist die Klemmenspannung eine Stellgroesse, kein
         Vollstands-Signal,
      3. beides haelt calibration_hold_s lang an, wobei Aussetzer bis
         calibration_grace_s den Timer nicht zuruecksetzen.

    Eine fehlende Spannung (veralteter Sensor) loescht beide Timer
    unabhaengig von der Karenz."""
    if corrected_v_per_cell is None:
        _clear_hold_timers(bank)
        return

    tolerance_empty = calibration_tolerance(params, current_a, bank.capacity_ah, "empty")
    tolerance_full = calibration_tolerance(params, current_a, bank.capacity_ah, "full")
    low_active = corrected_v_per_cell <= params.empty_v_per_cell + tolerance_empty
    high_active = (corrected_v_per_cell >= params.full_v_per_cell - tolerance_full
                   and full_taper_satisfied(params, current_a, bank.capacity_ah))

    low_since = _hold_timer(bank, "low", now, params.calibration_grace_s, low_active)
    high_since = _hold_timer(bank, "high", now, params.calibration_grace_s, high_active)

    if low_since is not None and now - low_since >= params.calibration_hold_s:
        _record_calibration(bank, "empty", 0.0, now, low_since, corrected_v_per_cell,
                          current_a, raw_voltage_v,
                          params.empty_v_per_cell + tolerance_empty, tolerance_empty,
                          True)
    elif high_since is not None and now - high_since >= params.calibration_hold_s:
        _record_calibration(bank, "full", bank.capacity_ah, now, high_since, corrected_v_per_cell,
                          current_a, raw_voltage_v,
                          params.full_v_per_cell - tolerance_full, tolerance_full,
                          full_taper_satisfied(params, current_a, bank.capacity_ah))


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
