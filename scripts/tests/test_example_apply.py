"""Tests for scripts/bootstrap/lib/example_apply.py and the example data it applies."""
import json
import pathlib
import socket
import sys
import threading

import pytest

REPO = pathlib.Path(__file__).resolve().parents[2]
LIB = REPO / "scripts" / "bootstrap" / "lib"
SCHEMA = REPO / "services" / "automation" / "automation_rules.schema.json"
EXAMPLE_PATH = LIB / "example.json"

sys.path.insert(0, str(LIB))
import example_apply  # noqa: E402

EXAMPLE = json.loads(EXAMPLE_PATH.read_text(encoding="utf-8"))
PREFIX = "outstation/example_plug/relay/0/set"


def _dirs(tmp_path):
    devices = tmp_path / "devices"
    data = tmp_path / "data"
    devices.mkdir()
    data.mkdir()
    return devices, data


def _load(path):
    return json.loads(path.read_text(encoding="utf-8"))


def test_node_is_empty_true_for_empty_templates(tmp_path):
    devices, _ = _dirs(tmp_path)
    (devices / "shelly_devices.json").write_text("[]", encoding="utf-8")
    (devices / "apsystems_devices.json").write_text("[]", encoding="utf-8")
    (devices / "automation_rules.json").write_text(
        json.dumps({"version": 1, "settings": {}, "rules": []}), encoding="utf-8")
    assert example_apply.node_is_empty(devices) is True


def test_node_is_empty_false_with_a_device(tmp_path):
    devices, _ = _dirs(tmp_path)
    (devices / "shelly_devices.json").write_text(
        json.dumps([{"id": "eigenes_geraet"}]), encoding="utf-8")
    assert example_apply.node_is_empty(devices) is False


def test_node_is_empty_false_with_a_rule(tmp_path):
    devices, _ = _dirs(tmp_path)
    (devices / "automation_rules.json").write_text(
        json.dumps({"version": 1, "rules": [{"id": "eigene_regel"}]}), encoding="utf-8")
    assert example_apply.node_is_empty(devices) is False


def test_apply_adds_devices_and_scenario(tmp_path):
    devices, data = _dirs(tmp_path)
    example_apply.apply(EXAMPLE, devices, data)

    shelly_ids = [d["id"] for d in _load(devices / "shelly_devices.json")]
    assert "example_grid" in shelly_ids
    assert "example_plug" in shelly_ids

    rules = _load(devices / "automation_rules.json")
    assert rules["scenarios"][0]["id"] == "example_surplus"
    assert rules["settings"]["publish_allowed_prefixes"] == [PREFIX]

    assert _load(data / "example-seed.json") == EXAMPLE


def test_apply_is_idempotent(tmp_path):
    devices, data = _dirs(tmp_path)
    example_apply.apply(EXAMPLE, devices, data)
    example_apply.apply(EXAMPLE, devices, data)

    shelly_ids = [d["id"] for d in _load(devices / "shelly_devices.json")]
    apsystems_ids = [d["id"] for d in _load(devices / "apsystems_devices.json")]
    rules = _load(devices / "automation_rules.json")
    scenario_ids = [s["id"] for s in rules["scenarios"]]

    assert shelly_ids.count("example_grid") == 1
    assert shelly_ids.count("example_plug") == 1
    assert apsystems_ids.count("example_pv") == 1
    assert scenario_ids.count("example_surplus") == 1
    assert rules["settings"]["publish_allowed_prefixes"] == [PREFIX]


def test_apply_keeps_manual_prefixes(tmp_path):
    devices, data = _dirs(tmp_path)
    (devices / "automation_rules.json").write_text(json.dumps({
        "version": 1,
        "settings": {"publish_allowed_prefixes": ["x/y"]},
        "rules": [],
    }), encoding="utf-8")

    example_apply.apply(EXAMPLE, devices, data)

    rules = _load(devices / "automation_rules.json")
    assert rules["settings"]["publish_allowed_prefixes"] == ["x/y"]


def _read_packet(conn):
    first = conn.recv(1)
    if not first:
        raise OSError("connection closed")
    length, shift = 0, 0
    while True:
        byte = conn.recv(1)[0]
        length += (byte & 0x7F) << shift
        shift += 7
        if not byte & 0x80:
            break
    body = b""
    while len(body) < length:
        chunk = conn.recv(length - len(body))
        if not chunk:
            raise OSError("connection closed")
        body += chunk
    return first[0], body


def _fake_broker(connack_code=0):
    srv = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
    srv.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
    srv.bind(("127.0.0.1", 0))
    srv.listen(1)
    port = srv.getsockname()[1]
    received = []

    def serve():
        try:
            conn, _ = srv.accept()
            with conn:
                received.append(_read_packet(conn))  # CONNECT
                conn.sendall(bytes([0x20, 2, 0, connack_code]))
                if connack_code == 0:
                    received.append(_read_packet(conn))  # PUBLISH
                    received.append(_read_packet(conn))  # DISCONNECT
        except OSError:
            pass
        finally:
            srv.close()

    thread = threading.Thread(target=serve, daemon=True)
    thread.start()
    return port, received, thread


def test_publish_retained_sends_retain_flag():
    port, received, thread = _fake_broker(connack_code=0)
    example_apply.publish_retained("127.0.0.1", port, "user", "pw", PREFIX, "ON")
    thread.join(timeout=5)

    assert received[0][0] == 0x10  # CONNECT
    publish_type, publish_body = received[1]
    assert publish_type == 0x31  # PUBLISH, retain flag set, QoS 0
    assert PREFIX.encode() in publish_body
    assert publish_body.endswith(b"ON")
    assert received[2][0] == 0xE0  # DISCONNECT


def test_publish_retained_raises_on_refused_connack():
    port, _received, thread = _fake_broker(connack_code=5)
    with pytest.raises(RuntimeError):
        example_apply.publish_retained("127.0.0.1", port, "user", "pw", PREFIX, "ON")
    thread.join(timeout=5)


def test_example_matches_schema(tmp_path):
    jsonschema = pytest.importorskip("jsonschema")
    devices, data = _dirs(tmp_path)
    example_apply.apply(EXAMPLE, devices, data)

    rules = _load(devices / "automation_rules.json")
    schema = _load(SCHEMA)
    jsonschema.validate(instance=rules, schema=schema)
