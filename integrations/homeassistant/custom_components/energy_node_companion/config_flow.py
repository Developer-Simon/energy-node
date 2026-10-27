"""Ein Formular: Adresse des Dashboards und ob das Zertifikat geprueft wird."""
from __future__ import annotations

from collections.abc import Mapping
from urllib.parse import urlparse

import aiohttp
import voluptuous as vol
from homeassistant.config_entries import ConfigEntry, ConfigFlow, ConfigFlowResult, OptionsFlowWithReload
from homeassistant.core import HomeAssistant, callback
from homeassistant.helpers import device_registry as dr
from homeassistant.helpers.aiohttp_client import async_get_clientsession
from homeassistant.helpers.selector import (
    EntitySelector, EntitySelectorConfig, IconSelector, SelectSelector, SelectSelectorConfig, SelectSelectorMode,
    TextSelector,
)

from .client import AuthRequired, DashboardClient, ExchangeError
from .const import (
    CONF_FINGERPRINT, CONF_HISTORY, CONF_PAIRING_CODE, CONF_PANEL, CONF_PANEL_ICON, CONF_PANEL_TITLE, CONF_STATS,
    CONF_TOKEN, CONF_URL, CONF_VERIFY_SSL, COUNTER_PREFIX, DOMAIN, PANEL_ALL, PANEL_ICON, PANEL_MODES, PANEL_OFF,
    PANEL_TITLE, PROTOCOL, STATS_ROLES, counter_key,
)
from .pairing import PairingFailed, async_fetch_fingerprint, async_pair, companion_base

# Das Geraet, das das Dashboard per MQTT Discovery anlegt
# (energydiscovery.DeviceIdentifier). Sein configuration_url zeigt auf Caddy
# (Port 80). Die Integration spricht dagegen den Port des Dashboards direkt
# an: Caddy ersetzt ein X-Forwarded-Proto, das der spaetere Panel-Proxy
# fuer die Anmeldung ueber HTTPS weiterreichen muss.
MQTT_DEVICE = ("mqtt", "energy_node")
DEFAULT_PORT = 8080
# hassfest verbietet URLs in strings.json, das Beispiel kommt als Platzhalter.
EXAMPLE_URL = "http://energy-node.tail1234.ts.net:8080"

SCHEMA = vol.Schema({
    vol.Required(CONF_URL): str,
    vol.Required(CONF_VERIFY_SSL, default=True): bool,
})


def suggested_url(hass: HomeAssistant) -> str | None:
    """Tailscale-Name aus dem Geraete-Link, mit dem Standard-Port des Dashboards."""
    device = dr.async_get(hass).async_get_device(identifiers={MQTT_DEVICE})
    if device is None or not device.configuration_url:
        return None
    host = urlparse(device.configuration_url).hostname
    return f"http://{host}:{DEFAULT_PORT}" if host else None


def _normalise(user_input: dict) -> tuple[str, bool]:
    return str(user_input[CONF_URL]).strip().rstrip("/"), bool(user_input[CONF_VERIFY_SSL])


def _title(url: str) -> str:
    return urlparse(url).netloc or url


async def _validate(hass: HomeAssistant, url: str, verify_ssl: bool) -> dict[str, str]:
    if not url.startswith(("http://", "https://")):
        return {CONF_URL: "invalid_url"}
    client = DashboardClient(async_get_clientsession(hass, verify_ssl=verify_ssl), url)
    try:
        await client.login()
        announcement = await client.announcement()
    except (aiohttp.ClientError, TimeoutError, AuthRequired, ExchangeError, ValueError):
        return {"base": "cannot_connect"}
    if announcement.get("protocol") != PROTOCOL:
        return {"base": "wrong_protocol"}
    if "series" not in announcement:
        return {"base": "dashboard_too_old"}
    return {}


class EnergyNodeConfigFlow(ConfigFlow, domain=DOMAIN):
    """Ein Eintrag je Dashboard."""

    VERSION = 1

    @staticmethod
    @callback
    def async_get_options_flow(config_entry: ConfigEntry) -> EnergyNodeOptionsFlow:
        return EnergyNodeOptionsFlow()

    async def async_step_user(self, user_input: dict | None = None) -> ConfigFlowResult:
        errors: dict[str, str] = {}
        if user_input is not None:
            url, verify_ssl = _normalise(user_input)
            await self.async_set_unique_id(url)
            self._abort_if_unique_id_configured()
            errors = await _validate(self.hass, url, verify_ssl)
            if not errors:
                return self.async_create_entry(title=_title(url), data={CONF_URL: url, CONF_VERIFY_SSL: verify_ssl})
        suggestion = user_input
        if suggestion is None:
            suggested = suggested_url(self.hass)
            suggestion = {CONF_URL: suggested} if suggested else {}
        return self.async_show_form(
            step_id="user",
            data_schema=self.add_suggested_values_to_schema(SCHEMA, suggestion),
            errors=errors,
            description_placeholders={"example_url": EXAMPLE_URL},
        )

    async def async_step_reconfigure(self, user_input: dict | None = None) -> ConfigFlowResult:
        """Adresse oder Port aendern. Die entry_id bleibt, der Panel-Pfad also auch."""
        entry = self._get_reconfigure_entry()
        errors: dict[str, str] = {}
        if user_input is not None:
            url, verify_ssl = _normalise(user_input)
            if url != entry.data[CONF_URL]:
                self._async_abort_entries_match({CONF_URL: url})
            errors = await _validate(self.hass, url, verify_ssl)
            if not errors:
                return self.async_update_reload_and_abort(
                    entry, unique_id=url, title=_title(url), data={CONF_URL: url, CONF_VERIFY_SSL: verify_ssl}
                )
        return self.async_show_form(
            step_id="reconfigure",
            data_schema=self.add_suggested_values_to_schema(SCHEMA, user_input or dict(entry.data)),
            errors=errors,
        )


