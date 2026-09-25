"""Config flow for battery_soc integration."""
from __future__ import annotations

from collections.abc import Mapping
from typing import Any

import voluptuous as vol
from homeassistant import config_entries
from homeassistant.const import ATTR_UNIT_OF_MEASUREMENT
from homeassistant.core import HomeAssistant, callback
from homeassistant.helpers import entity_registry as er
from homeassistant.helpers.selector import (
    BooleanSelector,
    EntitySelector,
    EntitySelectorConfig,
    NumberSelector,
    NumberSelectorConfig,
    NumberSelectorMode,
    SelectSelector,
    SelectSelectorConfig,
    SelectSelectorMode,
    TextSelector,
)
from homeassistant.util import slugify

from .battery_soc_core import (
    AC_POWER_SLOTS, DC_POWER_SLOTS, POWER_SLOTS, SOC_CURVES, SYSTEM_TYPES,
    validate_sources,
)
from .const import (
    BANK_A_VOLTAGE_MEASURES,
    BANK_LAYOUTS,
    CONF_BANK_A_VOLTAGE_ENTITY,
    CONF_BANK_A_VOLTAGE_MEASURES,
    CONF_BANK_A_VOLTAGE_SCALE,
    CONF_BANK_B_VOLTAGE_ENTITY,
    CONF_BANK_B_VOLTAGE_SCALE,
    CONF_BANK_LAYOUT,
    CONF_FALLBACK_INTERVAL_S,
    CONF_SYSTEM_TYPE,
    DOMAIN,
    LAYOUT_SERIES,
    LAYOUT_SINGLE,
    SLOT_ENTITY_KEYS,
    SYSTEM_AC_COUPLED,
    SYSTEM_DC_ONLY,
)
from .helpers import layout_from_config, params_from_config, source_config
from .units import base_unit


AC_ONLY_TUNABLES = ("charger_ac_dc_efficiency", "inverter_dc_ac_efficiency", "dc_max_age_s")
# The only chemistry the SoC curves are made for.
BATTERY_CHEMISTRIES = ("lifepo4",)


def _select(options, translation_key: str) -> SelectSelector:
    return SelectSelector(SelectSelectorConfig(options=list(options),
                                               translation_key=translation_key,
                                               mode=SelectSelectorMode.LIST))


def _entity(device_class: str | list[str]) -> EntitySelector:
    return EntitySelector(EntitySelectorConfig(domain="sensor", device_class=device_class))


def _suggest(defaults: Mapping[str, Any], key: str) -> dict[str, Any]:
    """Prefill an entity field without making it impossible to clear."""
    value = defaults.get(key)
    return {"description": {"suggested_value": value}} if value else {}


def _capacity() -> NumberSelector:
    # Decimals matter: embedded packs are a few Ah (3.6 Ah in ha-battery-soc#1).
    return NumberSelector(NumberSelectorConfig(min=0.1, max=10000, step=0.1,
                                               mode=NumberSelectorMode.BOX))


def _cell_count() -> NumberSelector:
    return NumberSelector(NumberSelectorConfig(min=1, max=100, step=1,
                                               mode=NumberSelectorMode.BOX))


def _voltage_scale() -> NumberSelector:
    return NumberSelector(NumberSelectorConfig(min=0.1, max=10.0, step=0.01,
                                               mode=NumberSelectorMode.BOX))


def _user_schema() -> dict[str, Any]:
    return {
        vol.Required("name"): TextSelector(),
        **_system_type_schema({}),
    }


def _system_type_schema(defaults: Mapping[str, Any]) -> dict[str, Any]:
    return {
        vol.Required(CONF_SYSTEM_TYPE,
                     default=defaults.get(CONF_SYSTEM_TYPE, SYSTEM_AC_COUPLED)):
            _select(SYSTEM_TYPES, CONF_SYSTEM_TYPE),
    }


