"""Referenzvektoren fuer den Go-Sonnenalgorithmus (dashboard/internal/sun).

Liest den Sonnenauf- und -untergang aus services/automation/sun.py fuer vier
Standorte (inkl. 70 Grad Nord) und zehn Daten im Jahr (inkl. Sonnenwenden) und
schreibt ein JSON-Array auf stdout. Jeder Eintrag hat die Felder:

  date  "YYYY-MM-DD", Kalendertag (UTC-Datum des Sonnenmittags)
  lat   Breite in Grad, Nord positiv
  lon   Laenge in Grad, Ost positiv
  rise  Sonnenaufgang als RFC3339 in UTC (Sekunden, Format "...Z"), oder null
  set   Sonnenuntergang als RFC3339 in UTC (Sekunden, Format "...Z"), oder null

null bedeutet Polartag oder Polarnacht: die Sonne geht an dem Tag nicht auf
oder nicht unter. Die Zeiten sind auf ganze Sekunden gerundet.

Aufruf (aus dem Worktree-Wurzelverzeichnis):
  .venv/bin/python3 scripts/dev/sun_vectors.py > dashboard/internal/sun/testdata/sun_vectors.json
"""

from __future__ import annotations

import datetime
import importlib.util
import json
import os
import sys

ROOT = os.path.abspath(os.path.join(os.path.dirname(__file__), "..", ".."))
SUN_PATH = os.path.join(ROOT, "services", "automation", "sun.py")

LOCATIONS = [
    (52.52, 13.405),   # Berlin
    (48.137, 11.576),  # Muenchen
    (-33.87, 151.21),  # Sydney (Suedhalbkugel)
    (70.0, 25.0),      # Nordnorwegen, Polarnacht und Polartag im Jahr
]

DATES = [
    (2026, 1, 1),
    (2026, 2, 14),
    (2026, 3, 20),
    (2026, 4, 15),
    (2026, 5, 31),
    (2026, 6, 21),
    (2026, 8, 15),
    (2026, 9, 22),
    (2026, 11, 21),
    (2026, 12, 21),
]


def _load_sun():
    spec = importlib.util.spec_from_file_location("sun_py", SUN_PATH)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


def _rfc3339(ts: float) -> str:
    moment = datetime.datetime.fromtimestamp(round(ts), datetime.timezone.utc)
    return moment.strftime("%Y-%m-%dT%H:%M:%SZ")


def main() -> int:
    sun = _load_sun()
    vectors = []
    for lat, lon in LOCATIONS:
        for year, month, day in DATES:
            date = datetime.date(year, month, day)
            result = sun.sun_times(date, lat, lon)
            if result is None:
                rise = set_ = None
            else:
                rise, set_ = _rfc3339(result[0]), _rfc3339(result[1])
            vectors.append({
                "date": date.isoformat(),
                "lat": lat,
                "lon": lon,
                "rise": rise,
                "set": set_,
            })
    json.dump(vectors, sys.stdout, indent=2)
    sys.stdout.write("\n")
    return 0


if __name__ == "__main__":
    sys.exit(main())
