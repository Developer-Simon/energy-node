import time
import pytest
from tests.conftest import make_params
from battery_soc_core.state import BankState, build_units, SocState, set_state_of_charge, CalibrationEvent, CALIBRATION_EVENT_LIMIT


def test_parallel_builds_one_pack_unit_with_summed_capacity():
    units = build_units(make_params(topology="parallel", bank_a_capacity_ah=100, bank_b_capacity_ah=100))
    assert [u.name for u in units] == ["pack"]
    assert units[0].capacity_ah == 200


def test_series_builds_two_bank_units():
    units = build_units(make_params(topology="series", bank_a_capacity_ah=100, bank_b_capacity_ah=100))
    assert [u.name for u in units] == ["bank_a", "bank_b"]


def test_roundtrip_to_dict_and_load_dict():
    p = make_params()
    s = SocState(p, last_tick=0.0)
    s.units[0].coulomb_ah = 42.0
    s.units[0].last_calibration_iso = "2026-08-29T10:00:00+0000"
    restored = SocState(p, last_tick=0.0)
    restored.load_dict(s.to_dict())
    assert restored.units[0].coulomb_ah == 42.0
    assert restored.units[0].last_calibration_iso == "2026-08-29T10:00:00+0000"


def test_legacy_parallel_sums_both_bank_counters():
    p = make_params(topology="parallel", bank_a_capacity_ah=100, bank_b_capacity_ah=100)
    s = SocState(p, last_tick=0.0)
    s.load_legacy_dict({"bank_a_coulomb_ah": 30.0, "bank_b_coulomb_ah": 25.0,
                        "bank_a_last_calibration_iso": "2026-01-01T00:00:00+0000"}, "parallel")
    assert s.units[0].coulomb_ah == 55.0
    assert s.units[0].last_calibration_iso == "2026-01-01T00:00:00+0000"


def test_legacy_series_keeps_banks_separate():
    p = make_params(topology="series", bank_a_capacity_ah=100, bank_b_capacity_ah=100)
    s = SocState(p, last_tick=0.0)
    s.load_legacy_dict({"bank_a_coulomb_ah": 30.0, "bank_b_coulomb_ah": 25.0}, "series")
    assert [u.coulomb_ah for u in s.units] == [30.0, 25.0]


def test_set_state_of_charge_parallel_sets_counter_and_stamps_calibration():
    p = make_params(topology="parallel", bank_a_capacity_ah=100, bank_b_capacity_ah=100)
    s = SocState(p, last_tick=0.0)
    set_state_of_charge(s, p, 75)
    assert s.units[0].coulomb_ah == pytest.approx(150.0)  # 75% of 200 Ah
    assert s.units[0].last_calibration_iso is not None


def test_set_state_of_charge_series_needs_a_unit_name():
    p = make_params(topology="series", bank_a_capacity_ah=100, bank_b_capacity_ah=100)
    s = SocState(p, last_tick=0.0)
    with pytest.raises(ValueError):
        set_state_of_charge(s, p, 50)
    set_state_of_charge(s, p, 50, unit_name="bank_b")
    assert s.units[1].coulomb_ah == pytest.approx(50.0)


def test_set_state_of_charge_clamps_out_of_range():
    p = make_params()
    s = SocState(p, last_tick=0.0)
    set_state_of_charge(s, p, 150)
    assert s.units[0].soc_pct == 100.0


def test_bank_state_starts_with_an_empty_charge_balance():
    bank = BankState("pack", 8, 200.0)
    assert bank.charged_ah == 0.0
    assert bank.discharged_ah == 0.0
    assert bank.events == []


def test_calibration_event_roundtrips_through_dict():
    """Der Ringpuffer landet in state.json - was rausgeht, muss
    unveraendert wieder reinkommen."""
    event = CalibrationEvent(
        iso="2026-09-06T17:26:22+0200", unit="pack", side="full",
        coulomb_before_ah=166.0, coulomb_after_ah=200.0, residual_ah=34.0,
        voltage_v=27.9, cell_count=8, corrected_v_per_cell=3.482, current_a=6.1,
        threshold_v_per_cell=3.48, tolerance_v_per_cell=0.02,
        hold_s=612.0, taper_met=True, charged_ah=95.4, discharged_ah=61.2,
    )
    assert CalibrationEvent.from_dict(event.to_dict()) == event


def test_event_ring_keeps_only_the_newest_entries():
    bank = BankState("pack", 8, 200.0)
    for n in range(CALIBRATION_EVENT_LIMIT + 5):
        bank.append_event(CalibrationEvent(
            iso=f"2026-09-06T00:00:{n:02d}+0200", unit="pack", side="full",
            coulomb_before_ah=float(n), coulomb_after_ah=200.0,
            residual_ah=200.0 - n, voltage_v=27.9, cell_count=8,
            corrected_v_per_cell=3.48,
            current_a=5.0, threshold_v_per_cell=3.48, tolerance_v_per_cell=0.02,
            hold_s=600.0, taper_met=True, charged_ah=0.0, discharged_ah=0.0))
    assert len(bank.events) == CALIBRATION_EVENT_LIMIT
    assert bank.events[-1].coulomb_before_ah == CALIBRATION_EVENT_LIMIT + 4
