"""Panel-Proxy mit Kopplung: Companion-Port, Kopplungstoken als Bearer, Sitzung je HA-Benutzer."""
import asyncio
import secrets
import ssl
from unittest.mock import patch

import pytest
from aiohttp import web
from aiohttp.test_utils import TestServer
from pytest_homeassistant_custom_component.common import MockConfigEntry

from custom_components.energy_node_companion.const import (
    CONF_FINGERPRINT, CONF_HISTORY, CONF_STATS, CONF_TOKEN, CONF_URL, CONF_VERIFY_SSL, DOMAIN,
)
from custom_components.energy_node_companion.link import CompanionLink
from custom_components.energy_node_companion.pairing import fingerprint_of

from .companion_tls import _self_signed_certificate

pytestmark = pytest.mark.enable_socket

PAIRING = "33" * 32
COMPANION = "custom_components.energy_node_companion.companion_base"


@pytest.fixture(autouse=True)
def auto_enable_custom_integrations(recorder_mock, enable_custom_integrations):
    yield


def _unpaired():
    return web.json_response({"code": "companion_unauthorized"}, status=401)


@pytest.fixture
async def node(hass, tmp_path):
    """Companion-Port (TLS, Kopplungstoken geprueft) und Dashboard (HTTP, nur Gastanmeldung)."""
    cert_path, key_path, der = _self_signed_certificate(tmp_path)
    fingerprint = fingerprint_of(der)
    state = {
        "seen": [],
        "control": [],
        "issued": [],
        "sessions": 0,
        "session_status": 200,
        "expired": set(),
        "revoke_proxy": False,
        "guest_logins": 0,
        "release": asyncio.Event(),
    }

    def paired(request):
        return request.headers.get("Authorization") == f"Bearer {PAIRING}"

    async def stream(request):
        state["control"].append(("stream", request.headers.get("X-Forwarded-Prefix")))
        if not paired(request):
            return _unpaired()
        response = web.StreamResponse(headers={"Content-Type": "text/event-stream"})
        await response.prepare(request)
        await state["release"].wait()
        return response

    async def session(request):
        state["control"].append(("session", request.headers.get("X-Forwarded-Prefix")))
        if not paired(request):
            return _unpaired()
        await request.json()
        state["sessions"] += 1
        if state["session_status"] != 200:
            return web.json_response({"code": "account_disabled"}, status=state["session_status"])
        token = secrets.token_hex(16)
        state["issued"].append(token)
        return web.json_response({"token": token, "csrf_token": "csrf", "expires_at": "2099-01-01T00:00:00Z"})

    async def proxied(request):
        state["seen"].append({"path": request.path, "method": request.method, "headers": request.headers.copy()})
        if state["revoke_proxy"] or not paired(request):
            return _unpaired()
        if request.headers.get("Cookie", "") in state["expired"]:
            return web.json_response({"code": "authentication_required"}, status=401)
        return web.json_response({"ok": True})

    companion_app = web.Application()
    companion_app.router.add_get("/api/v1/companion/stream", stream)
    companion_app.router.add_post("/api/v1/companion/session", session)
    companion_app.router.add_route("*", "/{tail:.*}", proxied)
    context = ssl.create_default_context(ssl.Purpose.CLIENT_AUTH)
    context.load_cert_chain(cert_path, key_path)
    companion = TestServer(companion_app, host="127.0.0.1")
    await companion.start_server(ssl=context)

    async def guest(request):
        state["guest_logins"] += 1
        response = web.json_response({"guest": True})
        response.set_cookie("energy_node_guest_session", f"token-{state['guest_logins']}")
        return response

    dashboard_app = web.Application()
    dashboard_app.router.add_post("/api/v1/auth/guest", guest)
    dashboard = TestServer(dashboard_app, host="127.0.0.1")
    await dashboard.start_server()

    async def start():
        url = f"http://127.0.0.1:{dashboard.port}"
        entry = MockConfigEntry(
            domain=DOMAIN,
            unique_id=url,
            options={CONF_HISTORY: False, CONF_STATS: False},
            data={
                CONF_URL: url,
                CONF_VERIFY_SSL: True,
                CONF_TOKEN: PAIRING,
                CONF_FINGERPRINT: fingerprint,
            },
        )
        entry.add_to_hass(hass)
        with patch(COMPANION, return_value=f"https://127.0.0.1:{companion.port}"):
            assert await hass.config_entries.async_setup(entry.entry_id)
        state["entry"] = entry
        return entry

    state["start"] = start
    yield state
    state["release"].set()
    if "entry" in state:
        await hass.config_entries.async_unload(state["entry"].entry_id)
    await companion.close()
    await dashboard.close()


async def _panel_cookie(hass_client, entry):
    client = await hass_client()
    response = await client.post(f"/api/energy_node_companion/panel_session/{entry.entry_id}")
    assert response.status == 200, await response.text()
    return f"energy_node_panel={response.cookies['energy_node_panel'].value}"


async def _proxy(hass_client_no_auth, entry, cookie, path="api/v1/health", method="GET", headers=None, **kwargs):
    client = await hass_client_no_auth()
    return await client.request(
        method, f"/api/energy_node_companion/proxy/{entry.entry_id}/{path}",
        headers={"Cookie": cookie, **(headers or {})}, **kwargs,
    )


