"""Header-Regeln des Panel-Proxys. Ohne Home Assistant pruefbar.

Der Proxy setzt die Weiterleitungs-Header immer selbst. Was der Browser
dort mitschickt, faellt weg: das Dashboard glaubt X-Forwarded-Proto
(isSecureRequest) und erlaubt bei "https" die Anmeldung mit Passwort.
"""
from __future__ import annotations

from collections.abc import Mapping
from http.cookies import CookieError, SimpleCookie

from multidict import CIMultiDict

# Wie secureSessionCookie / guestSessionCookie in
# dashboard/internal/httpapi/httpapi.go.
SECURE_SESSION_COOKIE = "energy_node_session"
GUEST_SESSION_COOKIE = "energy_node_guest_session"

_DROP_REQUEST = frozenset(name.lower() for name in (
    "Host", "Connection", "Keep-Alive", "Proxy-Authenticate", "Proxy-Authorization",
    "TE", "Trailer", "Transfer-Encoding", "Upgrade", "Content-Length", "Accept-Encoding",
    "Cookie", "Authorization", "Forwarded", "X-Forwarded-For", "X-Forwarded-Host",
    "X-Forwarded-Proto", "X-Forwarded-Prefix", "X-Ingress-Path", "X-Real-IP",
))
# aiohttp entpackt komprimierte Antworten selbst, Laenge und Kodierung
# stimmen danach nicht mehr.
_DROP_RESPONSE = frozenset(name.lower() for name in (
    "Connection", "Keep-Alive", "Transfer-Encoding", "Content-Length", "Content-Encoding",
))


def session_cookie_name(secure: bool) -> str:
    """Der Cookiename, den das Dashboard fuer diese Anfrage liest."""
    return SECURE_SESSION_COOKIE if secure else GUEST_SESSION_COOKIE


def own_session(cookie_header: str, secure: bool) -> str | None:
    """Das eigene Dashboard-Sitzungscookie des Browsers als "name=wert"."""
    jar: SimpleCookie = SimpleCookie()
    try:
        jar.load(cookie_header or "")
    except CookieError:
        return None
    name = session_cookie_name(secure)
    morsel = jar.get(name)
    if morsel is None or not morsel.value:
        return None
    return f"{name}={morsel.value}"


def guest_cookie(token: str, secure: bool) -> str:
    """Die geteilte Gastsitzung unter dem passenden Cookienamen."""
    return f"{session_cookie_name(secure)}={token}"


def upstream_headers(
    incoming: Mapping[str, str],
    *,
    scheme: str,
    host: str,
    remote: str | None,
    prefix: str,
    session_cookie: str,
) -> CIMultiDict[str]:
    """Header fuer die Anfrage an das Dashboard.

    Cookie enthaelt genau eine Dashboard-Sitzung: HA-Cookies und das
    Panel-Cookie bleiben im Browser, ebenso der HA-Token aus Authorization.
    """
    headers: CIMultiDict[str] = CIMultiDict(
        (name, value) for name, value in incoming.items() if name.lower() not in _DROP_REQUEST
    )
    headers["Cookie"] = session_cookie
    headers["X-Forwarded-Prefix"] = prefix
    headers["X-Forwarded-Proto"] = scheme
    headers["X-Forwarded-Host"] = host
    if remote:
        headers["X-Forwarded-For"] = remote
    return headers


def response_headers(upstream: Mapping[str, str]) -> CIMultiDict[str]:
    """Header der Dashboard-Antwort fuer den Browser, Set-Cookie unveraendert."""
    return CIMultiDict(
        (name, value) for name, value in upstream.items() if name.lower() not in _DROP_RESPONSE
    )


def is_logout(path: str) -> bool:
    """Ob der Proxy-Pfad die Abmeldung des Dashboards ist."""
    return path.strip("/") == "api/v1/auth/logout"
