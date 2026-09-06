"""Tests for battery_soc_core.calibration module.

Test coverage for voltage correction, calibration tolerance, simulated curves,
and voltage plausibility. Ported from src/battery_soc/tests/test_battery_soc_mqtt.py.
"""
from __future__ import annotations

import time

import pytest

from battery_soc_core.calibration import (
    apply_calibration, apply_voltage_plausibility, calibration_tolerance,
    corrected_voltage_per_cell, simulated_open_circuit_v_per_cell,
    voltage_based_soc_pct,
)
from battery_soc_core.state import BankState
from tests.conftest import make_params


# ---------------------------------------------------------------------------
# Lastkompensation (load offset)
# ---------------------------------------------------------------------------
@pytest.mark.parametrize("current_a,offset_mv", [
    (5.0, 5),     # 0.05C
    (15.0, 25),   # 0.15C
    (30.0, 60),   # 0.30C
    (80.0, 120),  # 0.80C
    (150.0, 200), # > 1C
])
def test_load_offset_table_is_the_default_path(current_a, offset_mv):
    """Ohne Override muss exakt die bisherige Bin-Tabelle herauskommen -
    in Lade- und in Entladerichtung."""
    charging = corrected_voltage_per_cell(26.8, 8, current_a, 100.0)
    discharging = corrected_voltage_per_cell(26.8, 8, -current_a, 100.0)
    assert charging == pytest.approx(26.8 / 8 - offset_mv / 1000.0)
    assert discharging == pytest.approx(26.8 / 8 + offset_mv / 1000.0)


def test_internal_resistance_override_is_linear():
    assert corrected_voltage_per_cell(26.8, 8, 20.0, 100.0, 1.2) == pytest.approx(3.326)
    assert corrected_voltage_per_cell(26.8, 8, -20.0, 100.0, 1.2) == pytest.approx(3.374)
    # 0.0 heisst ausdruecklich "keine Lastkorrektur" und ist von "nicht
    # gesetzt" (None -> Tabelle) unterscheidbar.
    assert corrected_voltage_per_cell(26.8, 8, 20.0, 100.0, 0.0) == pytest.approx(3.35)


def test_zero_current_matches_in_both_branches():
    assert (corrected_voltage_per_cell(26.8, 8, 0.0, 100.0)
            == corrected_voltage_per_cell(26.8, 8, 0.0, 100.0, 1.2))


# ---------------------------------------------------------------------------
# Kalibrierung (calibration)
# ---------------------------------------------------------------------------
def test_calibration_debounce_and_freeze():
    params = make_params(calibration_hold_s=120.0, empty_v_per_cell=2.7,
                         full_v_per_cell=3.5, calibration_tolerance_v_per_cell=0.0)
    bank = BankState("pack", 8, 100.0)
    bank.coulomb_ah = 50.0
    t0 = time.time()

    apply_calibration(params, bank, 2.4, t0, current_a=0.0)
    assert bank.coulomb_ah == 50.0  # Debounce laeuft noch
    apply_calibration(params, bank, 2.4, t0 + 119, current_a=0.0)
    assert bank.coulomb_ah == 50.0

    apply_calibration(params, bank, 2.4, t0 + 121, current_a=0.0)
    assert bank.coulomb_ah == 0.0
    assert bank.last_calibration_iso is not None


def test_calibration_tolerance_is_full_at_rest_and_gone_under_load():
    params = make_params(calibration_tolerance_v_per_cell=0.08)
    # 100 Ah Bank: 0 A und 1 A liegen unter 0.02C, 20 A weit ueber 0.10C.
    assert calibration_tolerance(params, 0.0, 100.0) == pytest.approx(0.08)
    assert calibration_tolerance(params, 1.0, 100.0) == pytest.approx(0.08)
    assert calibration_tolerance(params, 20.0, 100.0) == 0.0
    # Dazwischen linear, und richtungsblind: Laden und Entladen gleich.
    mid = calibration_tolerance(params, 6.0, 100.0)
    assert mid == pytest.approx(0.08 * 0.5)
    assert calibration_tolerance(params, -6.0, 100.0) == pytest.approx(mid)


