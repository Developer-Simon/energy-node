import http.server
import json
import queue
import select
import socket
import threading
import time

import pytest

from fake_companion import HOUR_MS, ROLES, TIER, FakeCompanion, Session, hour_values

PEER = "peer-1"


def wait_until(predicate, timeout=5.0):
    deadline = time.monotonic() + timeout
    while time.monotonic() < deadline:
        if predicate():
            return
        time.sleep(0.02)
    raise AssertionError("Bedingung nicht erreicht")


class HistoryDashboard:
    """Minimales HTTP-Stub des Verlauf-Austauschs (ohne TLS, Port 0)."""

    def __init__(self):
        self.max_rows = 2
        self.offers = []
        self.delivers = []  # (monotonic Zeitpunkt, Koerper)
        self.stream_connections = 0
        self.open_streams = 0
        self.login_gate = threading.Event()  # gesetzt: Anmeldung antwortet sofort
        self.login_gate.set()
        self.login_seen = threading.Event()  # gesetzt, sobald eine Anmeldung eintrifft
        self.hello_gate = threading.Event()  # gesetzt: hello wird sofort gesendet
        self.hello_gate.set()
        self._streams = []
        self._lock = threading.Lock()
        stub = self

        class Handler(http.server.BaseHTTPRequestHandler):
            def log_message(self, *args):
                pass

            def _body(self):
                length = int(self.headers.get("Content-Length", 0))
                return json.loads(self.rfile.read(length) or b"{}")

            def _send(self, status, body=None, headers=()):
                data = json.dumps(body if body is not None else {}).encode()
                self.send_response(status)
                for name, value in headers:
                    self.send_header(name, value)
                self.send_header("Content-Type", "application/json")
                self.send_header("Content-Length", str(len(data)))
                self.end_headers()
                self.wfile.write(data)

            def do_GET(self):
                if self.path == "/api/v1/history/exchange":
                    self._send(200, {"tiers": [TIER], "max_rows_per_deliver": stub.max_rows})
                elif self.path == "/api/v1/history/exchange/stream":
                    self._stream()
                else:
                    self._send(404, {})

            def do_POST(self):
                if self.path == "/api/v1/auth/guest":
                    stub.login_seen.set()
                    stub.login_gate.wait(10)
                    self._send(200, {}, [("Set-Cookie", "sid=guest-1; Path=/")])
                    return
                body = self._body()
                if self.path.endswith("/offer"):
                    with stub._lock:
                        stub.offers.append(body)
                    self._send(200, {})
                elif self.path.endswith("/deliver"):
                    with stub._lock:
                        stub.delivers.append((time.monotonic(), body))
                    self._send(200, {})
                else:
                    self._send(404, {})

            def _write(self, event, payload):
                self.wfile.write(f"event: {event}\ndata: {json.dumps(payload)}\n\n".encode())
                self.wfile.flush()

            def _peer_closed(self):
                readable, _, _ = select.select([self.connection], [], [], 0)
                if not readable:
                    return False
                try:
                    return self.connection.recv(1, socket.MSG_PEEK) == b""
                except OSError:
                    return True

            def _stream(self):
                events = queue.Queue()
                with stub._lock:
                    stub.stream_connections += 1
                    stub.open_streams += 1
                    stub._streams.append(events)
                try:
                    self.send_response(200)
                    self.send_header("Content-Type", "text/event-stream")
                    self.send_header("Cache-Control", "no-cache")
                    self.end_headers()
                    hello_pending = True
                    while True:
                        if hello_pending and stub.hello_gate.is_set():
                            self._write("hello", {"peer_id": PEER})
                            hello_pending = False
                        try:
                            item = events.get(timeout=0.05)
                        except queue.Empty:
                            if self._peer_closed():
                                return
                            continue
                        if item is None:
                            return
                        self._write(*item)
                except OSError:
                    return
                finally:
                    with stub._lock:
                        stub.open_streams -= 1

        self.server = http.server.ThreadingHTTPServer(("127.0.0.1", 0), Handler)
        self.server.daemon_threads = True
        self.url = f"http://127.0.0.1:{self.server.server_address[1]}"
        threading.Thread(target=self.server.serve_forever, daemon=True).start()

    def send(self, event, payload):
        wait_until(lambda: self._streams)
        self._streams[-1].put((event, payload))

    def deliveries(self, req_id="r1"):
        with self._lock:
            return [(at, body) for at, body in self.delivers if body.get("req_id") == req_id]

    def close(self):
        with self._lock:
            for events in self._streams:
                events.put(None)
        self.server.shutdown()
        self.server.server_close()


@pytest.fixture
def history_dashboard():
    dashboard = HistoryDashboard()
    yield dashboard
    dashboard.close()


@pytest.fixture
def make_companion(history_dashboard):
    created = []

    def make(days=3):
        lines = []
        companion = FakeCompanion(Session(history_dashboard.url), days, log=lines.append)
        created.append(companion)
        return companion, lines

    yield make
    for companion in created:
        companion.stop()


