from homeassistant import config_entries
from homeassistant.data_entry_flow import FlowResultType
from homeassistant.helpers import entity_registry as er

from custom_components.battery_soc.const import DOMAIN, INVERT_KEYS
from custom_components.battery_soc.helpers import params_from_config, source_config
from tests.conftest import (
    ADVANCED_DC, ADVANCED_DEFAULTS, AC_ONLY_TUNABLES, FLOW_BANK_B_PARALLEL,
    FLOW_BANK_B_SERIES, FLOW_SOURCES_AC, FLOW_SOURCES_DC, FLOW_USER_AC, FLOW_USER_DC,
)


async def _run(hass, *steps):
    """Start a user flow and feed it the given step inputs in order."""
    result = await hass.config_entries.flow.async_init(
        DOMAIN, context={"source": config_entries.SOURCE_USER})
    for data in steps:
        result = await hass.config_entries.flow.async_configure(result["flow_id"], data)
    return result


async def _create_entry(hass):
    """The AC-coupled, two banks in parallel entry the options tests start from."""
    result = await _run(hass, FLOW_USER_AC, FLOW_SOURCES_AC, FLOW_BANK_B_PARALLEL,
                        ADVANCED_DEFAULTS)
    assert result["type"] == FlowResultType.CREATE_ENTRY
    return hass.config_entries.async_entries(DOMAIN)[0]


def _schema_keys(result):
    return {str(k) for k in result["data_schema"].schema}


async def test_user_step_routes_by_system_type(hass):
    assert (await _run(hass, FLOW_USER_AC))["step_id"] == "sources_ac"
    assert (await _run(hass, dict(FLOW_USER_DC, name="Other")))["step_id"] == "sources_dc"


async def test_dc_sources_step_has_no_ac_fields(hass):
    keys = _schema_keys(await _run(hass, FLOW_USER_DC))
    assert "charger_power_entity" not in keys and "inverter_power_invert" not in keys
    assert {"charger_dc_power_entity", "inverter_dc_power_invert", "bank_layout"} <= keys


async def test_ac_parallel_flow_creates_a_v2_entry(hass):
    result = await _run(hass, FLOW_USER_AC, FLOW_SOURCES_AC, FLOW_BANK_B_PARALLEL,
                        ADVANCED_DEFAULTS)
    assert result["type"] == FlowResultType.CREATE_ENTRY
    assert result["title"] == "Werkstatt Akku"
    data = result["data"]
    assert data["system_type"] == "ac_coupled"
    assert (data["bank_b_enabled"], data["topology"]) == (True, "parallel")
    assert "bank_layout" not in data
    assert data["bank_b_voltage_entity"] == ""
    assert all(data[key] is False for key in INVERT_KEYS)
    assert hass.config_entries.async_entries(DOMAIN)[0].version == 2


async def test_single_bank_skips_bank_b_and_disables_it(hass):
    result = await _run(hass, FLOW_USER_AC, dict(FLOW_SOURCES_AC, bank_layout="single"))
    assert result["step_id"] == "advanced"
    result = await hass.config_entries.flow.async_configure(result["flow_id"], ADVANCED_DEFAULTS)
    data = result["data"]
    assert (data["bank_b_enabled"], data["topology"]) == (False, "parallel")
    assert "bank_b_capacity_ah" not in data and "bank_b_cell_count" not in data


async def test_series_flow_asks_for_bank_b_voltage(hass):
    result = await _run(hass, FLOW_USER_AC, dict(FLOW_SOURCES_AC, bank_layout="series"))
    assert result["step_id"] == "bank_b"
    assert {"bank_b_voltage_entity", "bank_b_voltage_scale"} <= _schema_keys(result)
    result = await hass.config_entries.flow.async_configure(result["flow_id"], FLOW_BANK_B_SERIES)
    result = await hass.config_entries.flow.async_configure(result["flow_id"], ADVANCED_DEFAULTS)
    assert result["data"]["topology"] == "series"
    assert result["data"]["bank_b_voltage_entity"] == "sensor.bank_b_voltage"


async def test_parallel_bank_b_step_has_no_voltage_fields(hass):
    result = await _run(hass, FLOW_USER_AC, FLOW_SOURCES_AC)
    assert result["step_id"] == "bank_b"
    assert _schema_keys(result) == {"bank_b_capacity_ah", "bank_b_cell_count"}