def _sources_schema(system_type: str, defaults: Mapping[str, Any]) -> dict[str, Any]:
    """Bank layout, power sources with invert, and bank A."""
    if defaults.get(CONF_BANK_LAYOUT):
        layout = defaults[CONF_BANK_LAYOUT]
    elif "bank_b_enabled" in defaults:
        layout = layout_from_config(defaults)
    else:
        layout = LAYOUT_SINGLE  # new setups: the common case, no silent capacity sum
    schema: dict[str, Any] = {
        vol.Required(CONF_BANK_LAYOUT, default=layout): _select(BANK_LAYOUTS, CONF_BANK_LAYOUT),
    }
    slots = (AC_POWER_SLOTS if system_type == SYSTEM_AC_COUPLED else ()) + DC_POWER_SLOTS
    for slot in slots:
        key = SLOT_ENTITY_KEYS[slot]
        device_class = "power" if slot in AC_POWER_SLOTS else ["power", "current"]
        schema[vol.Optional(key, **_suggest(defaults, key))] = _entity(device_class)
        schema[vol.Optional(f"{slot}_invert",
                            default=defaults.get(f"{slot}_invert", False))] = BooleanSelector()
    schema.update({
        vol.Required(CONF_BANK_A_VOLTAGE_ENTITY,
                     **_suggest(defaults, CONF_BANK_A_VOLTAGE_ENTITY)): _entity("voltage"),
        vol.Optional(CONF_BANK_A_VOLTAGE_SCALE,
                     default=defaults.get(CONF_BANK_A_VOLTAGE_SCALE, 1.0)): _voltage_scale(),
        vol.Optional("bank_a_capacity_ah",
                     default=defaults.get("bank_a_capacity_ah", 100)): _capacity(),
        vol.Optional("bank_a_cell_count",
                     default=defaults.get("bank_a_cell_count", 8)): _cell_count(),
        # A stored free-text value from the old text field falls back to the one option.
        vol.Optional("battery_chemistry", default=(
            defaults.get("battery_chemistry") if defaults.get("battery_chemistry")
            in BATTERY_CHEMISTRIES else BATTERY_CHEMISTRIES[0])):
            _select(BATTERY_CHEMISTRIES, "battery_chemistry"),
        vol.Optional("soc_curve", default=defaults.get("soc_curve", "generic_lifepo4")):
            SelectSelector(SelectSelectorConfig(options=sorted(SOC_CURVES.keys()))),
    })
    return schema


def _bank_b_schema(layout: str, defaults: Mapping[str, Any]) -> dict[str, Any]:
    """Bank B fields; the voltages only exist for banks in series."""
    schema: dict[str, Any] = {
        vol.Optional("bank_b_capacity_ah", default=defaults.get(
            "bank_b_capacity_ah", defaults.get("bank_a_capacity_ah", 100))): _capacity(),
        vol.Optional("bank_b_cell_count", default=defaults.get(
            "bank_b_cell_count", defaults.get("bank_a_cell_count", 8))): _cell_count(),
    }
    if layout == LAYOUT_SERIES:
        schema[vol.Required(CONF_BANK_A_VOLTAGE_MEASURES, default=defaults.get(
            CONF_BANK_A_VOLTAGE_MEASURES, BANK_A_VOLTAGE_MEASURES[0]))] = \
            _select(BANK_A_VOLTAGE_MEASURES, CONF_BANK_A_VOLTAGE_MEASURES)
        schema[vol.Required(CONF_BANK_B_VOLTAGE_ENTITY,
                            **_suggest(defaults, CONF_BANK_B_VOLTAGE_ENTITY))] = _entity("voltage")
        schema[vol.Optional(CONF_BANK_B_VOLTAGE_SCALE,
                            default=defaults.get(CONF_BANK_B_VOLTAGE_SCALE, 1.0))] = _voltage_scale()
    return schema


