"""energy-node: Home Assistant als Verlaufslieferant und Panel des Dashboards."""
from __future__ import annotations

import logging
from collections.abc import Callable
from functools import partial
from pathlib import Path

import aiohttp
from homeassistant.components import frontend, panel_custom
from homeassistant.components.http import StaticPathConfig
from homeassistant.config_entries import ConfigEntry
from homeassistant.core import Event, HomeAssistant, callback
from homeassistant.helpers import config_validation as cv
from homeassistant.helpers.aiohttp_client import async_create_clientsession
from homeassistant.helpers.typing import ConfigType

from .client import DashboardClient
from .const import (
    CONF_FINGERPRINT, CONF_HISTORY, CONF_PANEL, CONF_PANEL_ICON, CONF_PANEL_TITLE, CONF_STATS, CONF_TOKEN, CONF_URL,
    CONF_VERIFY_SSL, DOMAIN, PANEL_ADMINS, PANEL_ALL, PANEL_OFF, PANEL_ELEMENT, PANEL_ICON, PANEL_JS_VERSION,
    PANEL_TITLE, STATIC_URL, panel_url_path,
)
from .link import CompanionLink
from .panel_access import PanelSessions
from .pairing import companion_base
from .peer import HistoryPeer
from .runtime import EnergyNodeData
from .series_map import resolve
from .source import async_rows
from .stats import StatsOffer, async_hour_rows, resolve_stats
from .views import DashboardProxyView, PanelSessionView

CONFIG_SCHEMA = cv.config_entry_only_config_schema(DOMAIN)
_LOGGER = logging.getLogger(__name__)


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
    # Pruefkanal mit eigener Sitzung: der Peer-Client fuehrt Cookies von Hand,
    # der Strom braucht nur das Token und den gepinnten Fingerabdruck.
    link = None
    if entry.data.get(CONF_TOKEN):
        link_session = async_create_clientsession(hass, verify_ssl=False, cookie_jar=aiohttp.DummyCookieJar())
        link = CompanionLink(
            hass,
            link_session,
            companion_base(entry.data[CONF_URL]),
            entry.data[CONF_TOKEN],
            entry.data[CONF_FINGERPRINT],
            on_unpaired=_on_unpaired(hass, entry),
        )
    entry.runtime_data = EnergyNodeData(
        client=client,
        session=session,
        base_url=entry.data[CONF_URL],
        panel_mode=entry.options.get(CONF_PANEL, PANEL_ALL),
        panel_sessions=PanelSessions(),
        link=link,
    )
    if link is not None:
        entry.async_create_background_task(hass, link.run(), name=f"energy_node_companion link {entry.data[CONF_URL]}")
        # Benutzermeldungen gehen nur an gekoppelte Eintraege.
        listener = _user_listener(hass, entry, link)
        entry.async_on_unload(hass.bus.async_listen("user_updated", listener))
        entry.async_on_unload(hass.bus.async_listen("user_removed", listener))
    history = entry.options.get(CONF_HISTORY, True)
    stats = None
    if entry.options.get(CONF_STATS, True):
        options = dict(entry.options)
        stats = StatsOffer(partial(resolve_stats, hass, options), partial(async_hour_rows, hass))
    if history or stats is not None:
        peer = HistoryPeer(client, partial(resolve, hass), partial(async_rows, hass), stats=stats, history=history)
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


def _on_unpaired(hass: HomeAssistant, entry: ConfigEntry) -> Callable[[], None]:
    """Das Dashboard hat die Kopplung verworfen: Token weg, Eintrag laeuft als Gast weiter."""

    def callback() -> None:
        _LOGGER.warning("Dashboard no longer accepts the pairing, falling back to guest access")
        data = {key: value for key, value in entry.data.items() if key not in (CONF_TOKEN, CONF_FINGERPRINT)}
        hass.config_entries.async_update_entry(entry, data=data)
        hass.config_entries.async_schedule_reload(entry.entry_id)

    return callback


def _user_listener(hass: HomeAssistant, entry: ConfigEntry, link: CompanionLink) -> Callable[[Event], None]:
    """Meldet jede Aenderung eines HA-Benutzers an das Dashboard."""

    @callback
    def on_event(event: Event) -> None:
        user_id = event.data.get("user_id")
        if user_id:
            entry.async_create_background_task(
                hass, link.async_user_changed(user_id), name="energy_node_companion user changed"
            )

    return on_event


async def async_unload_entry(hass: HomeAssistant, entry: ConfigEntry) -> bool:
    return True