OPTIONS_SCHEMA = vol.Schema({
    vol.Required(CONF_HISTORY, default=True): bool,
    vol.Required(CONF_STATS, default=True): bool,
    vol.Required(CONF_PANEL, default=PANEL_ALL): SelectSelector(SelectSelectorConfig(
        options=list(PANEL_MODES), translation_key="panel", mode=SelectSelectorMode.LIST,
    )),
    # Optional mit Vorschlag statt Standardwert: ein geleertes Feld fehlt
    # beim Speichern, und das Panel nimmt wieder Name und Symbol ab Werk.
    vol.Optional(CONF_PANEL_TITLE): TextSelector(),
    vol.Optional(CONF_PANEL_ICON): IconSelector(),
    # Kopplungscode aus dem Dashboard. Gefuellt koppelt das Speichern neu.
    vol.Optional(CONF_PAIRING_CODE): TextSelector(),
})

# Ein Zaehler je Rolle, leer heisst Automatisch (Discovery-Sensor).
STATS_SCHEMA = vol.Schema({
    vol.Optional(counter_key(role)): EntitySelector(EntitySelectorConfig(domain="sensor", device_class="energy"))
    for role in STATS_ROLES
})


def _counters(options: Mapping) -> dict:
    return {key: value for key, value in options.items() if key.startswith(COUNTER_PREFIX) and value}


def _is_counter(hass: HomeAssistant, entity_id: str) -> bool:
    state = hass.states.get(entity_id)
    return state is not None and state.attributes.get("state_class") in ("total", "total_increasing")


class EnergyNodeOptionsFlow(OptionsFlowWithReload):
    """Verlaeufe, Energiestatistik, Panel. Speichern laedt den Eintrag neu."""

    _base: dict

    async def async_step_init(self, user_input: dict | None = None) -> ConfigFlowResult:
        errors: dict[str, str] = {}
        if user_input is not None:
            # Der Code dient nur zum Koppeln und landet nie in den Optionen.
            code = str(user_input.pop(CONF_PAIRING_CODE, "") or "").strip()
            if not user_input[CONF_HISTORY] and not user_input[CONF_STATS] and user_input[CONF_PANEL] == PANEL_OFF:
                errors["base"] = "nothing_enabled"
            elif code:
                errors = await self._async_pair(code)
            if not errors:
                if user_input[CONF_STATS]:
                    self._base = user_input
                    return await self.async_step_stats()
                # Zaehler bleiben erhalten, falls die Statistik spaeter wieder an geht.
                return self.async_create_entry(data={**_counters(self.config_entry.options), **user_input})
        current = {
            CONF_HISTORY: True, CONF_STATS: True, CONF_PANEL: PANEL_ALL,
            CONF_PANEL_TITLE: PANEL_TITLE, CONF_PANEL_ICON: PANEL_ICON,
            **self.config_entry.options,
        }
        return self.async_show_form(
            step_id="init",
            data_schema=self.add_suggested_values_to_schema(OPTIONS_SCHEMA, user_input or current),
            errors=errors,
        )

    async def _async_pair(self, code: str) -> dict[str, str]:
        """Koppelt mit dem Dashboard und speichert Token und Fingerabdruck im Eintrag."""
        base = companion_base(self.config_entry.data[CONF_URL])
        try:
            fingerprint = await async_fetch_fingerprint(self.hass, base)
            token = await async_pair(
                async_get_clientsession(self.hass, verify_ssl=False), base, code, fingerprint,
            )
        except PairingFailed as err:
            return {"pairing_code": "pairing_invalid" if err.reason == "invalid_code" else "pairing_failed"}
        self.hass.config_entries.async_update_entry(
            self.config_entry, data={**self.config_entry.data, CONF_TOKEN: token, CONF_FINGERPRINT: fingerprint},
        )
        return {}

    async def async_step_stats(self, user_input: dict | None = None) -> ConfigFlowResult:
        errors: dict[str, str] = {}
        if user_input is not None:
            counters = _counters(user_input)
            for key, entity_id in counters.items():
                if not _is_counter(self.hass, entity_id):
                    errors[key] = "not_a_counter"
            if not errors:
                return self.async_create_entry(data={**self._base, **counters})
        return self.async_show_form(
            step_id="stats",
            data_schema=self.add_suggested_values_to_schema(STATS_SCHEMA, user_input or _counters(self.config_entry.options)),
            errors=errors,
        )
