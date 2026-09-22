"""Tests fuer den optionalen Wake-Webhook eines sleepy Shelly-Geraets.

Ein Gen1-Shelly kann per Action/Report-URL beim Aufwachen selbst eine URL
aufrufen. Der Webhook-Listener ist standardmaessig AUS (service_config.
webhook_enabled == False/None) und muss explizit aktiviert werden. Er loest
beim Aufruf lediglich einen sofortigen Poll fuer genau das aufgerufene
Geraet aus, statt auf den naechsten Zyklus zu warten - das Geraet ist in
diesem Moment kurz wach und per HTTP erreichbar.
"""

import asyncio
import dataclasses
import http.client
import http.server
import sys
import threading
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

import shelly_rpc_mqtt as shelly


def ht_config(**overrides):
    defaults = dict(
        id="garten_ht",
        name="Shelly H&T Garten",
        host="192.0.2.30",
        generation=1,
        switch_channels=0,
        has_temperature=True,
        has_humidity=True,
        sleepy=True,
        offline_grace_s=3600.0,
    )
    defaults.update(overrides)
    return shelly.ShellyDeviceConfig(**defaults)


class FakeClient:
    def publish(self, topic, payload=None, qos=0, retain=True):
        pass


def _running_loop_service(app_config, devices, **service_overrides):
    entry = {
        "service_id": "shelly-x",
        "poll_interval_s": 20,
        "diagnostic_poll_multiplier": 15,
        "http_timeout_s": 5,
    }
    entry.update(service_overrides)
    config = app_config(services={"shelly": entry})
    service = shelly.ShellyService(devices, None, config, "shelly")
    service.client = FakeClient()
    service.loop = asyncio.new_event_loop()
    thread = threading.Thread(target=service.loop.run_forever, daemon=True)
    thread.start()
    return service, thread


def _stop(service, thread):
    service.loop.call_soon_threadsafe(service.loop.stop)
    thread.join(timeout=2)


def test_trigger_wake_polls_only_the_matching_device(app_config, monkeypatch):
    target = ht_config(id="a")
    other = ht_config(id="b")
    service, thread = _running_loop_service(app_config, [target, other])

    calls = []

    async def fake_poll_one_device(client, cfg, timeout_s, simulation_active, simulated_state, sleepy_state):
        calls.append(cfg.id)

    monkeypatch.setattr(shelly, "poll_one_device", fake_poll_one_device)

    try:
        future = service.trigger_wake("a")
        assert future is not None
        future.result(timeout=2)
    finally:
        _stop(service, thread)

    assert calls == ["a"]


def test_trigger_wake_returns_none_for_unknown_device(app_config):
    service, thread = _running_loop_service(app_config, [ht_config(id="a")])
    try:
        assert service.trigger_wake("unbekannt") is None
    finally:
        _stop(service, thread)


def test_wake_webhook_endpoint_triggers_wake_for_known_device_only(app_config, monkeypatch):
    service, thread = _running_loop_service(app_config, [ht_config(id="a")])
    triggered = []
    monkeypatch.setattr(service, "trigger_wake", lambda device_id: triggered.append(device_id))

    handler_cls = shelly.make_wake_webhook_handler(service)
    httpd = http.server.HTTPServer(("127.0.0.1", 0), handler_cls)
    server_thread = threading.Thread(target=httpd.serve_forever, daemon=True)
    server_thread.start()

    try:
        conn = http.client.HTTPConnection("127.0.0.1", httpd.server_port, timeout=2)

        conn.request("GET", "/shelly/wake/a")
        resp = conn.getresponse()
        assert resp.status == 204
        resp.read()

        conn.request("GET", "/shelly/wake/unbekannt")
        resp = conn.getresponse()
        assert resp.status == 404
        resp.read()

        conn.request("GET", "/anderer/pfad")
        resp = conn.getresponse()
        assert resp.status == 404
        resp.read()
    finally:
        httpd.shutdown()
        server_thread.join(timeout=2)
        _stop(service, thread)

    assert triggered == ["a"]


def test_webhook_server_does_not_start_when_disabled(app_config):
    service, thread = _running_loop_service(app_config, [ht_config(id="a")])
    try:
        service.start_webhook_server()
        assert service._webhook_server is None
    finally:
        _stop(service, thread)


def test_webhook_server_starts_when_enabled_and_is_reachable(app_config):
    service, thread = _running_loop_service(
        app_config, [ht_config(id="a")], webhook_enabled=True, webhook_port=8082,
    )
    # Port 0 (OS waehlt einen freien Port) ist ein gueltiger Bind-Wert, aber
    # appconfig lehnt "0" als konfigurierte Zahl ab (muss > 0 sein) - fuer den
    # Test also erst nach dem Laden auf 0 umstellen, um keinen festen Port zu belegen.
    service.service_config = dataclasses.replace(service.service_config, webhook_port=0)
    try:
        service.start_webhook_server()
        assert service._webhook_server is not None
        port = service._webhook_server.server_port

        conn = http.client.HTTPConnection("127.0.0.1", port, timeout=2)
        conn.request("GET", "/shelly/wake/unbekannt")
        resp = conn.getresponse()
        assert resp.status == 404
        resp.read()
    finally:
        service.stop_webhook_server()
        _stop(service, thread)
