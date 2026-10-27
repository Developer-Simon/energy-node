"""HTTP-Views: Panel-Sitzung ausstellen und das Dashboard durchreichen."""
from __future__ import annotations

import json
import logging

import aiohttp
from aiohttp import web
from homeassistant.components.http import KEY_HASS, KEY_HASS_USER, HomeAssistantView
from homeassistant.config_entries import ConfigEntry, ConfigEntryState
from homeassistant.core import HomeAssistant

from .client import ExchangeError
from .const import DOMAIN, PANEL_COOKIE, PANEL_SESSION_TTL_S, proxy_prefix
from .link import CompanionLink
from .panel_access import may_use_panel
from .proxy_headers import guest_cookie, is_logout, own_session, response_headers, upstream_headers
from .runtime import EnergyNodeData

_LOGGER = logging.getLogger(__name__)

# Ohne Gesamtfrist: der Ereignisstrom /api/v1/events laeuft unbegrenzt.
UPSTREAM_TIMEOUT = aiohttp.ClientTimeout(total=None, sock_connect=15)
# Nur Anfragen ohne Nebenwirkung werden nach einer neuen Gastanmeldung
# wiederholt.
RETRYABLE = ("GET", "HEAD")
# Antwortcode des Companion-Tors, wenn das Kopplungstoken nicht passt.
UNPAIRED_CODE = "companion_unauthorized"


def _loaded(hass: HomeAssistant, entry_id: str) -> ConfigEntry | None:
    entry = hass.config_entries.async_get_entry(entry_id)
    if entry is None or entry.domain != DOMAIN or entry.state is not ConfigEntryState.LOADED:
        return None
    return entry


def _error_code(payload: bytes) -> str | None:
    try:
        answer = json.loads(payload)
    except ValueError:
        return None
    return answer.get("code") if isinstance(answer, dict) else None


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
        try:
            if data.link is None:
                upstream = await self._guest(data, request, path, body, own, secure)
            else:
                upstream = await self._paired(hass, data, request, user, path, body, own, secure)
        except (aiohttp.ClientError, TimeoutError, ExchangeError) as err:
            _LOGGER.debug("Dashboard %s not reachable: %s", data.base_url, err)
            return web.Response(status=502, text="Dashboard not reachable")
        if isinstance(upstream, web.StreamResponse):
            return upstream
        return await self._relay(request, upstream)

    get = _handle
    post = _handle
    put = _handle
    delete = _handle
    patch = _handle
    head = _handle

    async def _guest(
        self,
        data: EnergyNodeData,
        request: web.Request,
        path: str,
        body: bytes | None,
        own: str | None,
        secure: bool,
    ) -> aiohttp.ClientResponse:
        """Ohne Kopplung: eigene Sitzung des Browsers oder die geteilte Gastsitzung ueber HTTP."""
        url = f"{data.base_url}/{path}"
        token = "" if own else await data.client.ensure_session()
        upstream = await self._send(data.session, request, url, body, own or guest_cookie(token, secure))
        if upstream.status == 401 and own is None and request.method in RETRYABLE:
            # Dashboard neu gestartet oder Gastsitzung abgelaufen.
            upstream.release()
            data.client.drop_session(token)
            token = await data.client.ensure_session()
            upstream = await self._send(data.session, request, url, body, guest_cookie(token, secure))
        return upstream

    async def _paired(
        self,
        hass: HomeAssistant,
        data: EnergyNodeData,
        request: web.Request,
        user,
        path: str,
        body: bytes | None,
        own: str | None,
        secure: bool,
    ) -> aiohttp.ClientResponse | web.Response:
        """Mit Kopplung: Companion-Port, Kopplungstoken als Bearer, Sitzung als Cookie.

        Reihenfolge: eigene Sitzung des Browsers, dann die Sitzung des HA-Benutzers,
        zuletzt die Gastsitzung. Ein 401 des Tors heisst, die Kopplung ist vorbei.
        """
        link = data.link
        assert link is not None
        url = f"{link.base}/{path}"
        retryable = request.method in RETRYABLE
        if own is not None:
            response, _ = await self._answer(link, await self._send_paired(request, url, body, link, own))
            return response

        user_token = await self._user_token(hass, link, user)
        if user_token is not None:
            response, expired = await self._answer(
                link, await self._send_paired(request, url, body, link, guest_cookie(user_token, secure))
            )
            if expired and retryable:
                # Sitzung abgelaufen: einmal neu anmelden und wiederholen.
                link.user_sessions.drop(user.id, user_token)
                user_token = await self._user_token(hass, link, user)
                if user_token is not None:
                    response, _ = await self._answer(
                        link, await self._send_paired(request, url, body, link, guest_cookie(user_token, secure))
                    )
            if user_token is not None:
                return response

        # Gesperrtes Konto oder keine Sitzung des Benutzers: geteilte Gastsitzung.
        guest = await data.client.ensure_session()
        response, expired = await self._answer(
            link, await self._send_paired(request, url, body, link, guest_cookie(guest, secure))
        )
        if expired and retryable:
            data.client.drop_session(guest)
            guest = await data.client.ensure_session()
            response, _ = await self._answer(
                link, await self._send_paired(request, url, body, link, guest_cookie(guest, secure))
            )
        return response

    async def _user_token(self, hass: HomeAssistant, link: CompanionLink, user) -> str | None:
        """Sitzung des HA-Benutzers. None bei gesperrtem Konto oder wenn der Companion-Port nicht antwortet."""
        try:
            return await link.user_sessions.async_get(hass, user)
        except (aiohttp.ClientError, TimeoutError, ExchangeError) as err:
            _LOGGER.debug("companion session not available: %s", err)
            return None

    async def _send_paired(
        self, request: web.Request, url: str, body: bytes | None, link: CompanionLink, cookie: str
    ) -> aiohttp.ClientResponse:
        return await self._send(
            link.session, request, url, body, cookie, authorization=link.headers()["Authorization"], ssl=link.ssl
        )

    async def _answer(
        self, link: CompanionLink, upstream: aiohttp.ClientResponse
    ) -> tuple[aiohttp.ClientResponse | web.Response, bool]:
        """Ob die Antwort weitergereicht wird, und ob eine Sitzung abgelaufen ist (401 ohne Tor-Code).

        Ein 401 mit Tor-Code heisst, das Kopplungstoken passt nicht mehr: die Kopplung ist vorbei.
        """
        if upstream.status != 401:
            return upstream, False
        payload = await upstream.read()
        upstream.release()
        if _error_code(payload) == UNPAIRED_CODE:
            link.report_unpaired()
            return web.Response(status=502, text="Dashboard not reachable"), False
        return web.Response(status=401, headers=response_headers(upstream.headers), body=payload), True

    async def _send(
        self,
        session: aiohttp.ClientSession,
        request: web.Request,
        url: str,
        body: bytes | None,
        cookie: str,
        *,
        authorization: str | None = None,
        ssl=None,
    ) -> aiohttp.ClientResponse:
        headers = upstream_headers(
            request.headers,
            scheme=request.scheme,
            host=request.host,
            remote=request.remote,
            prefix=proxy_prefix(request.match_info["entry_id"]),
            session_cookie=cookie,
            authorization=authorization,
        )
        extra = {} if ssl is None else {"ssl": ssl}
        return await session.request(
            request.method,
            url,
            headers=headers,
            params=request.query,
            data=body,
            allow_redirects=False,
            timeout=UPSTREAM_TIMEOUT,
            **extra,
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