async def test_bank_b_rejects_a_mismatched_parallel_cell_count(hass):
    result = await _run(hass, FLOW_USER_AC, FLOW_SOURCES_AC,
                        dict(FLOW_BANK_B_PARALLEL, bank_b_cell_count=16))
    assert result["step_id"] == "bank_b"
    assert result["errors"]["base"]


async def test_issue_scenario_dc_flow(hass):
    """Review focus 4: 3.6 Ah and 5 cells must be enterable."""
    result = await _run(hass, FLOW_USER_DC, FLOW_SOURCES_DC)
    assert result["step_id"] == "advanced"
    assert not (set(AC_ONLY_TUNABLES) & _schema_keys(result))
    result = await hass.config_entries.flow.async_configure(result["flow_id"], ADVANCED_DC)
    assert result["type"] == FlowResultType.CREATE_ENTRY
    data = result["data"]
    assert data["system_type"] == "dc_only"
    assert data["charger_power_entity"] == "" and data["inverter_power_entity"] == ""
    assert data["inverter_dc_power_invert"] is True
    assert data["charger_dc_power_invert"] is False
    assert data["bank_a_capacity_ah"] == 3.6
    assert data["bank_b_enabled"] is False


async def test_missing_discharge_source_is_reported(hass):
    sources = {k: v for k, v in FLOW_SOURCES_AC.items() if k != "inverter_power_entity"}
    result = await _run(hass, FLOW_USER_AC, sources)
    assert result["step_id"] == "sources_ac"
    assert result["errors"] == {"base": "discharge_source_required"}


async def test_current_sensor_on_an_ac_slot_is_rejected(hass):
    hass.states.async_set("sensor.shunt", "1.2", {"unit_of_measurement": "A"})
    result = await _run(hass, FLOW_USER_AC,
                        dict(FLOW_SOURCES_AC, charger_power_entity="sensor.shunt"))
    assert result["errors"] == {"base": "current_only_on_dc"}


async def test_unit_falls_back_to_the_entity_registry(hass):
    reg_entry = er.async_get(hass).async_get_or_create(
        "sensor", "test", "shunt-1", suggested_object_id="shunt",
        unit_of_measurement="mA")
    result = await _run(hass, FLOW_USER_AC,
                        dict(FLOW_SOURCES_AC, inverter_power_entity=reg_entry.entity_id))
    assert result["errors"] == {"base": "current_only_on_dc"}


async def test_unknown_unit_does_not_block_saving(hass):
    """The entity exists nowhere yet: save anyway, the runtime rule applies."""
    result = await _run(hass, FLOW_USER_DC,
                        dict(FLOW_SOURCES_DC, charger_dc_power_entity="sensor.not_there_yet"))
    assert result["step_id"] == "advanced"


async def test_duplicate_name_aborts(hass):
    await _create_entry(hass)
    result = await _run(hass, FLOW_USER_AC)
    assert result["type"] == FlowResultType.ABORT
    assert result["reason"] == "already_configured"


async def test_advanced_step_rejects_bad_efficiency(hass):
    result = await _run(hass, FLOW_USER_AC, dict(FLOW_SOURCES_AC, bank_layout="single"),
                        dict(ADVANCED_DEFAULTS, charge_efficiency=1.4))
    assert result["type"] == FlowResultType.FORM
    assert result["errors"]["base"]


def _default(result, key):
    for marker in result["data_schema"].schema:
        if str(marker) == key:
            return marker.default()
    raise KeyError(key)


async def _options(hass, entry, *steps):
    result = await hass.config_entries.options.async_init(entry.entry_id)
    for data in steps:
        result = await hass.config_entries.options.async_configure(result["flow_id"], data)
    return result


async def test_options_start_with_the_stored_system_type_and_layout(hass):
    entry = await _create_entry(hass)
    result = await _options(hass, entry)
    assert result["step_id"] == "init"
    assert _default(result, "system_type") == "ac_coupled"
    result = await _options(hass, entry, {"system_type": "ac_coupled"})
    assert result["step_id"] == "sources_ac"
    assert _default(result, "bank_layout") == "parallel"


