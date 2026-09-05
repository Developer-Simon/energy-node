"""Python-Seite der geteilten Value-Template-Fixture.

Dieselbe Datei laeuft in dashboard/internal/registry/value_template_test.go
gegen die Go-Implementierung. Weichen die beiden ab, faellt es hier auf.
"""

import json
import os
import sys
import time
from pathlib import Path

import pytest

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

import ha_template

FIXTURE = (
    Path(__file__).resolve().parents[3]
    / "dashboard/internal/registry/testdata/value-template-cases.json"
)


def _load_cases():
    # Fehlt die Datei, soll der Test scheitern - nicht still ueberspringen.
    return json.loads(FIXTURE.read_text(encoding="utf-8"))


@pytest.mark.parametrize("case", _load_cases(), ids=lambda c: c["name"])
def test_extract_value_matches_the_shared_fixture(case, monkeypatch):
    if case.get("tz"):
        assert case["tz"] == "Europe/Berlin", f"unsupported tz {case['tz']!r}"
        monkeypatch.setenv("TZ", "Europe/Berlin")
        time.tzset()
    got = ha_template.extract_value(case["payload"], case["template"])
    assert got == case["expect"], (
        f"extract_value({case['payload']!r}, {case['template']!r}) "
        f"= {got!r}, want {case['expect']!r}"
    )


@pytest.fixture(autouse=True)
def _restore_timezone():
    yield
    # monkeypatch setzt TZ zurueck, aber time.tzset() muss die Aenderung
    # noch einmal uebernehmen, sonst faerbt die Zone in andere Tests ab.
    time.tzset()
