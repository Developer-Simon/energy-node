"""Tests for battery_soc_core inputs, freshness, and availability."""
import time
from tests.conftest import make_params
from battery_soc_core.inputs import (
    SocInputs, sample_is_fresh, input_groups, stale_groups, availability,
    AvailabilityResult,
)


def _fresh(**kw):
    """Helper to create a SocInputs with some configured and fresh values."""
    now = time.time()
    i = SocInputs()
    i.charger_power_configured = i.inverter_power_configured = i.bank_a_voltage_configured = True
    i.charger_power_ts = i.inverter_power_ts = i.bank_a_voltage_ts = now
    for k, v in kw.items():
        setattr(i, k, v)
    return i


def test_unconfigured_inputs_are_not_stale():
    """Unconfigured inputs don't appear in stale_groups, so empty dict means no problems."""
    p = make_params()
    i = SocInputs()  # nothing configured
    assert stale_groups(p, i, time.time()) == {}


def test_stale_input_names_ignores_bank_b_when_disabled():
    """Bank B is not in groups when disabled."""
    p = make_params(topology="parallel", bank_b_enabled=False)
    i = SocInputs()
    i.bank_b_voltage_configured = True
    i.bank_b_voltage_ts = 0.0
    groups = input_groups(p, i)
    names = [name for name, _entries in groups]
    assert "Spannung Bank B" not in names


def test_ac_fallback_does_not_mark_the_input_stale():
    """If DC is stale but AC is fresh, the group is not stale (fallback logic)."""
    p = make_params()
    now = time.time()
    i = SocInputs()
    i.charger_dc_power_configured = True
    i.charger_dc_power_ts = 0.0   # DC stale
    i.charger_power_configured = True
    i.charger_power_ts = now  # AC fresh

    result = stale_groups(p, i, now)
    assert result.get("Ladeleistung") is False  # not stale because AC is fresh


def test_input_group_is_stale_only_when_every_source_is_stale():
    """Group is stale only when ALL its sources are stale."""
    p = make_params()
    now = time.time()
    i = _fresh()
    i.charger_dc_power_configured = True
    i.charger_dc_power_ts = now  # DC fresh
    i.charger_power_ts = now - 9999  # AC stale

    result = stale_groups(p, i, now)
    assert result.get("Ladeleistung") is False


def test_input_groups_list_dc_before_ac_and_drop_empty_topics():
    """DC before AC, and unconfigured sources are filtered out."""
    p = make_params()
    i = SocInputs()
    i.charger_dc_power_configured = True
    i.charger_dc_power_ts = 100.0
    i.charger_power_configured = True
    i.charger_power_ts = 200.0

    groups = dict(input_groups(p, i))
    entries = groups.get("Ladeleistung", [])

    # Should have DC first, then AC
    assert len(entries) == 2
    assert entries[0] == (True, 100.0)  # DC
    assert entries[1] == (True, 200.0)  # AC


def test_parallel_does_not_report_bank_b_as_unconfigured():
    """In parallel mode, Bank B group should not appear if not configured."""
    p = make_params(topology="parallel", bank_b_enabled=False)
    i = SocInputs()
    groups = input_groups(p, i)
    names = [name for name, _entries in groups]

    assert "Spannung Bank B" not in names
    assert "Busspannung" in names


# Worked examples from the brief
def test_unconfigured_inputs_are_not_stale_worked():
    """Worked example: completely unconfigured system has no stale groups."""
    p = make_params()
    i = SocInputs()
    assert stale_groups(p, i, time.time()) == {}


def test_group_is_stale_only_when_every_source_is_stale_worked():
    """Worked example: group with at least one fresh source is not stale."""
    p = make_params()
    now = time.time()
    i = _fresh()
    i.charger_dc_power_configured = True
    i.charger_dc_power_ts = now  # DC fresh
    i.charger_power_ts = now - 9999  # AC stale
    assert stale_groups(p, i, now).get("Ladeleistung") is False


def test_availability_true_when_any_group_has_a_fresh_source_worked():
    """Worked example: availability is true when at least one source is fresh."""
    p = make_params()
    res = availability(p, _fresh(), time.time())
    assert res.available is True
    assert res.any_configured is True


def test_sample_is_fresh():
    """sample_is_fresh checks configured flag and age."""
    now = 1000.0

    # Unconfigured is never fresh
    assert sample_is_fresh(False, 950.0, now, 60.0) is False

    # Configured and within age window
    assert sample_is_fresh(True, 950.0, now, 60.0) is True

    # Configured but too old
    assert sample_is_fresh(True, 900.0, now, 60.0) is False

    # Boundary: exactly at the edge
    assert sample_is_fresh(True, 940.0, now, 60.0) is True


def test_availability_missing_includes_unconfigured_groups():
    """Groups with no entries appear as missing."""
    p = make_params()
    i = SocInputs()  # nothing configured
    res = availability(p, i, time.time())

    assert res.available is False
    assert "Ladeleistung" in res.missing
    assert res.any_configured is False


def test_availability_missing_includes_stale_groups():
    """Groups with all stale sources appear as missing."""
    p = make_params()
    now = time.time()
    i = SocInputs()
    i.charger_power_configured = True
    i.charger_power_ts = now - 200.0  # stale (default stale_input_s is 120s)

    res = availability(p, i, now)
    assert res.available is False
    assert "Ladeleistung" in res.missing


def test_series_topology_includes_both_banks():
    """Series topology includes both bank voltage groups."""
    p = make_params(topology="series", bank_b_enabled=True)
    i = SocInputs()
    i.bank_a_voltage_configured = True
    i.bank_b_voltage_configured = True

    groups = input_groups(p, i)
    names = [name for name, _entries in groups]

    assert "Spannung Bank A" in names
    assert "Spannung Bank B" in names
    assert "Busspannung" not in names  # parallel name, not series
