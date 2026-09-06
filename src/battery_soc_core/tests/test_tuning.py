"""Tests fuer battery_soc_core.tuning - die Auswertung der
Kalibrierereignisse zu Einstellungsvorschlaegen."""
import pytest

from tests.conftest import make_params
from battery_soc_core.state import CalibrationEvent
from battery_soc_core.tuning import MIN_SAMPLES_FOR_SUGGESTION, analyse


def full_event(charged_ah, discharged_ah, current_a=5.0, taper_met=True, n=0):
    """Ein Voll-Anker mit vorgegebener Ladungsbilanz seit dem vorigen."""
    return CalibrationEvent(
        iso=f"2026-09-0{n % 9 + 1}T12:00:00+0200", unit="pack", side="full",
        coulomb_before_ah=200.0 - (discharged_ah - charged_ah),
        coulomb_after_ah=200.0, residual_ah=discharged_ah - charged_ah,
        voltage_v=27.9, cell_count=8, corrected_v_per_cell=3.49, current_a=current_a,
        threshold_v_per_cell=3.48, tolerance_v_per_cell=0.02, hold_s=600.0,
        taper_met=taper_met, charged_ah=charged_ah, discharged_ah=discharged_ah)


def test_too_few_events_yields_no_suggestion():
    params = make_params(inverter_dc_ac_efficiency=0.90)
    events = [full_event(100.0, 105.0, n=n) for n in range(MIN_SAMPLES_FOR_SUGGESTION - 1)]
    suggestions, findings = analyse(params, events)
    assert suggestions == []
    assert any(f.code == "zu_wenig_daten" for f in findings)


def test_consistent_residual_suggests_a_higher_inverter_efficiency():
    """Der Zaehler faellt jedes Intervall um 5 Ah zu tief: es wurde mehr
    Entladung gebucht als wirklich geflossen ist, also ist der echte
    Wirkungsgrad hoeher als angenommen."""
    params = make_params(inverter_dc_ac_efficiency=0.90)
    events = [full_event(100.0, 105.0, n=n) for n in range(8)]
    suggestions, _ = analyse(params, events)
    inv = next(s for s in suggestions if s.key == "inverter_dc_ac_efficiency")
    # b = 100/105 = 0.952 -> eta_neu = 0.90/0.952 = 0.945
    assert inv.suggested_value == pytest.approx(0.945, abs=0.002)
    assert inv.current_value == 0.90
    assert inv.sample_count == 8
    assert inv.confidence == "hoch"


def test_scattered_residuals_lower_the_confidence():
    params = make_params(inverter_dc_ac_efficiency=0.85)
    noisy = [80.0, 130.0, 95.0, 145.0, 70.0, 150.0, 90.0, 140.0]
    events = [full_event(100.0, d, n=n) for n, d in enumerate(noisy)]
    suggestions, _ = analyse(params, events)
    inv = next(s for s in suggestions if s.key == "inverter_dc_ac_efficiency")
    assert inv.confidence == "gering"


def test_suggestion_is_clamped_to_a_physical_range():
    """Ein Wirkungsgrad ueber 1 waere ein Perpetuum mobile - der
    Algorithmus muss dann schweigen statt Unsinn vorzuschlagen."""
    params = make_params(inverter_dc_ac_efficiency=0.95)
    events = [full_event(140.0, 100.0, n=n) for n in range(8)]
    suggestions, findings = analyse(params, events)
    assert not any(s.key == "inverter_dc_ac_efficiency" for s in suggestions)
    assert any(f.code == "unplausibel" for f in findings)


def test_untrustworthy_anchors_are_excluded():
    """Ein Anker ohne erfuelltes Taper-Kriterium ist kein Beleg fuer
    'voll' - solche Intervalle duerfen die Schaetzung nicht stuetzen."""
    params = make_params(inverter_dc_ac_efficiency=0.90)
    events = [full_event(100.0, 105.0, taper_met=False, n=n) for n in range(8)]
    suggestions, findings = analyse(params, events)
    assert suggestions == []
    assert any(f.code == "anker_unzuverlaessig" for f in findings)


def test_high_current_at_calibration_is_reported():
    params = make_params(inverter_dc_ac_efficiency=0.90, full_taper_c_rate=0.05,
                         bank_a_capacity_ah=100.0, bank_b_capacity_ah=100.0)
    events = [full_event(100.0, 105.0, current_a=17.0, n=n) for n in range(8)]
    _suggestions, findings = analyse(params, events)
    assert any(f.code == "schwelle_zu_tief" for f in findings)


def test_missing_bottom_anchor_is_reported_not_guessed():
    """Ohne Voll->Leer-Strecke ist die Kapazitaet nicht bestimmbar. Der
    Algorithmus muss das sagen, statt einen Wert zu erfinden."""
    params = make_params(inverter_dc_ac_efficiency=0.90)
    events = [full_event(100.0, 105.0, n=n) for n in range(8)]
    suggestions, findings = analyse(params, events)
    assert not any(s.key.endswith("capacity_ah") for s in suggestions)
    assert any(f.code == "kein_unterer_anker" for f in findings)
