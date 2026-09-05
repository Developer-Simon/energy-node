"""Tests for the battery_soc service actions."""
from __future__ import annotations

import pytest
from homeassistant.exceptions import ServiceValidationError

from custom_components.battery_soc.const import DOMAIN
from tests.conftest import USER_PARALLEL, USER_SERIES, ADVANCED_DEFAULTS, _mk_config_entry


async def test_service_sets_soc_parallel(hass):
    """Test set_state_of_charge service on parallel topology."""
    # Create and setup entry
    entry = _mk_config_entry(user_data=USER_PARALLEL, advanced_data=ADVANCED_DEFAULTS)
    entry.add_to_hass(hass)

    # Set up source entity states
    hass.states.async_set("sensor.meanwell_power", "300")
    hass.states.async_set("sensor.lumentree_power", "40")
    hass.states.async_set("sensor.bank_voltage", "26.8")

    # Setup the integration
    assert await hass.config_entries.async_setup(entry.entry_id)
    await hass.async_block_till_done()

    # Resolve a real target entity id (use any battery_soc SOC sensor)
    target = next(
        (s.entity_id for s in hass.states.async_all("sensor")
         if "_soc" in s.entity_id),
        None
    )
    assert target is not None, "Could not find target sensor entity"

    # Call the service
    await hass.services.async_call(
        DOMAIN,
        "set_state_of_charge",
        {"entity_id": target, "state_of_charge": 25},
        blocking=True
    )
    await hass.async_block_till_done()

    # Verify the state was updated
    coord = hass.data[DOMAIN][entry.entry_id]
    assert coord.state.units[0].soc_pct == pytest.approx(25.0, rel=1e-3)

    # Cleanup
    assert await hass.config_entries.async_unload(entry.entry_id)


async def test_service_series_requires_bank(hass):
    """Test set_state_of_charge service requires bank for series topology."""
    # Create and setup entry
    entry = _mk_config_entry(user_data=USER_SERIES, advanced_data=ADVANCED_DEFAULTS)
    entry.add_to_hass(hass)

    # Set up source entity states for series
    hass.states.async_set("sensor.meanwell_power", "300")
    hass.states.async_set("sensor.lumentree_power", "40")
    hass.states.async_set("sensor.bank_voltage", "26.8")
    hass.states.async_set("sensor.bank_b_voltage", "26.0")

    # Setup the integration
    assert await hass.config_entries.async_setup(entry.entry_id)
    await hass.async_block_till_done()

    # Resolve a real target entity id (use any battery_soc SOC sensor)
    target = next(
        (s.entity_id for s in hass.states.async_all("sensor")
         if "_soc" in s.entity_id),
        None
    )
    assert target is not None, "Could not find target sensor entity"

    # Call without bank should raise ServiceValidationError
    with pytest.raises(ServiceValidationError):
        await hass.services.async_call(
            DOMAIN,
            "set_state_of_charge",
            {"entity_id": target, "state_of_charge": 40},
            blocking=True
        )
    await hass.async_block_till_done()

    # Cleanup
    assert await hass.config_entries.async_unload(entry.entry_id)


async def test_service_series_targets_bank_b(hass):
    """Test set_state_of_charge service targets correct bank in series topology."""
    # Create and setup entry
    entry = _mk_config_entry(user_data=USER_SERIES, advanced_data=ADVANCED_DEFAULTS)
    entry.add_to_hass(hass)

    # Set up source entity states for series
    hass.states.async_set("sensor.meanwell_power", "300")
    hass.states.async_set("sensor.lumentree_power", "40")
    hass.states.async_set("sensor.bank_voltage", "26.8")
    hass.states.async_set("sensor.bank_b_voltage", "26.0")

    # Setup the integration
    assert await hass.config_entries.async_setup(entry.entry_id)
    await hass.async_block_till_done()

    # Resolve a real target entity id (use any battery_soc SOC sensor)
    target = next(
        (s.entity_id for s in hass.states.async_all("sensor")
         if "_soc" in s.entity_id),
        None
    )
    assert target is not None, "Could not find target sensor entity"

    # Call the service with bank="b"
    await hass.services.async_call(
        DOMAIN,
        "set_state_of_charge",
        {"entity_id": target, "state_of_charge": 30, "bank": "b"},
        blocking=True
    )
    await hass.async_block_till_done()

    # Verify the correct unit was updated (bank_b is units[1])
    coord = hass.data[DOMAIN][entry.entry_id]
    assert coord.state.units[1].soc_pct == pytest.approx(30.0, rel=1e-3)

    # Cleanup
    assert await hass.config_entries.async_unload(entry.entry_id)