def _advanced_schema_dict(defaults: Mapping[str, Any], system_type: str = SYSTEM_AC_COUPLED) -> dict[str, Any]:
    """Get the advanced step schema dictionary with defaults."""
    schema = {
        vol.Optional("empty_v_per_cell", default=defaults.get("empty_v_per_cell", 2.7)): NumberSelector(
            NumberSelectorConfig(step=0.1)
        ),
        vol.Optional("full_v_per_cell", default=defaults.get("full_v_per_cell", 3.5)): NumberSelector(
            NumberSelectorConfig(step=0.1)
        ),
        vol.Optional("charger_ac_dc_efficiency", default=defaults.get("charger_ac_dc_efficiency", 0.9)): NumberSelector(
            NumberSelectorConfig(step=0.01)
        ),
        vol.Optional("inverter_dc_ac_efficiency", default=defaults.get("inverter_dc_ac_efficiency", 0.9)): NumberSelector(
            NumberSelectorConfig(step=0.01)
        ),
        vol.Optional("charge_efficiency", default=defaults.get("charge_efficiency", 0.98)): NumberSelector(
            NumberSelectorConfig(step=0.01)
        ),
        vol.Optional("calibration_tolerance_v_per_cell", default=defaults.get("calibration_tolerance_v_per_cell", 0.08)): NumberSelector(
            NumberSelectorConfig(step=0.01)
        ),
        vol.Optional("calibration_hold_s", default=defaults.get("calibration_hold_s", 120)): NumberSelector(
            NumberSelectorConfig(step=1)
        ),
        vol.Optional("voltage_soc_mismatch_warn_pct", default=defaults.get("voltage_soc_mismatch_warn_pct", 25)): NumberSelector(
            NumberSelectorConfig(step=1)
        ),
        vol.Optional("voltage_mismatch_hold_s", default=defaults.get("voltage_mismatch_hold_s", 300)): NumberSelector(
            NumberSelectorConfig(step=1)
        ),
        vol.Optional("imbalance_warn_v", default=defaults.get("imbalance_warn_v", 0.5)): NumberSelector(
            NumberSelectorConfig(step=0.01)
        ),
        vol.Optional("stale_input_s", default=defaults.get("stale_input_s", 120)): NumberSelector(
            NumberSelectorConfig(step=1)
        ),
        vol.Optional("dc_max_age_s", default=defaults.get("dc_max_age_s", 60)): NumberSelector(
            NumberSelectorConfig(step=1)
        ),
        vol.Optional("require_fresh_inputs", default=defaults.get("require_fresh_inputs", False)): BooleanSelector(),
        vol.Optional("internal_resistance_mohm_per_cell"): NumberSelector(
            NumberSelectorConfig(step=0.1)
        ),
        # Tasks 1-3: new SocParams calibration tunables without defaults (None = unset)
        vol.Optional("full_taper_c_rate"): NumberSelector(
            NumberSelectorConfig(min=0, max=1, step=0.01)
        ),
        vol.Optional("calibration_tolerance_empty_v_per_cell"): NumberSelector(
            NumberSelectorConfig(min=0, step=0.01)
        ),
        vol.Optional("calibration_tolerance_full_v_per_cell"): NumberSelector(
            NumberSelectorConfig(min=0, step=0.01)
        ),
        vol.Optional("calibration_grace_s", default=defaults.get("calibration_grace_s", 0)): NumberSelector(
            NumberSelectorConfig(min=0, step=1)
        ),
        vol.Optional(CONF_FALLBACK_INTERVAL_S, default=defaults.get(CONF_FALLBACK_INTERVAL_S, 30)): NumberSelector(
            NumberSelectorConfig(step=1)
        ),
    }
    if system_type == SYSTEM_DC_ONLY:
        # No AC measurement: no converter efficiency, no AC fallback to age out.
        schema = {k: v for k, v in schema.items() if str(k) not in AC_ONLY_TUNABLES}
    return schema


def _finalize(data: Mapping[str, Any], layout: str) -> dict[str, Any]:
    """Map the form onto the stored fields and blank what the system type and
    bank layout hide.

    Blanking (not dropping) matters for the options flow: options only
    overlay entry.data, so a hidden AC entity must be written as "" or the
    old value would survive the merge."""
    out = {k: v for k, v in data.items() if k != CONF_BANK_LAYOUT}
    out["bank_b_enabled"], out["topology"] = BANK_LAYOUTS[layout]
    for key in SLOT_ENTITY_KEYS.values():
        out.setdefault(key, "")
    for slot in POWER_SLOTS:
        out.setdefault(f"{slot}_invert", False)
    if out.get(CONF_SYSTEM_TYPE) == SYSTEM_DC_ONLY:
        for slot in AC_POWER_SLOTS:
            out[SLOT_ENTITY_KEYS[slot]] = ""
            out[f"{slot}_invert"] = False
    if layout != LAYOUT_SERIES:
        out[CONF_BANK_B_VOLTAGE_ENTITY] = ""
        out.pop(CONF_BANK_B_VOLTAGE_SCALE, None)
        # Written, not dropped: an options flow must override a stored "stack".
        out[CONF_BANK_A_VOLTAGE_MEASURES] = BANK_A_VOLTAGE_MEASURES[0]
    if layout == LAYOUT_SINGLE:
        out.pop("bank_b_capacity_ah", None)
        out.pop("bank_b_cell_count", None)
    return out


