"""Gemeinsame Fixtures fuer die Smoke-Tests (nur Standardbibliothek).

Die Fixture `stub_dashboard` spielt die Companion-Seite des Dashboards mit TLS:
Kopplung, Pruefstrom, Pruefantworten und Benutzer-Meldungen. Sie wird ueber
Befehle gesteuert (Pruefung senden, Strom beenden, Antwortstatus setzen) und
merkt sich, was der Companion ans Dashboard geschickt hat.
"""

import hashlib
import hmac
import http.server
import json
import queue
import secrets
import select
import socket
import ssl
import struct
import subprocess
import sys
import threading
import time

import pytest

from fake_companion import TIER

STUB_CODE = "K7PM3QXD8HWT"
PROOF_PREFIX = "energy-node-pair-v1"
PING_SECONDS = 0.5


def _mac(code, message):
    return hmac.new(code.upper().encode(), message.encode(), hashlib.sha256).hexdigest()


class StubDashboard:
    def __init__(self, cert, key):
        self.code = STUB_CODE
        self.pin = hashlib.sha256(ssl.PEM_cert_to_DER_cert(cert.read_text())).hexdigest()
        self.verify_status = 200
        self.tokens = []
        self.pair_requests = []  # (status, body)
        self.answers = []  # (request_id, status, body)
        self.user_changes = []  # Koerper der user-changed-Meldungen
        self.open_streams = 0
        self._streams = []  # Warteschlangen der offenen Ströme
        self._lock = threading.Lock()
        self.hold = None  # Event, das verify-Antworten zurueckhaelt, solange es nicht gesetzt ist
        self.hold_entered = threading.Event()  # gesetzt, sobald eine Antwort festgehalten wird
        stub = self

        class Handler(http.server.BaseHTTPRequestHandler):
            def log_message(self, *args):
                pass

            def _body(self):
                length = int(self.headers.get("Content-Length", 0))
                return json.loads(self.rfile.read(length) or b"{}")

            def _send(self, status, body=None):
                data = json.dumps(body if body is not None else {}).encode()
                self.send_response(status)
                self.send_header("Content-Type", "application/json")
                self.send_header("Content-Length", str(len(data)))
                self.end_headers()
                self.wfile.write(data)

            def _authorized(self):
                return any(self.headers.get("Authorization") == f"Bearer {t}" for t in stub.tokens)

            def do_POST(self):
                path = self.path
                if path == "/api/v1/companion/pair":
                    body = self._body()
                    nonce = str(body.get("nonce", ""))
                    expected = _mac(stub.code, f"{PROOF_PREFIX}|client|{stub.pin}|{nonce}")
                    if not hmac.compare_digest(expected, str(body.get("proof", ""))):
                        stub.pair_requests.append((403, body))
                        self._send(403, {"error": "Kopplungscode stimmt nicht"})
                        return
                    token = secrets.token_urlsafe(24)
                    stub.tokens.append(token)
                    proof = _mac(stub.code, f"{PROOF_PREFIX}|server|{stub.pin}|{nonce}|{token}")
                    stub.pair_requests.append((200, body))
                    self._send(200, {"token": token, "proof": proof})
                    return
                if not self._authorized():
                    self._send(401, {"error": "nicht angemeldet"})
                    return
                if path.startswith("/api/v1/companion/verify/"):
                    request_id = path.rsplit("/", 1)[1]
                    body = self._body()
                    gate = stub.hold
                    if gate is not None:
                        stub.hold_entered.set()
                        gate.wait(10)
                    status = stub.verify_status
                    stub.answers.append((request_id, status, body))
                    self._send(status, {})
                    return
                if path == "/api/v1/companion/user-changed":
                    body = self._body()
                    stub.user_changes.append(body)
                    self._send(200, {})
                    return
                self._send(404, {"error": "unbekannt"})

            def do_GET(self):
                if self.path != "/api/v1/companion/stream":
                    self._send(404, {"error": "unbekannt"})
                    return
                if not self._authorized():
                    self._send(401, {"error": "nicht angemeldet"})
                    return
                items = queue.Queue()
                with stub._lock:
                    stub._streams.append(items)
                    stub.open_streams += 1
                try:
                    self.send_response(200)
                    self.send_header("Content-Type", "text/event-stream")
                    self.send_header("Cache-Control", "no-cache")
                    self.end_headers()
                    self.wfile.flush()
                    while True:
                        try:
                            item = items.get(timeout=PING_SECONDS)
                        except queue.Empty:
                            self.wfile.write(b": ping\n\n")
                            self.wfile.flush()
                            continue
                        if item is None:
                            break
                        name, data = item
                        if name == "kill":
                            # RST statt sauberem Ende: der Companion sieht einen Fehler
                            self.connection.setsockopt(socket.SOL_SOCKET, socket.SO_LINGER, struct.pack("ii", 1, 0))
                            self.connection.close()
                            break
                        self.wfile.write(f"event: {name}\ndata: {json.dumps(data)}\n\n".encode())
                        self.wfile.flush()
                except (BrokenPipeError, ConnectionResetError, OSError):
                    pass
                finally:
                    with stub._lock:
                        stub._streams.remove(items)
                        stub.open_streams -= 1

        class Server(http.server.ThreadingHTTPServer):
            daemon_threads = True

            def handle_error(self, request, client_address):
                # Verbindungsabbrueche sind gewollt (Pinabfrage, geschlossene Ströme), kein Fehler des Stubs.
                if not isinstance(sys.exc_info()[1], (OSError, ssl.SSLError)):
                    super().handle_error(request, client_address)

        self.server = Server(("127.0.0.1", 0), Handler)
        context = ssl.SSLContext(ssl.PROTOCOL_TLS_SERVER)
        context.load_cert_chain(str(cert), str(key))
        self.server.socket = context.wrap_socket(self.server.socket, server_side=True)
        self.port = self.server.server_address[1]
        self.url = f"https://127.0.0.1:{self.port}"
        self._thread = threading.Thread(target=self.server.serve_forever, daemon=True)
        self._thread.start()

    def _broadcast(self, item):
        with self._lock:
            for items in self._streams:
                items.put(item)

    def push_verify(self, request_id, username, password):
        """Sendet ein verify-Ereignis an alle offenen Ströme."""
        self._broadcast(("verify", {"id": request_id, "username": username, "password": password}))

    def end_stream(self):
        """Beendet alle offenen Ströme, wie das Dashboard beim Entkoppeln."""
        self._broadcast(None)

    def kill_stream(self):
        """Bricht alle offenen Ströme mit einem Verbindungsreset ab."""
        self._broadcast(("kill", None))

    def hold_verify(self):
        """Haelt die naechste verify-Antwort zurueck, bis release_verify gerufen wird."""
        self.hold_entered.clear()
        self.hold = threading.Event()

    def release_verify(self):
        gate, self.hold = self.hold, None
        if gate is not None:
            gate.set()

    def set_verify_status(self, status):
        self.verify_status = status

    def answer(self, request_id, timeout=5.0):
        """Wartet auf die Antwort des Companions zu einer Prüfung und gibt deren Körper zurück."""
        deadline = time.monotonic() + timeout
        while time.monotonic() < deadline:
            for found_id, _status, body in list(self.answers):
                if found_id == request_id:
                    return body
            time.sleep(0.02)
        raise AssertionError(f"keine Antwort auf Prüfung {request_id}")

    def close(self):
        self.end_stream()
        self.server.shutdown()
        self.server.server_close()


def make_self_signed_cert(directory):
    """Erzeugt ein selbst signiertes Zertifikat (cert, key) im Verzeichnis."""
    cert = directory / "stub-cert.pem"
    key = directory / "stub-key.pem"
    subprocess.run(
        [
            "openssl", "req", "-x509", "-newkey", "ec",
            "-pkeyopt", "ec_paramgen_curve:prime256v1", "-nodes",
            "-keyout", str(key), "-out", str(cert),
            "-subj", "/CN=stub", "-days", "1",
        ],
        check=True,
        capture_output=True,
    )
    return cert, key


@pytest.fixture
def stub_dashboard(tmp_path):
    cert, key = make_self_signed_cert(tmp_path)
    stub = StubDashboard(cert, key)
    yield stub
    stub.close()


EXCHANGE_PEER = "peer-1"


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
                            self._write("hello", {"peer_id": EXCHANGE_PEER})
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
def stub_exchange():
    dashboard = HistoryDashboard()
    yield dashboard
    dashboard.close()
