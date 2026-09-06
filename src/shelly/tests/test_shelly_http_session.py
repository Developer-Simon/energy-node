"""Der Shelly-Dienst hält HTTP-Verbindungen offen (Keep-alive) und baut sie
bei Verbindungsverlust neu auf. Siehe docs/knowledge/performance-and-resources.md
Abschnitt 5.1."""

import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

import shelly_rpc_mqtt as shelly


class FakeResponse:
    def __init__(self, payload=None):
        self._payload = {} if payload is None else payload

    def raise_for_status(self):
        pass

    def json(self):
        return self._payload


def test_http_get_goes_through_the_shared_session(monkeypatch):
    def forbidden(*args, **kwargs):
        raise AssertionError("modulweites requests.get statt Session benutzt")

    monkeypatch.setattr(shelly.requests, "get", forbidden)

    calls = {}

    def fake_session_get(url, auth=None, timeout=None):
        calls["url"] = url
        calls["timeout"] = timeout
        return FakeResponse({"ok": 1})

    monkeypatch.setattr(shelly._SESSION, "get", fake_session_get)

    assert shelly._http_get("http://192.0.2.1/status", timeout_s=7.5) == {"ok": 1}
    assert calls == {"url": "http://192.0.2.1/status", "timeout": 7.5}


def test_http_rpc_posts_through_the_shared_session(monkeypatch):
    def forbidden(*args, **kwargs):
        raise AssertionError("modulweites requests.post statt Session benutzt")

    monkeypatch.setattr(shelly.requests, "post", forbidden)

    seen = {}

    def fake_session_post(url, json=None, auth=None, timeout=None):
        seen["url"] = url
        seen["json"] = json
        seen["timeout"] = timeout
        return FakeResponse({"result": {"ok": 2}})

    monkeypatch.setattr(shelly._SESSION, "post", fake_session_post)

    result = shelly._http_rpc("192.0.2.2", "Shelly.GetStatus", timeout_s=4.0)

    assert result == {"ok": 2}
    assert seen["url"] == "http://192.0.2.2/rpc"
    assert seen["json"] == {"id": 1, "method": "Shelly.GetStatus"}
    assert seen["timeout"] == 4.0


def test_session_reestablishes_dropped_connections():
    """Betreiber-Anforderung: bei Verbindungsverlust muss auf jeden Fall eine
    neue Verbindung aufgebaut werden koennen. Die http://-Adapter der Session
    tragen eine Retry-Policy, die Connect-Fehler abdeckt und auch fuer POST
    (Gen2-RPC-Status) greift."""
    adapter = shelly._SESSION.get_adapter("http://192.0.2.1/")
    retry = adapter.max_retries

    assert retry.total >= 1
    assert (retry.connect or 0) >= 1
    assert retry.allowed_methods is None or "POST" in retry.allowed_methods
