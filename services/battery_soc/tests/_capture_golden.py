"""Snapshot of the MQTT service's observable output, captured BEFORE the core
extraction and re-verified against the post-extraction adapter (Task 16).

Run once from the repo root:  .venv/bin/python services/battery_soc/tests/_capture_golden.py
Re-run only if you deliberately change the service contract (then review the diff).
"""
import json
import sys
import time
from pathlib import Path

MODULE_DIR = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(MODULE_DIR))
import battery_soc_mqtt as svc  # current implementation
import mqtt_discovery

GOLDEN = Path(__file__).resolve().parent / "golden"


class FakeClient:
    def __init__(self):
        self.published = []

    def publish(self, topic, payload=None, qos=0, retain=True):
        self.published.append((topic, payload))

    def subscribe(self, *_a, **_kw):
        pass

    def state_payload(self):
        for topic, payload in reversed(self.published):
            if topic.endswith("/state"):
                return json.loads(payload)
        raise AssertionError("kein /state-Payload")

    def discovery_configs(self):
        out = {}
        for topic, payload in self.published:
            if topic.startswith("homeassistant/") and topic.endswith("/config") and payload:
                out[topic] = json.loads(payload)
        return out


class FakeSlave:
    simulation = {}

    def handle_message(self, *_a):
        return False

    def simulation_active_for(self, device_id):
        return self.simulation.get(device_id, False)

    def publish_simulation_discovery(self, *_a, **_kw):
        pass

    def start(self, *_a):
        pass

    def note_update(self, *_a):
        pass


def base_config(**overrides):
    defaults = dict(
        id="battery_soc",
        name="Batterie-Ladezustand",
        charger_power_topic="shelly/meanwell/power",
        inverter_power_topic="shelly/lumentree/power",
        bank_a_voltage_topic="bms/bank_a/voltage",
        state_file=Path("/nonexistent/golden/state.json"),
    )
    defaults.update(overrides)
    return svc.BatteryConfig(**defaults)


def set_sample(inputs, field, value, ts):
    """Setzt Messwert + Zeitstempel eines SocInputs-Samples ueber den
    field-Praefix (z.B. "charger_power", "bank_a_voltage") - die Wertspalte
    heisst je nach Feldart _w oder _v, der Zeitstempel immer _ts. Damit
    kennen die Szenario-Setups unten keine SocInputs-Attributnamen direkt und
    bleiben stabil, falls sich die Core-Feldbenennung nochmal aendert."""
    for suffix in ("_w", "_v"):
        if hasattr(inputs, field + suffix):
            setattr(inputs, field + suffix, value)
            break
    else:
        raise AttributeError(f"unbekanntes SocInputs-Feld: {field}")
    setattr(inputs, f"{field}_ts", ts)


def fresh_parallel(rt):
    now = time.time()
    i = rt.inputs
    set_sample(i, "charger_power", 300.0, now)
    set_sample(i, "inverter_power", 40.0, now)
    set_sample(i, "bank_a_voltage", 26.8, now)


def stale_power(rt):
    now = time.time()
    i = rt.inputs
    set_sample(i, "charger_power", 300.0, now - 9999)
    set_sample(i, "inverter_power", 40.0, now - 9999)
    set_sample(i, "bank_a_voltage", 26.8, now)


def fresh_series(rt):
    now = time.time()
    i = rt.inputs
    set_sample(i, "charger_power", 300.0, now)
    set_sample(i, "inverter_power", 40.0, now)
    set_sample(i, "bank_a_voltage", 26.8, now)
    set_sample(i, "bank_b_voltage", 27.4, now)


SCENARIOS = [
    ("parallel_fresh", {}, fresh_parallel, 0.25, False),
    ("parallel_stale_power", {}, stale_power, 0.25, False),
    ("parallel_strict_stale", {"require_fresh_inputs": True}, stale_power, 0.25, False),
    ("parallel_single_bank", {"bank_b_enabled": False}, fresh_parallel, 0.25, False),
    ("parallel_dc_override", {
        "charger_dc_power_topic": "trucki/t2mg/dcpower",
    }, lambda rt: (fresh_parallel(rt),
                   set_sample(rt.inputs, "charger_dc_power", 280.0, time.time())), 0.25, False),
    ("series_fresh", {
        "topology": "series",
        "bank_a_voltage_topic": "bms/bank_a/voltage",
        "bank_b_voltage_topic": "bms/bank_b/voltage",
    }, fresh_series, 0.25, False),
    ("series_imbalance", {
        "topology": "series",
        "bank_a_voltage_topic": "bms/bank_a/voltage",
        "bank_b_voltage_topic": "bms/bank_b/voltage",
        "imbalance_warn_v": 0.3,
    # Nur der Wert aendert sich, der Zeitstempel bleibt der von fresh_series -
    # die Bank ist weiterhin frisch gemessen, nur eben unsymmetrisch.
    }, lambda rt: (fresh_series(rt), setattr(rt.inputs, "bank_b_voltage_v", 25.9)), 0.25, False),
    ("parallel_simulation", {}, fresh_parallel, 0.25, True),
]


def run_scenario(name, overrides, setup, dt_hours, simulation):
    svc.configs = [base_config(**overrides)]
    svc.runtimes = [svc.BatteryRuntime(svc.configs[0])]
    fake_slave = FakeSlave()
    fake_slave.simulation = {"battery_soc": simulation}
    svc.slave = fake_slave
    rt = svc.runtimes[0]
    setup(rt)
    client = FakeClient()
    mqtt_discovery.publish_discovery(client, rt.config, rt.state)
    svc.compute_and_publish(client, rt, dt_hours, simulation)
    return client.state_payload(), client.discovery_configs()


def main():
    GOLDEN.mkdir(exist_ok=True)
    for name, overrides, setup, dt_hours, simulation in SCENARIOS:
        state, discovery = run_scenario(name, overrides, setup, dt_hours, simulation)
        (GOLDEN / f"{name}.state.json").write_text(json.dumps(state, indent=2, sort_keys=True))
        (GOLDEN / f"{name}.discovery.json").write_text(json.dumps(discovery, indent=2, sort_keys=True))
        print("captured", name)


if __name__ == "__main__":
    main()