def test_calibration_tolerance_off_restores_hard_thresholds():
    params = make_params(calibration_tolerance_v_per_cell=0.0)
    assert calibration_tolerance(params, 0.0, 100.0) == 0.0


def test_taper_current_calibrates_full_below_the_nominal_threshold():
    """Der Fall, fuer den das Fenster existiert: das Ladegeraet erreicht die
    100-%-Ruhespannung nie, regelt aber den Strom auf fast null herunter."""
    params = make_params(calibration_hold_s=0.0, full_v_per_cell=3.50,
                         calibration_tolerance_v_per_cell=0.08)
    bank = BankState("pack", 8, 100.0)
    bank.coulomb_ah = 50.0
    now = time.time()

    # 3.45 V/Zelle verfehlt die harte Schwelle, liegt aber im Fenster.
    apply_calibration(params, bank, 3.45, now, current_a=1.0)
    assert bank.coulomb_ah == 100.0


def test_bulk_current_does_not_calibrate_below_the_nominal_threshold():
    """Gegenprobe: derselbe Spannungswert bei Bulk-Strom darf nicht
    kalibrieren - dort ist die IR-Korrektur zu unsicher."""
    params = make_params(calibration_hold_s=0.0, full_v_per_cell=3.50,
                         calibration_tolerance_v_per_cell=0.08)
    bank = BankState("pack", 8, 100.0)
    bank.coulomb_ah = 50.0

    apply_calibration(params, bank, 3.45, time.time(), current_a=30.0)
    assert bank.coulomb_ah == 50.0


def test_tolerance_window_does_not_reach_a_half_full_pack():
    """Wichtigste Schutzgrenze: ein ruhendes Paket bei 50 % (26,2 V nach
    Datenblatt) darf auch mit voller Toleranz nicht auf 100 % springen."""
    params = make_params(calibration_hold_s=0.0, soc_curve="dyness_ar2.5",
                         empty_v_per_cell=2.70, full_v_per_cell=3.50,
                         calibration_tolerance_v_per_cell=0.08)
    bank = BankState("pack", 8, 100.0)
    bank.coulomb_ah = 50.0

    apply_calibration(params, bank, 26.2 / 8, time.time(), current_a=0.0)
    assert bank.coulomb_ah == 50.0


# ---------------------------------------------------------------------------
# Spannungsbasierte Plausibilitaetspruefung (voltage-based SoC)
# ---------------------------------------------------------------------------
@pytest.mark.parametrize("soc_pct", [0.0, 20.0, 50.0, 80.0, 100.0])
def test_voltage_based_soc_inverts_the_open_circuit_curve(soc_pct):
    """voltage_based_soc_pct() ist die Umkehrung von
    simulated_open_circuit_v_per_cell() - Rundtrip muss die Ausgangs-SoC
    ergeben."""
    params = make_params()
    voltage = simulated_open_circuit_v_per_cell(soc_pct, params)
    assert voltage_based_soc_pct(voltage, params) == pytest.approx(soc_pct, abs=0.1)


def test_voltage_based_soc_is_none_without_a_voltage():
    params = make_params()
    assert voltage_based_soc_pct(None, params) is None


# Dyness AR2.5-24V, Benutzerhandbuch Tabelle 2-1: Packspannung (8 Zellen) und
# der zugehoerige Ladezustand. Nagelt die Herkunft von DYNESS_AR25_CURVE fest -
# der bisherige Rundtrip-Test oben ist formunabhaengig und wuerde eine
# verschobene Kurve nicht bemerken.
@pytest.mark.parametrize("pack_voltage_v,expected_soc_pct", [
    (21.6, 0.0),
    (25.8, 20.0),
    (26.0, 30.0),
    (26.4, 70.0),
    (26.6, 95.0),
    (28.0, 100.0),
])
def test_dyness_curve_matches_the_data_sheet(pack_voltage_v, expected_soc_pct):
    params = make_params(soc_curve="dyness_ar2.5",
                         empty_v_per_cell=2.70, full_v_per_cell=3.50)
    soc = voltage_based_soc_pct(pack_voltage_v / 8, params)
    assert soc == pytest.approx(expected_soc_pct, abs=1.0)


