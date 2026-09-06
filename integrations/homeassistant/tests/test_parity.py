"""Parity tests: adapter vs core — every entity exists, no missing keys, output keyset matches."""
import pytest
from homeassistant.helpers import entity_registry as er

from custom_components.battery_soc.battery_soc_core import entity_specs, SocState, SocInputs, tick
from custom_components.battery_soc.entity import _category
from custom_components.battery_soc.helpers import params_from_config
from tests.conftest import USER_PARALLEL, USER_SERIES, ADVANCED_DEFAULTS, _mk_config_entry


@pytest.mark.parametrize("user,adv", [(USER_PARALLEL, ADVANCED_DEFAULTS), (USER_SERIES, ADVANCED_DEFAULTS)])
async def test_every_spec_entity_exists_with_matching_attrs(hass, user, adv):
    """Test that every EntityDesc in entity_specs results in a registered entity with matching attributes.

    Note: open_suggestions_* sensors are deliberately not in entity_specs — they are constructed
    directly in BatterySocSuggestionsSensor as an Adapter-Auswertung over the calibration event ring,
    not as a tick() output.
    """
    entry = _mk_config_entry(user, adv)
    entry.add_to_hass(hass)

    # Set up source entity states
    for e in ("sensor.meanwell_power", "sensor.lumentree_power",
              "sensor.bank_voltage", "sensor.bank_b_voltage"):
        hass.states.async_set(e, "26.8" if "voltage" in e else "10")

    # Setup integration
    assert await hass.config_entries.async_setup(entry.entry_id)
    await hass.async_block_till_done()

    # Build params and get entity registry
    params = params_from_config({**entry.data, **entry.options})
    reg = er.async_get(hass)

    # Map unique_id suffix -> entity
    by_oid = {}
    for ent in reg.entities.values():
        if ent.config_entry_id == entry.entry_id:
            # Split unique_id on entry_id to extract object_id
            suffix = ent.unique_id.split(entry.entry_id + "_", 1)[1]
            by_oid[suffix] = ent

    # Verify each spec has a registered entity with matching attributes
    for d in entity_specs(params):
        assert d.object_id in by_oid, f"Missing entity: {d.object_id}"
        ent = by_oid[d.object_id]
        assert (ent.original_device_class or None) == (d.device_class or None), \
            f"device_class mismatch for {d.object_id}: expected {d.device_class}, got {ent.original_device_class}"
        assert (ent.unit_of_measurement or None) == (d.unit or None), \
            f"unit mismatch for {d.object_id}: expected {d.unit}, got {ent.unit_of_measurement}"
        assert ent.entity_category == _category(d.entity_category), \
            f"entity_category mismatch for {d.object_id}: expected {_category(d.entity_category)}, got {ent.entity_category}"
        assert (ent.disabled_by is None) == bool(d.enabled_by_default), \
            f"enabled_by_default mismatch for {d.object_id}: expected {d.enabled_by_default}, got {ent.disabled_by is None}"


@pytest.mark.parametrize("user,adv", [(USER_PARALLEL, ADVANCED_DEFAULTS), (USER_SERIES, ADVANCED_DEFAULTS)])
async def test_coordinator_data_has_no_missing_value_keys(hass, user, adv):
    """Test that coordinator.data contains all value_keys needed by entity_specs."""
    entry = _mk_config_entry(user, adv)
    entry.add_to_hass(hass)

    # Set up source entity states
    for e in ("sensor.meanwell_power", "sensor.lumentree_power",
              "sensor.bank_voltage", "sensor.bank_b_voltage"):
        hass.states.async_set(e, "26.8" if "voltage" in e else "10")

    # Setup integration
    assert await hass.config_entries.async_setup(entry.entry_id)
    await hass.async_block_till_done()

    # Get coordinator and check all needed keys are present
    coord = hass.data["battery_soc"][entry.entry_id]
    params = params_from_config({**entry.data, **entry.options})
    need = {d.value_key for d in entity_specs(params) if d.component in ("sensor", "binary_sensor")}
    assert need <= set(coord.data), \
        f"Missing keys in coordinator.data: {need - set(coord.data)}"


@pytest.mark.parametrize("user,adv", [(USER_PARALLEL, ADVANCED_DEFAULTS), (USER_SERIES, ADVANCED_DEFAULTS)])
async def test_adapter_output_keyset_equals_core_tick(hass, user, adv):
    """Test that adapter's output keyset matches core tick output — core is single source of truth."""
    entry = _mk_config_entry(user, adv)
    entry.add_to_hass(hass)

    # Set up source entity states
    for e in ("sensor.meanwell_power", "sensor.lumentree_power",
              "sensor.bank_voltage", "sensor.bank_b_voltage"):
        hass.states.async_set(e, "26.8" if "voltage" in e else "10")

    # Setup integration
    assert await hass.config_entries.async_setup(entry.entry_id)
    await hass.async_block_till_done()

    # Get coordinator and run fresh core tick
    coord = hass.data["battery_soc"][entry.entry_id]
    fresh = tick(coord.params, SocState(coord.params), SocInputs(), 1000.0).outputs

    # Verify keysets are identical (adapter adds/drops nothing) excluding _tuning which is the
    # coordinator's calibration-analysis block, deliberately not a tick() output — see BatterySocSuggestionsSensor
    assert set(coord.data) - {"_tuning"} == set(fresh), \
        f"Keyset mismatch: adapter has {set(coord.data) - {'_tuning'}}, core produces {set(fresh)}"
