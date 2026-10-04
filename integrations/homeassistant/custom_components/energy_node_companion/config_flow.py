"""Ein Formular: Adresse des Dashboards und ob das Zertifikat geprueft wird."""
from __future__ import annotations

from urllib.parse import urlparse

import aiohttp
import voluptuous as vol
from homeassistant.config_entries import ConfigEntry, ConfigFlow, ConfigFlowResult, OptionsFlowWithReload
from homeassistant.core import HomeAssistant, callback
from homeassistant.helpers import device_registry as dr
from homeassistant.helpers.aiohttp_client import async_get_clientsession
from homeassistant.helpers.selector import SelectSelector, SelectSelectorConfig, SelectSelectorMode

from .client import AuthRequired, DashboardClient, ExchangeError
from .const import CONF_PANEL, CONF_URL, CONF_VERIFY_SSL, DOMAIN, PANEL_ALL, PANEL_MODES, PROTOCOL

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


class EnergyNodeOptionsFlow(OptionsFlowWithReload):
    """Wer das Dashboard in der Seitenleiste sieht. Speichern laedt den Eintrag neu."""

    async def async_step_init(self, user_input: dict | None = None) -> ConfigFlowResult:
        if user_input is not None:
            return self.async_create_entry(data=user_input)
        current = self.config_entry.options.get(CONF_PANEL, PANEL_ALL)
        schema = vol.Schema({
            vol.Required(CONF_PANEL, default=current): SelectSelector(SelectSelectorConfig(
                options=list(PANEL_MODES), translation_key="panel", mode=SelectSelectorMode.LIST,
            )),
        })
        return self.async_show_form(step_id="init", data_schema=schema)
