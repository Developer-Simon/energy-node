import asyncio
import json
import sys
from pathlib import Path
from types import SimpleNamespace

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

import apsystems_ez1_mqtt as aps


def make_device(**overrides) -> aps.APsystemsDevice:
    defaults = dict(id="ez1_test", name="EZ1 Test", host="192.0.2.30")
    defaults.update(overrides)
    cfg = aps.APsystemsDeviceConfig(**defaults)
    return aps.APsystemsDevice(cfg, node_device_id="test-node")


class FakeClient:
    def __init__(self):
        self.published: list[tuple[str, object]] = []

    def publish(self, topic, payload=None, qos=0, retain=True):
        self.published.append((topic, payload))

    def subscribe(self, topic):
        pass


class FakeInverterFull:
    def __init__(self, max_power=800, device_info_max_power=800, ram_supported=True):
        self.max_power_value = max_power
        self._device_info_max_power = device_info_max_power
        self._ram_supported = ram_supported
        self.set_max_power_calls: list[int] = []

    async def get_device_info(self):
        return SimpleNamespace(
            deviceId="dev1", devVer="1.9.0", ssid="wifi", ipAddr="192.0.2.30",
            minPower=30, maxPower=self._device_info_max_power, isBatterySystem=False,
        )

    async def get_alarm_info(self):
        return SimpleNamespace(
            offgrid=False, shortcircuit_1=False, shortcircuit_2=False, operating=True
        )

    async def get_max_power(self):
        return self.max_power_value

    async def set_max_power(self, value):
        self.set_max_power_calls.append(value)
        self.max_power_value = value
        return value

    async def get_device_power_status(self):
        return True

    async def _request(self, endpoint: str):
        if not self._ram_supported:
            raise aps.InverterReturnedError()
        if endpoint == "getDefaultMaxPower":
            return {"data": {"maxPower": str(self._device_info_max_power)}}
        if endpoint.startswith("setDefaultMaxPower"):
            new_val = int(endpoint.split("=", 1)[1])
            self._device_info_max_power = new_val
            return {"data": {"maxPower": str(new_val)}}
        raise AssertionError(f"unerwarteter Endpoint: {endpoint}")


def _discovery_config(client: FakeClient, object_id: str):
    topic = f"homeassistant/sensor/ez1_test/{object_id}/config"
    for t, payload in client.published:
        if t == topic:
            return json.loads(payload)
    return None


def test_discovery_includes_flash_default_diagnostic_sensor():
    device = make_device()
    client = FakeClient()

    aps.publish_device_discovery(client, device)

    config = _discovery_config(client, "max_power_flash_default")
    assert config is not None
    assert config["entity_category"] == "diagnostic"
    assert config["enabled_by_default"] is False
    assert config["unit_of_measurement"] == "W"
    assert config["state_topic"] == f"{device.cfg.base_topic}/max_power_flash_default_w"


def test_poll_extended_info_publishes_flash_default_in_ram_mode():
    device = make_device()
    device.inverter = FakeInverterFull(device_info_max_power=800)
    client = FakeClient()

    asyncio.run(aps.poll_extended_info(device, client, simulation_active=False))

    published = dict(client.published)
    assert published[f"{device.cfg.base_topic}/max_power_flash_default_w"] == "800"


def test_poll_extended_info_omits_flash_default_without_ram_mode():
    device = make_device()
    device.inverter = FakeInverterFull(ram_supported=False)
    client = FakeClient()

    asyncio.run(aps.poll_extended_info(device, client, simulation_active=False))

    topics = [t for t, _ in client.published]
    assert f"{device.cfg.base_topic}/max_power_flash_default_w" not in topics
