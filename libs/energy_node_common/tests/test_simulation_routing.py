import json
import unittest

from energy_node_common.slave import Slave
from energy_node_common.settings import (
    master_settings_set_topic,
    settings_state_topic,
    settings_set_topic,
)


class FakeClient:
    def __init__(self):
        self.subscriptions = []
        self.published = []

    def subscribe(self, topic):
        self.subscriptions.append(topic)

    def publish(self, topic, payload=None, **kwargs):
        self.published.append((topic, str(payload)))


class SimulationRoutingTests(unittest.TestCase):
    def make_slave(self):
        slave = Slave(
            "service",
            lambda: None,
            node_device_id="node",
        )
        slave.register_devices(["device_a", "device_b"])
        return slave

    def test_individual_command_only_changes_target_device(self):
        client = FakeClient()
        slave = self.make_slave()

        handled = slave.handle_message(
            client,
            settings_set_topic("device_a", "simulation_active"),
            "1",
        )

        self.assertTrue(handled)
        self.assertTrue(slave.simulation_active_for("device_a"))
        self.assertFalse(slave.simulation_active_for("device_b"))
        self.assertIn(
            (settings_state_topic("device_a", "simulation_active"), "1"),
            client.published,
        )

    def test_master_command_changes_all_registered_devices(self):
        client = FakeClient()
        slave = self.make_slave()

        handled = slave.handle_message(
            client,
            master_settings_set_topic("node", "simulation_active"),
            "on",
        )

        self.assertTrue(handled)
        self.assertTrue(slave.simulation_active_for("device_a"))
        self.assertTrue(slave.simulation_active_for("device_b"))
        self.assertIn(
            (settings_state_topic("device_a", "simulation_active"), "1"),
            client.published,
        )
        self.assertIn(
            (settings_state_topic("device_b", "simulation_active"), "1"),
            client.published,
        )

    def test_register_devices_preserves_existing_state(self):
        client = FakeClient()
        slave = self.make_slave()
        slave.handle_message(
            client,
            settings_set_topic("device_a", "simulation_active"),
            "1",
        )

        slave.register_devices(["device_a", "device_c"])

        self.assertTrue(slave.simulation_active_for("device_a"))
        self.assertFalse(slave.simulation_active_for("device_c"))
        self.assertFalse(slave.simulation_active_for("device_b"))

    def test_retained_state_restores_without_republishing(self):
        client = FakeClient()
        slave = self.make_slave()

        handled = slave.handle_message(
            client,
            settings_state_topic("device_b", "simulation_active"),
            "1",
        )

        self.assertTrue(handled)
        self.assertTrue(slave.simulation_active_for("device_b"))
        self.assertEqual(client.published, [])


def test_apply_config_defaults_updates_values():
    slave = Slave(service_id="shelly", poll_core=lambda: None,
                  default_poll_interval_s=20, default_diagnostic_multiplier=15)

    slave.apply_config_defaults(poll_interval_s=30, diagnostic_multiplier=5)

    assert slave.poll_interval_s == 30
    assert slave.diagnostic_poll_multiplier == 5


def test_poll_interval_set_topic_is_ignored_config_wins():
    # Nach dem Master-Rueckbau gibt es kein /set-Topic mehr, das die Rate
    # zur Laufzeit ueberschreiben koennte: die config.json ist die Wahrheit.
    client = FakeClient()
    slave = Slave(service_id="shelly", poll_core=lambda: None,
                  default_poll_interval_s=20, default_diagnostic_multiplier=15)

    handled = slave.handle_message(
        client, "outstation/shelly/settings/poll_interval_s/set", "45"
    )

    assert handled is False
    assert slave.poll_interval_s == 20

    slave.apply_config_defaults(poll_interval_s=30, diagnostic_multiplier=5)
    assert slave.poll_interval_s == 30
    assert slave.diagnostic_poll_multiplier == 5


def test_config_reload_still_dispatches():
    client = FakeClient()
    called = []
    slave = Slave(service_id="x", poll_core=lambda: None,
                  on_config_reload=lambda: called.append(True))

    handled = slave.handle_message(client, "outstation/x/config/reload", "")

    assert handled is True
    assert called == [True]


def test_status_payload_keeps_poll_fields():
    client = FakeClient()
    slave = Slave(service_id="x", poll_core=lambda: None,
                  default_poll_interval_s=42, default_diagnostic_multiplier=7)

    slave._publish_status(client)

    topic, payload = client.published[-1]
    data = json.loads(payload)
    assert topic == "outstation/x/settings/status"
    assert data["poll_interval_s"] == 42
    assert data["diagnostic_poll_multiplier"] == 7


if __name__ == "__main__":
    unittest.main()
