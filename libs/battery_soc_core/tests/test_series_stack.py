"""Series banks: bank A is the upper bank, the stack runs from A+ to B-.

A- is connected to B+ (the middle tap). The bank A sensor either measures
bank A alone (A+ -> A-) or the whole stack (A+ -> B-). In the second case
bank A = stack - bank B."""
import pytest

from tests.conftest import make_params
from battery_soc_core.engine import tick
from battery_soc_core.inputs import (
    SocInputs, bank_voltages, effective_ts, pack_voltage_v, stale_groups,
)
from battery_soc_core.state import SocState

NOW = 10_000.0


def series_inputs(a=None, b=None, a_ts=NOW, b_ts=NOW, **kw):
    base = dict(
        charger_power_configured=True, charger_power_ts=NOW, charger_power_w=520.0,
        inverter_power_configured=True, inverter_power_ts=NOW,
        bank_a_voltage_configured=True, bank_a_voltage_ts=a_ts, bank_a_voltage_v=a,
        bank_b_voltage_configured=True, bank_b_voltage_ts=b_ts, bank_b_voltage_v=b,
    )
    base.update(kw)
    return SocInputs(**base)


def stack_params(**kw):
    return make_params(topology="series", bank_a_voltage_measures="stack", **kw)


def test_default_measures_bank_a_alone():
    p = make_params(topology="series")
    assert p.bank_a_voltage_measures == "bank_a"
    assert bank_voltages(p, series_inputs(25.0, 27.0)) == [25.0, 27.0]
    assert pack_voltage_v(p, series_inputs(25.0, 27.0)) == pytest.approx(52.0)


def test_stack_mode_derives_bank_a_from_stack_minus_bank_b():
    p = stack_params()
    i = series_inputs(52.0, 27.0)
    assert bank_voltages(p, i) == [pytest.approx(25.0), 27.0]
    assert pack_voltage_v(p, i) == pytest.approx(52.0)


def test_stack_mode_without_bank_b_has_no_bank_a_voltage():
    p = stack_params()
    assert bank_voltages(p, series_inputs(52.0, None)) == [None, None]
    assert pack_voltage_v(p, series_inputs(52.0, None)) is None


def test_parallel_ignores_the_mode():
    p = make_params(topology="parallel", bank_a_voltage_measures="stack")
    assert bank_voltages(p, series_inputs(26.8, 13.0)) == [26.8]


def test_tick_publishes_the_derived_bank_a_voltage():
    p = stack_params(bank_a_capacity_ah=100.0, bank_b_capacity_ah=100.0)
    s = SocState(p, last_tick=NOW)
    out = tick(p, s, series_inputs(52.0, 27.0), NOW, dt_hours=0.0).outputs
    assert out["bank_a_voltage_v"] == pytest.approx(25.0)
    assert out["bank_b_voltage_v"] == pytest.approx(27.0)
    assert out["voltage_delta_v"] == pytest.approx(-2.0)
    # Both banks carry the stack current: P / (A + B) = P / stack.
    assert out["bank_a_current_a"] == pytest.approx(round(520.0 * 0.9 / 52.0, 2))


def test_bank_a_ages_with_bank_b_in_stack_mode():
    """The derived bank A voltage is only as fresh as both sensors."""
    p = stack_params(stale_input_s=120.0)
    i = series_inputs(52.0, 27.0, b_ts=NOW - 200.0)
    stale = stale_groups(p, i, NOW)
    assert stale["Spannung Bank A"] is True
    assert stale["Spannung Bank B"] is True


def test_bank_a_keeps_its_own_age_when_measured_alone():
    p = make_params(topology="series", stale_input_s=120.0)
    i = series_inputs(25.0, 27.0, b_ts=NOW - 200.0)
    assert stale_groups(p, i, NOW)["Spannung Bank A"] is False


def test_current_slot_converts_with_the_stack_voltage():
    p = stack_params()
    i = series_inputs(52.0, 27.0, charger_dc_power_configured=True,
                      charger_dc_power_ts=NOW, charger_dc_power_w=2.0,
                      charger_dc_power_unit="A")
    assert effective_ts(p, i, "charger_dc_power") == NOW


def test_unknown_mode_is_rejected():
    with pytest.raises(ValueError, match="bank_a_voltage_measures"):
        make_params(topology="series", bank_a_voltage_measures="middle").validate()
