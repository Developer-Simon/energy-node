"""Tests for the battery_soc integration setup/unload/reload."""
from custom_components.battery_soc.const import DOMAIN
from tests.conftest import USER_PARALLEL, ADVANCED_DEFAULTS, _mk_config_entry


async def test_setup_and_unload(hass):
    """Test setup entry and unload entry."""
    entry = _mk_config_entry(USER_PARALLEL, ADVANCED_DEFAULTS)
    entry.add_to_hass(hass)

    # Set the required sensor states
    hass.states.async_set("sensor.meanwell_power", "100")
    hass.states.async_set("sensor.lumentree_power", "0")
    hass.states.async_set("sensor.bank_voltage", "26.8")

    # Setup entry
    assert await hass.config_entries.async_setup(entry.entry_id)
    await hass.async_block_till_done()

    # Verify coordinator was created and stored
    assert DOMAIN in hass.data
    assert entry.entry_id in hass.data[DOMAIN]

    # Unload entry
    assert await hass.config_entries.async_unload(entry.entry_id)
    await hass.async_block_till_done()

    # Verify coordinator was removed
    assert entry.entry_id not in hass.data[DOMAIN]

    # Clean up the config entry
    assert entry.entry_id not in hass.config_entries.async_entries(DOMAIN)


async def test_options_update_triggers_reload(hass):
    """Test that updating options triggers a reload with new coordinator."""
    entry = _mk_config_entry(USER_PARALLEL, ADVANCED_DEFAULTS)
    entry.add_to_hass(hass)

    # Set the required sensor states
    hass.states.async_set("sensor.meanwell_power", "0")
    hass.states.async_set("sensor.lumentree_power", "0")
    hass.states.async_set("sensor.bank_voltage", "26.8")

    # Setup entry
    assert await hass.config_entries.async_setup(entry.entry_id)
    await hass.async_block_till_done()

    # Update options with modified calibration_hold_s
    hass.config_entries.async_update_entry(
        entry, options=dict(ADVANCED_DEFAULTS, calibration_hold_s=999)
    )
    await hass.async_block_till_done()

    # Verify coordinator was reloaded with new params
    coord = hass.data[DOMAIN][entry.entry_id]
    assert coord.params.calibration_hold_s == 999

    # Clean up
    await hass.config_entries.async_unload(entry.entry_id)
    await hass.async_block_till_done()
