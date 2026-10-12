import struct
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))
import minibroker  # noqa: E402


def publish_body(topic, payload):
    return struct.pack("!H", len(topic)) + topic.encode() + payload


class FakeConn:
    def __init__(self):
        self.sent = []

    def sendall(self, data):
        self.sent.append(data)


class FakeLock:
    def __enter__(self):
        return self

    def __exit__(self, *args):
        return False


def test_parse_publish_reads_topic_payload_and_retain():
    assert minibroker.parse_publish(0x01, publish_body("a/b", b"x")) == ("a/b", b"x", True)
    assert minibroker.parse_publish(0x00, publish_body("a/b", b"")) == ("a/b", b"", False)


def test_parse_publish_ignores_qos_above_zero():
    # QoS 1: 2 Byte Packet-ID nach dem Topic. Der Broker bestaetigt das nicht,
    # deshalb wird es gar nicht erst uebernommen.
    assert minibroker.parse_publish(0x02, publish_body("a/b", b"\x00\x01x")) is None


def test_relay_stores_retained_and_sends_to_every_connection():
    messages = {}
    one, two = FakeConn(), FakeConn()
    connections = {one: FakeLock(), two: FakeLock()}
    minibroker.relay(messages, connections, "homeassistant/sensor/energy_node/pv_power/config", b"{}", True)
    assert messages["homeassistant/sensor/energy_node/pv_power/config"] == b"{}"
    assert one.sent and two.sent


def test_relay_does_not_store_non_retained():
    messages = {}
    conn = FakeConn()
    minibroker.relay(messages, {conn: FakeLock()}, "outstation/x/set", b"1", False)
    assert "outstation/x/set" not in messages
    assert conn.sent == [minibroker.publish_packet("outstation/x/set", b"1", retain=False)]


def test_relay_empty_retained_payload_removes_the_entry():
    messages = {"homeassistant/sensor/energy_node/pv_power/config": b"{}"}
    minibroker.relay(messages, {}, "homeassistant/sensor/energy_node/pv_power/config", b"", True)
    assert "homeassistant/sensor/energy_node/pv_power/config" not in messages