def test_simulated_open_circuit_curve_spans_the_configured_thresholds():
    """Die simulierte Kurve reicht vom eingestellten empty_v_per_cell bis
    zum full_v_per_cell."""
    params = make_params(empty_v_per_cell=2.5, full_v_per_cell=3.6)
    assert simulated_open_circuit_v_per_cell(0.0, params) == pytest.approx(2.5, abs=0.01)
    assert simulated_open_circuit_v_per_cell(100.0, params) == pytest.approx(3.6, abs=0.01)


def test_simulated_open_circuit_curve_is_monotonic_and_flat_in_the_middle():
    """Die Kurve darf nie sinken, und im Mittelteil (ca. 15-85 %) ist sie
    praktisch flach."""
    params = make_params()
    prev_v = simulated_open_circuit_v_per_cell(0.0, params)
    for soc_pct in range(5, 101, 5):
        v = simulated_open_circuit_v_per_cell(soc_pct, params)
        assert v >= prev_v, f"Kurve sinkt bei {soc_pct} %"
        prev_v = v

    # Im flachen Bereich (15-85 %) darf die Aenderung nicht groß sein
    v_15 = simulated_open_circuit_v_per_cell(15.0, params)
    v_85 = simulated_open_circuit_v_per_cell(85.0, params)
    span = params.full_v_per_cell - params.empty_v_per_cell
    flat_range = v_85 - v_15
    assert flat_range < 0.15 * span, "Mittelteil ist zu steil"


# ---------------------------------------------------------------------------
# Spannungs-SoC-Plausibilitaetspruefung
# ---------------------------------------------------------------------------
def test_voltage_plausibility_needs_hold_time_before_warning():
    params = make_params(voltage_soc_mismatch_warn_pct=10.0, voltage_mismatch_hold_s=100.0)
    bank = BankState("pack", 8, 100.0)
    bank.coulomb_ah = 50.0  # soc_pct == 50.0
    now = time.time()

    apply_voltage_plausibility(params, bank, 90.0, now)  # Abweichung 40 > 10
    assert bank.voltage_mismatch is False  # Haltezeit laeuft noch

    apply_voltage_plausibility(params, bank, 90.0, now + 101)
    assert bank.voltage_mismatch is True


def test_voltage_plausibility_resets_when_back_in_range():
    params = make_params(voltage_soc_mismatch_warn_pct=10.0, voltage_mismatch_hold_s=0.0)
    bank = BankState("pack", 8, 100.0)
    bank.coulomb_ah = 50.0
    now = time.time()
    apply_voltage_plausibility(params, bank, 90.0, now)
    assert bank.voltage_mismatch is True

    apply_voltage_plausibility(params, bank, 55.0, now + 1)
    assert bank.voltage_mismatch is False
    assert bank.pending_mismatch_since is None


def test_voltage_plausibility_ignores_a_missing_voltage_estimate():
    params = make_params()
    bank = BankState("pack", 8, 100.0)
    bank.coulomb_ah = 50.0
    bank.voltage_mismatch = True
    bank.pending_mismatch_since = time.time()

    apply_voltage_plausibility(params, bank, None, time.time())
    assert bank.voltage_mismatch is False
    assert bank.pending_mismatch_since is None


# ---------------------------------------------------------------------------
# Taper-Kriterium (Task 1)
# ---------------------------------------------------------------------------
def test_full_taper_blocks_calibration_at_bulk_current():
    """Der Fall aus der Anlage: 27,9 V Packspannung, aber 17 A Ladestrom.
    Das Pack nimmt noch Ladung auf, also ist es nicht voll - egal wie hoch
    die Spannung steht, denn die stellt der Laderegler."""
    params = make_params(calibration_hold_s=0.0, full_v_per_cell=3.5,
                         calibration_tolerance_v_per_cell=0.02,
                         full_taper_c_rate=0.05)
    bank = BankState("pack", 8, 200.0)
    bank.coulomb_ah = 150.0
    now = time.time()
    # 3.49 V/Zelle liegt ueber der Schwelle 3.48 - ohne Taper wuerde das
    # sofort kalibrieren.
    apply_calibration(params, bank, 3.49, now, current_a=17.0)
    assert bank.coulomb_ah == 150.0
    assert bank.pending_high_since is None


