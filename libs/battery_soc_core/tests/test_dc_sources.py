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


# ---------------------------------------------------------------------------
# DC without AC (spec section 4)
# ---------------------------------------------------------------------------
def dc_only_inputs(charge, discharge, age=0.0, unit="W", voltage=26.8):
    return SocInputs(
        charger_dc_power_configured=True, charger_dc_power_ts=NOW - age,
        charger_dc_power_w=charge, charger_dc_power_unit=unit,
        inverter_dc_power_configured=True, inverter_dc_power_ts=NOW - age,
        inverter_dc_power_w=discharge, inverter_dc_power_unit=unit,
        bank_a_voltage_configured=True, bank_a_voltage_ts=NOW, bank_a_voltage_v=voltage,
    )


def test_dc_only_side_ignores_dc_max_age():
    """90 s is past dc_max_age_s (60) but within stale_input_s (120): still DC."""
    p = make_params(dc_max_age_s=60.0, stale_input_s=120.0)
    ep = power(p, dc_only_inputs(100.0, 30.0, age=90.0))
    assert (ep.charger_source, ep.inverter_source) == ("dc", "dc")
    assert ep.ac_fallback_active is False
    assert ep.net_power_w == pytest.approx(70.0)


def test_dc_only_side_stale_counts_as_zero_in_lenient_mode():
    p = make_params(stale_input_s=120.0, require_fresh_inputs=False)
    ep = power(p, dc_only_inputs(100.0, 30.0, age=130.0))
    assert ep.net_power_w == 0.0
    assert ep.charger_source == "dc"
    assert ep.ac_fallback_active is False


def test_dc_only_stale_is_reported_by_tick():
    p = make_params(stale_input_s=120.0)
    s = SocState(p, last_tick=NOW)
    out = tick(p, s, dc_only_inputs(100.0, 30.0, age=130.0), NOW).outputs
    assert out["inputs_stale"] is True
    assert "Ladeleistung" in out["stale_inputs"]


def test_side_with_ac_keeps_the_dc_max_age_override():
    """Mixed: charger AC+DC (fallback possible), inverter DC only."""
    p = make_params(dc_max_age_s=60.0)
    i = dc_only_inputs(100.0, 30.0, age=90.0)
    i.charger_power_configured, i.charger_power_ts, i.charger_power_w = True, NOW, 200.0
    ep = power(p, i)
    assert ep.charger_source == "ac"
    assert ep.inverter_source == "dc"
    assert ep.ac_fallback_active is True


# ---------------------------------------------------------------------------
# Issue scenario (ha-battery-soc#1): MeshCore repeater, 5S LiFePO4, 3.6 Ah,
# one signed INA219 current in both DC slots, the discharge slot inverted.
# ---------------------------------------------------------------------------
from battery_soc_core.entities import entity_specs
from battery_soc_core.sources import SourceConfig, validate_sources


ISSUE_PARAMS = dict(bank_b_enabled=False, bank_a_cell_count=5, bank_a_capacity_ah=3.6)
ISSUE_SOURCES = SourceConfig(
    configured=frozenset({"charger_dc_power", "inverter_dc_power", "bank_a_voltage"}),
    units={"charger_dc_power": "A", "inverter_dc_power": "A"},
    bank_b_enabled=False, system_type="dc_only",
)


@pytest.mark.parametrize("signed_a", [0.5, -0.3])
def test_issue_scenario_signed_current_in_both_directions(signed_a):
    p = make_params(**ISSUE_PARAMS)
    p.validate()
    s = SocState(p, last_tick=NOW)
    # The adapter writes the sensor unchanged into the charge slot and
    # inverted into the discharge slot; the clamp splits it.
    i = dc_only_inputs(signed_a, -signed_a, unit="A", voltage=16.5)
    out = tick(p, s, i, NOW).outputs
    assert out["net_power_w"] == pytest.approx(round(signed_a * 16.5, 1))
    assert out["pack_current_a"] == pytest.approx(signed_a)
    assert out["charger_power_source"] == "dc"
    assert out["inverter_power_source"] == "dc"
    assert out["ac_fallback_active"] is False


def test_issue_scenario_config_is_valid_and_has_no_ac_fallback_entity():
    p = make_params(**ISSUE_PARAMS)
    assert validate_sources(ISSUE_SOURCES) == []
    assert "ac_fallback" not in {d.object_id for d in entity_specs(p, ISSUE_SOURCES)}
