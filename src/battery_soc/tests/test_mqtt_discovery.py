"""Discovery-Payload-Parity: mqtt_discovery baut aus der Core-Entity-Spec
exakt die HA-Discovery-Configs, die der Dienst vor der Extraktion publiziert
hat (golden/*.discovery.json)."""
import json
import sys
import time
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

import mqtt_discovery  # noqa: E402
from battery_soc_core.state import SocState  # noqa: E402

GOLDEN = Path(__file__).resolve().parent / "golden"


class FakeClient:
    def __init__(self):
        self.published = []

    def publish(self, topic, payload=None, qos=0, retain=True):
        self.published.append((topic, payload))


def _configs(name):
    # reuse the scenario overrides from _capture_golden
    from _capture_golden import SCENARIOS, base_config
    for n, overrides, *_ in SCENARIOS:
        if n == name:
            return base_config(**overrides)
    raise KeyError(name)


def _run(name):
    cfg = _configs(name)
    state = SocState(cfg.soc_params(), last_tick=time.time())
    client = FakeClient()
    mqtt_discovery.publish_discovery(client, cfg, state)
    got = {t: json.loads(p) for t, p in client.published
           if t.startswith("homeassistant/") and t.endswith("/config") and p}
    want = json.loads((GOLDEN / f"{name}.discovery.json").read_text())
    return got, want


def test_discovery_matches_golden_parallel_fresh():
    got, want = _run("parallel_fresh")
    # golden was captured before the manual-SoC number existed: allow the new key
    for topic in want:
        assert got.get(topic) == want[topic], topic


def test_discovery_matches_golden_series_fresh():
    got, want = _run("series_fresh")
    for topic in want:
        assert got.get(topic) == want[topic], topic


def test_publish_discovery_removes_entities_of_the_other_topology():
    """Eine retained Discovery-Config verschwindet nur durch ein leeres
    retained Payload - sonst bleibt die Entity in HA als 'nicht verfuegbar'."""
    cfg = _configs("parallel_fresh")
    state = SocState(cfg.soc_params(), last_tick=time.time())
    client = FakeClient()
    mqtt_discovery.publish_discovery(client, cfg, state)

    cleared = {topic for topic, payload in client.published if payload == ""}
    assert any(t.endswith("/soc_a/config") for t in cleared)
    assert any(t.endswith("/imbalance_warning/config") for t in cleared)
    # Was zur aktuellen Topologie gehoert, darf nicht geloescht werden.
    assert not any(t.endswith("/soc_combined/config") for t in cleared)
