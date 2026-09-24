"""Tests for the battery_soc coordinator."""
import time
from datetime import timedelta

import pytest
from homeassistant.util import dt as dt_util
from pytest_homeassistant_custom_component.common import async_fire_time_changed

from custom_components.battery_soc.coordinator import BatterySocCoordinator
from custom_components.battery_soc.const import DOMAIN
from tests.conftest import _mk_entry, W


async def test_recalc_populates_data_from_tick(hass):
    """Test that _recalc runs tick and populates coordinator.data."""
    entry = _mk_entry(hass)
    coord = BatterySocCoordinator(hass, entry)

    # Set up entity states
    hass.states.async_set("sensor.meanwell_power", "300", W)
    hass.states.async_set("sensor.lumentree_power", "40", W)
    hass.states.async_set("sensor.bank_voltage", "26.8")

    await coord.async_load()

    try:
        # Verify tick was run and populated coordinator.data
        assert coord.data is not None
        assert "soc_combined_pct" in coord.data
        # Expected: 300 * 0.9 - 40 / 0.9 = 270 - 44.44... = 225.56
        assert coord.data["net_power_w"] == pytest.approx(300 * 0.9 - 40 / 0.9, rel=1e-3)
    finally:
        await coord.async_shutdown()


async def test_state_restored_from_store(hass):
    """Test that coordinator loads and restores saved state from Store."""
    entry = _mk_entry(hass)

    # Pre-seed the store
    from homeassistant.helpers.storage import Store
    await Store(hass, 1, f"{DOMAIN}.{entry.entry_id}").async_save(
        {"units": {"pack": {"coulomb_ah": 180.0, "last_calibration_iso": None}}}
    )

    coord = BatterySocCoordinator(hass, entry)
    await coord.async_load()

    try:
        # Verify state was restored
        assert coord.state.units[0].coulomb_ah == 180.0
    finally:
        await coord.async_shutdown()


async def test_state_change_triggers_recalc(hass):
    """Test that source entity state changes trigger recalculation."""
    entry = _mk_entry(hass)
    coord = BatterySocCoordinator(hass, entry)
    hass.states.async_set("sensor.meanwell_power", "0", W)
    hass.states.async_set("sensor.lumentree_power", "0", W)
    hass.states.async_set("sensor.bank_voltage", "26.8")
    await coord.async_load()
    coord.async_start_listeners()
    before = coord.data["net_power_w"]
    hass.states.async_set("sensor.meanwell_power", "500", W)
    await hass.async_block_till_done()
    assert coord.data["net_power_w"] != before
    assert coord.data["net_power_w"] == pytest.approx(500 * 0.9, rel=1e-3)
    await coord.async_shutdown()


async def test_unavailable_source_does_not_crash_and_keeps_last_value(hass):
    """Test that unavailable source doesn't crash and keeps last value."""
    entry = _mk_entry(hass)
    coord = BatterySocCoordinator(hass, entry)
    hass.states.async_set("sensor.bank_voltage", "26.8")
    hass.states.async_set("sensor.meanwell_power", "100", W)
    hass.states.async_set("sensor.lumentree_power", "0", W)
    await coord.async_load()
    coord.async_start_listeners()
    hass.states.async_set("sensor.bank_voltage", "unavailable")
    await hass.async_block_till_done()
    # With stale_input_s not yet exceeded, the last voltage is retained
    assert coord.data["pack_voltage_v"] == pytest.approx(26.8, rel=1e-3)
    await coord.async_shutdown()


async def test_fallback_timer_advances_coulomb_counter(hass):
    """Test that fallback timer advances coulomb integration."""
    entry = _mk_entry(hass, overrides={"fallback_interval_s": 5})
    coord = BatterySocCoordinator(hass, entry)
    hass.states.async_set("sensor.meanwell_power", "1000", W)   # strong charge
    hass.states.async_set("sensor.lumentree_power", "0", W)
    hass.states.async_set("sensor.bank_voltage", "26.8")
    await coord.async_load()
    coord.async_start_listeners()
    start = coord.state.units[0].coulomb_ah
    async_fire_time_changed(hass, dt_util.utcnow() + timedelta(seconds=6))
    await hass.async_block_till_done()
    assert coord.state.units[0].coulomb_ah > start
    await coord.async_shutdown()


