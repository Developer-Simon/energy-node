"""Config flow for battery_soc integration."""
from __future__ import annotations

from collections.abc import Mapping
from typing import Any

import voluptuous as vol
from homeassistant import config_entries
from homeassistant.core import callback
from homeassistant.helpers.selector import (
    BooleanSelector,
    EntitySelector,
    EntitySelectorConfig,
    NumberSelector,
    NumberSelectorConfig,
    SelectSelector,
    SelectSelectorConfig,
    TextSelector,
)
from homeassistant.util import slugify

from .battery_soc_core import SOC_CURVES, SocParams
from .const import (
    CONF_BANK_A_VOLTAGE_ENTITY,
    CONF_BANK_A_VOLTAGE_SCALE,
    CONF_BANK_B_VOLTAGE_ENTITY,
    CONF_BANK_B_VOLTAGE_SCALE,
    CONF_CHARGER_DC_POWER_ENTITY,
    CONF_CHARGER_POWER_ENTITY,
    CONF_INVERTER_DC_POWER_ENTITY,
    CONF_INVERTER_POWER_ENTITY,
    CONF_FALLBACK_INTERVAL_S,
    DOMAIN,
)
from .helpers import params_from_config


def _advanced_schema_dict(defaults: Mapping[str, Any]) -> dict[str, Any]:
    """Get the advanced step schema dictionary with defaults."""
    return {
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
        vol.Optional(CONF_FALLBACK_INTERVAL_S, default=defaults.get(CONF_FALLBACK_INTERVAL_S, 30)): NumberSelector(
            NumberSelectorConfig(step=1)
        ),
    }


def _sources_schema_dict(defaults: Mapping[str, Any]) -> dict[str, Any]:
    """Get the sources/init step schema for options flow."""
    schema = {
        vol.Required(CONF_CHARGER_POWER_ENTITY, default=defaults.get(CONF_CHARGER_POWER_ENTITY)): EntitySelector(
            EntitySelectorConfig(domain="sensor", device_class="power")
        ),
        vol.Required(CONF_INVERTER_POWER_ENTITY, default=defaults.get(CONF_INVERTER_POWER_ENTITY)): EntitySelector(
            EntitySelectorConfig(domain="sensor", device_class="power")
        ),
        vol.Required(CONF_BANK_A_VOLTAGE_ENTITY, default=defaults.get(CONF_BANK_A_VOLTAGE_ENTITY)): EntitySelector(
            EntitySelectorConfig(domain="sensor", device_class="voltage")
        ),
    }

    # Optional entity fields: use default if present in defaults, otherwise no default
    if CONF_BANK_B_VOLTAGE_ENTITY in defaults and defaults[CONF_BANK_B_VOLTAGE_ENTITY]:
        schema[vol.Optional(CONF_BANK_B_VOLTAGE_ENTITY, default=defaults[CONF_BANK_B_VOLTAGE_ENTITY])] = EntitySelector(
            EntitySelectorConfig(domain="sensor", device_class="voltage")
        )
    else:
        schema[vol.Optional(CONF_BANK_B_VOLTAGE_ENTITY)] = EntitySelector(
            EntitySelectorConfig(domain="sensor", device_class="voltage")
        )

    if CONF_CHARGER_DC_POWER_ENTITY in defaults and defaults[CONF_CHARGER_DC_POWER_ENTITY]:
        schema[vol.Optional(CONF_CHARGER_DC_POWER_ENTITY, default=defaults[CONF_CHARGER_DC_POWER_ENTITY])] = EntitySelector(
            EntitySelectorConfig(domain="sensor", device_class="power")
        )
    else:
        schema[vol.Optional(CONF_CHARGER_DC_POWER_ENTITY)] = EntitySelector(
            EntitySelectorConfig(domain="sensor", device_class="power")
        )

    if CONF_INVERTER_DC_POWER_ENTITY in defaults and defaults[CONF_INVERTER_DC_POWER_ENTITY]:
        schema[vol.Optional(CONF_INVERTER_DC_POWER_ENTITY, default=defaults[CONF_INVERTER_DC_POWER_ENTITY])] = EntitySelector(
            EntitySelectorConfig(domain="sensor", device_class="power")
        )
    else:
        schema[vol.Optional(CONF_INVERTER_DC_POWER_ENTITY)] = EntitySelector(
            EntitySelectorConfig(domain="sensor", device_class="power")
        )

    schema.update({
        vol.Optional(CONF_BANK_A_VOLTAGE_SCALE, default=defaults.get(CONF_BANK_A_VOLTAGE_SCALE, 1.0)): NumberSelector(
            NumberSelectorConfig(min=0.1, max=10.0, step=0.1)
        ),
        vol.Optional(CONF_BANK_B_VOLTAGE_SCALE, default=defaults.get(CONF_BANK_B_VOLTAGE_SCALE, 1.0)): NumberSelector(
            NumberSelectorConfig(min=0.1, max=10.0, step=0.1)
        ),
        vol.Optional(CONF_FALLBACK_INTERVAL_S, default=defaults.get(CONF_FALLBACK_INTERVAL_S, 30)): NumberSelector(
            NumberSelectorConfig(step=1)
        ),
    })

    return schema


