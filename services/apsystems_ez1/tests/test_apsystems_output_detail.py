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


class FakeInverterDetail:
    def __init__(self, detail_data=None, fail=False):
        self._detail_data = detail_data
        self._fail = fail
        self.calls: list[str] = []

    async def get_device_info(self):
        return SimpleNamespace(
            deviceId="dev1", devVer="1.9.0", ssid="wifi", ipAddr="192.0.2.30",
            minPower=30, maxPower=800, isBatterySystem=False,
        )

    async def get_alarm_info(self):
        return SimpleNamespace(
            offgrid=False, shortcircuit_1=False, shortcircuit_2=False, operating=True
        )

    async def get_max_power(self):
        return 630

    async def get_device_power_status(self):
        return True

    async def _request(self, endpoint: str):
        self.calls.append(endpoint)
        if endpoint == "getDefaultMaxPower":
            raise aps.InverterReturnedError()  # RAM-Erkennung ist hier nicht Testgegenstand
        if endpoint == "getOutputDataDetail":
            if self._fail:
                raise aps.InverterReturnedError()
            return {"data": self._detail_data}
        raise AssertionError(f"unerwarteter Endpoint: {endpoint}")


def test_fetch_output_detail_parses_full_response():
    device = make_device()
    device.inverter = FakeInverterDetail(
        detail_data={"v1": "32.1", "v2": "31.8", "c1": "1.2", "c2": "1.1", "gv": "231.5", "gf": "50.01", "t": "38.2"}
    )

    detail = asyncio.run(aps.fetch_output_detail(device))

    assert detail.v1 == 32.1
    assert detail.v2 == 31.8
    assert detail.c1 == 1.2
    assert detail.c2 == 1.1
    assert detail.gv == 231.5
    assert detail.gf == 50.01
    assert detail.t == 38.2
    assert device._detail_supported is True


def test_fetch_output_detail_treats_missing_dc_fields_as_supported():
    device = make_device()
    device.inverter = FakeInverterDetail(
        detail_data={"v1": None, "v2": None, "c1": None, "c2": None, "gv": "231.5", "gf": "50.01", "t": "38.2"}
    )

    detail = asyncio.run(aps.fetch_output_detail(device))

    assert detail is not None
    assert detail.v1 is None
    assert detail.gv == 231.5
    assert device._detail_supported is True


def test_fetch_output_detail_marks_unsupported_when_no_extended_fields():
    device = make_device()
    device.inverter = FakeInverterDetail(detail_data={"p1": "100", "e1": "1.0"})

    detail = asyncio.run(aps.fetch_output_detail(device))

    assert detail is None
    assert device._detail_supported is False


def test_fetch_output_detail_marks_unsupported_on_request_failure():
    device = make_device()
    device.inverter = FakeInverterDetail(fail=True)

    detail = asyncio.run(aps.fetch_output_detail(device))

    assert detail is None
    assert device._detail_supported is False


def test_fetch_output_detail_skips_request_once_marked_unsupported():
    device = make_device()
    device.inverter = FakeInverterDetail(fail=True)

    asyncio.run(aps.fetch_output_detail(device))
    calls_after_first = list(device.inverter.calls)
    asyncio.run(aps.fetch_output_detail(device))

    assert device.inverter.calls == calls_after_first


def test_poll_extended_info_publishes_output_detail_fields():
    device = make_device()
    device.inverter = FakeInverterDetail(
        detail_data={"v1": "32.1", "v2": "31.8", "c1": "1.2", "c2": "1.1", "gv": "231.5", "gf": "50.01", "t": "38.2"}
    )
    client = FakeClient()

    asyncio.run(aps.poll_extended_info(device, client, simulation_active=False))

    published = dict(client.published)
    assert published[f"{device.cfg.base_topic}/output_detail/gv"] == "231.5"
    assert published[f"{device.cfg.base_topic}/output_detail/t"] == "38.2"


def test_poll_extended_info_simulation_publishes_output_detail_fields():
    device = make_device()
    client = FakeClient()

    asyncio.run(aps.poll_extended_info(device, client, simulation_active=True))

    topics = [t for t, _ in client.published]
    assert f"{device.cfg.base_topic}/output_detail/gv" in topics
    assert f"{device.cfg.base_topic}/output_detail/t" in topics
