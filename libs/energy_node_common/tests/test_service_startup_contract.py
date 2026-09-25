"""Jeder Geraetedienst meldet eine ungueltige Datei als rejected, statt beim
Start abzubrechen, und reicht seinen ReloadableConfig an den Slave weiter -
sonst fehlen config_revision/applied_revision im Status, und die
Konfigurationsseite wartet vergeblich auf eine Antwort (Spec Abschnitt 9)."""
from pathlib import Path

import pytest

SERVICES = Path(__file__).resolve().parents[3] / "services"
SERVICE_FILES = sorted(
    path for path in SERVICES.glob("*/*.py")
    if "tests" not in path.parts and "Slave(" in path.read_text(encoding="utf-8")
)


def _slave_calls(source):
    """Jeder Slave(...)-Aufruf samt Argumenten, Klammern ausgezaehlt."""
    calls = []
    start = source.find("Slave(")
    while start != -1:
        depth = 0
        for end in range(start + len("Slave"), len(source)):
            if source[end] == "(":
                depth += 1
            elif source[end] == ")":
                depth -= 1
                if depth == 0:
                    break
        calls.append(source[start:end + 1])
        start = source.find("Slave(", end)
    return calls


def test_every_slave_based_service_is_checked():
    names = {path.parent.name for path in SERVICE_FILES}
    assert {"apsystems_ez1", "shelly", "tuya_mqtt", "trucki", "battery_soc", "automation"} <= names


@pytest.mark.parametrize("path", SERVICE_FILES, ids=lambda p: p.parent.name)
def test_slave_gets_the_config_store(path):
    calls = _slave_calls(path.read_text(encoding="utf-8"))
    assert calls
    for call in calls:
        assert "config_store=" in call, f"{path.name}: {call[:60]}... without config_store="


@pytest.mark.parametrize("path", SERVICE_FILES, ids=lambda p: p.parent.name)
def test_startup_uses_load_or(path):
    source = path.read_text(encoding="utf-8")
    assert "config_store.load()" not in source, f"{path.name}: start aborts on an invalid file"
    assert "load_or(" in source, f"{path.name}: no fallback start"
