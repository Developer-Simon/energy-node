"""energy-node: Home Assistant als Verlaufslieferant und Panel des Dashboards."""
from __future__ import annotations

from functools import partial
from pathlib import Path

import aiohttp
from homeassistant.components import frontend, panel_custom
from homeassistant.components.http import StaticPathConfig
from homeassistant.config_entries import ConfigEntry
from homeassistant.core import HomeAssistant
from homeassistant.helpers import config_validation as cv
from homeassistant.helpers.aiohttp_client import async_create_clientsession
from homeassistant.helpers.typing import ConfigType

from .client import DashboardClient
from .const import (
    CONF_HISTORY, CONF_PANEL, CONF_PANEL_ICON, CONF_PANEL_TITLE, CONF_URL, CONF_VERIFY_SSL, DOMAIN, PANEL_ADMINS,
    PANEL_ALL, PANEL_OFF, PANEL_ELEMENT, PANEL_ICON, PANEL_JS_VERSION, PANEL_TITLE, STATIC_URL, panel_url_path,
)
from .panel_access import PanelSessions
from .peer import HistoryPeer
from .runtime import EnergyNodeData
from .series_map import resolve
from .source import async_rows
from .views import DashboardProxyView, PanelSessionView

CONFIG_SCHEMA = cv.config_entry_only_config_schema(DOMAIN)


async def async_setup(hass: HomeAssistant, config: ConfigType) -> bool:
    # Views lassen sich nicht abmelden. Sie werden deshalb einmal je
    # HA-Start angemeldet und suchen sich ihren Eintrag pro Anfrage.
    hass.http.register_view(PanelSessionView())
    hass.http.register_view(DashboardProxyView())
    await hass.http.async_register_static_paths(
        [StaticPathConfig(STATIC_URL, str(Path(__file__).parent / "www"), False)]
    )
    return True


async def async_setup_entry(hass: HomeAssistant, entry: ConfigEntry) -> bool:
    # Eine eigene Sitzung je Eintrag. Cookies fuehrt der DashboardClient von
    # Hand. DummyCookieJar ist hier Pflicht: der Panel-Proxy nutzt dieselbe
    # Sitzung, und ein echter Jar wuerde das Admin-Cookie eines Browsers fuer
    # alle anderen aufheben. Schliessen muss man sie nicht: HA trennt eine
    # Sitzung aus async_create_clientsession beim Entladen des Eintrags.
    session = async_create_clientsession(
        hass, verify_ssl=entry.data.get(CONF_VERIFY_SSL, True), cookie_jar=aiohttp.DummyCookieJar()
    )
    client = DashboardClient(session, entry.data[CONF_URL])
    entry.runtime_data = EnergyNodeData(
        client=client,
        session=session,
        base_url=entry.data[CONF_URL],
        panel_mode=entry.options.get(CONF_PANEL, PANEL_ALL),
        panel_sessions=PanelSessions(),
    )
    if entry.options.get(CONF_HISTORY, True):
        peer = HistoryPeer(client, partial(resolve, hass), partial(async_rows, hass))
        # Hintergrundaufgaben eines Eintrags bricht Home Assistant beim
        # Entladen selbst ab.
        entry.async_create_background_task(hass, peer.run(), name=f"energy_node_companion {entry.data[CONF_URL]}")

    mode = entry.runtime_data.panel_mode
    if mode != PANEL_OFF:
        url_path = panel_url_path(entry.entry_id)
        await panel_custom.async_register_panel(
            hass,
            frontend_url_path=url_path,
            webcomponent_name=PANEL_ELEMENT,
            # Ein leeres Feld faellt auf den Standard zurueck.
            sidebar_title=entry.options.get(CONF_PANEL_TITLE) or PANEL_TITLE,
            sidebar_icon=entry.options.get(CONF_PANEL_ICON) or PANEL_ICON,
            module_url=f"{STATIC_URL}/energy-node-panel.js?v={PANEL_JS_VERSION}",
            config={"entry_id": entry.entry_id},
            require_admin=mode == PANEL_ADMINS,
        )
        entry.async_on_unload(lambda: frontend.async_remove_panel(hass, url_path, warn_if_unknown=False))
    return True


async def async_unload_entry(hass: HomeAssistant, entry: ConfigEntry) -> bool:
    return True
