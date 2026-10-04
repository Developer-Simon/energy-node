"""HTTP-Views: Panel-Sitzung ausstellen und das Dashboard durchreichen."""
from __future__ import annotations

import logging

import aiohttp
from aiohttp import web
from homeassistant.components.http import KEY_HASS, KEY_HASS_USER, HomeAssistantView
from homeassistant.config_entries import ConfigEntry, ConfigEntryState
from homeassistant.core import HomeAssistant

from .client import ExchangeError
from .const import DOMAIN, PANEL_COOKIE, PANEL_SESSION_TTL_S, proxy_prefix
from .panel_access import may_use_panel
from .proxy_headers import guest_cookie, is_logout, own_session, response_headers, upstream_headers
from .runtime import EnergyNodeData

_LOGGER = logging.getLogger(__name__)

# Ohne Gesamtfrist: der Ereignisstrom /api/v1/events laeuft unbegrenzt.
UPSTREAM_TIMEOUT = aiohttp.ClientTimeout(total=None, sock_connect=15)
# Nur Anfragen ohne Nebenwirkung werden nach einer neuen Gastanmeldung
# wiederholt.
RETRYABLE = ("GET", "HEAD")


def _loaded(hass: HomeAssistant, entry_id: str) -> ConfigEntry | None:
    entry = hass.config_entries.async_get_entry(entry_id)
    if entry is None or entry.domain != DOMAIN or entry.state is not ConfigEntryState.LOADED:
        return None
    return entry


class PanelSessionView(HomeAssistantView):
    """Stellt einem angemeldeten HA-Benutzer das Panel-Cookie aus."""

    url = "/api/energy_node_companion/panel_session/{entry_id}"
    name = "api:energy_node_companion:panel_session"
    requires_auth = True

    async def post(self, request: web.Request, entry_id: str) -> web.Response:
        entry = _loaded(request.app[KEY_HASS], entry_id)
        if entry is None:
            return self.json_message("unknown entry", 404)
        data: EnergyNodeData = entry.runtime_data
        user = request[KEY_HASS_USER]
        if not may_use_panel(user, data.panel_mode):
            return self.json_message("panel not allowed", 403)
        prefix = proxy_prefix(entry_id)
        response = self.json({"path": f"{prefix}/", "expires_in": PANEL_SESSION_TTL_S})
        response.set_cookie(
            PANEL_COOKIE,
            data.panel_sessions.issue(user.id),
            path=f"{prefix}/",
            max_age=PANEL_SESSION_TTL_S,
            httponly=True,
            samesite="Strict",
            secure=request.secure,
        )
        return response


class DashboardProxyView(HomeAssistantView):
    """Reicht das Dashboard unter dem Proxy-Pfad des Eintrags durch."""

    url = "/api/energy_node_companion/proxy/{entry_id}/{path:.*}"
    name = "api:energy_node_companion:proxy"
    # Ein iframe schickt keinen HA-Token. Statt dessen gilt das Panel-Cookie.
    requires_auth = False

    async def _handle(self, request: web.Request, entry_id: str, path: str) -> web.StreamResponse:
        hass = request.app[KEY_HASS]
        entry = _loaded(hass, entry_id)
        if entry is None:
            return web.Response(status=404)
        data: EnergyNodeData = entry.runtime_data
        user_id = data.panel_sessions.validate(request.cookies.get(PANEL_COOKIE, ""))
        user = await hass.auth.async_get_user(user_id) if user_id else None
        if user is None or not user.is_active or not may_use_panel(user, data.panel_mode):
            return web.Response(status=401, text="Panel session missing or expired")

        secure = request.scheme == "https"
        own = own_session(request.headers.get("Cookie", ""), secure)
        if own is None and request.method == "POST" and is_logout(path):
            # Die geteilte Gastsitzung gehoert der Integration. Ein Abmelden
            # im Panel darf sie nicht beenden, sonst braucht der Peer eine neue
            # Gastanmeldung (SD-Schreibvorgang).
            return web.json_response({"status": "logged_out"})

        body = await request.read() if request.body_exists else None
        url = f"{data.base_url}/{path}"
        try:
            token = "" if own else await data.client.ensure_session()
            upstream = await self._send(data, request, url, body, own or guest_cookie(token, secure))
            if upstream.status == 401 and own is None and request.method in RETRYABLE:
                # Dashboard neu gestartet oder Gastsitzung abgelaufen.
                upstream.release()
                data.client.drop_session(token)
                token = await data.client.ensure_session()
                upstream = await self._send(data, request, url, body, guest_cookie(token, secure))
        except (aiohttp.ClientError, TimeoutError, ExchangeError) as err:
            _LOGGER.debug("Dashboard %s not reachable: %s", data.base_url, err)
            return web.Response(status=502, text="Dashboard not reachable")
        return await self._relay(request, upstream)

    get = _handle
    post = _handle
    put = _handle
    delete = _handle
    patch = _handle
    head = _handle

    async def _send(
        self, data: EnergyNodeData, request: web.Request, url: str, body: bytes | None, cookie: str
    ) -> aiohttp.ClientResponse:
        headers = upstream_headers(
            request.headers,
            scheme=request.scheme,
            host=request.host,
            remote=request.remote,
            prefix=proxy_prefix(request.match_info["entry_id"]),
            session_cookie=cookie,
        )
        return await data.session.request(
            request.method,
            url,
            headers=headers,
            params=request.query,
            data=body,
            allow_redirects=False,
            timeout=UPSTREAM_TIMEOUT,
        )

    async def _relay(self, request: web.Request, upstream: aiohttp.ClientResponse) -> web.StreamResponse:
        headers = response_headers(upstream.headers)
        async with upstream:
            if request.method == "HEAD" or upstream.status in (204, 304) or upstream.status < 200:
                return web.Response(status=upstream.status, headers=headers)
            response = web.StreamResponse(status=upstream.status, headers=headers)
            await response.prepare(request)
            try:
                # iter_any gibt jeden Block sofort weiter. Der Ereignisstrom
                # darf nicht gepuffert werden, sonst stehen die Live-Werte.
                async for chunk in upstream.content.iter_any():
                    await response.write(chunk)
            except (aiohttp.ClientError, ConnectionResetError) as err:
                _LOGGER.debug("Proxy stream ended: %s", err)
            return response
