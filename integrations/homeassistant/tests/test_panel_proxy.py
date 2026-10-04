"""Panel-Sitzung und Proxy gegen ein Fake-Dashboard ueber Loopback."""
import asyncio
from unittest.mock import AsyncMock, patch

import pytest
from aiohttp import web
from aiohttp.test_utils import TestServer
from pytest_homeassistant_custom_component.common import MockConfigEntry

from custom_components.energy_node_companion.const import CONF_PANEL, CONF_URL, CONF_VERIFY_SSL, DOMAIN, PANEL_ADMINS, PANEL_OFF

pytestmark = pytest.mark.enable_socket


@pytest.fixture(autouse=True)
def auto_enable_custom_integrations(recorder_mock, enable_custom_integrations):
    yield


def _dashboard(state):
    async def guest(request):
        state["logins"] += 1
        response = web.json_response({"guest": True})
        response.set_cookie("energy_node_guest_session", f"token-{state['logins']}")
        return response

    async def anything(request):
        cookie = request.headers.get("Cookie", "")
        state["seen"].append({
            "method": request.method,
            "path": request.path,
            "query": dict(request.query),
            "headers": request.headers.copy(),
            "body": await request.read(),
        })
        if cookie in state["expired"]:
            return web.json_response({"code": "authentication_required"}, status=401)
        if request.path == "/api/v1/auth/login":
            response = web.json_response({"username": "admin"})
            response.set_cookie(
                "energy_node_session", "admin-token",
                path=request.headers["X-Forwarded-Prefix"] + "/", httponly=True,
            )
            return response
        if request.path == "/api/v1/events":
            response = web.StreamResponse(headers={"Content-Type": "text/event-stream"})
            await response.prepare(request)
            await response.write(b"event: energy\ndata: {}\n\n")
            await state["release"].wait()
            return response
        return web.json_response({"ok": True})

    app = web.Application()
    app.router.add_post("/api/v1/auth/guest", guest)
    app.router.add_route("*", "/{tail:.*}", anything)
    return app


@pytest.fixture
async def node(hass):
    """Startet Fake-Dashboard und Integration. Aufruf: await node(options)."""
    state = {"logins": 0, "seen": [], "expired": set(), "release": asyncio.Event()}
    server = TestServer(_dashboard(state), host="127.0.0.1")
    await server.start_server()
    state["server"] = server

    async def start(options=None):
        url = str(server.make_url("")).rstrip("/")
        entry = MockConfigEntry(
            domain=DOMAIN, unique_id=url, options=options or {},
            data={CONF_URL: url, CONF_VERIFY_SSL: True},
        )
        entry.add_to_hass(hass)
        with patch("custom_components.energy_node_companion.HistoryPeer.run", AsyncMock()):
            assert await hass.config_entries.async_setup(entry.entry_id)
            await hass.async_block_till_done()
        return state, entry

    yield start
    state["release"].set()
    await server.close()


async def _panel_cookie(hass_client, entry, token=None):
    client = await (hass_client() if token is None else hass_client(token))
    response = await client.post(f"/api/energy_node_companion/panel_session/{entry.entry_id}")
    assert response.status == 200, await response.text()
    return f"energy_node_panel={response.cookies['energy_node_panel'].value}"


async def _proxy(hass_client_no_auth, entry, cookie, path="api/v1/health", method="GET", headers=None, **kwargs):
    client = await hass_client_no_auth()
    return await client.request(
        method, f"/api/energy_node_companion/proxy/{entry.entry_id}/{path}",
        headers={"Cookie": cookie, **(headers or {})}, **kwargs,
    )


async def test_panel_session_sets_a_scoped_http_only_cookie(node, hass_client):
    _, entry = await node()
    client = await hass_client()
    response = await client.post(f"/api/energy_node_companion/panel_session/{entry.entry_id}")
    assert response.status == 200
    body = await response.json()
    assert body == {"path": f"/api/energy_node_companion/proxy/{entry.entry_id}/", "expires_in": 7200}
    morsel = response.cookies["energy_node_panel"]
    assert morsel["path"] == f"/api/energy_node_companion/proxy/{entry.entry_id}/"
    assert morsel["httponly"]
    assert morsel["samesite"] == "Strict"