def test_full_taper_allows_calibration_below_tail_current():
    """5 A je Pack ist der Tail-Strom aus dem Dyness-Datenblatt; bei zwei
    parallelen Packs (200 Ah) sind das 10 A = 0.05 C."""
    params = make_params(calibration_hold_s=0.0, full_v_per_cell=3.5,
                         calibration_tolerance_v_per_cell=0.02,
                         full_taper_c_rate=0.05)
    bank = BankState("pack", 8, 200.0)
    bank.coulomb_ah = 150.0
    apply_calibration(params, bank, 3.49, time.time(), current_a=9.0)
    assert bank.coulomb_ah == 200.0


def test_full_taper_is_direction_blind():
    """Ein Pack, das bei hoher Spannung kraeftig entladen wird, ist genauso
    wenig 'voll' wie eines, das kraeftig laedt."""
    params = make_params(calibration_hold_s=0.0, full_v_per_cell=3.5,
                         calibration_tolerance_v_per_cell=0.02,
                         full_taper_c_rate=0.05)
    bank = BankState("pack", 8, 200.0)
    bank.coulomb_ah = 150.0
    apply_calibration(params, bank, 3.49, time.time(), current_a=-30.0)
    assert bank.coulomb_ah == 150.0


def test_full_taper_unset_keeps_old_behaviour():
    """Bestandsinstallationen ohne den neuen Schluessel duerfen sich nicht
    aendern - das ist die Rueckfall-Garantie fuer den Produktiv-Pi."""
    params = make_params(calibration_hold_s=0.0, full_v_per_cell=3.5,
                         calibration_tolerance_v_per_cell=0.02)
    assert params.full_taper_c_rate is None
    bank = BankState("pack", 8, 200.0)
    bank.coulomb_ah = 150.0
    apply_calibration(params, bank, 3.50, time.time(), current_a=17.0)
    assert bank.coulomb_ah == 200.0


def test_full_taper_does_not_touch_the_empty_side():
    """Die Leer-Kalibrierung hat ihr eigenes Kriterium und darf vom
    Taper-Gate nicht mitblockiert werden."""
    params = make_params(calibration_hold_s=0.0, empty_v_per_cell=2.7,
                         calibration_tolerance_v_per_cell=0.02,
                         full_taper_c_rate=0.05)
    bank = BankState("pack", 8, 200.0)
    bank.coulomb_ah = 50.0
    apply_calibration(params, bank, 2.70, time.time(), current_a=-40.0)
    assert bank.coulomb_ah == 0.0


# ---------------------------------------------------------------------------
# Getrennte Toleranzen (Task 2)
# ---------------------------------------------------------------------------
def test_tolerance_falls_back_to_the_shared_value():
    params = make_params(calibration_tolerance_v_per_cell=0.05)
    assert params.tolerance_for("empty") == 0.05
    assert params.tolerance_for("full") == 0.05


def test_tolerance_per_side_overrides_the_shared_value():
    """Der Anlagenfall: oben eng, weil der Laderegler die Vollspannung
    erreicht - unten weit, weil das BMS lange vor der Leerspannung
    abschaltet."""
    params = make_params(calibration_tolerance_v_per_cell=0.02,
                         calibration_tolerance_empty_v_per_cell=0.20)
    assert params.tolerance_for("full") == 0.02
    assert params.tolerance_for("empty") == 0.20


def test_tolerance_zero_per_side_is_distinguishable_from_unset():
    """0.0 heisst ausdruecklich 'keine Toleranz', nicht 'nicht gesetzt' -
    dasselbe Muster wie bei internal_resistance_mohm_per_cell."""
    params = make_params(calibration_tolerance_v_per_cell=0.08,
                         calibration_tolerance_full_v_per_cell=0.0)
    assert params.tolerance_for("full") == 0.0
    assert params.tolerance_for("empty") == 0.08


