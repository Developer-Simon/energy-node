"""Issue scenario (ha-battery-soc#1) end to end through the MQTT service:
dc_only, single bank, 5 cells, 3.6 Ah, one signed INA219 current (A) in both
DC slots, the discharge slot inverted."""
import json
import sys
import time
from pathlib import Path

import pytest

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

import battery_soc_mqtt as battery_soc  # noqa: E402
import mqtt_discovery  # noqa: E402
from battery_soc_core.engine import tick  # noqa: E402
from mqtt_inputs import apply_message  # noqa: E402

ISSUE_ENTRY = {
    "id": "repeater", "name": "Repeater", "system_type": "dc_only", "bank_b_enabled": False,
    "bank_a_cell_count": 5, "bank_a_capacity_ah": 3.6,
    "charger_dc_power_topic": "ina219/current", "charger_dc_power_unit": "A",
    "inverter_dc_power_topic": "ina219/current", "inverter_dc_power_unit": "A",
    "inverter_dc_power_invert": True,
    "bank_a_voltage_topic": "ina219/voltage",
}


class FakeClient:
    def __init__(self):
        self.published = []

    def publish(self, topic, payload=None, qos=0, retain=True):
        self.published.append((topic, payload))


@pytest.fixture
def runtime(tmp_path):
    path = tmp_path / "battery_soc_devices.json"
    path.write_text(json.dumps([dict(ISSUE_ENTRY, state_file=str(tmp_path / "state.json"))]))
    [config] = battery_soc.load_configs(path)
    return battery_soc.BatteryRuntime(config)


@pytest.mark.parametrize("signed_a", [0.5, -0.3])
def test_signed_current_gives_the_net_power_in_both_directions(runtime, signed_a):
    now = time.time()
    apply_message(runtime.config, runtime.inputs, "ina219/voltage", "16.5", now)
    assert apply_message(runtime.config, runtime.inputs, "ina219/current", str(signed_a), now)
    out = tick(runtime.config.soc_params(), runtime.state, runtime.inputs, now).outputs
    assert out["net_power_w"] == pytest.approx(round(signed_a * 16.5, 1))
    assert out["charger_power_source"] == "dc"
    assert out["inverter_power_source"] == "dc"
    assert out["ac_fallback_active"] is False


def test_no_ac_fallback_entity_is_announced(runtime):
    client = FakeClient()
    mqtt_discovery.publish_discovery(client, runtime.config, runtime.state)
    announced = {t: p for t, p in client.published if t.endswith("/ac_fallback/config")}
    assert announced == {"homeassistant/binary_sensor/repeater/ac_fallback/config": ""}
