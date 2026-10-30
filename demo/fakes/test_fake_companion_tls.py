import http.server
import ssl
import threading
import urllib.error

import pytest

from conftest import make_self_signed_cert
from fake_companion import Session


class _TlsServer(http.server.ThreadingHTTPServer):
    daemon_threads = True

    def handle_error(self, request, client_address):
        # Abgelehnte Zertifikate sind der Zweck mancher Tests, kein Fehler des Stubs.
        pass


@pytest.fixture
def tls_guest_stub(tmp_path):
    """Kleiner HTTPS-Stub mit selbst signiertem Zertifikat und Gast-Anmeldung."""
    cert, key = make_self_signed_cert(tmp_path)

    class Handler(http.server.BaseHTTPRequestHandler):
        def log_message(self, *args):
            pass

        def do_POST(self):
            if self.path == "/api/v1/auth/guest":
                self.send_response(200)
                self.send_header("Set-Cookie", "sid=guest-1; Path=/")
                self.send_header("Content-Length", "0")
                self.end_headers()
            else:
                self.send_response(404)
                self.send_header("Content-Length", "0")
                self.end_headers()

    server = _TlsServer(("127.0.0.1", 0), Handler)
    context = ssl.SSLContext(ssl.PROTOCOL_TLS_SERVER)
    context.load_cert_chain(str(cert), str(key))
    server.socket = context.wrap_socket(server.socket, server_side=True)
    thread = threading.Thread(target=server.serve_forever, daemon=True)
    thread.start()
    yield f"https://127.0.0.1:{server.server_address[1]}"
    server.shutdown()
    server.server_close()


def unverifying_context():
    """Wie console_companion: die Fake-Peers pruefen das Dashboard-Zertifikat nicht."""
    context = ssl.create_default_context()
    context.check_hostname = False
    context.verify_mode = ssl.CERT_NONE
    return context


def test_guest_login_over_https_with_unverifying_context_sets_cookie(tls_guest_stub):
    session = Session(tls_guest_stub, context=unverifying_context())

    session.login()

    assert session.cookie == "sid=guest-1"


def test_guest_login_over_https_without_context_fails_with_ssl_error(tls_guest_stub):
    # Dokumentiert, warum der Kontext noetig ist: das selbst signierte Zertifikat wird abgelehnt.
    session = Session(tls_guest_stub)

    with pytest.raises(urllib.error.URLError, match="CERTIFICATE_VERIFY_FAILED"):
        session.login()


def test_session_for_https_logs_in_without_verifying_the_certificate(tls_guest_stub):
    from fake_companion import session_for

    session = session_for(tls_guest_stub)
    session.login()
    assert session.cookie == "sid=guest-1"


def test_session_for_http_keeps_the_default_context():
    from fake_companion import session_for

    assert session_for("http://127.0.0.1:1").context is None
