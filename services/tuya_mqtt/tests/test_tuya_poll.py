import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

import pytest

import tuya_mqtt


class FakeMqttClient:
    def __init__(self):
        self.published = []

    def publish(self, topic, payload=None, qos=0, retain=True):
        self.published.append((topic, payload))


def make_device(**overrides):
    cfg = tuya_mqtt.TuyaDeviceConfig(
        id="heizungs_ventil",
        name="Heizungsventil",
        device_id="dev123",
        local_key="key123",
        ip="192.0.2.50",
        **overrides,
    )
    return tuya_mqtt.TuyaDevice(cfg)


class FakeTuyaDevice:
    """Zweiter status()-Aufruf auf demselben Objekt schlägt fehl, wie es das
    TinyTuya-'Unexpected Payload'-Muster auf dem persistenten Socket zeigt."""

    def __init__(self, fail_status=False):
        self.fail_status = fail_status
        self.status_calls = 0

    def set_version(self, version):
        pass

    def set_socketPersistent(self, persistent):
        pass

    def status(self):
        self.status_calls += 1
        if self.fail_status:
            return {"Error": "Unexpected Payload from Device", "Err": "904", "Payload": None}
        return {"dps": {"1": True}}


def test_poll_one_retries_once_after_stale_socket_error(monkeypatch):
    device = make_device()
    connections = [FakeTuyaDevice(fail_status=True), FakeTuyaDevice(fail_status=False)]
    monkeypatch.setattr(tuya_mqtt, "connect_device", lambda cfg: connections.pop(0))
    device.device = connections.pop(0)
    client = FakeMqttClient()

    tuya_mqtt.poll_one(device, client, simulation_active=False)

    topics = dict(client.published)
    assert topics["outstation/heizungs_ventil/switch"] == "ON"
    assert topics["outstation/heizungs_ventil/status/online"] == "1"


def test_poll_one_raises_when_retry_also_fails(monkeypatch):
    device = make_device()
    device.device = FakeTuyaDevice(fail_status=True)
    monkeypatch.setattr(tuya_mqtt, "connect_device", lambda cfg: FakeTuyaDevice(fail_status=True))
    client = FakeMqttClient()

    with pytest.raises(RuntimeError):
        tuya_mqtt.poll_one(device, client, simulation_active=False)
