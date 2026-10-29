import json
import struct
import sys
import threading
import urllib.error
import urllib.request
from pathlib import Path

import pytest

sys.path.insert(0, str(Path(__file__).resolve().parent))
import minibroker  # noqa: E402
from minibroker import BrokerState  # noqa: E402
from test_minibroker import FakeConn as BaseFakeConn, FakeLock  # noqa: E402


class FakeConn(BaseFakeConn):
    def shutdown(self, how):
        pass


class BrokenConn(FakeConn):
    def shutdown(self, how):
        raise OSError("gebrochen")


FIXTURE = {
    "homeassistant/sensor/netz/power/config": json.dumps({"availability_topic": "zaehler/netz/online",
        "payload_available": "true", "payload_not_available": "false", "device": {"name": "Grid Meter"}}).encode(),
    "homeassistant/sensor/netz/energy/config": json.dumps({"availability_topic": "zaehler/netz/online",
        "payload_available": "true", "payload_not_available": "false", "device": {"name": "Grid Meter"}}).encode(),
    "zaehler/netz/online": b"true",
}


def make_state(messages=None, simulate=False, rejected=()):
    return BrokerState(dict(FIXTURE if messages is None else messages), 18883, simulate, set(rejected))


def scan(tmp_path, state, beat=False):
    minibroker.service_status_scan(str(tmp_path), state, beat)


def settings_status(state, service):
    return json.loads(state.messages[f"outstation/{service}/settings/status"])


def test_parse_client_id_reads_connect_payload():
    body = struct.pack("!H", 4) + b"MQTT" + b"\x04\x02\x00\x3c" + struct.pack("!H", 6) + b"client"
    assert minibroker.parse_client_id(body) == "client"
    assert minibroker.parse_client_id(b"\x00") == ""


def test_devices_are_unique_per_availability_topic():
    state = make_state()
    assert state.devices() == [{"topic": "zaehler/netz/online", "name": "Grid Meter", "online": True}]


def test_set_device_offline_sends_not_available_and_logs_warning():
    state = make_state()
    conn = FakeConn()
    state.connections[conn] = FakeLock()
    state.set_device("zaehler/netz/online", False)
    assert state.messages["zaehler/netz/online"] == b"false"
    assert minibroker.publish_packet("zaehler/netz/online", b"false", retain=False) in conn.sent
    entry = state.log.since(0)[1][-1]
    assert entry["level"] == "warn"
    assert entry["text"] == "Grid Meter offline gemeldet"
    assert state.devices()[0]["online"] is False


def test_unknown_device_and_mode_are_value_errors():
    state = make_state()
    with pytest.raises(ValueError, match="unbekanntes Gerät"):
        state.set_device("gibt/es/nicht", True)
    with pytest.raises(ValueError, match="unbekannter Modus"):
        state.set_service("shelly_devices", "kaputt")


def test_reset_restores_start_flags_and_devices():
    state = make_state(simulate=True, rejected={"shelly_devices"})
    state.set_simulate(False)
    state.set_service("shelly_devices", "accept")
    state.set_device("zaehler/netz/online", False)
    state.reset()
    assert state.simulate is True
    assert state.modes == {"shelly_devices": "reject"}
    assert state.devices()[0]["online"] is True
    assert state.log.since(0)[1][-1]["text"] == "Broker zurückgesetzt"


def test_simulate_tick_writes_no_log_entry():
    state = make_state({"pv/wechselrichter/status": b'{"apower": 0}'}, simulate=True)
    minibroker.simulate_tick(state, 1.0)
    assert json.loads(state.messages["pv/wechselrichter/status"])["apower"] != 0
    assert state.log.since(0)[1] == []


def test_simulate_off_skips_tick():
    state = make_state({"pv/wechselrichter/status": b'{"apower": 0}'}, simulate=True)
    state.set_simulate(False)
    before = dict(state.messages)
    minibroker.simulate_tick(state, 1.0)
    assert state.messages == before


def test_disconnect_all_survives_broken_sockets():
    state = make_state()
    state.connections[FakeConn()] = FakeLock()
    state.connections[BrokenConn()] = FakeLock()
    assert state.disconnect_all() == 2
    entry = state.log.since(0)[1][-1]
    assert entry["level"] == "warn"
    assert entry["text"] == "2 Verbindungen getrennt"


def test_silent_service_publishes_nothing(tmp_path):
    (tmp_path / "shelly_devices.json").write_text('{"devices": []}')
    state = make_state()
    state.set_service("shelly_devices", "silent")
    scan(tmp_path, state, beat=True)
    assert "outstation/shelly/settings/status" not in state.messages
    assert "outstation/shelly/status/online" not in state.messages


def test_switching_mode_republishes_without_file_change(tmp_path):
    (tmp_path / "battery_soc_devices.json").write_text('{"devices": []}')
    state = make_state()
    scan(tmp_path, state)
    assert settings_status(state, "battery_soc")["runtime_status"] == "ok"
    state.set_service("battery_soc_devices", "reject")
    scan(tmp_path, state, beat=False)
    assert settings_status(state, "battery_soc")["runtime_status"] == "rejected"


def test_without_control_rejected_flag_behaves_as_before(tmp_path):
    (tmp_path / "battery_soc_devices.json").write_text('{"devices": []}')
    state = make_state(rejected={"battery_soc_devices"})
    scan(tmp_path, state)
    status = settings_status(state, "battery_soc")
    assert status["runtime_status"] == "rejected"
    assert status["error"] == minibroker.REJECT_ERROR


@pytest.fixture
def control():
    state = make_state(simulate=True)
    server = minibroker.make_control_server(state, 0)
    threading.Thread(target=server.serve_forever, daemon=True).start()
    yield f"http://127.0.0.1:{server.server_address[1]}", state
    server.shutdown()
    server.server_close()


def call(base, method, path, body=None):
    data = None if body is None else json.dumps(body).encode("utf-8")
    request = urllib.request.Request(base + path, data=data, method=method,
                                     headers={"Content-Type": "application/json"})
    try:
        with urllib.request.urlopen(request, timeout=5) as response:
            return response.status, json.loads(response.read())
    except urllib.error.HTTPError as error:
        return error.code, json.loads(error.read())


def test_control_state_and_actions(control):
    base, state = control
    status, body = call(base, "GET", "/state")
    assert status == 200
    assert body["port"] == 18883
    assert body["simulate"] is True
    status, body = call(base, "POST", "/simulate", {"on": False})
    assert status == 200
    assert body["simulate"] is False
    assert state.simulate is False


def test_control_bad_input_is_400_with_error(control):
    base, _ = control
    status, body = call(base, "POST", "/service", {"config": "x", "mode": "kaputt"})
    assert status == 400
    assert body == {"error": "unbekannter Modus"}


def test_control_unknown_path_is_404(control):
    base, _ = control
    status, body = call(base, "GET", "/gibt-es-nicht")
    assert status == 404
    assert body == {"error": "nicht gefunden"}


def test_control_log_returns_entries_since(control):
    base, state = control
    state.log.add("mqtt", "eins")
    state.log.add("mqtt", "zwei")
    status, body = call(base, "GET", "/log?since=1")
    assert status == 200
    assert body["last"] == 2
    assert [entry["text"] for entry in body["entries"]] == ["zwei"]
