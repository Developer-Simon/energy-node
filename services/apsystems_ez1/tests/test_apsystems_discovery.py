import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

import apsystems_ez1_mqtt as aps


class FakeClient:
    def __init__(self):
        self.published: list[tuple[str, object]] = []

    def publish(self, topic, payload=None, qos=0, retain=True):
        self.published.append((topic, payload))


def make_device() -> aps.APsystemsDevice:
    cfg = aps.APsystemsDeviceConfig(id="ez1_test", name="EZ1 Test", host="192.0.2.30")
    return aps.APsystemsDevice(cfg, node_device_id="test-node")


def test_power_status_is_announced_as_switch():
    client = FakeClient()

    aps.publish_device_discovery(client, make_device())

    topics = [topic for topic, _ in client.published]
    assert "homeassistant/switch/ez1_test/power_status/config" in topics


def test_discovery_publishes_no_removal_message():
    # The dashboard derives the unique_id of an empty discovery payload from
    # <device>_<object> and ignores the component, so a removal on the old
    # sensor topic would also drop the switch that shares that unique_id.
    client = FakeClient()

    aps.publish_device_discovery(client, make_device())

    removals = [topic for topic, payload in client.published if not payload]
    assert removals == []
