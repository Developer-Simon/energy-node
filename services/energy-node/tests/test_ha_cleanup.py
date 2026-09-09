import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

import energy_node_mqtt as node


class FakeClient:
    def __init__(self):
        self.published = []

    def publish(self, topic, payload=None, qos=0, retain=False):
        self.published.append((topic, payload, retain))


def test_cleanup_clears_central_master_discovery_topics():
    client = FakeClient()
    node.cleanup_legacy_master_entities(client, "energy_node", ["apsystems", "tuya"])
    sent = {t for (t, _, _) in client.published}

    assert "homeassistant/number/energy_node/diagnostic_poll_multiplier/config" in sent
    assert "homeassistant/number/energy_node/poll_interval_s/config" in sent
    assert "homeassistant/number/energy_node/apsystems_poll_interval_s/config" in sent
    assert "homeassistant/number/energy_node/tuya_poll_interval_s/config" in sent
    assert "homeassistant/sensor/energy_node/apsystems_last_update/config" in sent
    assert "homeassistant/switch/energy_node/simulation_active/config" in sent
    assert "outstation/energy_node/settings/simulation_active" in sent
    assert "outstation/energy_node/settings/apsystems/status" in sent


def test_cleanup_is_empty_and_retained():
    client = FakeClient()
    node.cleanup_legacy_master_entities(client, "energy_node", ["apsystems"])
    assert client.published  # non-empty
    for (_, payload, retain) in client.published:
        assert payload == ""
        assert retain is True


def test_cleanup_never_touches_the_live_simulation_set_topic():
    client = FakeClient()
    node.cleanup_legacy_master_entities(client, "energy_node", ["apsystems"])
    for (topic, _, _) in client.published:
        assert not topic.endswith("/set")