def test_wide_empty_tolerance_makes_the_bottom_anchor_reachable():
    """23,2 V Packspannung ist die BMS-Warnschwelle des Dyness - tiefer
    kommt die Anlage nicht. Mit 0.20 V/Zelle Toleranz kalibriert sie dort,
    ohne bleibt der Zaehler unten ohne Anker."""
    reachable = make_params(calibration_hold_s=0.0, empty_v_per_cell=2.7,
                            calibration_tolerance_v_per_cell=0.02,
                            calibration_tolerance_empty_v_per_cell=0.20)
    bank = BankState("pack", 8, 200.0)
    bank.coulomb_ah = 40.0
    apply_calibration(reachable, bank, 23.2 / 8, time.time(), current_a=-2.0)
    assert bank.coulomb_ah == 0.0

    unreachable = make_params(calibration_hold_s=0.0, empty_v_per_cell=2.7,
                              calibration_tolerance_v_per_cell=0.02)
    bank2 = BankState("pack", 8, 200.0)
    bank2.coulomb_ah = 40.0
    apply_calibration(unreachable, bank2, 23.2 / 8, time.time(), current_a=-2.0)
    assert bank2.coulomb_ah == 40.0


# ---------------------------------------------------------------------------
# Karenzzeit (Task 3)
# ---------------------------------------------------------------------------
def _params_with_grace(grace_s):
    return make_params(calibration_hold_s=600.0, full_v_per_cell=3.5,
                       calibration_tolerance_v_per_cell=0.02,
                       full_taper_c_rate=0.05,
                       calibration_grace_s=grace_s)


def test_short_dropout_does_not_reset_the_hold_timer():
    """Der T2MG pausiert beim Ueberschussladen 30-60 s. Ohne Karenz kann
    calibration_hold_s in dieser Anlage nie ablaufen."""
    params = _params_with_grace(90.0)
    bank = BankState("pack", 8, 200.0)
    bank.coulomb_ah = 150.0
    t0 = 1000.0
    apply_calibration(params, bank, 3.49, t0, current_a=5.0)          # Timer startet
    assert bank.pending_high_since == t0
    apply_calibration(params, bank, 3.20, t0 + 300, current_a=0.0)    # Aussetzer
    assert bank.pending_high_since == t0                              # ueberlebt
    apply_calibration(params, bank, 3.49, t0 + 340, current_a=5.0)    # wieder da
    assert bank.pending_high_since == t0
    apply_calibration(params, bank, 3.49, t0 + 601, current_a=5.0)    # 600 s voll
    assert bank.coulomb_ah == 200.0


def test_long_dropout_resets_the_hold_timer():
    params = _params_with_grace(90.0)
    bank = BankState("pack", 8, 200.0)
    bank.coulomb_ah = 150.0
    t0 = 1000.0
    apply_calibration(params, bank, 3.49, t0, current_a=5.0)
    apply_calibration(params, bank, 3.20, t0 + 300, current_a=0.0)
    apply_calibration(params, bank, 3.20, t0 + 400, current_a=0.0)    # 100 s > 90 s
    assert bank.pending_high_since is None


def test_grace_zero_keeps_the_old_hard_reset():
    params = _params_with_grace(0.0)
    bank = BankState("pack", 8, 200.0)
    bank.coulomb_ah = 150.0
    apply_calibration(params, bank, 3.49, 1000.0, current_a=5.0)
    apply_calibration(params, bank, 3.20, 1001.0, current_a=0.0)
    assert bank.pending_high_since is None


def test_grace_does_not_survive_a_stale_voltage():
    """Bei veralteter Spannung liefert die Engine None. Das ist kein
    Aussetzer des Ladereglers, sondern ein blinder Sensor - da darf keine
    Haltezeit weiterlaufen, sonst kalibriert ein haengender Sensor."""
    params = _params_with_grace(600.0)
    bank = BankState("pack", 8, 200.0)
    apply_calibration(params, bank, 3.49, 1000.0, current_a=5.0)
    apply_calibration(params, bank, None, 1010.0, current_a=5.0)
    assert bank.pending_high_since is None
