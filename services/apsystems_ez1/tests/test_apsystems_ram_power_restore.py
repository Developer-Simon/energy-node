import asyncio
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
    """Deckt alle in poll_extended_info benoetigten Methoden ab."""

    def __init__(self, max_power=800, device_info_max_power=800, ram_supported=True):
        self.max_power_value = max_power
        self._device_info_max_power = device_info_max_power
        self._ram_supported = ram_supported
        self.set_max_power_calls: list[int] = []
        self.request_calls: list[str] = []

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
        self.request_calls.append(endpoint)
        if not self._ram_supported:
            raise aps.InverterReturnedError()
        if endpoint == "getDefaultMaxPower":
            return {"data": {"maxPower": str(self._device_info_max_power)}}
        if endpoint.startswith("setDefaultMaxPower"):
            new_val = int(endpoint.split("=", 1)[1])
            self._device_info_max_power = new_val
            return {"data": {"maxPower": str(new_val)}}
        raise AssertionError(f"unerwarteter Endpoint: {endpoint}")


def test_poll_extended_info_seeds_desired_power_from_first_read():
    device = make_device()
    device.inverter = FakeInverterFull(max_power=630)
    client = FakeClient()

    asyncio.run(aps.poll_extended_info(device, client, simulation_active=False))

    assert device._desired_max_power_w == 630


def test_poll_extended_info_restores_ram_power_after_drift():
    device = make_device()
    device.inverter = FakeInverterFull(max_power=630)
    client = FakeClient()
    # Erster Durchlauf: RAM-Modus erkannt, Baseline gesetzt.
    asyncio.run(aps.poll_extended_info(device, client, simulation_active=False))
    assert device._ram_mode is True

    # Wechselrichter ist ueber Nacht neu gestartet: RAM ist auf den
    # Flash-Wert (800W) zurueckgefallen.
    device.inverter.max_power_value = 800

    asyncio.run(aps.poll_extended_info(device, client, simulation_active=False))

    assert device.inverter.set_max_power_calls == [630]
    assert device.inverter.max_power_value == 630
    published = dict(
        (topic, payload) for topic, payload in client.published
    )
    assert published[f"{device.cfg.base_topic}/max_power_limit_w"] == "630"


def test_poll_extended_info_does_not_restore_without_ram_support():
    device = make_device()
    device.inverter = FakeInverterFull(max_power=630, ram_supported=False)
    client = FakeClient()
    asyncio.run(aps.poll_extended_info(device, client, simulation_active=False))
    assert device._ram_mode is False

    # Wert weicht ab, aber ohne bestaetigten RAM-Modus darf nicht
    # automatisch nachgeschrieben werden (Flash-Verschleiss vermeiden).
    device.inverter.max_power_value = 800
    asyncio.run(aps.poll_extended_info(device, client, simulation_active=False))

    assert device.inverter.set_max_power_calls == []


def test_set_max_power_safe_updates_desired_value_on_success():
    device = make_device()
    device.inverter = FakeInverterFull(max_power=630)
    client = FakeClient()

    asyncio.run(aps.set_max_power_safe(device, client, 500, simulation_active=False))

    assert device._desired_max_power_w == 500
