"""Testvektoren fuer das Nachspielen (replay_vectors.json).

Der Python-Dienst ist maszgeblich: dieser Test treibt die echte Engine
(Engine fuer Regeln, ScenarioEngine fuer Szenarien) mit gestellter Uhr durch
jede Folge und vergleicht Ausloesungen und Phasen.

Format je Vektor:
- "rule" oder "scenario": die Regel oder das Szenario im Dokumentformat
- "settings" (optional): Abweichungen von DEFAULT_SETTINGS
- "start": lokale Startzeit (Europe/Berlin), Startzeitpunkt der Engine
- "steps": Folge mit "t" in Sekunden ab start. Zwischen den Schritten gilt der
  letzte Zustand. Fehlt ein Schluessel, bleibt er unveraendert. "balance": null
  heisst veraltet, ein Objekt gilt als frisch. "learned": {"site/x": 12} setzt den
  Lernwert zum Zeitpunkt des Schritts, {"site/x": {"value": 12, "age_s": 60}}
  stempelt ihn 60 s frueher, {"site/x": null} entfernt ihn.
- "expect": "fires" sind die Sekunden, in denen die Engine ausloest (Regel:
  fired_at, Szenario: Start), "phases" je Schritt das Ergebnis der Regel bzw.
  die Phase des Szenarios.

Getaktet wird in tick_interval_s ab start bis zum letzten Schritt, wie der
Dienst. Schritte liegen deshalb auf diesem Takt.

Ausfuehren mit `.venv/bin/pytest services/automation/tests/test_replay_vectors.py -q`.
"""

import datetime
import json
import os
import sys
import time
from pathlib import Path

import pytest

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

import automation_mqtt as automation
from learned import learned_topic

VECTORS_PATH = Path(__file__).with_name("replay_vectors.json")
VECTORS = json.loads(VECTORS_PATH.read_text(encoding="utf-8"))

DEFAULT_SETTINGS = {
    "tick_interval_s": 10,
    "settling_seconds": 0,
    "balance_max_age_s": 30,
    "latitude": 52.52,
    "longitude": 13.405,
}
KEEP = object()


@pytest.fixture(autouse=True)
def berlin_timezone():
    """Fensterlogik und Sonnenzeiten rechnen in Ortszeit. Fest auf Europe/Berlin,
    damit die Vektoren nicht von der Zeitzone des Testrechners abhaengen."""
    previous = os.environ.get("TZ")
    os.environ["TZ"] = "Europe/Berlin"
    time.tzset()
    yield
    if previous is None:
        os.environ.pop("TZ", None)
    else:
        os.environ["TZ"] = previous
    time.tzset()


def local_ts(text: str) -> float:
    return datetime.datetime.fromisoformat(text).timestamp()


def build_document(vector: dict, tmp_path) -> automation.RulesDocument:
    settings = {**DEFAULT_SETTINGS, **vector.get("settings", {})}
    doc = {"version": 1, "settings": settings, "rules": [], "scenarios": []}
    if "rule" in vector:
        doc["rules"] = [vector["rule"]]
    else:
        doc["scenarios"] = [vector["scenario"]]
    path = tmp_path / "automation_rules.json"
    path.write_text(json.dumps(doc), encoding="utf-8")
    return automation.load_and_validate(str(path))


def replay(vector: dict, tmp_path) -> dict:
    doc = build_document(vector, tmp_path)
    tick_s = doc.settings.tick_interval_s
    steps = vector["steps"]
    start = local_ts(vector["start"])
    times = [step["t"] for step in steps]
    assert times == sorted(set(times)), "steps must be ascending and unique"
    assert all(t % tick_s == 0 for t in times), "steps must lie on the tick grid"

    executor = automation.ActionExecutor(publish_fn=lambda topic, payload, retain: None,
                                         publish_allowed_prefixes=["werkstatt/"])
    if "rule" in vector:
        engine = automation.Engine(executor, started_at=start, event_sink=lambda event: None)
        entity_id = vector["rule"]["id"]
    else:
        engine = automation.ScenarioEngine(executor, started_at=start, event_sink=lambda event: None)
        entity_id = vector["scenario"]["id"]

    by_time = {step["t"]: step for step in steps}
    balance, balance_known = None, False
    store: dict = {}
    fires, phase_at = [], {}

    for offset in range(0, times[-1] + 1, tick_s):
        step = by_time.get(offset)
        if step is not None:
            if "balance" in step:
                balance = step["balance"]
                balance_known = balance is not None
            for key, spec in step.get("learned", {}).items():
                topic = learned_topic(key)
                if spec is None:
                    store.pop(topic, None)
                    continue
                value, age = (spec["value"], spec.get("age_s", 0)) if isinstance(spec, dict) else (spec, 0)
                store[topic] = json.dumps({"value": value, "at": start + offset - age})

        now_ts = start + offset
        results = engine.tick(doc, balance=balance if balance_known else None,
                              balance_age=0 if balance_known else None,
                              topic_values=dict(store), now_ts=now_ts)
        result = results[entity_id]
        if "rule" in vector:
            if result["fired_at"] == now_ts:
                fires.append(offset)
            phase = result["result"]
        else:
            if result["phase"] == "active" and result["since"] == now_ts:
                fires.append(offset)
            phase = result["phase"]
        if step is not None:
            phase_at[offset] = phase

    return {"fires": fires, "phases": [phase_at[t] for t in times]}


@pytest.mark.parametrize("vector", VECTORS, ids=[v["name"] for v in VECTORS])
def test_replay_vector(vector, tmp_path):
    actual = replay(vector, tmp_path)
    assert actual == vector["expect"], f"{vector['name']}: actual {actual}, expected {vector['expect']}"


def test_vector_names_are_unique_and_there_are_at_least_24():
    names = [v["name"] for v in VECTORS]
    assert len(names) == len(set(names))
    assert len(names) >= 24
