"""Golden-Paritaet: der Core+Adapter muss nach der Extraktion exakt dieselben
/state- und Discovery-Payloads liefern wie der Dienst vor der Extraktion
(golden/*.json, siehe _capture_golden.py)."""
import json
import sys
from pathlib import Path

import pytest

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

import mqtt_discovery
import battery_soc_mqtt as svc
from _capture_golden import SCENARIOS, base_config, FakeClient, FakeSlave

GOLDEN = Path(__file__).resolve().parent / "golden"


@pytest.mark.parametrize("name", [s[0] for s in SCENARIOS])
def test_golden_fixture_present(name):
    assert (GOLDEN / f"{name}.state.json").is_file()
    assert (GOLDEN / f"{name}.discovery.json").is_file()


def _run_new(name):
    overrides, setup, dt_hours, simulation = next(
        (o, s, d, sim) for n, o, s, d, sim in SCENARIOS if n == name)
    cfg = base_config(**overrides)
    rt = svc.BatteryRuntime(cfg)
    setup(rt)
    fake_slave = FakeSlave()
    fake_slave.simulation = {"battery_soc": simulation}
    svc.slave = fake_slave
    client = FakeClient()
    mqtt_discovery.publish_discovery(client, cfg, rt.state)
    svc.compute_and_publish(client, rt, dt_hours, simulation)
    return client.state_payload(), client.discovery_configs()


@pytest.mark.parametrize("name", [s[0] for s in SCENARIOS])
def test_state_payload_matches_golden(name):
    got, _ = _run_new(name)
    want = json.loads((GOLDEN / f"{name}.state.json").read_text())
    assert got == want


@pytest.mark.parametrize("name", [s[0] for s in SCENARIOS])
def test_discovery_configs_match_golden(name):
    _, got = _run_new(name)
    want = json.loads((GOLDEN / f"{name}.discovery.json").read_text())
    # golden ⊆ got: die neue manual-SoC-number ist eine sanktionierte Ergaenzung.
    for topic, payload in want.items():
        assert got.get(topic) == payload, topic
