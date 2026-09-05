"""Tests for binary_sensor platform."""
from __future__ import annotations

import pytest

from custom_components.battery_soc.const import DOMAIN

from tests.conftest import USER_PARALLEL, ADVANCED_DEFAULTS, _mk_config_entry


@pytest.mark.asyncio
async def test_inputs_stale_when_no_source_states(hass):
    """Test that inputs_stale binary sensor is on when no source entities are set."""
    entry = _mk_config_entry(USER_PARALLEL, ADVANCED_DEFAULTS)
    entry.add_to_hass(hass)
    # Don't set any source sensor states — inputs will be stale
    assert await hass.config_entries.async_setup(entry.entry_id)
    await hass.async_block_till_done()

    # Find the inputs_stale binary sensor
    states = hass.states.async_all("binary_sensor")
    inputs_stale = next(
        s for s in states
        if s.entity_id.endswith("_inputs_stale") or "veraltet" in s.attributes.get("friendly_name", "").lower()
    )
    assert inputs_stale.state == "on"