class BatterySocConfigFlow(config_entries.ConfigFlow, domain=DOMAIN):
    """Config flow for battery_soc."""

    VERSION = 1

    def __init__(self):
        """Initialize config flow."""
        super().__init__()
        self._data = {}

    async def async_step_user(self, user_input=None):
        """Handle the user step."""
        errors = {}

        if user_input is not None:
            # Check if topology is series and bank_b_voltage_entity is missing
            topology = user_input.get("topology", "parallel")
            bank_b_voltage_entity = user_input.get(CONF_BANK_B_VOLTAGE_ENTITY, "").strip()

            if topology == "series" and not bank_b_voltage_entity:
                # Re-show form with error
                schema = vol.Schema(self._get_user_schema_dict())
                return self.async_show_form(
                    step_id="user",
                    data_schema=schema,
                    errors={"base": "bank_b_voltage_required"},
                )

            # Validate using SocParams
            try:
                params = SocParams.from_dict(user_input)
                params.validate()
            except ValueError as e:
                errors["base"] = str(e)[:100]

            if not errors:
                # Unique ID check
                await self.async_set_unique_id(
                    slugify(user_input.get("name", ""))
                )
                self._abort_if_unique_id_configured()

                # Ensure empty optional fields are removed or have proper defaults
                if not user_input.get(CONF_BANK_B_VOLTAGE_ENTITY, "").strip():
                    user_input[CONF_BANK_B_VOLTAGE_ENTITY] = ""
                if not user_input.get(CONF_CHARGER_DC_POWER_ENTITY, "").strip():
                    user_input[CONF_CHARGER_DC_POWER_ENTITY] = ""
                if not user_input.get(CONF_INVERTER_DC_POWER_ENTITY, "").strip():
                    user_input[CONF_INVERTER_DC_POWER_ENTITY] = ""

                # Stash data and go to advanced
                self._data = user_input
                return await self.async_step_advanced()
            else:
                # Re-show form with errors
                schema = vol.Schema(self._get_user_schema_dict())
                return self.async_show_form(
                    step_id="user",
                    data_schema=schema,
                    errors=errors,
                )

        # Show schema
        schema = vol.Schema(self._get_user_schema_dict())
        return self.async_show_form(
            step_id="user",
            data_schema=schema,
        )

    def _get_user_schema_dict(self):
        """Get the user step schema dictionary."""
        schema = {
            vol.Required("name"): TextSelector(),
            vol.Required("topology", default="parallel"): SelectSelector(
                SelectSelectorConfig(options=["parallel", "series"])
            ),
            vol.Required(CONF_CHARGER_POWER_ENTITY): EntitySelector(
                EntitySelectorConfig(domain="sensor", device_class="power")
            ),
            vol.Required(CONF_INVERTER_POWER_ENTITY): EntitySelector(
                EntitySelectorConfig(domain="sensor", device_class="power")
            ),
            vol.Required(CONF_BANK_A_VOLTAGE_ENTITY): EntitySelector(
                EntitySelectorConfig(domain="sensor", device_class="voltage")
            ),
        }

        # Always include bank_b_voltage_entity as optional; validation logic checks if required
        schema[vol.Optional(CONF_BANK_B_VOLTAGE_ENTITY)] = EntitySelector(
            EntitySelectorConfig(domain="sensor", device_class="voltage")
        )

        schema.update(
            {
                vol.Optional(CONF_CHARGER_DC_POWER_ENTITY): (
                    EntitySelector(
                        EntitySelectorConfig(
                            domain="sensor", device_class="power"
                        )
                    )
                ),
                vol.Optional(CONF_INVERTER_DC_POWER_ENTITY): (
                    EntitySelector(
                        EntitySelectorConfig(
                            domain="sensor", device_class="power"
                        )
                    )
                ),
                vol.Optional(
                    CONF_BANK_A_VOLTAGE_SCALE, default=1.0
                ): NumberSelector(
                    NumberSelectorConfig(min=0.1, max=10.0, step=0.1)
                ),
                vol.Optional(
                    CONF_BANK_B_VOLTAGE_SCALE, default=1.0
                ): NumberSelector(
                    NumberSelectorConfig(min=0.1, max=10.0, step=0.1)
                ),
                vol.Optional(
                    "bank_a_capacity_ah", default=100
                ): NumberSelector(
                    NumberSelectorConfig(min=1, max=10000, step=1)
                ),
                vol.Optional(
                    "bank_b_capacity_ah", default=100
                ): NumberSelector(
                    NumberSelectorConfig(min=1, max=10000, step=1)
                ),
                vol.Optional(
                    "bank_a_cell_count", default=8
                ): NumberSelector(
                    NumberSelectorConfig(min=1, max=100, step=1)
                ),
                vol.Optional(
                    "bank_b_cell_count", default=8
                ): NumberSelector(
                    NumberSelectorConfig(min=1, max=100, step=1)
                ),
                vol.Optional("bank_b_enabled", default=True): BooleanSelector(),
                vol.Optional("battery_chemistry", default="lifepo4"): TextSelector(),
                vol.Optional(
                    "soc_curve", default="generic_lifepo4"
                ): SelectSelector(
                    SelectSelectorConfig(options=sorted(SOC_CURVES.keys()))
                ),
            }
        )

        return schema

    async def async_step_advanced(self, user_input=None):
        """Handle the advanced configuration step."""
        errors = {}

        if user_input is not None:
            # Merge user input with stored data
            full = {**self._data, **user_input}

            # Validate using params_from_config
            try:
                params_from_config(full)
            except ValueError as exc:
                errors["base"] = str(exc)[:100]

            if not errors:
                # Create entry with all data
                return self.async_create_entry(
                    title=self._data["name"],
                    data=full,
                )
            else:
                # Re-show form with errors
                schema = vol.Schema(self._get_advanced_schema_dict())
                return self.async_show_form(
                    step_id="advanced",
                    data_schema=schema,
                    errors=errors,
                )

        # Show form with defaults
        schema = vol.Schema(self._get_advanced_schema_dict())
        return self.async_show_form(
            step_id="advanced",
            data_schema=schema,
        )

    def _get_advanced_schema_dict(self):
        """Get the advanced step schema dictionary."""
        return _advanced_schema_dict({})

    @staticmethod
    @callback
    def async_get_options_flow(config_entry):
        """Return the options flow handler."""
        return BatterySocOptionsFlow()


