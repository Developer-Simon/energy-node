#!/usr/bin/env python3
"""Sync the shared field descriptions from the battery_soc service schema into
the Home Assistant integration's English strings.

The MQTT service's JSON schema is the single source for every field that both
adapters offer (capacity, cell count, calibration tunables, ...). Each config
and options step field of the HA integration whose key is a property of the
schema gets that property's ``description`` as its ``data_description``. Fields
only HA has (entity pickers, bank layout, system type, ...) are left untouched
and stay maintained in ``strings.json``. The German translation
(``translations/de.json``) is maintained by hand.

Usage (from anywhere inside the repo)::

    .venv/bin/python scripts/sync_ha_descriptions.py            # rewrite the HA strings
    .venv/bin/python scripts/sync_ha_descriptions.py --check    # drift detector (exit 1 on drift)

Stdlib only.
"""
from __future__ import annotations

import json
import sys
from pathlib import Path

REPO = Path(__file__).resolve().parents[1]
SCHEMA_REL = Path("services/battery_soc/battery_soc_devices.schema.json")
HA_REL = Path("integrations/homeassistant/custom_components/battery_soc")
TARGETS_REL = (HA_REL / "strings.json", HA_REL / "translations/en.json")
# Same key, different meaning: HA's "name" is the config entry's title.
NOT_SHARED = frozenset({"name"})


def schema_descriptions(schema: dict) -> dict[str, str]:
    """Property name -> description of one battery entry in the schema."""
    props = schema["items"]["properties"]
    return {key: prop["description"] for key, prop in props.items()
            if prop.get("description") and key not in NOT_SHARED}


def synced(strings: dict, descriptions: dict[str, str]) -> dict:
    """The strings with every shared field's data_description taken from the schema.

    data_description follows the order of the step's data labels. Entries for
    keys that are not labelled in the step are kept at the end."""
    out = json.loads(json.dumps(strings))
    for section in ("config", "options"):
        for step in out.get(section, {}).get("step", {}).values():
            labels = step.get("data", {})
            old = step.get("data_description", {})
            new = {}
            for key in labels:
                if key in descriptions:
                    new[key] = descriptions[key]
                elif key in old:
                    new[key] = old[key]
            new.update({k: v for k, v in old.items() if k not in new})
            if new:
                step["data_description"] = new
    return out


def render(data: dict) -> str:
    return json.dumps(data, indent=2, ensure_ascii=False) + "\n"


def plan(repo: Path = REPO) -> dict[Path, str]:
    """Target path -> wanted content, only for targets that differ."""
    descriptions = schema_descriptions(json.loads((repo / SCHEMA_REL).read_text()))
    stale = {}
    for rel in TARGETS_REL:
        path = repo / rel
        current = path.read_text()
        wanted = render(synced(json.loads(current), descriptions))
        if wanted != current:
            stale[path] = wanted
    return stale


def main(argv: list[str]) -> int:
    check = "--check" in argv
    stale = plan()
    if check:
        if stale:
            for path in stale:
                print(f"out of sync with {SCHEMA_REL}: {path.relative_to(REPO)}")
            print("run `.venv/bin/python scripts/sync_ha_descriptions.py`")
            return 1
        print("HA descriptions in sync")
        return 0
    for path, content in stale.items():
        path.write_text(content)
    print(f"synced {len(stale)} file(s)")
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
