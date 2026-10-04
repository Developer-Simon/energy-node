"""Der Protokollteil gegen einen Fake-Client, dazu ein Durchlauf gegen ein Fake-Dashboard."""
import asyncio
import contextlib

import aiohttp
import pytest
from aiohttp import web
from aiohttp.test_utils import TestServer

from custom_components.energy_node.buckets import raster_window
from custom_components.energy_node.client import DashboardClient, ExchangeError
from custom_components.energy_node.peer import HistoryPeer
from custom_components.energy_node.series_map import SeriesSource

HOUR = 3_600_000
NOW = 1_800_000_000_000 + HOUR // 2  # halb nach einer vollen Stunde
PV = SeriesSource("role:pv", "W", "sensor.pv")
ANNOUNCEMENT = {
    "protocol": 1,
    "max_rows_per_deliver": 500,
    "max_rows_per_request": 20000,
    "series": [{"id": "role:pv", "unit": "W"}],
}


def _row(ts):
    return {"series": "role:pv", "ts": ts, "min": 1.0, "max": 1.0, "avg": 1.0, "n": 1, "u": "W"}


class FakeClient:
    def __init__(self, announcement=ANNOUNCEMENT):
        self.announcement_value = dict(announcement)
        self.posts = []
        self.has_session = True

    async def login(self):
        self.has_session = True

    async def announcement(self):
        return self.announcement_value

    async def post(self, suffix, body):
        self.posts.append((suffix, body))
        return 204


class FakeRows:
    def __init__(self, rows):
        self.rows = rows
        self.calls = []

    async def __call__(self, source, tier, start, end):
        self.calls.append((source.series, tier, start, end))
        return [row for row in self.rows if start <= row["ts"] < end]


def _peer(rows, clock=None, client=None, resolved=(PV,)):
    client = client or FakeClient()
    seen = []

    def resolve(announced):
        seen.append(announced)
        return list(resolved)

    clock = clock or [NOW]
    peer = HistoryPeer(client, resolve, rows, now_ms=lambda: clock[0])
    return peer, client, seen, clock


async def _hello(peer):
    await peer.refresh_announcement()
    task = peer.handle("hello", {"protocol": 1, "peer_id": "p-ha", "peers": ["server"]})
    await task


async def test_hello_offers_coverage_with_label():
    rows = FakeRows([_row(NOW - 2 * HOUR), _row(NOW - 2 * HOUR + 60_000)])
    peer, client, seen, _ = _peer(rows)
    await _hello(peer)

    assert seen == [ANNOUNCEMENT["series"]]
    suffix, body = client.posts[0]
    assert suffix == "/offer"
    assert body["peer"] == "p-ha" and body["label"] == "Home Assistant"
    assert sum(body["coverage"]["1m"]["role:pv"]["n"]) == 2
    assert body["coverage"]["1m"]["role:pv"]["from"] == raster_window("1m", NOW)[0]


async def test_series_without_rows_are_not_offered():
    peer, client, _, _ = _peer(FakeRows([]))
    await _hello(peer)
    assert client.posts == []


async def test_peer_joined_reuses_cached_coverage_within_the_hour():
    rows = FakeRows([_row(NOW - 2 * HOUR)])
    peer, client, _, _ = _peer(rows)
    await _hello(peer)
    calls = len(rows.calls)

    await peer.handle("peer-joined", {"peer": "p-browser"})

    assert len(rows.calls) == calls
    assert [suffix for suffix, _ in client.posts] == ["/offer", "/offer"]


async def test_coverage_recomputed_after_hour_boundary():
    rows = FakeRows([_row(NOW - 2 * HOUR)])
    peer, client, _, clock = _peer(rows)
    await _hello(peer)
    calls = len(rows.calls)

    clock[0] = NOW + HOUR
    await peer.handle("peer-joined", {"peer": "p-browser"})

    assert len(rows.calls) > calls
    assert client.posts[-1][1]["coverage"]["1m"]["role:pv"]["from"] == raster_window("1m", NOW + HOUR)[0]


async def test_unchanged_coverage_is_not_reoffered_on_refresh():
    peer, client, _, _ = _peer(FakeRows([_row(NOW - 2 * HOUR)]))
    await _hello(peer)
    await peer.refresh()
    assert [suffix for suffix, _ in client.posts] == ["/offer"]


