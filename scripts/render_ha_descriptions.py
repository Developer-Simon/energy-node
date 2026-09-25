#!/usr/bin/env python3
"""Shared field descriptions of the Home Assistant integration battery_soc.

The MQTT service's JSON schema is the single source for every field
description both adapters offer (capacity, cell count, calibration tunables,
the source inputs, ...). In the monorepo, the integration's English strings
(``strings.json``, ``translations/en.json``) carry a placeholder instead of a
copy::

    "bank_a_capacity_ah": "[%schema:bank_a_capacity_ah%]"
    "charger_dc_power_entity": "[%schema:charger_dc_power_topic%] Current sensors ..."

An HA field matches the schema property of the same name, or ``<x>_entity``
matches ``<x>_topic``. Text after the placeholder is HA-only. Fields the schema
does not have keep their full text. ``translations/de.json`` is maintained by
hand and must not contain placeholders.

``scripts/publish_mirror.sh`` renders the placeholders into the mirror tree,
and the mirror's release workflow refuses to release while one is left.

Usage (from anywhere inside the repo)::

    .venv/bin/python scripts/render_ha_descriptions.py --check
        lint the monorepo strings (placeholder used where a schema text exists,
        every placeholder resolves, none in de.json)
    .venv/bin/python scripts/render_ha_descriptions.py --render DIR --schema SCHEMA
        replace every placeholder in DIR/strings.json and DIR/translations/*.json

Stdlib only.
"""
from __future__ import annotations

import argparse
import json
import re
import sys
from pathlib import Path

REPO = Path(__file__).resolve().parents[1]
SCHEMA_REL = Path("services/battery_soc/battery_soc_devices.schema.json")
HA_REL = Path("integrations/homeassistant/custom_components/battery_soc")
PLACEHOLDER_FILES = ("strings.json", "translations/en.json")
HAND_FILES = ("translations/de.json",)
PLACEHOLDER = re.compile(r"\[%schema:([a-z0-9_]+)%\]")
# Same key, different meaning: HA's "name" is the config entry's title.
NOT_SHARED = frozenset({"name"})


def _properties(node: dict) -> dict:
    """Properties of an object schema including its conditional branches
    (allOf / then / else). The conditions themselves (if) do not count."""
    props = dict(node.get("properties", {}))
    for entry in node.get("allOf", []):
        props.update(_properties(entry))
    for key in ("then", "else"):
        if key in node:
            props.update(_properties(node[key]))
    return props


def schema_descriptions(schema_path: Path) -> dict[str, str]:
    """Property name -> description of one battery entry in the schema."""
    props = _properties(json.loads(schema_path.read_text())["items"])
    return {key: prop["description"] for key, prop in props.items() if prop.get("description")}


def counterpart(key: str, descriptions: dict[str, str]) -> str | None:
    """The schema property whose description an HA field shares, if any."""
    if key in NOT_SHARED:
        return None
    if key in descriptions:
        return key
    if key.endswith("_entity") and key[: -len("_entity")] + "_topic" in descriptions:
        return key[: -len("_entity")] + "_topic"
    return None


def _descriptions(strings: dict):
    """(where, field, text) for every data_description entry."""
    for section in ("config", "options"):
        for step_id, step in strings.get(section, {}).get("step", {}).items():
            for key, text in step.get("data_description", {}).items():
                yield f"{section}.step.{step_id}.data_description.{key}", key, text


def _strings(value):
    if isinstance(value, dict):
        for v in value.values():
            yield from _strings(v)
    elif isinstance(value, str):
        yield value


def lint(repo: Path = REPO) -> list[str]:
    """Problems in the monorepo strings; empty when everything is in order."""
    descriptions = schema_descriptions(repo / SCHEMA_REL)
    problems = []
    for rel in PLACEHOLDER_FILES:
        strings = json.loads((repo / HA_REL / rel).read_text())
        for where, key, text in _descriptions(strings):
            source = counterpart(key, descriptions)
            if source and not text.startswith(f"[%schema:{source}%]"):
                problems.append(f"{rel}: {where} must start with [%schema:{source}%]")
        for text in _strings(strings):
            for name in PLACEHOLDER.findall(text):
                if name not in descriptions:
                    problems.append(f"{rel}: [%schema:{name}%] has no schema description")
    for rel in HAND_FILES:
        strings = json.loads((repo / HA_REL / rel).read_text())
        if any(PLACEHOLDER.search(text) for text in _strings(strings)):
            problems.append(f"{rel}: placeholders are only allowed in {', '.join(PLACEHOLDER_FILES)}")
    return problems


def render_text(text: str, descriptions: dict[str, str]) -> str:
    def replace(match):
        name = match.group(1)
        if name not in descriptions:
            raise KeyError(f"[%schema:{name}%] has no schema description")
        return descriptions[name]
    return PLACEHOLDER.sub(replace, text)


def render_value(value, descriptions: dict[str, str]):
    if isinstance(value, dict):
        return {k: render_value(v, descriptions) for k, v in value.items()}
    if isinstance(value, str):
        return render_text(value, descriptions)
    return value


def render_tree(component_dir: Path, schema_path: Path) -> list[Path]:
    """Render every placeholder in the component's string files, in place."""
    descriptions = schema_descriptions(schema_path)
    files = [component_dir / "strings.json", *sorted((component_dir / "translations").glob("*.json"))]
    changed = []
    for path in files:
        if not path.is_file():
            continue
        current = path.read_text()
        if not PLACEHOLDER.search(current):
            continue
        rendered = render_value(json.loads(current), descriptions)
        path.write_text(json.dumps(rendered, indent=2, ensure_ascii=False) + "\n")
        changed.append(path)
    return changed


def main(argv: list[str]) -> int:
    parser = argparse.ArgumentParser(description=__doc__.split("\n\n")[0])
    mode = parser.add_mutually_exclusive_group(required=True)
    mode.add_argument("--check", action="store_true")
    mode.add_argument("--render", type=Path, metavar="DIR")
    parser.add_argument("--schema", type=Path, default=REPO / SCHEMA_REL)
    args = parser.parse_args(argv)

    if args.check:
        problems = lint()
        for problem in problems:
            print(problem)
        if problems:
            print(f"fix the strings or {SCHEMA_REL}, see this script's docstring")
            return 1
        print("HA schema descriptions OK")
        return 0

    changed = render_tree(args.render, args.schema)
    print(f"rendered schema descriptions into {len(changed)} file(s)")
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
