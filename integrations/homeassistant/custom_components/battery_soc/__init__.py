"""The battery_soc integration."""
from __future__ import annotations

from pathlib import Path

from homeassistant.components.frontend import add_extra_js_url
from homeassistant.components.http import StaticPathConfig
from homeassistant.config_entries import ConfigEntry
from homeassistant.core import HomeAssistant
from homeassistant.exceptions import ServiceValidationError
from homeassistant.helpers import config_validation as cv
from homeassistant.helpers.service import async_extract_config_entry_ids
import voluptuous as vol

from .const import DOMAIN, PLATFORMS, SERVICE_SET_SOC, SERVICE_APPLY_SUGGESTION, ATTR_STATE_OF_CHARGE, ATTR_BANK, FRONTEND_URL_BASE, FRONTEND_CARD_FILES
from .coordinator import BatterySocCoordinator
from .battery_soc_core import set_state_of_charge

# Service schema for set_state_of_charge
SET_SOC_SCHEMA = cv.make_entity_service_schema({
    vol.Required(ATTR_STATE_OF_CHARGE): vol.All(vol.Coerce(float), vol.Range(min=0, max=100)),
    vol.Optional(ATTR_BANK): vol.In(["a", "b"]),
})

# Service schema for apply_suggestion
APPLY_SUGGESTION_SCHEMA = vol.Schema({
    vol.Required("entry_id"): str,
    vol.Required("key"): vol.In(["inverter_dc_ac_efficiency", "internal_resistance_mohm_per_cell"]),
    vol.Optional("unit"): str,
})


def _make_set_soc_handler(hass: HomeAssistant):
    """Create a handler for the set_state_of_charge service."""
    async def _async_set_soc(call) -> None:
        """Handle the set_state_of_charge service call."""
        entry_ids = await async_extract_config_entry_ids(hass, call)
        for entry_id in entry_ids:
            coord = hass.data.get(DOMAIN, {}).get(entry_id)
            if coord is None:
                continue  # Target not ours

            # Determine unit_name based on topology
            if coord.params.topology == "parallel":
                unit_name = None
            else:  # series
                bank = call.data.get(ATTR_BANK)
                if not bank:
                    raise ServiceValidationError("bank is required for series topology")
                unit_name = f"bank_{bank}"

            # Set state of charge
            set_state_of_charge(
                coord.state,
                coord.params,
                call.data[ATTR_STATE_OF_CHARGE],
                unit_name=unit_name
            )
            coord._recalc()
            await coord._save()

    return _async_set_soc


def _make_apply_suggestion_handler(hass: HomeAssistant):
    """Create a handler for the apply_suggestion service."""
    async def _async_apply_suggestion(call) -> None:
        """Handle the apply_suggestion service call."""
        entry_id = call.data["entry_id"]
        key = call.data["key"]
        want_unit = call.data.get("unit")

        coordinator = hass.data.get(DOMAIN, {}).get(entry_id)
        if coordinator is None:
            raise ServiceValidationError(f"Unbekannter Eintrag: {entry_id}")

        pending = None
        for unit_name, block in (coordinator.data or {}).get("_tuning", {}).items():
            if want_unit and unit_name != want_unit:
                continue
            for s in block.get("suggestions", []):
                if s["key"] == key:
                    pending = s
                    break
        if pending is None:
            raise ServiceValidationError(
                f"Fuer '{key}' liegt derzeit kein offener Vorschlag vor."
            )

        entry = hass.config_entries.async_get_entry(entry_id)
        hass.config_entries.async_update_entry(
            entry, options={**entry.options, key: pending["suggested_value"]}
        )
        await hass.config_entries.async_reload(entry_id)

    return _async_apply_suggestion


async def _async_register_frontend(hass: HomeAssistant) -> None:
    """Serve the Lovelace card and load it into the frontend.

    Runs once per Home Assistant start, not once per config entry: a second
    registration of the same static path raises.
    """
    if hass.data.get(f"{DOMAIN}_frontend_registered"):
        return
    http = getattr(hass, "http", None)
    if http is None:
        # Die Lovelace-Karte ist Beiwerk - sie darf den Config-Entry nie
        # kippen. Ohne die http-Komponente (z. B. in Testinstanzen oder
        # minimalen Setups) bleibt die Karte einfach unregistriert.
        return

    www = Path(__file__).parent / "www"
    await http.async_register_static_paths(
        [StaticPathConfig(FRONTEND_URL_BASE, str(www), False)]
    )
    for name in FRONTEND_CARD_FILES:
        add_extra_js_url(hass, f"{FRONTEND_URL_BASE}/{name}")
    hass.data[f"{DOMAIN}_frontend_registered"] = True


async def async_setup_entry(hass: HomeAssistant, entry: ConfigEntry) -> bool:
    """Set up battery_soc from a config entry."""
    coord = BatterySocCoordinator(hass, entry)
    await coord.async_load()
    coord.async_start_listeners()
    hass.data.setdefault(DOMAIN, {})[entry.entry_id] = coord
    await hass.config_entries.async_forward_entry_setups(entry, PLATFORMS)
    entry.async_on_unload(entry.add_update_listener(async_reload_entry))

    # Register services if not already registered
    if not hass.services.has_service(DOMAIN, SERVICE_SET_SOC):
        hass.services.async_register(
            DOMAIN, SERVICE_SET_SOC, _make_set_soc_handler(hass), schema=SET_SOC_SCHEMA
        )
    if not hass.services.has_service(DOMAIN, SERVICE_APPLY_SUGGESTION):
        hass.services.async_register(
            DOMAIN, SERVICE_APPLY_SUGGESTION, _make_apply_suggestion_handler(hass), schema=APPLY_SUGGESTION_SCHEMA
        )

    await _async_register_frontend(hass)

    return True


async def async_unload_entry(hass: HomeAssistant, entry: ConfigEntry) -> bool:
    """Unload a config entry."""
    unloaded = await hass.config_entries.async_unload_platforms(entry, PLATFORMS)
    if unloaded:
        coord = hass.data[DOMAIN].pop(entry.entry_id)
        await coord.async_shutdown()

        # Unregister services if no more entries
        if not hass.data[DOMAIN]:
            hass.services.async_remove(DOMAIN, SERVICE_SET_SOC)
            hass.services.async_remove(DOMAIN, SERVICE_APPLY_SUGGESTION)

    return unloaded


async def async_reload_entry(hass: HomeAssistant, entry: ConfigEntry) -> None:
    """Reload a config entry."""
    await hass.config_entries.async_reload(entry.entry_id)
