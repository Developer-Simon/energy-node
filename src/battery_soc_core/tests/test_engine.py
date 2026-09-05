"""Tests for battery_soc_core.engine — the pure tick().

Ported from src/battery_soc/tests/test_battery_soc_mqtt.py, where the same
scenarios drove compute_and_publish(). Here they build SocParams + SocState +
SocInputs, call tick(...), and read result.outputs — the byte-for-byte
equivalent of today's /state payload.
"""
import time

import pytest

from tests.conftest import make_params
from battery_soc_core.state import SocState
from battery_soc_core.inputs import SocInputs
from battery_soc_core.engine import tick


def fresh_inputs(now=None, **kw):
    now = now or time.time()
    i = SocInputs()
    i.charger_power_configured = i.inverter_power_configured = i.bank_a_voltage_configured = True
    i.charger_power_ts = i.inverter_power_ts = i.bank_a_voltage_ts = now
    i.bank_a_voltage_v = 26.8
    for k, v in kw.items():
        setattr(i, k, v)
    return i


def series_fresh(now=None, **kw):
    """fresh_inputs plus a configured, fresh bank B at 26.8 V."""
    now = now or time.time()
    base = dict(bank_b_voltage_configured=True, bank_b_voltage_ts=now,
                bank_b_voltage_v=26.8)
    base.update(kw)
    return fresh_inputs(now, **base)


# ---------------------------------------------------------------------------
# Strommodell je Topologie
# ---------------------------------------------------------------------------
def test_parallel_publishes_one_soc_and_no_bank_fields():
    p = make_params(topology="parallel")
    s = SocState(p, last_tick=time.time() - 900)
    out = tick(p, s, fresh_inputs(charger_power_w=300.0, inverter_power_w=40.0),
               time.time()).outputs
    assert "soc_combined_pct" in out
    assert out["soc_combined_pct"] is not None
    assert out["pack_voltage_v"] == pytest.approx(26.8)
    for key in ("soc_a_pct", "soc_b_pct", "bank_a_voltage_v", "bank_b_voltage_v",
                "voltage_delta_v", "imbalance_warning"):
        assert key not in out, key


def test_parallel_current_uses_the_whole_pack():
    """Eine Batterie: der gesamte Netto-Strom laeuft durch den einen Zaehler."""
    p = make_params(topology="parallel")
    s = SocState(p)
    s.units[0].coulomb_ah = 0.0
    tick(p, s, fresh_inputs(charger_power_w=100.0, inverter_power_w=0.0),
         time.time(), dt_hours=1.0)

    expected = (100.0 * 0.9) / 26.8 * 0.98
    assert s.units[0].coulomb_ah == pytest.approx(expected)


def test_series_gives_both_banks_the_same_current():
    """In Reihe fliesst physikalisch derselbe Strom durch beide Baenke."""
    p = make_params(topology="series", bank_a_capacity_ah=100.0,
                    bank_b_capacity_ah=100.0)
    s = SocState(p)
    now = time.time()
    out = tick(p, s, series_fresh(now, bank_a_voltage_v=25.0,
                                  bank_b_voltage_v=27.0, charger_power_w=520.0,
                                  inverter_power_w=0.0), now, dt_hours=0.0).outputs

    assert out["bank_a_current_a"] == out["bank_b_current_a"]
    expected = 520.0 * 0.9 / (25.0 + 27.0)
    assert out["bank_a_current_a"] == pytest.approx(round(expected, 2))


def test_series_combined_soc_is_the_weaker_bank():
    """Eine Reihenschaltung ist leer, sobald die schwaechste Bank leer ist."""
    p = make_params(topology="series", bank_a_capacity_ah=100.0,
                    bank_b_capacity_ah=100.0)
    s = SocState(p)
    s.units[0].coulomb_ah = 80.0
    s.units[1].coulomb_ah = 30.0
    now = time.time()
    out = tick(p, s, series_fresh(now), now, dt_hours=0.0).outputs

    assert out["soc_combined_pct"] == pytest.approx(30.0)


def test_series_time_to_full_uses_the_total_voltage_and_the_fuller_bank():
    """Geladen wird, bis die ERSTE Bank voll ist; der Strom haengt an der
    SUMMENspannung des Stapels."""
    p = make_params(topology="series", bank_a_capacity_ah=100.0,
                    bank_b_capacity_ah=100.0)
    s = SocState(p)
    s.units[0].coulomb_ah = 50.0   # Kopfraum 50 Ah -> begrenzt
    s.units[1].coulomb_ah = 40.0   # Kopfraum 60 Ah
    now = time.time()
    out = tick(p, s, series_fresh(now, bank_a_voltage_v=26.0,
                                  bank_b_voltage_v=26.0,
                                  charger_power_w=1000.0 / 0.9,
                                  inverter_power_w=0.0), now, dt_hours=0.0).outputs

    current_a = 1000.0 / 52.0            # Summenspannung, nicht 26 V
    assert out["time_to_full_h"] == pytest.approx(round(50.0 / current_a, 2),
                                                  abs=0.02)