def _detect_unit(hass: HomeAssistant, entity_id: str) -> str | None:
    """"W"/"A" from the current state, else from the entity registry."""
    state = hass.states.get(entity_id)
    uom = state.attributes.get(ATTR_UNIT_OF_MEASUREMENT) if state else None
    if uom is None:
        reg_entry = er.async_get(hass).async_get(entity_id)
        uom = reg_entry.unit_of_measurement if reg_entry else None
    return base_unit(uom)


def _source_errors(hass: HomeAssistant, cfg: Mapping[str, Any], *,
                   complete: bool) -> dict[str, str]:
    """First source error as a form error.

    Before the bank_b step (complete=False), bank B is not entered yet: its
    voltage rule and the SocParams checks wait for that step."""
    units = {}
    for slot in POWER_SLOTS:
        entity_id = cfg.get(SLOT_ENTITY_KEYS[slot])
        unit = _detect_unit(hass, entity_id) if entity_id else None
        if unit:
            units[slot] = unit
    codes = validate_sources(source_config(cfg, units))
    if not complete:
        codes = [c for c in codes if c != "bank_b_voltage_required"]
    if codes:
        return {"base": codes[0]}
    if complete:
        try:
            params_from_config(cfg)
        except ValueError as exc:
            return {"base": str(exc)[:100]}
    return {}


class BatterySocConfigFlow(config_entries.ConfigFlow, domain=DOMAIN):
    """Config flow: user -> sources_ac|sources_dc -> [bank_b] -> advanced."""

    VERSION = 2

    def __init__(self):
        """Initialize config flow."""
        super().__init__()
        self._data: dict[str, Any] = {}
        self._layout = LAYOUT_SINGLE

    async def async_step_user(self, user_input=None):
        """Name and system type."""
        if user_input is not None:
            await self.async_set_unique_id(slugify(user_input["name"]))
            self._abort_if_unique_id_configured()
            self._data = dict(user_input)
            return await self._async_step_sources()
        return self.async_show_form(step_id="user", data_schema=vol.Schema(_user_schema()))

    async def async_step_sources_ac(self, user_input=None):
        """Sources of an AC-coupled system."""
        return await self._async_step_sources(user_input)

    async def async_step_sources_dc(self, user_input=None):
        """Sources of a DC-only system."""
        return await self._async_step_sources(user_input)

    async def _async_step_sources(self, user_input=None):
        system_type = self._data[CONF_SYSTEM_TYPE]
        errors: dict[str, str] = {}
        if user_input is not None:
            self._layout = user_input[CONF_BANK_LAYOUT]
            candidate = _finalize({**self._data, **user_input}, self._layout)
            errors = _source_errors(self.hass, candidate,
                                    complete=self._layout == LAYOUT_SINGLE)
            if not errors:
                self._data = candidate
                if self._layout == LAYOUT_SINGLE:
                    return await self.async_step_advanced()
                return await self.async_step_bank_b()
        step_id = "sources_ac" if system_type == SYSTEM_AC_COUPLED else "sources_dc"
        return self.async_show_form(
            step_id=step_id,
            data_schema=vol.Schema(_sources_schema(system_type, user_input or {})),
            errors=errors,
        )

    async def async_step_bank_b(self, user_input=None):
        """Bank B, only for two banks."""
        errors: dict[str, str] = {}
        if user_input is not None:
            candidate = _finalize({**self._data, **user_input}, self._layout)
            errors = _source_errors(self.hass, candidate, complete=True)
            if not errors:
                self._data = candidate
                return await self.async_step_advanced()
        return self.async_show_form(
            step_id="bank_b",
            data_schema=vol.Schema(_bank_b_schema(self._layout, user_input or self._data)),
            errors=errors,
        )

    async def async_step_advanced(self, user_input=None):
        """Handle the advanced configuration step."""
        system_type = self._data[CONF_SYSTEM_TYPE]
        errors: dict[str, str] = {}
        if user_input is not None:
            full = {**self._data, **user_input}
            try:
                params_from_config(full)
            except ValueError as exc:
                errors["base"] = str(exc)[:100]
            if not errors:
                return self.async_create_entry(title=self._data["name"], data=full)
        return self.async_show_form(
            step_id="advanced",
            data_schema=vol.Schema(_advanced_schema_dict(user_input or {}, system_type)),
            errors=errors,
        )

    @staticmethod
    @callback
    def async_get_options_flow(config_entry):
        """Return the options flow handler."""
        return BatterySocOptionsFlow()


