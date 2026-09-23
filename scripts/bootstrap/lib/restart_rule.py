#!/usr/bin/env python3
"""Entscheidet, ob eine Dienst-Unit nach einem Update neu starten muss.

Eine Stelle fuer die Regel: die Dienstschritte (service_step.sh) fragen sie
zur Laufzeit, plan.sh fuer die Vorschau des Installers. Das Go-Gegenstueck im
Dashboard (dashboard/internal/updaterhost/restart.go) wird gegen dieselbe
Fallliste geprueft (scripts/bootstrap/testdata/restart_cases.json).

Antworten:
  ""         kein Neustart noetig
  "all"      EN_RESTART=all: der Betreiber will alle Dienste neu starten
  "first"    kein installiertes Manifest oder der Dienst steht nicht darin
  "unknown"  eine Version fehlt (aelteres Manifest): im Zweifel neu starten
  "version"  die Version des Dienstes hat sich geaendert
  "library"  eine gemeinsame Bibliothek hat sich geaendert, die er importiert
"""
import json
import os
import pathlib
import sys

# Bibliothek -> Dienstverzeichnisse, die sie importieren. None = alle Dienste.
# test_restart_rule.py haelt diese Tabelle gegen die echten Imports.
LIBRARY_USERS = {
    "energy_node_common": None,
    "battery_soc_core": {"battery_soc"},
}


def _step(manifest, step_id):
    for step in (manifest or {}).get("steps") or []:
        if str(step.get("id")) == str(step_id):
            return step
    return None


def restart_reason(candidate, installed, step_id, restart_all=False):
    if restart_all:
        return "all"
    if not isinstance(candidate, dict):
        return "unknown"
    if not isinstance(installed, dict):
        return "first"
    new = _step(candidate, step_id)
    if new is None:
        return ""
    old = _step(installed, step_id)
    if old is None:
        return "first"
    if not old.get("version") or not new.get("version"):
        return "unknown"
    if old["version"] != new["version"]:
        return "version"
    old_components = installed.get("components") or {}
    new_components = candidate.get("components") or {}
    for name, users in LIBRARY_USERS.items():
        version = new_components.get(name)
        if version and old_components.get(name) != version and (users is None or new.get("dir") in users):
            return "library"
    return ""


def _load(path):
    try:
        return json.loads(pathlib.Path(path).read_text(encoding="utf-8"))
    except (OSError, ValueError):
        return None


def main(argv):
    bundle_dir, state_dir, step_id = argv[1:4]
    reason = restart_reason(
        _load(pathlib.Path(bundle_dir) / "manifest.json"),
        _load(pathlib.Path(state_dir) / "installed-manifest.json"),
        step_id,
        os.environ.get("EN_RESTART") == "all",
    )
    print(reason)


if __name__ == "__main__":
    main(sys.argv)