# ---------------------------------------------------------------------------
# Leistungsbilanz
# ---------------------------------------------------------------------------
def test_net_dc_power_applies_each_converter_efficiency():
    """Laufen Ladegeraet und Umrichter gleichzeitig, wirken beide Verluste."""
    p = make_params()
    s = SocState(p)
    out = tick(p, s, fresh_inputs(charger_power_w=100.0, inverter_power_w=50.0),
               time.time(), dt_hours=0.0).outputs

    expected = 100.0 * 0.9 - 50.0 / 0.9
    assert out["net_power_w"] == pytest.approx(round(expected, 1))
    assert out["net_power_w"] != pytest.approx(45.0)


def test_charge_integration_uses_dc_power_and_coulombic_efficiency():
    p = make_params(topology="series", bank_a_capacity_ah=100.0,
                    bank_b_capacity_ah=100.0)
    s = SocState(p)
    s.units[0].coulomb_ah = 0.0
    now = time.time()
    tick(p, s, series_fresh(now, charger_power_w=100.0, inverter_power_w=0.0),
         now, dt_hours=1.0)

    expected = 100.0 * 0.9 / (26.8 + 26.8) * 0.98
    assert s.units[0].coulomb_ah == pytest.approx(expected)


def test_negative_shelly_readings_do_not_amplify_through_the_divisor():
    """Kleine negative Leerlaufwerte duerfen die Division nicht vergroessern."""
    p = make_params()
    s = SocState(p)
    out = tick(p, s, fresh_inputs(charger_power_w=-0.4, inverter_power_w=-0.4),
               time.time(), dt_hours=0.0).outputs

    assert out["net_power_w"] == 0.0


@pytest.mark.parametrize("dt_hours", [-1.0, 5.0])
def test_dt_hours_guard_ignores_negative_and_oversized_ticks(dt_hours):
    p = make_params()
    s = SocState(p)
    before = s.units[0].coulomb_ah
    tick(p, s, fresh_inputs(charger_power_w=500.0), time.time(),
         dt_hours=dt_hours)
    assert s.units[0].coulomb_ah == before


def test_dc_power_replaces_the_ac_measurement_without_efficiency():
    """Der DC-Wert steht schon auf dem Gleichstrombus - kein Wandler-
    Wirkungsgrad darf darauf noch angewandt werden."""
    p = make_params()
    s = SocState(p)
    now = time.time()
    out = tick(p, s, fresh_inputs(now, charger_power_w=100.0,
                                  charger_dc_power_w=80.0,
                                  charger_dc_power_configured=True,
                                  charger_dc_power_ts=now,
                                  inverter_power_w=0.0), now, dt_hours=0.0).outputs

    assert out["net_power_w"] == pytest.approx(80.0)
    assert out["charger_power_source"] == "dc"
    assert out["inverter_power_source"] == "ac"
    assert out["ac_fallback_active"] is False


def test_dc_value_older_than_dc_max_age_falls_back_to_ac():
    p = make_params(dc_max_age_s=60.0)
    s = SocState(p)
    now = time.time()
    out = tick(p, s, fresh_inputs(now, charger_power_w=100.0,
                                  charger_dc_power_w=80.0,
                                  charger_dc_power_configured=True,
                                  charger_dc_power_ts=now - 61.0,
                                  inverter_power_w=0.0), now, dt_hours=0.0).outputs

    assert out["net_power_w"] == pytest.approx(90.0)
    assert out["charger_power_source"] == "ac"
    assert out["ac_fallback_active"] is True


def test_without_a_dc_input_nothing_counts_as_fallback():
    """Ohne konfigurierten DC-Eingang ist der AC-Pfad der Normalfall und darf
    die Warnung nicht dauerhaft anschalten."""
    p = make_params()
    s = SocState(p)
    out = tick(p, s, fresh_inputs(charger_power_w=100.0), time.time(),
               dt_hours=0.0).outputs

    assert out["ac_fallback_active"] is False


# ---------------------------------------------------------------------------
# Stale-Behandlung
# ---------------------------------------------------------------------------
def test_stale_input_freezes_coulomb_counter():
    p = make_params()
    s = SocState(p)
    now = time.time()
    i = fresh_inputs(now, charger_power_w=500.0)
    i.charger_power_ts = now - 10 * p.stale_input_s
    before = s.units[0].coulomb_ah

    out = tick(p, s, i, now, dt_hours=1.0).outputs

    assert s.units[0].coulomb_ah == before
    assert out["inputs_stale"] is True
    assert "Ladeleistung" in out["stale_inputs"]


def test_lenient_default_treats_stale_power_as_zero_not_frozen():
    """Default (require_fresh_inputs=False): eine veraltete Ladeleistung zaehlt
    als 0 W, die frische Umrichterleistung fliesst trotzdem weiter ein."""
    p = make_params()
    s = SocState(p)
    s.units[0].coulomb_ah = 50.0
    now = time.time()
    i = fresh_inputs(now, charger_power_w=500.0, inverter_power_w=90.0)
    i.charger_power_ts = now - 10 * p.stale_input_s

    out = tick(p, s, i, now, dt_hours=1.0).outputs

    expected_discharge_w = 90.0 / p.inverter_dc_ac_efficiency
    expected_current_a = -expected_discharge_w / 26.8
    assert s.units[0].coulomb_ah == pytest.approx(50.0 + expected_current_a)
    assert out["inputs_stale"] is True


