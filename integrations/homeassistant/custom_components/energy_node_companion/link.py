"""Dauerhafte Verbindung zum Companion-Strom des Dashboards.

Ueber den Strom kommen Anmeldepruefungen ("verify"). Die Integration meldet
ausserdem Aenderungen an HA-Benutzern zurueck. Ein 401 heisst, das Dashboard
kennt das Token nicht mehr: die Kopplung ist dann vorbei.
"""
from __future__ import annotations

import asyncio
import logging
from collections.abc import Callable

import aiohttp
from homeassistant.core import HomeAssistant

from .client import CALL_TIMEOUT, STREAM_TIMEOUT, ExchangeError, iter_sse
from .const import RECONNECT_MAX_S, RECONNECT_MIN_S
from .ha_users import async_check_login, async_identity
from .pairing import pinned

_LOGGER = logging.getLogger(__name__)


class Unpaired(Exception):
    """Das Dashboard nimmt das Token nicht mehr an."""


class UserSessions:
    """Panel-Sitzung je HA-Benutzer, im Speicher und nur fuer die Laufzeit des Eintrags."""

    def __init__(self, link: CompanionLink) -> None:
        self._link = link
        self._tokens: dict[str, str] = {}
        self._locks: dict[str, asyncio.Lock] = {}

    async def async_get(self, hass: HomeAssistant, user) -> str | None:
        """Token des Benutzers, sonst eine neue Sitzung. None, wenn das Konto gesperrt ist."""
        token = self._tokens.get(user.id)
        if token is not None:
            return token
        lock = self._locks.setdefault(user.id, asyncio.Lock())
        async with lock:
            token = self._tokens.get(user.id)
            if token is None:
                token = await self._link.async_session(await async_identity(hass, user))
                if token is not None:
                    self._tokens[user.id] = token
            return token

    def drop(self, user_id: str, token: str) -> None:
        """Verwirft das Token, aber nur wenn es noch das aktuelle ist."""
        if self._tokens.get(user_id) == token:
            del self._tokens[user_id]


class CompanionLink:
    """Haelt den Pruefstrom offen und beantwortet, was darauf ankommt."""

    def __init__(
        self,
        hass: HomeAssistant,
        session: aiohttp.ClientSession,
        base: str,
        token: str,
        fingerprint: str,
        on_unpaired: Callable[[], None],
    ) -> None:
        self._hass = hass
        self._session = session
        self._base = base.rstrip("/")
        self._token = token
        self._fingerprint = fingerprint
        self._on_unpaired = on_unpaired
        self._unpaired = False
        self.user_sessions = UserSessions(self)

    @property
    def base(self) -> str:
        return self._base

    @property
    def session(self) -> aiohttp.ClientSession:
        """Sitzung fuer den Companion-Port. Der Fingerabdruck kommt pro Anfrage ueber ssl."""
        return self._session

    @property
    def ssl(self) -> aiohttp.Fingerprint:
        return pinned(self._fingerprint)

    def headers(self) -> dict[str, str]:
        return {"Authorization": f"Bearer {self._token}"}

    def report_unpaired(self) -> None:
        """Das Dashboard hat das Kopplungstoken abgelehnt."""
        self._unpair()

    async def run(self) -> None:
        """Liest den Strom und verbindet sich mit Backoff neu. Endet bei Unpaired."""
        delay = RECONNECT_MIN_S
        while True:
            try:
                await self._stream()
                delay = RECONNECT_MIN_S
            except Unpaired:
                self._unpair()
                return
            except asyncio.CancelledError:
                raise
            except (aiohttp.ClientError, TimeoutError, ExchangeError) as err:
                _LOGGER.debug("companion stream: %s", err)
            except Exception:  # noqa: BLE001 - die Schleife darf nie enden
                _LOGGER.exception("companion stream failed")
            await asyncio.sleep(delay)
            delay = min(delay * 2, RECONNECT_MAX_S)

    async def _stream(self) -> None:
        async with self._session.get(
            f"{self._base}/api/v1/companion/stream", headers=self.headers(), ssl=self.ssl, timeout=STREAM_TIMEOUT
        ) as response:
            if response.status == 401:
                raise Unpaired()
            if response.status != 200:
                raise ExchangeError(f"companion stream: HTTP {response.status}")
            async for name, payload in iter_sse(response):
                if name == "verify":
                    self._hass.async_create_background_task(self._verify(payload), name="energy_node_companion verify")
                if self._unpaired:
                    raise Unpaired()

    async def _verify(self, request: dict) -> None:
        try:
            result = await async_check_login(
                self._hass, str(request.get("username", "")), str(request.get("password", ""))
            )
        except Exception:  # noqa: BLE001 - das Dashboard wartet auf eine Antwort
            _LOGGER.exception("companion login check failed")
            result = {"ok": False}
        await self._post(f"verify/{request.get('id', '')}", result)

    async def async_user_changed(self, user_id: str) -> None:
        user = await self._hass.auth.async_get_user(user_id)
        if user is None:
            body = {"ha_user_id": user_id, "removed": True}
        else:
            body = {**await async_identity(self._hass, user), "active": user.is_active, "removed": False}
        await self._post("user-changed", body)

    async def async_session(self, identity: dict) -> str | None:
        """Panel-Sitzung fuer einen HA-Benutzer. None, wenn das Konto gesperrt ist."""
        async with self._session.post(
            f"{self._base}/api/v1/companion/session",
            json=identity,
            headers=self.headers(),
            ssl=self.ssl,
            timeout=CALL_TIMEOUT,
        ) as response:
            if response.status == 401:
                self._unpair()
                return None
            if response.status == 403:
                return None
            if response.status != 200:
                raise ExchangeError(f"companion session: HTTP {response.status}")
            answer = await response.json()
        return str(answer["token"])

    async def _post(self, path: str, body: dict) -> int:
        async with self._session.post(
            f"{self._base}/api/v1/companion/{path}", json=body, headers=self.headers(), ssl=self.ssl, timeout=CALL_TIMEOUT
        ) as response:
            if response.status == 401:
                self._unpair()
            return response.status

    def _unpair(self) -> None:
        if self._unpaired:
            return
        self._unpaired = True
        self._on_unpaired()
