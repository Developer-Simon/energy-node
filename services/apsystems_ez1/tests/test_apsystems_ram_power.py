import asyncio
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

from APsystemsEZ1 import InverterReturnedError

import apsystems_ez1_mqtt as aps


def make_device(**overrides) -> aps.APsystemsDevice:
    defaults = dict(id="ez1_test", name="EZ1 Test", host="192.0.2.30")
    defaults.update(overrides)
    cfg = aps.APsystemsDeviceConfig(**defaults)
    return aps.APsystemsDevice(cfg, node_device_id="test-node")


class FakeInverterRam:
    """Simuliert die undokumentierten getDefaultMaxPower/setDefaultMaxPower-
    Endpunkte, wie sie von APsystemsEZ1M._request() angesprochen werden."""

    def __init__(self, default_max_power=None, fail_default_max_power=False):
        self.calls: list[str] = []
        self._default_max_power = default_max_power
        self._fail = fail_default_max_power

    async def _request(self, endpoint: str):
        self.calls.append(endpoint)
        if endpoint == "getDefaultMaxPower":
            if self._fail:
                raise InverterReturnedError()
            return {"data": {"maxPower": str(self._default_max_power)}}
        if endpoint.startswith("setDefaultMaxPower"):
            new_val = int(endpoint.split("=", 1)[1])
            self._default_max_power = new_val
            return {"data": {"maxPower": str(new_val)}}
        raise AssertionError(f"unerwarteter Endpoint: {endpoint}")


def test_ensure_ram_power_mode_raises_flash_ceiling_once_below_hardware_max():
    device = make_device()
    device.inverter = FakeInverterRam(default_max_power=630)

    asyncio.run(aps.ensure_ram_power_mode(device, hardware_max_power_w=800))

    assert device._ram_mode is True
    assert device._flash_default_max_power_w == 800
    assert "setDefaultMaxPower?p=800" in device.inverter.calls


def test_ensure_ram_power_mode_skips_raise_when_flash_already_at_hardware_max():
    device = make_device()
    device.inverter = FakeInverterRam(default_max_power=800)

    asyncio.run(aps.ensure_ram_power_mode(device, hardware_max_power_w=800))

    assert device._ram_mode is True
    assert device._flash_default_max_power_w == 800
    assert not any(c.startswith("setDefaultMaxPower") for c in device.inverter.calls)


def test_ensure_ram_power_mode_falls_back_when_endpoint_unsupported():
    device = make_device()
    device.inverter = FakeInverterRam(fail_default_max_power=True)

    asyncio.run(aps.ensure_ram_power_mode(device, hardware_max_power_w=800))

    assert device._ram_mode is False
    assert device._flash_default_max_power_w is None
    assert not any(c.startswith("setDefaultMaxPower") for c in device.inverter.calls)


def test_ensure_ram_power_mode_runs_detection_only_once():
    device = make_device()
    device.inverter = FakeInverterRam(default_max_power=800)

    asyncio.run(aps.ensure_ram_power_mode(device, hardware_max_power_w=800))
    calls_after_first = list(device.inverter.calls)
    asyncio.run(aps.ensure_ram_power_mode(device, hardware_max_power_w=800))

    assert device.inverter.calls == calls_after_first
