import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "src"))

import hashlib
import json

from energy_node_common.config import ConfigRejected, ReloadableConfig
from energy_node_common.settings import config_reload_topic, settings_status_topic
from energy_node_common.settings import SlaveStatus
from energy_node_common.slave import Slave


class FakeClient:
    def __init__(self):
        self.published = []

    def subscribe(self, topic):
        pass

    def publish(self, topic, payload=None, **kwargs):
        self.published.append((topic, payload))

    def last_status(self, service_id="svc"):
        topic = settings_status_topic(service_id)
        for published_topic, payload in reversed(self.published):
            if published_topic == topic:
                return json.loads(payload)
        raise AssertionError("no status published")


def _sha(text):
    return hashlib.sha256(text.encode("utf-8")).hexdigest()


def _store(tmp_path, text):
    path = tmp_path / "devices.json"
    path.write_text(text, encoding="utf-8")

    def loader(p):
        content = Path(p).read_text(encoding="utf-8")
        if content.startswith("bad"):
            raise ConfigRejected(f"abgelehnt: {content}", code="charge_source_required")
        return content

    return path, ReloadableConfig(path, loader)


def _slave(store, reload_calls=None):
    def on_reload():
        if reload_calls is not None:
            reload_calls.append(1)
        store.commit(store.load_candidate())

    return Slave("svc", lambda: None, on_config_reload=on_reload, config_store=store)


def test_new_status_fields_round_trip():
    status = SlaveStatus(poll_interval_s=10, diagnostic_poll_multiplier=2, actual_poll_interval_s=10,
                         runtime_status="rejected", error="kaputt", error_code="charge_source_required",
                         config_revision="abc", applied_revision="def")
    data = status.to_dict()
    assert data["error_code"] == "charge_source_required"
    assert data["config_revision"] == "abc"
    assert data["applied_revision"] == "def"
    assert SlaveStatus.from_dict(data) == status


def test_old_status_payloads_still_parse():
    status = SlaveStatus.from_dict({"poll_interval_s": 1, "runtime_status": "ok"})
    assert status.error_code == "" and status.config_revision == "" and status.applied_revision == ""


def test_status_carries_the_revisions(tmp_path):
    _, store = _store(tmp_path, "good")
    store.load()
    client = FakeClient()
    _slave(store).note_update(client, ts=1)
    status = client.last_status()
    assert status["runtime_status"] == "ok"
    assert status["config_revision"] == status["applied_revision"] == _sha("good")


def test_rejected_reload_stays_rejected_across_polls(tmp_path):
    path, store = _store(tmp_path, "good")
    store.load()
    client = FakeClient()
    slave = _slave(store)
    path.write_text("bad one", encoding="utf-8")
    slave.handle_message(client, config_reload_topic("svc"), "reload")
    slave.note_update(client, ts=2)
    slave.note_update(client, ts=3)
    status = client.last_status()
    assert status["runtime_status"] == "rejected"
    assert status["error_code"] == "charge_source_required"
    assert status["error"] == "abgelehnt: bad one"
    assert status["config_revision"] == _sha("bad one")
    assert status["applied_revision"] == _sha("good")


def test_a_successful_reload_clears_the_rejection(tmp_path):
    path, store = _store(tmp_path, "good")
    store.load()
    client = FakeClient()
    slave = _slave(store)
    path.write_text("bad", encoding="utf-8")
    slave.handle_message(client, config_reload_topic("svc"), "reload")
    path.write_text("better", encoding="utf-8")
    slave.handle_message(client, config_reload_topic("svc"), "reload")
    slave.note_update(client, ts=4)
    status = client.last_status()
    assert status["runtime_status"] == "ok"
    assert status["error"] == "" and status["error_code"] == ""
    assert status["config_revision"] == status["applied_revision"] == _sha("better")


def test_startup_rejection_survives_polls_until_a_reload_succeeds(tmp_path):
    path, store = _store(tmp_path, "bad start")
    value, error = store.load_or([])
    assert value == [] and error is not None
    client = FakeClient()
    slave = _slave(store)
    slave.start(client)
    slave.note_update(client, ts=5)
    status = client.last_status()
    assert status["runtime_status"] == "rejected"
    assert status["error_code"] == "charge_source_required"
    assert status["config_revision"] == _sha("bad start")
    assert status["applied_revision"] == ""
    slave.stop()

    path.write_text("good", encoding="utf-8")
    slave.handle_message(client, config_reload_topic("svc"), "reload")
    assert client.last_status()["runtime_status"] == "ok"


def test_a_failure_outside_the_store_is_attributed_to_the_file_on_disk(tmp_path):
    """Scheitert der Reload schon an config.json (vor load_candidate), muss
    config_revision trotzdem die gespeicherte Datei nennen, sonst wartet die
    Seite bis zum Timeout auf eine Antwort, die laengst da ist."""
    path, store = _store(tmp_path, "good")
    store.load()
    path.write_text("changed", encoding="utf-8")

    def broken_reload():
        raise RuntimeError("config.json kaputt")

    client = FakeClient()
    slave = Slave("svc", lambda: None, on_config_reload=broken_reload, config_store=store)
    slave.handle_message(client, config_reload_topic("svc"), "reload")
    status = client.last_status()
    assert status["runtime_status"] == "rejected"
    assert status["error_code"] == ""
    assert status["config_revision"] == _sha("changed")


def test_simulation_command_rejection_stays_one_shot(tmp_path):
    _, store = _store(tmp_path, "good")
    store.load()
    client = FakeClient()
    slave = Slave("svc", lambda: None, node_device_id="node", config_store=store)
    slave.register_devices(["dev"])
    slave.handle_message(client, "outstation/node/settings/simulation_active/set", "vielleicht")
    assert client.last_status()["runtime_status"] == "rejected"
    slave.note_update(client, ts=6)
    assert client.last_status()["runtime_status"] == "ok"


def test_a_mock_store_is_not_mistaken_for_a_startup_error():
    """Dienst-Tests reichen teils MagicMock-Stores herein; deren
    Attribute sind Mocks, keine Exceptions oder Strings."""
    from unittest.mock import MagicMock

    client = FakeClient()
    slave = Slave("svc", lambda: None, config_store=MagicMock())
    slave.note_update(client, ts=7)
    status = client.last_status()
    assert status["runtime_status"] == "ok"
    assert status["config_revision"] == "" and status["applied_revision"] == ""
