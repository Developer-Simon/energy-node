"""Tests for number platform."""
from __future__ import annotations

import pytest

from custom_components.battery_soc.const import DOMAIN

from tests.conftest import USER_PARALLEL, ADVANCED_DEFAULTS, _mk_config_entry


@pytest.mark.asyncio
async def test_setting_the_number_anchors_the_counter(hass):
    """Test that setting the manual SoC number entity anchors the internal counter."""
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
    number_eid = next(s.entity_id for s in hass.states.async_all("number"))
    await hass.services.async_call(
        "number",
        "set_value",
        {"entity_id": number_eid, "value": 90},
        blocking=True,
    )
    await hass.async_block_till_done()
    coord = hass.data["battery_soc"][entry.entry_id]
    assert coord.state.units[0].soc_pct == 90.0
    assert coord.data["soc_combined_pct"] == 90.0