async def test_paired_proxy_uses_companion_port_and_bearer(node, hass_client, hass_client_no_auth):
    entry = await node["start"]()
    cookie = await _panel_cookie(hass_client, entry)
    response = await _proxy(hass_client_no_auth, entry, cookie, headers={"Authorization": "Bearer ha-secret"})
    assert response.status == 200
    seen = node["seen"][-1]
    assert seen["path"] == "/api/v1/health"
    assert seen["headers"]["Authorization"] == f"Bearer {PAIRING}"
    assert seen["headers"].getall("Cookie") == [f"energy_node_guest_session={node['issued'][0]}"]
    assert seen["headers"]["X-Forwarded-Proto"] == "http"
    assert seen["headers"]["X-Forwarded-Prefix"] == f"/api/energy_node_companion/proxy/{entry.entry_id}"


async def test_paired_proxy_uses_one_session_per_ha_user(node, hass_client, hass_client_no_auth):
    entry = await node["start"]()
    cookie = await _panel_cookie(hass_client, entry)
    for _ in range(2):
        assert (await _proxy(hass_client_no_auth, entry, cookie)).status == 200
    assert node["sessions"] == 1
    assert len(node["issued"]) == 1
    assert [seen["headers"].getall("Cookie") for seen in node["seen"]] == [
        [f"energy_node_guest_session={node['issued'][0]}"],
    ] * 2


async def test_paired_proxy_prefers_own_browser_session(node, hass_client, hass_client_no_auth):
    entry = await node["start"]()
    cookie = await _panel_cookie(hass_client, entry)
    response = await _proxy(hass_client_no_auth, entry, f"{cookie}; energy_node_guest_session=own")
    assert response.status == 200
    assert node["sessions"] == 0
    seen = node["seen"][-1]
    assert seen["headers"].getall("Cookie") == ["energy_node_guest_session=own"]
    assert seen["headers"]["Authorization"] == f"Bearer {PAIRING}"


async def test_disabled_ha_account_falls_back_to_guest(node, hass_client, hass_client_no_auth):
    node["session_status"] = 403
    entry = await node["start"]()
    cookie = await _panel_cookie(hass_client, entry)
    response = await _proxy(hass_client_no_auth, entry, cookie)
    assert response.status == 200
    assert node["sessions"] == 1
    assert node["guest_logins"] == 1
    seen = node["seen"][-1]
    assert seen["headers"]["Authorization"] == f"Bearer {PAIRING}"
    assert seen["headers"].getall("Cookie") == ["energy_node_guest_session=token-1"]


async def test_expired_user_session_is_renewed_once(node, hass_client, hass_client_no_auth):
    entry = await node["start"]()
    cookie = await _panel_cookie(hass_client, entry)
    assert (await _proxy(hass_client_no_auth, entry, cookie)).status == 200
    node["expired"].add(f"energy_node_guest_session={node['issued'][0]}")
    response = await _proxy(hass_client_no_auth, entry, cookie)
    assert response.status == 200
    assert node["sessions"] == 2
    assert node["seen"][-1]["headers"].getall("Cookie") == [f"energy_node_guest_session={node['issued'][1]}"]


async def test_logout_keeps_the_user_session(node, hass_client, hass_client_no_auth):
    entry = await node["start"]()
    cookie = await _panel_cookie(hass_client, entry)
    response = await _proxy(hass_client_no_auth, entry, cookie, path="api/v1/auth/logout", method="POST")
    assert response.status == 200
    assert await response.json() == {"status": "logged_out"}
    assert node["sessions"] == 0
    assert all(seen["path"] != "/api/v1/auth/logout" for seen in node["seen"])


async def test_companion_unauthorized_reports_unpaired_and_gives_502(node, hass_client, hass_client_no_auth):
    node["revoke_proxy"] = True
    entry = await node["start"]()
    cookie = await _panel_cookie(hass_client, entry)
    with patch.object(CompanionLink, "report_unpaired") as report:
        response = await _proxy(hass_client_no_auth, entry, cookie)
    assert response.status == 502
    report.assert_called_once_with()


@pytest.mark.parametrize(
    ("method", "path"),
    [
        ("POST", "api/v1/companion/session"),
        ("POST", "api/v1/companion/user-changed"),
        ("GET", "api/v1/companion/stream"),
        ("POST", "api/v1/companion/pair"),
        ("GET", "API/V1/Companion/stream"),
        ("GET", "api/v1/x/../companion/stream"),
        ("GET", "api%2Fv1%2Fcompanion%2Fstream"),
        ("POST", "/api/v1/companion/session"),
    ],
)
async def test_companion_control_routes_are_never_proxied(node, hass_client, hass_client_no_auth, method, path):
    entry = await node["start"]()
    cookie = await _panel_cookie(hass_client, entry)
    # Ohne Frist haengt der Strom ewig. Eine Weiterleitung waere dann ein Fehler.
    response = await asyncio.wait_for(
        _proxy(hass_client_no_auth, entry, cookie, path=path, method=method, data=b"{}"), 5
    )
    assert response.status == 404
    assert node["seen"] == []
    # Der eigene Strom der Verbindung kommt ohne X-Forwarded-Prefix. Nur ein
    # weitergeleiteter Aufruf zaehlt.
    assert [call for call in node["control"] if call[1] is not None] == []
    assert node["sessions"] == 0