class BatterySocOptionsFlow(config_entries.OptionsFlow):
    """Options flow: init (system type) -> sources_ac|sources_dc -> [bank_b] -> tunables."""

    def __init__(self):
        """Initialize options flow."""
        self._collected: dict[str, Any] = {}
        self._layout = LAYOUT_SINGLE

    @property
    def _current(self) -> dict[str, Any]:
        return {**self.config_entry.data, **self.config_entry.options}

    async def async_step_init(self, user_input=None):
        """System type, prefilled from the entry."""
        if user_input is not None:
            self._collected = dict(user_input)
            return await self._async_step_sources()
        return self.async_show_form(
            step_id="init", data_schema=vol.Schema(_system_type_schema(self._current)))

    async def async_step_sources_ac(self, user_input=None):
        """Sources of an AC-coupled system."""
        return await self._async_step_sources(user_input)

    async def async_step_sources_dc(self, user_input=None):
        """Sources of a DC-only system."""
        return await self._async_step_sources(user_input)

    async def _async_step_sources(self, user_input=None):
        system_type = self._collected[CONF_SYSTEM_TYPE]
        errors: dict[str, str] = {}
        if user_input is not None:
            self._layout = user_input[CONF_BANK_LAYOUT]
            candidate = _finalize({**self._collected, **user_input}, self._layout)
            errors = _source_errors(self.hass, {**self._current, **candidate},
                                    complete=self._layout == LAYOUT_SINGLE)
            if not errors:
                self._collected = candidate
                if self._layout == LAYOUT_SINGLE:
                    return await self.async_step_tunables()
                return await self.async_step_bank_b()
        step_id = "sources_ac" if system_type == SYSTEM_AC_COUPLED else "sources_dc"
        return self.async_show_form(
            step_id=step_id,
            data_schema=vol.Schema(_sources_schema(system_type, user_input or self._current)),
            errors=errors,
        )

    async def async_step_bank_b(self, user_input=None):
        """Bank B, only for two banks."""
        errors: dict[str, str] = {}
        if user_input is not None:
            candidate = _finalize({**self._collected, **user_input}, self._layout)
            errors = _source_errors(self.hass, {**self._current, **candidate}, complete=True)
            if not errors:
                self._collected = candidate
                return await self.async_step_tunables()
        defaults = user_input or {**self._current, **self._collected}
        return self.async_show_form(
            step_id="bank_b",
            data_schema=vol.Schema(_bank_b_schema(self._layout, defaults)),
            errors=errors,
        )

    async def async_step_tunables(self, user_input=None):
        """Tunables; AC-only ones are hidden for DC-only systems."""
        system_type = self._collected[CONF_SYSTEM_TYPE]
        errors: dict[str, str] = {}
        if user_input is not None:
            merged = {**self._collected, **user_input}
            try:
                params_from_config({**self._current, **merged})
            except ValueError as exc:
                errors["base"] = str(exc)[:100]
            if not errors:
                return self.async_create_entry(title="", data=merged)
        defaults = user_input or {**self._current, **self._collected}
        return self.async_show_form(
            step_id="tunables",
            data_schema=vol.Schema(_advanced_schema_dict(defaults, system_type)),
            errors=errors,
        )