async def test_panel_session_needs_ha_auth(node, hass_client_no_auth):
    _, entry = await node()
    client = await hass_client_no_auth()
    response = await client.post(f"/api/energy_node_companion/panel_session/{entry.entry_id}")
    assert response.status == 401


async def test_proxy_without_panel_session_is_refused(node, hass_client_no_auth):
    state, entry = await node()
    response = await _proxy(hass_client_no_auth, entry, "energy_node_panel=forged")
    assert response.status == 401
    assert state["seen"] == []
    assert state["logins"] == 0


async def test_unknown_entry_is_404(node, hass_client_no_auth):
    await node()
    client = await hass_client_no_auth()
    response = await client.get("/api/energy_node_companion/proxy/NOPE/api/v1/health")
    assert response.status == 404


async def test_proxy_injects_the_shared_guest_and_its_prefix(node, hass_client, hass_client_no_auth):
    state, entry = await node()
    cookie = await _panel_cookie(hass_client, entry)
    response = await _proxy(hass_client_no_auth, entry, cookie)
    assert response.status == 200
    assert await response.json() == {"ok": True}
    seen = state["seen"][-1]
    assert seen["path"] == "/api/v1/health"
    assert seen["headers"].getall("Cookie") == ["energy_node_guest_session=token-1"]
    assert seen["headers"]["X-Forwarded-Prefix"] == f"/api/energy_node_companion/proxy/{entry.entry_id}"
    assert seen["headers"]["X-Forwarded-Proto"] == "http"


async def test_guest_login_is_reused_across_requests(node, hass_client, hass_client_no_auth):
    state, entry = await node()
    cookie = await _panel_cookie(hass_client, entry)
    for _ in range(3):
        assert (await _proxy(hass_client_no_auth, entry, cookie)).status == 200
    assert state["logins"] == 1


async def test_forged_headers_and_ha_credentials_stay_behind(node, hass_client, hass_client_no_auth):
    state, entry = await node()
    cookie = await _panel_cookie(hass_client, entry)
    response = await _proxy(
        hass_client_no_auth, entry, f"{cookie}; ha_other=1",
        headers={
            "Authorization": "Bearer ha-secret",
            "X-Forwarded-Proto": "https",
            "X-Ingress-Path": "/api/hassio_ingress/evil",
        },
    )
    assert response.status == 200
    seen = state["seen"][-1]["headers"]
    assert seen.getall("Cookie") == ["energy_node_guest_session=token-1"]
    assert seen["X-Forwarded-Proto"] == "http"
    assert "Authorization" not in seen
    assert "X-Ingress-Path" not in seen


async def test_browser_session_wins_over_the_guest(node, hass_client, hass_client_no_auth):
    state, entry = await node()
    cookie = await _panel_cookie(hass_client, entry)
    response = await _proxy(hass_client_no_auth, entry, f"{cookie}; energy_node_guest_session=own")
    assert response.status == 200
    assert state["seen"][-1]["headers"].getall("Cookie") == ["energy_node_guest_session=own"]
    assert state["logins"] == 0


async def test_logout_of_the_shared_guest_stays_local(node, hass_client, hass_client_no_auth):
    state, entry = await node()
    cookie = await _panel_cookie(hass_client, entry)
    response = await _proxy(hass_client_no_auth, entry, cookie, path="api/v1/auth/logout", method="POST")
    assert response.status == 200
    assert await response.json() == {"status": "logged_out"}
    assert all(seen["path"] != "/api/v1/auth/logout" for seen in state["seen"])


async def test_logout_of_an_own_session_reaches_the_dashboard(node, hass_client, hass_client_no_auth):
    state, entry = await node()
    cookie = await _panel_cookie(hass_client, entry)
    await _proxy(
        hass_client_no_auth, entry, f"{cookie}; energy_node_guest_session=own",
        path="api/v1/auth/logout", method="POST",
    )
    assert state["seen"][-1]["path"] == "/api/v1/auth/logout"
    assert state["seen"][-1]["headers"]["Cookie"] == "energy_node_guest_session=own"