def test_strict_mode_still_freezes_the_coulomb_counter():
    """require_fresh_inputs=True: irgendein veralteter Eingang haelt die
    gesamte Zaehlung an."""
    p = make_params(require_fresh_inputs=True)
    s = SocState(p)
    now = time.time()
    i = fresh_inputs(now, charger_power_w=500.0, inverter_power_w=90.0)
    i.charger_power_ts = now - 10 * p.stale_input_s
    before = s.units[0].coulomb_ah

    tick(p, s, i, now, dt_hours=1.0)

    assert s.units[0].coulomb_ah == before


def test_lenient_mode_still_integrates_using_the_last_known_voltage():
    """Eine veraltete Spannungsmessung haelt im Lenient-Modus nur die
    Kalibrierung an, nicht die Coulomb-Zaehlung."""
    p = make_params()
    s = SocState(p)
    s.units[0].coulomb_ah = 0.0
    now = time.time()
    i = fresh_inputs(now, charger_power_w=100.0, inverter_power_w=0.0)
    i.bank_a_voltage_ts = now - 10 * p.stale_input_s

    tick(p, s, i, now, dt_hours=1.0)

    expected = (100.0 * 0.9) / 26.8 * 0.98
    assert s.units[0].coulomb_ah == pytest.approx(expected)


def test_calibration_is_frozen_while_inputs_are_stale():
    """Ohne Einfrieren koennte ein Ausfall die Haltezeit auf reinen Altdaten
    vollenden und die Bank faelschlich auf 100 % setzen."""
    p = make_params(calibration_hold_s=0.0)
    s = SocState(p)
    s.units[0].coulomb_ah = 50.0
    now = time.time()
    i = fresh_inputs(now, bank_a_voltage_v=8 * 3.7)  # weit ueber full_v_per_cell
    i.bank_a_voltage_ts = now - 10 * p.stale_input_s

    tick(p, s, i, now, dt_hours=0.0)

    assert s.units[0].coulomb_ah == 50.0
    assert s.units[0].pending_high_since is None
    assert s.units[0].pending_low_since is None


# ---------------------------------------------------------------------------
# Ein-Bank-Modus und Unsymmetrie
# ---------------------------------------------------------------------------
def test_parallel_single_bank_publishes_the_same_payload_shape():
    p = make_params(topology="parallel", bank_b_enabled=False)
    s = SocState(p)
    out = tick(p, s, fresh_inputs(), time.time(), dt_hours=0.0).outputs
    assert out["soc_combined_pct"] is not None
    assert "soc_b_pct" not in out


@pytest.mark.parametrize("delta_v", [0.6, -0.6])
def test_imbalance_warning_uses_absolute_delta(delta_v):
    p = make_params(topology="series", bank_a_capacity_ah=100.0,
                    bank_b_capacity_ah=100.0, imbalance_warn_v=0.5)
    s = SocState(p)
    now = time.time()
    out = tick(p, s, series_fresh(now, bank_b_voltage_v=26.8 - delta_v), now,
               dt_hours=0.0).outputs

    assert out["imbalance_warning"] is True


def test_imbalance_warning_stays_off_below_the_threshold():
    p = make_params(topology="series", bank_a_capacity_ah=100.0,
                    bank_b_capacity_ah=100.0, imbalance_warn_v=0.5)
    s = SocState(p)
    now = time.time()
    out = tick(p, s, series_fresh(now, bank_b_voltage_v=26.8 - 0.4), now,
               dt_hours=0.0).outputs

    assert out["imbalance_warning"] is False


def test_time_estimates_need_a_meaningful_net_power():
    p = make_params()
    s = SocState(p)
    out = tick(p, s, fresh_inputs(charger_power_w=0.0, inverter_power_w=0.0),
               time.time(), dt_hours=0.0).outputs
    assert out["time_to_full_h"] is None and out["time_to_empty_h"] is None

    out = tick(p, s, fresh_inputs(charger_power_w=500.0), time.time(),
               dt_hours=0.0).outputs
    assert out["time_to_full_h"] > 0
    assert out["time_to_empty_h"] is None


# ---------------------------------------------------------------------------
# Spannungsbasierte Plausibilitaetspruefung
# ---------------------------------------------------------------------------
def test_tick_reports_voltage_soc_mismatch():
    """Ein grob falsch kalibrierter Coulomb-Zaehler (SoC 0 %, Spannung weit
    oben) soll nach der Haltezeit als ON gemeldet werden."""
    p = make_params(topology="parallel", voltage_soc_mismatch_warn_pct=10.0,
                    voltage_mismatch_hold_s=0.0)
    s = SocState(p)
    s.units[0].coulomb_ah = 0.0
    out = tick(p, s, fresh_inputs(), time.time(), dt_hours=0.0).outputs

    assert out["pack_voltage_soc_pct"] > 50.0
    assert out["pack_voltage_soc_mismatch"] is True
