#!/usr/bin/env python3
"""Ergaenzt beide Kataloge um Schluessel von stdin.

Eingabe: {"schluessel": ["English", "Deutsch"], ...}
Vorhandene Schluessel werden ueberschrieben; die Dateien bleiben sortiert.
"""
import json
import pathlib
import sys

root = pathlib.Path(__file__).resolve().parent.parent / "catalogs"
add = json.load(sys.stdin)
for name, index in (("en.json", 0), ("de.json", 1)):
    path = root / name
    data = json.loads(path.read_text(encoding="utf-8"))
    for key, texts in add.items():
        data[key] = texts[index]
    path.write_text(json.dumps(data, indent=2, ensure_ascii=False, sort_keys=True) + "\n", encoding="utf-8")
print("catalogs: %d keys added or updated" % len(add))
