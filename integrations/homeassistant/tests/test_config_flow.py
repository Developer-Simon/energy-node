from homeassistant import config_entries
from homeassistant.data_entry_flow import FlowResultType

from custom_components.battery_soc.const import DOMAIN
from tests.conftest import USER_PARALLEL, USER_SERIES, ADVANCED_DEFAULTS


async def _create_entry(hass, user_data=USER_PARALLEL, advanced_data=ADVANCED_DEFAULTS):
    """Helper that runs the full config flow and returns the entry."""
    result = await hass.config_entries.flow.async_init(
        DOMAIN, context={"source": config_entries.SOURCE_USER})
    result = await hass.config_entries.flow.async_configure(result["flow_id"], user_data)
    result = await hass.config_entries.flow.async_configure(result["flow_id"], advanced_data)
    assert result["type"] == FlowResultType.CREATE_ENTRY
    return hass.config_entries.async_entries(DOMAIN)[0]


async def test_user_step_advances_to_advanced(hass):
    result = await hass.config_entries.flow.async_init(
        DOMAIN, context={"source": config_entries.SOURCE_USER})
    assert result["type"] == FlowResultType.FORM
    assert result["step_id"] == "user"

    result = await hass.config_entries.flow.async_configure(
        result["flow_id"], USER_PARALLEL)
    assert result["type"] == FlowResultType.FORM
    assert result["step_id"] == "advanced"


async def test_user_step_rejects_impossible_efficiency(hass):
    result = await hass.config_entries.flow.async_init(
        DOMAIN, context={"source": config_entries.SOURCE_USER})
    bad = dict(USER_PARALLEL, bank_a_cell_count=8, bank_b_cell_count=16)
    result = await hass.config_entries.flow.async_configure(result["flow_id"], bad)
    assert result["type"] == FlowResultType.FORM
    assert result["errors"]


async def test_full_flow_creates_entry(hass):
    result = await hass.config_entries.flow.async_init(
        DOMAIN, context={"source": config_entries.SOURCE_USER})
    result = await hass.config_entries.flow.async_configure(result["flow_id"], USER_PARALLEL)
    result = await hass.config_entries.flow.async_configure(result["flow_id"], ADVANCED_DEFAULTS)
    assert result["type"] == FlowResultType.CREATE_ENTRY
    assert result["title"] == "Werkstatt Akku"
    assert result["data"]["soc_curve"] == "dyness_ar2.5"
    assert result["data"]["charger_power_entity"] == "sensor.meanwell_power"


async def test_advanced_step_rejects_bad_efficiency(hass):
    result = await hass.config_entries.flow.async_init(
        DOMAIN, context={"source": config_entries.SOURCE_USER})
    result = await hass.config_entries.flow.async_configure(result["flow_id"], USER_PARALLEL)
    result = await hass.config_entries.flow.async_configure(
        result["flow_id"], dict(ADVANCED_DEFAULTS, charge_efficiency=1.4))
    assert result["type"] == FlowResultType.FORM
    assert result["errors"]["base"]


async def test_series_flow_creates_entry(hass):
    result = await hass.config_entries.flow.async_init(
        DOMAIN, context={"source": config_entries.SOURCE_USER})
    result = await hass.config_entries.flow.async_configure(result["flow_id"], USER_SERIES)
    result = await hass.config_entries.flow.async_configure(result["flow_id"], ADVANCED_DEFAULTS)
    assert result["type"] == FlowResultType.CREATE_ENTRY
    assert result["title"] == "Werkstatt Akku"
    assert result["data"]["topology"] == "series"
    assert result["data"]["bank_b_voltage_entity"] == "sensor.bank_b_voltage"


async def test_options_flow_updates_a_tunable(hass):
    entry = await _create_entry(hass, USER_PARALLEL, ADVANCED_DEFAULTS)
    result = await hass.config_entries.options.async_init(entry.entry_id)
    result = await hass.config_entries.options.async_configure(
        result["flow_id"], {
            "charger_power_entity": "sensor.meanwell_power",
            "inverter_power_entity": "sensor.lumentree_power",
            "bank_a_voltage_entity": "sensor.bank_voltage",
            "bank_a_voltage_scale": 1.0, "bank_b_voltage_scale": 1.0,
            "fallback_interval_s": 15,
        })
    result = await hass.config_entries.options.async_configure(
        result["flow_id"], dict(ADVANCED_DEFAULTS, calibration_hold_s=300))
    assert result["type"] == FlowResultType.CREATE_ENTRY
    assert entry.options["calibration_hold_s"] == 300
    assert entry.options["fallback_interval_s"] == 15


async def test_options_flow_accepts_new_calibration_tunables(hass):
    """The four fields from Tasks 1-3 must go through the tunables step
    and land unchanged in params_from_config."""
    from custom_components.battery_soc.helpers import params_from_config

    entry = await _create_entry(hass, USER_PARALLEL, ADVANCED_DEFAULTS)
    result = await hass.config_entries.options.async_init(entry.entry_id)
    result = await hass.config_entries.options.async_configure(
        result["flow_id"], {
            "charger_power_entity": "sensor.meanwell_power",
            "inverter_power_entity": "sensor.lumentree_power",
            "bank_a_voltage_entity": "sensor.bank_voltage",
            "bank_a_voltage_scale": 1.0, "bank_b_voltage_scale": 1.0,
            "fallback_interval_s": 15,
        })
    result = await hass.config_entries.options.async_configure(
        result["flow_id"],
        user_input={**ADVANCED_DEFAULTS,
                    "full_taper_c_rate": 0.05,
                    "calibration_tolerance_empty_v_per_cell": 0.20,
                    "calibration_tolerance_full_v_per_cell": 0.02,
                    "calibration_grace_s": 90},
    )
    assert result["type"] == FlowResultType.CREATE_ENTRY
    params = params_from_config({**entry.data, **result["data"]})
    assert params.full_taper_c_rate == 0.05
    assert params.calibration_tolerance_empty_v_per_cell == 0.20
    assert params.calibration_tolerance_full_v_per_cell == 0.02
    assert params.calibration_grace_s == 90


async def test_options_flow_leaves_taper_and_overrides_unset_by_default(hass):
    """Nothing entered -> None or dataclass default; the production Pi
    guarantee also holds for HA users."""
    from custom_components.battery_soc.helpers import params_from_config

    entry = await _create_entry(hass, USER_PARALLEL, ADVANCED_DEFAULTS)
    params = params_from_config({**entry.data, **entry.options})
    assert params.full_taper_c_rate is None
    assert params.calibration_tolerance_empty_v_per_cell is None
    assert params.calibration_tolerance_full_v_per_cell is None
    assert params.calibration_grace_s == 0.0