async def test_options_update_a_tunable(hass):
    entry = await _create_entry(hass)
    result = await _options(hass, entry, {"system_type": "ac_coupled"}, FLOW_SOURCES_AC,
                            FLOW_BANK_B_PARALLEL,
                            dict(ADVANCED_DEFAULTS, calibration_hold_s=300,
                                 fallback_interval_s=15))
    assert result["type"] == FlowResultType.CREATE_ENTRY
    assert entry.options["calibration_hold_s"] == 300
    assert entry.options["fallback_interval_s"] == 15


async def test_options_switch_to_dc_only_blanks_the_ac_sources(hass):
    """Review focus 3: AC entities from entry.data must not survive the merge."""
    entry = await _create_entry(hass)
    assert entry.data["charger_power_entity"] == "sensor.meanwell_power"
    result = await _options(hass, entry, {"system_type": "dc_only"},
                            dict(FLOW_SOURCES_DC, bank_layout="parallel"),
                            dict(FLOW_BANK_B_PARALLEL, bank_b_cell_count=5), ADVANCED_DC)
    assert result["type"] == FlowResultType.CREATE_ENTRY
    merged = {**entry.data, **entry.options}
    assert merged["charger_power_entity"] == "" and merged["inverter_power_entity"] == ""
    sources = source_config(merged)
    assert sources.system_type == "dc_only"
    assert sources.configured == {"charger_dc_power", "inverter_dc_power", "bank_a_voltage"}


async def test_options_single_bank_overrides_the_stored_bank_b(hass):
    entry = await _create_entry(hass)
    result = await _options(hass, entry, {"system_type": "ac_coupled"},
                            dict(FLOW_SOURCES_AC, bank_layout="single"), ADVANCED_DEFAULTS)
    assert result["type"] == FlowResultType.CREATE_ENTRY
    params = params_from_config({**entry.data, **entry.options})
    assert params.bank_b_enabled is False
    await hass.async_block_till_done()
    coord = hass.data[DOMAIN][entry.entry_id]
    assert coord.state.units[0].capacity_ah == 100  # bank B no longer added


async def test_options_report_a_missing_source(hass):
    entry = await _create_entry(hass)
    sources = {k: v for k, v in FLOW_SOURCES_AC.items() if k != "charger_power_entity"}
    result = await _options(hass, entry, {"system_type": "ac_coupled"}, sources)
    assert result["step_id"] == "sources_ac"
    assert result["errors"] == {"base": "charge_source_required"}


async def test_options_accept_new_calibration_tunables(hass):
    entry = await _create_entry(hass)
    result = await _options(
        hass, entry, {"system_type": "ac_coupled"}, FLOW_SOURCES_AC, FLOW_BANK_B_PARALLEL,
        {**ADVANCED_DEFAULTS, "full_taper_c_rate": 0.05,
         "calibration_tolerance_empty_v_per_cell": 0.20,
         "calibration_tolerance_full_v_per_cell": 0.02, "calibration_grace_s": 90})
    assert result["type"] == FlowResultType.CREATE_ENTRY
    params = params_from_config({**entry.data, **result["data"]})
    assert params.full_taper_c_rate == 0.05
    assert params.calibration_tolerance_empty_v_per_cell == 0.20
    assert params.calibration_tolerance_full_v_per_cell == 0.02
    assert params.calibration_grace_s == 90


async def test_new_entry_leaves_taper_and_overrides_unset(hass):
    entry = await _create_entry(hass)
    params = params_from_config({**entry.data, **entry.options})
    assert params.full_taper_c_rate is None
    assert params.calibration_tolerance_empty_v_per_cell is None
    assert params.calibration_tolerance_full_v_per_cell is None
    assert params.calibration_grace_s == 0.0


def _chemistry_marker(schema):
    return next(k for k in schema if str(k) == "battery_chemistry")


def test_battery_chemistry_is_a_single_option_radio_list():
    from homeassistant.helpers.selector import SelectSelectorMode
    from custom_components.battery_soc.config_flow import _sources_schema

    schema = _sources_schema("dc_only", {})
    marker = _chemistry_marker(schema)
    config = schema[marker].config
    assert config["options"] == ["lifepo4"]
    assert config["mode"] == SelectSelectorMode.LIST
    assert marker.default() == "lifepo4"


def test_old_free_text_chemistry_falls_back_to_the_one_option():
    from custom_components.battery_soc.config_flow import _sources_schema

    schema = _sources_schema("ac_coupled", {"battery_chemistry": "LiFePO4 (Dyness)"})
    assert _chemistry_marker(schema).default() == "lifepo4"