async def test_state_persistence_roundtrip(hass):
    """Test that charged_ah, discharged_ah, and events survive Store roundtrip."""
    from custom_components.battery_soc.battery_soc_core.state import CalibrationEvent

    entry = _mk_entry(hass)
    coord = BatterySocCoordinator(hass, entry)

    hass.states.async_set("sensor.meanwell_power", "100", W)
    hass.states.async_set("sensor.lumentree_power", "0", W)
    hass.states.async_set("sensor.bank_voltage", "26.8")

    await coord.async_load()

    # Seed some state
    original_charged = 50.5
    original_discharged = 25.3
    original_event = CalibrationEvent(
        iso="2026-09-06T12:00:00Z",
        unit="pack",
        side="full",
        coulomb_before_ah=95.0,
        coulomb_after_ah=100.0,
        residual_ah=5.0,
        voltage_v=28.0,
        cell_count=8,
        corrected_v_per_cell=3.5,
        current_a=1.0,
        threshold_v_per_cell=3.5,
        tolerance_v_per_cell=0.08,
        hold_s=120.0,
        taper_met=True,
        charged_ah=50.5,
        discharged_ah=25.3,
    )

    coord.state.units[0].charged_ah = original_charged
    coord.state.units[0].discharged_ah = original_discharged
    coord.state.units[0].events = [original_event]

    # Save state via Store (same path as coordinator._save)
    await coord._store.async_save(coord.state.to_dict())

    # Create a new coordinator and load
    coord2 = BatterySocCoordinator(hass, entry)
    await coord2.async_load()

    try:
        # Verify state was restored (use approximate comparison due to coulomb recalc)
        assert coord2.state.units[0].charged_ah == pytest.approx(original_charged, rel=1e-3)
        assert coord2.state.units[0].discharged_ah == pytest.approx(original_discharged, rel=1e-3)
        assert len(coord2.state.units[0].events) == 1
        assert coord2.state.units[0].events[0].iso == original_event.iso
        assert coord2.state.units[0].events[0].side == original_event.side
    finally:
        await coord.async_shutdown()
        await coord2.async_shutdown()


async def test_recalc_populates_tuning_per_unit(hass):
    """Test that _recalc populates coordinator.data["_tuning"] with calibration analysis."""
    from tests.conftest import _mk_config_entry, USER_PARALLEL, ADVANCED_DEFAULTS

    entry = _mk_config_entry(USER_PARALLEL, ADVANCED_DEFAULTS)
    entry.add_to_hass(hass)

    # Set up source entity states
    hass.states.async_set("sensor.meanwell_power", "200", W)
    hass.states.async_set("sensor.lumentree_power", "50", W)
    hass.states.async_set("sensor.bank_voltage", "26.8")

    assert await hass.config_entries.async_setup(entry.entry_id)
    await hass.async_block_till_done()

    coord = hass.data["battery_soc"][entry.entry_id]
    assert "_tuning" in coord.data
    assert set(coord.data["_tuning"]) == {u.name for u in coord.state.units}
    for block in coord.data["_tuning"].values():
        assert "suggestions" in block and "findings" in block


V = {"unit_of_measurement": "V"}
AMPS = {"unit_of_measurement": "A"}

# Issue scenario: one signed INA219 current in both DC slots, discharge inverted.
DC_ONLY = {
    "system_type": "dc_only",
    "charger_power_entity": "", "inverter_power_entity": "",
    "charger_dc_power_entity": "sensor.ina219_current",
    "inverter_dc_power_entity": "sensor.ina219_current",
    "inverter_dc_power_invert": True,
    "bank_b_enabled": False, "bank_a_cell_count": 5, "bank_a_capacity_ah": 3.6,
}