class BatterySocOptionsFlow(config_entries.OptionsFlow):
    """Options flow for battery_soc."""

    def __init__(self):
        """Initialize options flow."""
        self._collected = {}

    async def async_step_init(self, user_input=None):
        """Handle the options init step (sources)."""
        errors = {}

        if user_input is not None:
            # Stash sources and go to tunables
            self._collected = user_input
            return await self.async_step_tunables()

        # Pre-fill from entry.data + entry.options
        defaults = {**self.config_entry.data, **self.config_entry.options}
        schema = vol.Schema(_sources_schema_dict(defaults))
        return self.async_show_form(
            step_id="init",
            data_schema=schema,
        )

    async def async_step_tunables(self, user_input=None):
        """Handle the tunables/advanced configuration step."""
        errors = {}

        if user_input is not None:
            # Merge collected sources with tunables, excluding fallback_interval_s from tunables
            # (it's already set in sources step)
            tunables_input = {k: v for k, v in user_input.items() if k != CONF_FALLBACK_INTERVAL_S}
            merged = {**self._collected, **tunables_input}

            # Validate using params_from_config
            try:
                params_from_config({**self.config_entry.data, **merged})
            except ValueError as exc:
                errors["base"] = str(exc)[:100]

            if not errors:
                # Create entry with merged options
                return self.async_create_entry(
                    title="",
                    data=merged,
                )
            else:
                # Re-show form with errors
                schema = vol.Schema(_advanced_schema_dict(user_input))
                return self.async_show_form(
                    step_id="tunables",
                    data_schema=schema,
                    errors=errors,
                )

        # Pre-fill from entry.data + entry.options
        defaults = {**self.config_entry.data, **self.config_entry.options}
        schema = vol.Schema(_advanced_schema_dict(defaults))
        return self.async_show_form(
            step_id="tunables",
            data_schema=schema,
        )
