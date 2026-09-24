"""Config entry v1 -> v2 and cleanup of entities the entry no longer has."""
from homeassistant.helpers import entity_registry as er
from pytest_homeassistant_custom_component.common import MockConfigEntry

from custom_components.battery_soc.const import DOMAIN, INVERT_KEYS
from tests.conftest import ADVANCED_DEFAULTS, USER_PARALLEL, USER_SERIES, W


def _states(hass):
    hass.states.async_set("sensor.meanwell_power", "0", W)
    hass.states.async_set("sensor.lumentree_power", "0", W)
    hass.states.async_set("sensor.shunt", "0", W)
    hass.states.async_set("sensor.bank_voltage", "26.8")
    hass.states.async_set("sensor.bank_b_voltage", "26.8")


def _object_ids(hass, entry):
    prefix = f"{entry.entry_id}_"
    return {e.unique_id.removeprefix(prefix)
            for e in er.async_entries_for_config_entry(er.async_get(hass), entry.entry_id)}


async def test_v1_entry_migrates_to_v2(hass):
    entry = MockConfigEntry(domain=DOMAIN, version=1, unique_id="werkstatt-akku",
                            data={**USER_PARALLEL, **ADVANCED_DEFAULTS})
    entry.add_to_hass(hass)
    _states(hass)
    assert await hass.config_entries.async_setup(entry.entry_id)
    await hass.async_block_till_done()

    assert entry.version == 2
    assert entry.data["system_type"] == "ac_coupled"
    for key in INVERT_KEYS:
        assert entry.data[key] is False
    assert entry.data["charger_power_entity"] == "sensor.meanwell_power"


async def test_ac_fallback_entity_is_removed_when_no_side_has_ac_and_dc(hass):
    data = {**USER_PARALLEL, **ADVANCED_DEFAULTS, "charger_dc_power_entity": "sensor.shunt"}
    entry = MockConfigEntry(domain=DOMAIN, version=2, unique_id="werkstatt-akku", data=data)
    entry.add_to_hass(hass)
    _states(hass)
    assert await hass.config_entries.async_setup(entry.entry_id)
    await hass.async_block_till_done()
    assert "ac_fallback" in _object_ids(hass, entry)

    hass.config_entries.async_update_entry(
        entry, data={**data, "charger_dc_power_entity": ""})
    await hass.config_entries.async_reload(entry.entry_id)
    await hass.async_block_till_done()
    assert "ac_fallback" not in _object_ids(hass, entry)
    assert "soc_combined" in _object_ids(hass, entry)


async def test_series_entities_are_removed_after_switching_to_parallel(hass):
    data = {**USER_SERIES, **ADVANCED_DEFAULTS}
    entry = MockConfigEntry(domain=DOMAIN, version=2, unique_id="werkstatt-akku", data=data)
    entry.add_to_hass(hass)
    _states(hass)
    assert await hass.config_entries.async_setup(entry.entry_id)
    await hass.async_block_till_done()
    assert {"soc_a", "open_suggestions_bank_a"} <= _object_ids(hass, entry)

    hass.config_entries.async_update_entry(entry, data={**data, "topology": "parallel"})
    await hass.config_entries.async_reload(entry.entry_id)
    await hass.async_block_till_done()
    ids = _object_ids(hass, entry)
    assert "soc_a" not in ids and "open_suggestions_bank_a" not in ids
    assert {"soc_combined", "open_suggestions_pack"} <= ids
