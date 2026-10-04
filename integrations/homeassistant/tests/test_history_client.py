"""Der Client gegen ein kleines Fake-Dashboard ueber Loopback."""
import aiohttp
import pytest
from aiohttp import web
from aiohttp.test_utils import TestServer

from custom_components.energy_node.client import AuthRequired, DashboardClient, ExchangeError

pytestmark = pytest.mark.enable_socket


def _app(state):
    async def guest(request):
        state["logins"] += 1
        response = web.json_response({"guest": True})
        if not state["no_cookie"]:
            response.set_cookie("energy_node_session", f"token-{state['logins']}")
        return response

    async def announce(request):
        state["cookies"].append(request.headers.get("Cookie", ""))
        if state["expire"]:
            state["expire"] = False
            return web.json_response({"code": "authentication_required"}, status=401)
        return web.json_response({"protocol": 1, "series": []})

    async def stream(request):
        response = web.StreamResponse(headers={"Content-Type": "text/event-stream"})
        await response.prepare(request)
        await response.write(b'event: hello\ndata: {"protocol":1,"peer_id":"p-ha","peers":["server"]}\n\n')
        await response.write(b'data: {"ohne":"event"}\n\n')
        await response.write(b"event: broken\ndata: {kein json\n\n")
        await response.write(b"event: ping\ndata: {}\n\n")
        return response

    async def offer(request):
        state["offers"].append((request.headers.get("Cookie", ""), await request.json()))
        return web.Response(status=204)

    app = web.Application()
    app.router.add_post("/api/v1/auth/guest", guest)
    app.router.add_get("/api/v1/history/exchange", announce)
    app.router.add_get("/api/v1/history/exchange/stream", stream)
    app.router.add_post("/api/v1/history/exchange/offer", offer)
    return app


@pytest.fixture
async def dashboard():
    state = {"logins": 0, "cookies": [], "offers": [], "expire": False, "no_cookie": False}
    server = TestServer(_app(state), host="127.0.0.1")
    await server.start_server()
    session = aiohttp.ClientSession()
    yield state, DashboardClient(session, str(server.make_url("/")))
    await session.close()
    await server.close()


async def test_login_once_and_reuse_cookie(dashboard):
    state, client = dashboard
    assert not client.has_session
    await client.login()
    await client.announcement()
    await client.announcement()
    assert state["logins"] == 1
    assert state["cookies"] == ["energy_node_session=token-1", "energy_node_session=token-1"]


async def test_401_drops_the_session(dashboard):
    state, client = dashboard
    await client.login()
    state["expire"] = True
    with pytest.raises(AuthRequired):
        await client.announcement()
    assert not client.has_session
    await client.login()
    await client.announcement()
    assert state["cookies"][-1] == "energy_node_session=token-2"


async def test_login_without_cookie_fails(dashboard):
    state, client = dashboard
    state["no_cookie"] = True
    with pytest.raises(ExchangeError):
        await client.login()


async def test_stream_yields_named_json_events_only(dashboard):
    _, client = dashboard
    await client.login()
    events = [item async for item in client.stream()]
    assert events == [
        ("hello", {"protocol": 1, "peer_id": "p-ha", "peers": ["server"]}),
        ("ping", {}),
    ]


async def test_post_sends_json_with_the_session(dashboard):
    state, client = dashboard
    await client.login()
    status = await client.post("/offer", {"peer": "p-ha", "coverage": {}})
    assert status == 204
    assert state["offers"] == [("energy_node_session=token-1", {"peer": "p-ha", "coverage": {}})]
