import json
import threading
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from types import SimpleNamespace
from urllib.parse import parse_qs, urlsplit

import pytest

from console_remote import RemoteFake
from fakelog import Log


class _Handler(BaseHTTPRequestHandler):
    def log_message(self, *args):
        pass

    def _send(self, code, body):
        raw = json.dumps(body).encode()
        self.send_response(code)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(raw)))
        self.end_headers()
        self.wfile.write(raw)

    def do_GET(self):
        parts = urlsplit(self.path)
        if parts.path == "/state":
            self._send(200, {"ok": True})
        elif parts.path == "/log":
            since = int(parse_qs(parts.query).get("since", ["0"])[0])
            self.server.seen_since.append(since)
            entries = [e for e in self.server.entries if e["n"] > since]
            self._send(200, {"last": self.server.last, "entries": entries})
        else:
            self._send(404, {"error": "nicht da"})

    def do_POST(self):
        length = int(self.headers.get("Content-Length", "0"))
        body = json.loads(self.rfile.read(length) or b"{}")
        if self.path == "/act":
            self._send(200, {"echo": body})
        elif self.path == "/bad":
            self._send(400, {"error": "kaputt"})
        else:
            self._send(404, {"error": "nicht da"})


@pytest.fixture
def remote():
    server = ThreadingHTTPServer(("127.0.0.1", 0), _Handler)
    server.entries = []
    server.last = 0
    server.seen_since = []
    threading.Thread(target=server.serve_forever, daemon=True).start()
    try:
        yield SimpleNamespace(
            url=f"http://127.0.0.1:{server.server_address[1]}",
            server=server,
        )
    finally:
        server.shutdown()
        server.server_close()


@pytest.fixture
def log():
    return Log()


def test_get_and_post_return_json(remote):
    fake = RemoteFake(remote.url, "x", Log())
    assert fake.get("state") == {"ok": True}
    assert fake.post("act", {"x": 1}) == {"echo": {"x": 1}}


def test_error_body_becomes_value_error(remote):
    fake = RemoteFake(remote.url, "x", Log())
    with pytest.raises(ValueError, match="kaputt"):
        fake.post("bad", {})


def test_unreachable_raises_oserror():
    fake = RemoteFake("http://127.0.0.1:1", "x", Log(), timeout=0.5)
    with pytest.raises(OSError):
        fake.get("state")


def test_pump_log_copies_entries_once_with_level(remote, log):
    remote.server.entries = [
        {"n": 1, "text": "eins", "level": "info"},
        {"n": 2, "text": "zwei", "level": "warn"},
    ]
    remote.server.last = 2
    fake = RemoteFake(remote.url, "mqtt", log)
    fake.pump_log()
    fake.pump_log()
    _, entries = log.since(0)
    assert [e["text"] for e in entries] == ["eins", "zwei"]
    assert [e["level"] for e in entries] == ["info", "warn"]
    assert all(e["source"] == "mqtt" for e in entries)


def test_pump_log_restarts_cursor_after_fake_restart(remote, log):
    remote.server.last = 5
    fake = RemoteFake(remote.url, "mqtt", log)
    fake.pump_log()
    # Fake neu gestartet: Zaehler wieder bei 1
    remote.server.entries = [{"n": 1, "text": "neu", "level": "info"}]
    remote.server.last = 1
    fake.pump_log()
    _, entries = log.since(0)
    assert [e["text"] for e in entries] == ["neu"]
