"""energy-node: Home Assistant als Verlaufslieferant (und spaeter Panel) des Dashboards."""
from __future__ import annotations

from functools import partial

import aiohttp
from homeassistant.config_entries import ConfigEntry
from homeassistant.core import HomeAssistant
from homeassistant.helpers.aiohttp_client import async_create_clientsession

from .client import DashboardClient
from .const import CONF_URL, CONF_VERIFY_SSL
from .peer import HistoryPeer
from .series_map import resolve
from .source import async_rows


async def async_setup_entry(hass: HomeAssistant, entry: ConfigEntry) -> bool:
    # Eine eigene Sitzung je Eintrag. Cookies fuehrt der DashboardClient von
    # Hand: ein echter CookieJar wuerde das Gastcookie bei Hostnamen (*.ts.net)
    # zusaetzlich speichern und doppelt mitschicken.
    session = async_create_clientsession(
        hass, verify_ssl=entry.data.get(CONF_VERIFY_SSL, True), cookie_jar=aiohttp.DummyCookieJar()
    )
    peer = HistoryPeer(DashboardClient(session, entry.data[CONF_URL]), partial(resolve, hass), partial(async_rows, hass))
    # Hintergrundaufgaben eines Eintrags bricht Home Assistant beim Entladen
    # selbst ab.
    entry.async_create_background_task(hass, peer.run(), name=f"energy_node {entry.data[CONF_URL]}")
    return True


async def async_unload_entry(hass: HomeAssistant, entry: ConfigEntry) -> bool:
    return True
