"""DC slots with current (A), DC-only sides and the issue scenario.

Spec sections 2 (A -> W), 4 (DC without AC) and 10 (acceptance test)."""
import pytest

from tests.conftest import make_params
from battery_soc_core.engine import effective_power, tick
from battery_soc_core.inputs import SocInputs, input_groups, stale_groups
from battery_soc_core.state import SocState

NOW = 10_000.0


def inputs(**kw):
    """Fresh AC on both sides plus a fresh 26.8 V bus; kw overrides."""
    base = dict(
        charger_power_configured=True, charger_power_ts=NOW, charger_power_w=500.0,
        inverter_power_configured=True, inverter_power_ts=NOW, inverter_power_w=0.0,
        bank_a_voltage_configured=True, bank_a_voltage_ts=NOW, bank_a_voltage_v=26.8,
    )
    base.update(kw)
    return SocInputs(**base)


def power(params, i, now=NOW):
    return effective_power(params, i, now, stale_groups(params, i, now))


def dc_amps(**kw):
    base = dict(charger_dc_power_configured=True, charger_dc_power_ts=NOW,
                charger_dc_power_w=2.0, charger_dc_power_unit="A")
    base.update(kw)
    return base


# ---------------------------------------------------------------------------
# A -> W
# ---------------------------------------------------------------------------
def test_unit_defaults_to_watts():
    i = SocInputs()
    assert i.charger_dc_power_unit == "W"
    assert i.inverter_dc_power_unit == "W"


def test_current_slot_is_converted_with_the_bus_voltage():
    ep = power(make_params(), inputs(**dc_amps()))
    assert ep.charger_source == "dc"
    assert ep.net_power_w == pytest.approx(2.0 * 26.8)


def test_current_slot_in_series_uses_the_sum_of_both_banks():
    p = make_params(topology="series")
    i = inputs(bank_b_voltage_configured=True, bank_b_voltage_ts=NOW,
               bank_b_voltage_v=27.0, bank_a_voltage_v=25.0, **dc_amps())
    assert power(p, i).net_power_w == pytest.approx(2.0 * 52.0)


def test_current_slot_goes_stale_with_its_voltage():
    """Older than dc_max_age_s via the voltage -> AC takes over again."""
    p = make_params(dc_max_age_s=60.0)
    i = inputs(bank_a_voltage_ts=NOW - 61.0, **dc_amps())
    ep = power(p, i)
    assert ep.charger_source == "ac"
    assert ep.ac_fallback_active is True
    assert ep.net_power_w == pytest.approx(500.0 * 0.9)


def test_current_slot_without_any_voltage_is_stale():
    ep = power(make_params(), inputs(bank_a_voltage_v=None, **dc_amps()))
    assert ep.charger_source == "ac"


def test_series_current_slot_without_bank_b_voltage_is_stale():
    """Review focus 5: never convert with half the pack voltage."""
    p = make_params(topology="series")
    i = inputs(bank_b_voltage_configured=True, bank_b_voltage_ts=NOW,
               bank_b_voltage_v=None, **dc_amps())
    assert power(p, i).charger_source == "ac"


def test_group_of_a_current_slot_ages_with_the_voltage():
    """input_groups sees the effective timestamp, so stale_groups agrees."""
    p = make_params(stale_input_s=120.0)
    i = inputs(charger_power_configured=False,
               bank_a_voltage_ts=NOW - 200.0, **dc_amps())
    groups = dict(input_groups(p, i))
    assert groups["Ladeleistung"] == [(True, NOW - 200.0)]
    assert stale_groups(p, i, NOW)["Ladeleistung"] is True


def test_watt_slot_ignores_the_voltage():
    i = inputs(charger_dc_power_configured=True, charger_dc_power_ts=NOW,
               charger_dc_power_w=80.0, bank_a_voltage_v=None)
    ep = power(make_params(), i)
    assert ep.charger_source == "dc"
    assert ep.net_power_w == pytest.approx(80.0)
