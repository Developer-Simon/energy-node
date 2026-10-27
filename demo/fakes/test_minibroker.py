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


PLUG_CONFIG = (b'{"state_topic": "plug/balkon/output", "command_topic": "plug/balkon/output/set",'
               b' "payload_on": "1", "payload_off": "0", "state_on": "ON", "state_off": "OFF"}')


def test_relay_echoes_a_switch_command_on_its_state_topic():
    # Wie ein echtes Geraet: der Befehl auf command_topic kommt als neuer
    # Zustand auf state_topic zurueck, in der state_on/state_off-Form. Abgelegt
    # wird er retained, verbundene Clients bekommen ihn aber wie bei einem
    # echten Broker mit Retain-Flag 0 - nur so gilt der Befehl im Dashboard
    # als bestaetigt (registry: pendingResolved nur bei !retained).
    messages = {"homeassistant/switch/plug_balkon/steckdose/config": PLUG_CONFIG,
                "plug/balkon/output": b"OFF"}
    conn = FakeConn()
    minibroker.relay(messages, {conn: FakeLock()}, "plug/balkon/output/set", b"1", False)
    assert messages["plug/balkon/output"] == b"ON"
    assert minibroker.publish_packet("plug/balkon/output", b"ON", retain=False) in conn.sent
    assert minibroker.publish_packet("plug/balkon/output", b"ON", retain=True) not in conn.sent
    minibroker.relay(messages, {conn: FakeLock()}, "plug/balkon/output/set", b"0", False)
    assert messages["plug/balkon/output"] == b"OFF"


def test_relay_ignores_unknown_switch_payloads_and_other_topics():
    messages = {"homeassistant/switch/plug_balkon/steckdose/config": PLUG_CONFIG,
                "plug/balkon/output": b"OFF"}
    minibroker.relay(messages, {}, "plug/balkon/output/set", b"TOGGLE", False)
    minibroker.relay(messages, {}, "plug/other/set", b"1", False)
    assert messages["plug/balkon/output"] == b"OFF"