async def test_request_is_answered_in_chunks():
    client = FakeClient({**ANNOUNCEMENT, "max_rows_per_deliver": 2})
    rows = FakeRows([_row(NOW - 3 * HOUR + i * 60_000) for i in range(5)])
    peer, _, _, _ = _peer(rows, client=client)
    await _hello(peer)
    client.posts.clear()

    await peer.handle("request", {
        "peer": "p-browser", "req_id": "r1", "tier": "1m", "series": "role:pv",
        "ranges": [[NOW - 3 * HOUR, NOW - 2 * HOUR]],
    })

    delivers = [body for suffix, body in client.posts if suffix == "/deliver"]
    assert [len(body["rows"]) for body in delivers] == [2, 2, 1]
    assert [body["seq"] for body in delivers] == [0, 1, 2]
    assert [body["final"] for body in delivers] == [False, False, True]
    assert {(body["to"], body["req_id"], body["tier"], body["peer"]) for body in delivers} == {
        ("p-browser", "r1", "1m", "p-ha")
    }


async def test_request_never_reaches_past_now():
    rows = FakeRows([])
    peer, _, _, _ = _peer(rows)
    await _hello(peer)
    rows.calls.clear()

    await peer.handle("request", {
        "peer": "p-browser", "req_id": "r1", "tier": "1m", "series": "role:pv",
        "ranges": [[NOW - HOUR, NOW + HOUR]],
    })

    assert rows.calls == [("role:pv", "1m", NOW - HOUR, NOW)]


async def test_unknown_series_gets_an_empty_final_delivery():
    peer, client, _, _ = _peer(FakeRows([]))
    await _hello(peer)

    await peer.handle("request", {
        "peer": "p-browser", "req_id": "r9", "tier": "1m", "series": "shelly_temp", "ranges": [[0, HOUR]],
    })

    assert client.posts[-1] == ("/deliver", {
        "peer": "p-ha", "to": "p-browser", "req_id": "r9", "tier": "1m", "seq": 0, "final": True, "rows": [],
    })


async def test_hello_with_another_protocol_raises():
    peer, _, _, _ = _peer(FakeRows([]))
    await peer.refresh_announcement()
    with pytest.raises(ExchangeError):
        peer.handle("hello", {"protocol": 2, "peer_id": "p-ha"})


async def test_offers_and_deliveries_from_others_are_ignored():
    peer, client, _, _ = _peer(FakeRows([]))
    await _hello(peer)
    assert peer.handle("offer", {"peer": "p-browser", "coverage": {}}) is None
    assert peer.handle("deliver", {"peer": "p-browser", "rows": []}) is None
    assert client.posts == []


async def test_announcement_without_series_is_too_old():
    client = FakeClient({"protocol": 1})
    peer, _, _, _ = _peer(FakeRows([]), client=client)
    with pytest.raises(ExchangeError):
        await peer.refresh_announcement()


@pytest.mark.enable_socket
async def test_run_against_a_fake_dashboard():
    offered = asyncio.Event()
    delivered = asyncio.Event()
    state = {"delivers": [], "logins": 0}

    async def guest(request):
        state["logins"] += 1
        response = web.json_response({})
        response.set_cookie("energy_node_session", "t")
        return response

    async def announce(request):
        return web.json_response(ANNOUNCEMENT)

    async def stream(request):
        response = web.StreamResponse(headers={"Content-Type": "text/event-stream"})
        await response.prepare(request)
        await response.write(b'event: hello\ndata: {"protocol":1,"peer_id":"p-ha","peers":[]}\n\n')
        await offered.wait()
        request_event = (
            '{"peer":"p-browser","req_id":"r1","tier":"1m","series":"role:pv",'
            f'"ranges":[[{NOW - 3 * HOUR},{NOW - 2 * HOUR}]]}}'
        )
        await response.write(f"event: request\ndata: {request_event}\n\n".encode())
        await delivered.wait()
        return response

    async def offer(request):
        offered.set()
        return web.Response(status=204)

    async def deliver(request):
        body = await request.json()
        state["delivers"].append(body)
        if body["final"]:
            delivered.set()
        return web.Response(status=204)

    app = web.Application()
    app.router.add_post("/api/v1/auth/guest", guest)
    app.router.add_get("/api/v1/history/exchange", announce)
    app.router.add_get("/api/v1/history/exchange/stream", stream)
    app.router.add_post("/api/v1/history/exchange/offer", offer)
    app.router.add_post("/api/v1/history/exchange/deliver", deliver)
    server = TestServer(app, host="127.0.0.1")
    await server.start_server()
    session = aiohttp.ClientSession()
    peer = HistoryPeer(
        DashboardClient(session, str(server.make_url("/"))),
        lambda announced: [PV],
        FakeRows([_row(NOW - 3 * HOUR)]),
        now_ms=lambda: NOW,
    )
    task = asyncio.create_task(peer.run())
    try:
        await asyncio.wait_for(delivered.wait(), 5)
    finally:
        task.cancel()
        with contextlib.suppress(asyncio.CancelledError):
            await task
        await session.close()
        await server.close()

    assert state["logins"] == 1
    assert state["delivers"][-1]["rows"] == [_row(NOW - 3 * HOUR)]
