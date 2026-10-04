"""HTTP/SSE-Anbindung an den Verlauf-Austausch des Dashboards.

Der Austausch verlangt nur irgendeine Sitzung, keinen CSRF-Token. Der
Client holt sich deshalb eine Gastsitzung. Jede Gastanmeldung schreibt
users.json auf die SD-Karte der Node: die Sitzung wird darum behalten und
erst nach einem 401 neu geholt, nie bei jedem Wiederverbinden.
"""
from __future__ import annotations

import json
from collections.abc import AsyncIterator
from http.cookies import SimpleCookie

import aiohttp

# Ohne Gesamtfrist: der Strom laeuft unbegrenzt. Das Dashboard sendet alle
# 25 s ein ping, 90 s Stille heisst also, die Verbindung ist tot.
STREAM_TIMEOUT = aiohttp.ClientTimeout(total=None, sock_connect=15, sock_read=90)
CALL_TIMEOUT = aiohttp.ClientTimeout(total=30)


class AuthRequired(Exception):
    """Das Dashboard verlangt eine neue Sitzung."""


class ExchangeError(Exception):
    """Eine Antwort, mit der sich nicht weiterarbeiten laesst."""


class DashboardClient:
    """Spricht mit genau einem Dashboard."""

    def __init__(self, session: aiohttp.ClientSession, base_url: str) -> None:
        self._session = session
        self._base = base_url.rstrip("/")
        # Das Cookie wird von Hand gefuehrt: aiohttps CookieJar verwirft
        # Cookies von IP-Adressen, und die Node ist oft nur per IP erreichbar.
        # Die Sitzung dazu laeuft mit DummyCookieJar (siehe __init__.py).
        self._cookie = ""

    @property
    def has_session(self) -> bool:
        return bool(self._cookie)

    def _url(self, path: str) -> str:
        return f"{self._base}{path}"

    def _headers(self) -> dict[str, str]:
        return {"Cookie": self._cookie} if self._cookie else {}

    def _check(self, response: aiohttp.ClientResponse, what: str) -> None:
        if response.status == 401:
            self._cookie = ""
            raise AuthRequired(what)

    async def login(self) -> None:
        async with self._session.post(self._url("/api/v1/auth/guest"), timeout=CALL_TIMEOUT) as response:
            if response.status != 200:
                raise ExchangeError(f"guest login: HTTP {response.status}")
            jar: SimpleCookie = SimpleCookie()
            for header in response.headers.getall("Set-Cookie", []):
                jar.load(header)
            if not jar:
                raise ExchangeError("guest login: no session cookie")
            self._cookie = "; ".join(f"{name}={morsel.value}" for name, morsel in jar.items())

    async def announcement(self) -> dict:
        async with self._session.get(
            self._url("/api/v1/history/exchange"), headers=self._headers(), timeout=CALL_TIMEOUT
        ) as response:
            self._check(response, "announcement")
            if response.status != 200:
                raise ExchangeError(f"announcement: HTTP {response.status}")
            return await response.json()

    async def post(self, suffix: str, body: dict) -> int:
        async with self._session.post(
            self._url(f"/api/v1/history/exchange{suffix}"), json=body, headers=self._headers(), timeout=CALL_TIMEOUT
        ) as response:
            self._check(response, suffix)
            return response.status

    async def stream(self) -> AsyncIterator[tuple[str, dict]]:
        """SSE-Ereignisse als (Name, JSON-Objekt). Unbenannte und kaputte fallen weg."""
        async with self._session.get(
            self._url("/api/v1/history/exchange/stream"), headers=self._headers(), timeout=STREAM_TIMEOUT
        ) as response:
            self._check(response, "stream")
            if response.status != 200:
                raise ExchangeError(f"stream: HTTP {response.status}")
            event = ""
            data: list[str] = []
            async for raw in response.content:
                line = raw.decode("utf-8").rstrip("\r\n")
                if line:
                    if line.startswith("event:"):
                        event = line[6:].strip()
                    elif line.startswith("data:"):
                        data.append(line[5:].lstrip())
                    continue
                if event and data:
                    try:
                        payload = json.loads("\n".join(data))
                    except ValueError:
                        payload = None
                    if isinstance(payload, dict):
                        yield event, payload
                event, data = "", []
