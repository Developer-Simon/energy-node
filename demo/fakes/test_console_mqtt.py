import json
import threading

import pytest

import minibroker
from console_mqtt import MqttTab
from console_mqtt_ui import MQTT_HTML
from fake_console import Log
from minibroker import BrokerState

DEVICES = {
    "homeassistant/sensor/netz/power/config": {
        "availability_topic": "zaehler/netz/online", "payload_available": "true",
        "payload_not_available": "false", "device": {"name": "Netzzähler"}},
    "homeassistant/switch/entfeuchter/config": {
        "availability_topic": "plug/entfeuchter/online", "payload_available": "true",
        "payload_not_available": "false", "device": {"name": "Plug Entfeuchter"}},
}


def messages():
    found = {topic: json.dumps(config).encode("utf-8") for topic, config in DEVICES.items()}
    found["zaehler/netz/online"] = b"true"
    found["plug/entfeuchter/online"] = b"true"
    return found


@pytest.fixture
def broker_state():
    return BrokerState(messages(), 18883, True, set())


@pytest.fixture
def tab(broker_state):
    server = minibroker.make_control_server(broker_state, 0)
    threading.Thread(target=server.serve_forever, daemon=True).start()
    yield MqttTab(Log(), f"http://127.0.0.1:{server.server_address[1]}")
    server.shutdown()
    server.server_close()


def test_state_passes_broker_state_through(tab):
    state = tab.state()
    assert state["reachable"] is True
    assert state["port"] == 18883
    assert len(state["devices"]) == 2


def test_service_action_and_summary(tab, broker_state, tmp_path):
    (tmp_path / "shelly_devices.json").write_text("{}")
    minibroker.service_status_scan(str(tmp_path), broker_state, False)
    tab.act("service", {"config": "shelly_devices", "mode": "reject"})
    assert tab.summary() == {"level": "changed", "text": "1 Dienst abgelehnt"}


def test_summary_plural_and_offline(tab, broker_state):
    broker_state.set_device("zaehler/netz/online", False)
    broker_state.set_device("plug/entfeuchter/online", False)
    assert tab.summary() == {"level": "changed", "text": "2 Geräte offline"}


def test_summary_ok_text(tab):
    assert tab.summary() == {"level": "ok", "text": "Simulation läuft"}


def test_unreachable_broker():
    tab = MqttTab(Log(), "http://127.0.0.1:1")
    assert tab.state() == {"reachable": False}
    assert tab.summary() == {"level": "error", "text": "nicht erreichbar"}
    with pytest.raises(ValueError, match="MQTT-Broker nicht erreichbar"):
        tab.act("simulate", {"on": False})


def test_reset_reaches_broker(tab, broker_state):
    tab.act("simulate", {"on": False})
    assert broker_state.simulate is False
    tab.reset()
    assert broker_state.simulate is True


def test_fragment_markers():
    for marker in ('id="tab-mqtt"', "data-simulate", "data-device", "data-service", "data-topic",
                   "data-payload", "data-retain", "Alle trennen", "window.konsole.post"):
        assert marker in MQTT_HTML
