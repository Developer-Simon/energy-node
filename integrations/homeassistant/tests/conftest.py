import sys
from pathlib import Path

import pytest
from homeassistant.util import slugify
from pytest_homeassistant_custom_component.common import MockConfigEntry

from custom_components.battery_soc.const import DOMAIN

pytest_plugins = ["pytest_homeassistant_custom_component"]


def pytest_configure(config):
    """Add custom_components to sys.path before any tests run."""
    sys.path.insert(0, str(Path(__file__).resolve().parents[1]))


@pytest.fixture(autouse=True)
def auto_enable_custom_integrations(enable_custom_integrations):
    yield


# Test data — used in test_config_flow and test_coordinator
USER_PARALLEL = {
    "name": "Werkstatt Akku",
    "topology": "parallel",
    "charger_power_entity": "sensor.meanwell_power",
    "inverter_power_entity": "sensor.lumentree_power",
    "bank_a_voltage_entity": "sensor.bank_voltage",
    "bank_a_voltage_scale": 1.0,
    "bank_a_capacity_ah": 100,
    "bank_b_capacity_ah": 100,
    "bank_a_cell_count": 8,
    "bank_b_cell_count": 8,
    "bank_b_enabled": True,
    "battery_chemistry": "lifepo4",
    "soc_curve": "dyness_ar2.5",
}

ADVANCED_DEFAULTS = {
    "empty_v_per_cell": 2.7, "full_v_per_cell": 3.5,
    "charger_ac_dc_efficiency": 0.9, "inverter_dc_ac_efficiency": 0.9,
    "charge_efficiency": 0.98, "calibration_tolerance_v_per_cell": 0.08,
    "calibration_hold_s": 120, "voltage_soc_mismatch_warn_pct": 25,
    "voltage_mismatch_hold_s": 300, "imbalance_warn_v": 0.5,
    "stale_input_s": 120, "dc_max_age_s": 60, "require_fresh_inputs": False,
    "fallback_interval_s": 30,
}

USER_SERIES = dict(USER_PARALLEL, topology="series", bank_b_voltage_entity="sensor.bank_b_voltage")


def _mk_entry(hass, overrides=None):
    """Create and add a MockConfigEntry with merged data+options, return it.

    This is a sync helper that can be used in async tests directly (not awaited).
    Optionally merge overrides into the entry data dict.
    """
    data = {**USER_PARALLEL, **ADVANCED_DEFAULTS}
    if overrides:
        data.update(overrides)
    entry = MockConfigEntry(
        domain=DOMAIN,
        data=data,
        unique_id="werkstatt-akku"
    )
    entry.add_to_hass(hass)
    return entry


def _mk_config_entry(user_data=None, advanced_data=None, options=None):
    """Create a MockConfigEntry without adding to hass.

    Returns an unattached entry; the test calls .add_to_hass(hass) itself.
    """
    if user_data is None:
        user_data = USER_PARALLEL
    if advanced_data is None:
        advanced_data = ADVANCED_DEFAULTS
    if options is None:
        options = {}

    return MockConfigEntry(
        domain=DOMAIN,
        data={**user_data, **advanced_data},
        options=options,
        unique_id=slugify(user_data["name"]),
    )