async def test_expired_guest_is_renewed_once(node, hass_client, hass_client_no_auth):
    state, entry = await node()
    cookie = await _panel_cookie(hass_client, entry)
    assert (await _proxy(hass_client_no_auth, entry, cookie)).status == 200
    state["expired"].add("energy_node_guest_session=token-1")
    response = await _proxy(hass_client_no_auth, entry, cookie)
    assert response.status == 200
    assert state["logins"] == 2
    assert state["seen"][-1]["headers"]["Cookie"] == "energy_node_guest_session=token-2"


async def test_post_body_and_query_are_forwarded(node, hass_client, hass_client_no_auth):
    state, entry = await node()
    cookie = await _panel_cookie(hass_client, entry)
    response = await _proxy(
        hass_client_no_auth, entry, cookie, path="api/v1/settings?tab=mqtt", method="POST",
        data=b'{"a":1}', headers={"Content-Type": "application/json"},
    )
    assert response.status == 200
    seen = state["seen"][-1]
    assert seen["query"] == {"tab": "mqtt"}
    assert seen["body"] == b'{"a":1}'
    assert seen["headers"]["Content-Type"] == "application/json"


async def test_admin_login_cookie_is_not_kept_by_the_proxy(node, hass_client, hass_client_no_auth):
    state, entry = await node()
    cookie = await _panel_cookie(hass_client, entry)
    response = await _proxy(hass_client_no_auth, entry, cookie, path="api/v1/auth/login", method="POST", data=b"{}")
    assert response.status == 200
    assert "energy_node_session=admin-token" in response.headers.getall("Set-Cookie")[0]
    assert f"Path=/api/energy_node_companion/proxy/{entry.entry_id}/" in response.headers.getall("Set-Cookie")[0]
    # Ein anderer Browser ohne eigenes Cookie bekommt weiter nur den Gast.
    await _proxy(hass_client_no_auth, entry, cookie)
    assert state["seen"][-1]["headers"].getall("Cookie") == ["energy_node_guest_session=token-1"]


async def test_event_stream_is_not_buffered(node, hass_client, hass_client_no_auth):
    state, entry = await node()
    cookie = await _panel_cookie(hass_client, entry)
    response = await _proxy(hass_client_no_auth, entry, cookie, path="api/v1/events")
    assert response.status == 200
    assert response.headers["Content-Type"].startswith("text/event-stream")
    line = await asyncio.wait_for(response.content.readline(), timeout=5)
    assert line == b"event: energy\n"
    state["release"].set()


async def test_unreachable_dashboard_gives_502(node, hass_client, hass_client_no_auth):
    state, entry = await node()
    cookie = await _panel_cookie(hass_client, entry)
    await state["server"].close()
    response = await _proxy(hass_client_no_auth, entry, cookie)
    assert response.status == 502


async def test_admins_option_refuses_other_users(node, hass_client, hass_read_only_access_token):
    _, entry = await node({CONF_PANEL: PANEL_ADMINS})
    client = await hass_client(hass_read_only_access_token)
    response = await client.post(f"/api/energy_node_companion/panel_session/{entry.entry_id}")
    assert response.status == 403
    await _panel_cookie(hass_client, entry)  # Admin darf


async def test_off_option_refuses_everyone(node, hass_client):
    _, entry = await node({CONF_PANEL: PANEL_OFF})
    client = await hass_client()
    response = await client.post(f"/api/energy_node_companion/panel_session/{entry.entry_id}")
    assert response.status == 403


async def test_reload_invalidates_panel_sessions(node, hass, hass_client, hass_client_no_auth):
    _, entry = await node()
    cookie = await _panel_cookie(hass_client, entry)
    with patch("custom_components.energy_node_companion.HistoryPeer.run", AsyncMock()):
        assert await hass.config_entries.async_reload(entry.entry_id)
        await hass.async_block_till_done()
    assert (await _proxy(hass_client_no_auth, entry, cookie)).status == 401
