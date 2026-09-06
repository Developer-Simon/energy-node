"""Tests for sensor platform."""
from __future__ import annotations

import pytest
from homeassistant.helpers import entity_registry as er

from custom_components.battery_soc.battery_soc_core import entity_specs
from custom_components.battery_soc.const import DOMAIN
from custom_components.battery_soc.helpers import params_from_config

from tests.conftest import USER_PARALLEL, ADVANCED_DEFAULTS, _mk_config_entry


@pytest.mark.asyncio
async def test_all_sensor_specs_become_entities_parallel(hass):
    """Test that all sensor specs from entity_specs become entities."""
    entry = _mk_config_entry(USER_PARALLEL, ADVANCED_DEFAULTS)
    entry.add_to_hass(hass)
    for e, v in [
        ("sensor.meanwell_power", "200"),
        ("sensor.lumentree_power", "50"),
        ("sensor.bank_voltage", "26.8"),
    ]:
        hass.states.async_set(e, v)
    assert await hass.config_entries.async_setup(entry.entry_id)
    await hass.async_block_till_done()

    params = params_from_config({**entry.data, **entry.options})
    want = {d.object_id for d in entity_specs(params) if d.component == "sensor"}

    reg = er.async_get(hass)
    got = {
        ent.unique_id.split(entry.entry_id + "_", 1)[1]
        for ent in reg.entities.values()
        if ent.config_entry_id == entry.entry_id and ent.domain == "sensor"
    }
    assert want <= got


@pytest.mark.asyncio
async def test_soc_sensor_reports_a_number(hass):
    """Test that SoC sensor reports a numeric value."""
    entry = _mk_config_entry(USER_PARALLEL, ADVANCED_DEFAULTS)
    entry.add_to_hass(hass)
    for e, v in [
        ("sensor.meanwell_power", "0"),
        ("sensor.lumentree_power", "0"),
        ("sensor.bank_voltage", "26.8"),
    ]:
        hass.states.async_set(e, v)
    assert await hass.config_entries.async_setup(entry.entry_id)
    await hass.async_block_till_done()

    st = next(
        s
        for s in hass.states.async_all("sensor")
        if s.entity_id.endswith("_soc") or s.attributes.get("friendly_name", "").endswith("SoC")
    )
    assert st.state not in ("unknown", "unavailable")


@pytest.mark.asyncio
async def test_suggestions_sensor_exists_per_unit_with_attributes(hass):
    """Test that BatterySocSuggestionsSensor exists per unit with suggestions/findings attributes."""
    from homeassistant.helpers import entity_registry as er

    entry = _mk_config_entry(USER_PARALLEL, ADVANCED_DEFAULTS)
    entry.add_to_hass(hass)
    for e, v in [
        ("sensor.meanwell_power", "0"),
        ("sensor.lumentree_power", "0"),
        ("sensor.bank_voltage", "26.8"),
    ]:
        hass.states.async_set(e, v)
    assert await hass.config_entries.async_setup(entry.entry_id)
    await hass.async_block_till_done()

    coord = hass.data["battery_soc"][entry.entry_id]
    reg = er.async_get(hass)
    for unit in coord.state.units:
        # Find the suggestions sensor for this unit using entity registry
        found = False
        for ent in reg.entities.values():
            if (ent.config_entry_id == entry.entry_id and
                ent.domain == "sensor" and
                f"open_suggestions_{unit.name}" in ent.unique_id):
                found = True
                state = hass.states.get(ent.entity_id)
                assert state is not None, f"Suggestions sensor {ent.entity_id} not in states"
                assert "suggestions" in state.attributes
                assert "findings" in state.attributes
                break
        assert found, f"No suggestions sensor found for unit {unit.name}"