def started(history_dashboard, make_companion):
    companion, lines = make_companion()
    companion.start()
    wait_until(lambda: history_dashboard.offers)
    return companion, lines


def daylight_window(now, hours=5):
    """Fenster in der Vergangenheit mit PV-Erzeugung in allen Stunden."""
    for back in range(6, 120):
        begin = now - back * HOUR_MS
        if all(hour_values(begin + i * HOUR_MS)["pv"] > 0 for i in range(hours)):
            return begin, begin + hours * HOUR_MS
    raise AssertionError("kein Tageslicht-Fenster gefunden")


def ask(dashboard, begin, end, series="role:pv", req_id="r1"):
    dashboard.send("request", {
        "peer": "requester-1", "req_id": req_id, "series": series, "tier": TIER,
        "ranges": [[begin, end]]})


def test_start_makes_offer_after_hello(history_dashboard, make_companion):
    companion, lines = started(history_dashboard, make_companion)

    offer = history_dashboard.offers[0]
    assert offer["peer"] == PEER
    coverage = offer["coverage"][TIER]
    assert set(coverage) == {f"role:{role}" for role in ROLES}
    assert all("n" in census for census in coverage.values())
    wait_until(lambda: "Angebot gesendet" in lines)


def test_scale_multiplies_delivered_values(history_dashboard, make_companion):
    companion, _ = started(history_dashboard, make_companion)
    companion.scale["pv"] = 2.0
    begin, end = daylight_window(companion.now_hour())

    ask(history_dashboard, begin, end)
    wait_until(lambda: any(body["final"] for _, body in history_dashboard.deliveries()))

    rows = [row for _, body in history_dashboard.deliveries() for row in body["rows"]]
    assert len(rows) == 5
    for row in rows:
        assert row["avg"] == pytest.approx(hour_values(row["ts"])["pv"] * 2, abs=0.06)
        assert row["avg"] > 0


def test_silent_fault_sends_no_deliver(history_dashboard, make_companion):
    companion, lines = started(history_dashboard, make_companion)
    companion.faults["silent"] = True
    begin, end = daylight_window(companion.now_hour())

    ask(history_dashboard, begin, end)
    wait_until(lambda: any(line.startswith("Nachfrage") for line in lines))
    time.sleep(0.2)

    assert history_dashboard.deliveries() == []


def test_partial_fault_sends_only_first_chunk_not_final(history_dashboard, make_companion):
    companion, _ = started(history_dashboard, make_companion)
    companion.faults["partial"] = True
    begin, end = daylight_window(companion.now_hour())

    ask(history_dashboard, begin, end)
    wait_until(lambda: history_dashboard.deliveries())
    time.sleep(0.3)

    deliveries = history_dashboard.deliveries()
    assert len(deliveries) == 1
    body = deliveries[0][1]
    assert body["final"] is False
    assert len(body["rows"]) == history_dashboard.max_rows


def test_delay_fault_delays_first_deliver(history_dashboard, make_companion):
    companion, _ = started(history_dashboard, make_companion)
    companion.faults["delay_ms"] = 300
    begin, end = daylight_window(companion.now_hour())

    sent = time.monotonic()
    ask(history_dashboard, begin, end)
    wait_until(lambda: history_dashboard.deliveries())

    first_at = history_dashboard.deliveries()[0][0]
    assert first_at - sent >= 0.29


def test_stop_ends_loop_and_clears_peer(history_dashboard, make_companion):
    companion, _ = started(history_dashboard, make_companion)
    assert companion.peer_id == PEER

    companion.stop()
    time.sleep(0.5)

    assert companion.running is False
    assert companion.peer_id == ""
    assert history_dashboard.stream_connections == 1
    assert companion.state()["running"] is False


def test_stop_during_login_ends_stream_promptly(history_dashboard, make_companion):
    history_dashboard.login_gate.clear()
    history_dashboard.hello_gate.clear()  # kein hello: nur ein Ereignis wuerde die Schleife beenden
    companion, _ = make_companion()
    companion.start()
    assert history_dashboard.login_seen.wait(5)

    companion.stop()
    history_dashboard.login_gate.set()

    companion._thread.join(3)
    assert not companion._thread.is_alive()
    wait_until(lambda: history_dashboard.open_streams == 0, timeout=3)
    assert companion.running is False
    assert companion.peer_id == ""


def test_history_start_survives_unreachable_dashboard(make_companion):
    with socket.socket() as probe:
        probe.bind(("127.0.0.1", 0))
        port = probe.getsockname()[1]
    companion = FakeCompanion(Session(f"http://127.0.0.1:{port}"), 3)
    companion.start()
    try:
        wait_until(lambda: companion.state()["error"])
        assert companion.running is True
        assert companion.state()["error"]
    finally:
        companion.stop()


def test_cli_signature_unchanged(history_dashboard):
    companion = FakeCompanion(Session(history_dashboard.url), 400)

    assert companion.scale == {role: 1.0 for role in ROLES}
    assert companion.faults == {"delay_ms": 0, "silent": False, "partial": False}
    assert companion.running is False