async def test_one_signed_current_sensor_feeds_both_dc_slots(hass):
    entry = _mk_entry(hass, DC_ONLY)
    coord = BatterySocCoordinator(hass, entry)
    hass.states.async_set("sensor.bank_voltage", "16.5", V)
    # Negative before load: the prime path must apply invert and unit too.
    hass.states.async_set("sensor.ina219_current", "-0.3", AMPS)
    await coord.async_load()
    coord.async_start_listeners()
    try:
        assert coord.data["pack_current_a"] == pytest.approx(-0.3)
        assert coord.data["net_power_w"] == pytest.approx(-0.3 * 16.5, abs=0.1)
        assert coord.data["charger_power_source"] == "dc"
        assert coord.data["ac_fallback_active"] is False

        hass.states.async_set("sensor.ina219_current", "0.5", AMPS)
        await hass.async_block_till_done()
        assert coord.data["pack_current_a"] == pytest.approx(0.5)
        assert coord.data["net_power_w"] == pytest.approx(0.5 * 16.5, abs=0.1)
    finally:
        await coord.async_shutdown()


async def test_kilowatt_sensor_is_read_in_watts(hass):
    entry = _mk_entry(hass)
    coord = BatterySocCoordinator(hass, entry)
    hass.states.async_set("sensor.meanwell_power", "0.5", {"unit_of_measurement": "kW"})
    hass.states.async_set("sensor.lumentree_power", "0", W)
    hass.states.async_set("sensor.bank_voltage", "26.8", V)
    await coord.async_load()
    try:
        assert coord.data["net_power_w"] == pytest.approx(500 * 0.9, rel=1e-3)
    finally:
        await coord.async_shutdown()


async def test_milliamp_sensor_on_a_dc_slot(hass):
    entry = _mk_entry(hass, {"charger_dc_power_entity": "sensor.shunt"})
    coord = BatterySocCoordinator(hass, entry)
    hass.states.async_set("sensor.meanwell_power", "500", W)
    hass.states.async_set("sensor.lumentree_power", "0", W)
    hass.states.async_set("sensor.bank_voltage", "26.8", V)
    hass.states.async_set("sensor.shunt", "2000", {"unit_of_measurement": "mA"})
    await coord.async_load()
    try:
        assert coord.data["charger_power_source"] == "dc"
        assert coord.data["net_power_w"] == pytest.approx(2.0 * 26.8, abs=0.1)
    finally:
        await coord.async_shutdown()


@pytest.mark.parametrize("attrs,reason", [
    ({"unit_of_measurement": "Wh"}, "unsupported unit 'Wh'"),
    ({}, "no unit_of_measurement"),
    ({"unit_of_measurement": "A"}, "only accepted on DC slots"),
])
async def test_unusable_unit_discards_the_reading_and_warns_once(hass, caplog, attrs, reason):
    entry = _mk_entry(hass)
    coord = BatterySocCoordinator(hass, entry)
    hass.states.async_set("sensor.meanwell_power", "300", attrs)
    hass.states.async_set("sensor.lumentree_power", "0", W)
    hass.states.async_set("sensor.bank_voltage", "26.8", V)
    await coord.async_load()
    coord.async_start_listeners()
    try:
        assert coord.data["net_power_w"] == 0.0
        hass.states.async_set("sensor.meanwell_power", "310", attrs)
        await hass.async_block_till_done()
        assert coord.data["net_power_w"] == 0.0
        warnings = [r.getMessage() for r in caplog.records
                    if r.levelname == "WARNING" and "sensor.meanwell_power" in r.getMessage()]
        assert len(warnings) == 1
        assert reason in warnings[0]
        assert "charger_power" in warnings[0]
    finally:
        await coord.async_shutdown()


async def test_coordinator_exposes_its_sources(hass):
    entry = _mk_entry(hass, DC_ONLY)
    coord = BatterySocCoordinator(hass, entry)
    assert coord.sources.configured == {"charger_dc_power", "inverter_dc_power",
                                        "bank_a_voltage"}
    assert coord.sources.system_type == "dc_only"
    assert coord.sources.fallback_possible() is False
