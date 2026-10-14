#!/usr/bin/env python3
"""Changelog je Bundle-Version.

Eine Bundle-Version ist ein finaler Tag vX.Y.Z (gleich dashboard/VERSION an
diesem Commit; Vorabversionen mit "-" zaehlen nicht). Ihre Datei enthaelt je
Komponente die Releases nach der Version am vorherigen finalen Tag bis
einschliesslich der Version an diesem Tag. Ohne vorherigen Tag nur das
Release, das der eigenen Version entspricht. Die neueste Version ist der
Arbeitsbaum, auch wenn er noch keinen Tag hat (lokale Bundles, Smoke-Test).
"""
import json
import re
import subprocess
from pathlib import Path

DEFAULT_HISTORY = 20
SCHEMA_VERSION = 1
VERSION = re.compile(r"^v?(\d+)\.(\d+)\.(\d+)")


def parse_version(raw, suffix):
    """Version als "vX.Y.Z" aus dem Inhalt einer VERSION- oder manifest.json-Datei."""
    if suffix == ".json":
        return "v" + str(json.loads(raw)["version"]).lstrip("v")
    return raw.strip()


def _parts(version):
    match = VERSION.match(version or "")
    return tuple(int(n) for n in match.groups()) if match else (0, 0, 0)


def compare_versions(a, b):
    pa, pb = _parts(a), _parts(b)
    return (pa > pb) - (pa < pb)


def in_range(version, start, end):
    if not start:
        return compare_versions(version, end) == 0
    return compare_versions(version, start) > 0 and compare_versions(version, end) <= 0


def _bundle_rows(table):
    return [row for row in table["components"] if row.get("bundle", True)]


def _readable_version(raw, suffix):
    """Version oder None: fehlt sie (aeltere manifest.json ohne "version") oder ist sie unlesbar,
    gilt die Komponente an diesem Tag als unbekannt, statt die Historie abzubrechen."""
    try:
        return parse_version(raw, suffix)
    except (KeyError, ValueError, TypeError, OSError):
        return None


def versions_at(repo, ref, table):
    out = {}
    for row in _bundle_rows(table):
        result = subprocess.run(["git", "-C", str(repo), "show", f"{ref}:{row['version_file']}"],
                                capture_output=True, text=True)
        if result.returncode == 0:
            version = _readable_version(result.stdout, Path(row["version_file"]).suffix)
            if version:
                out[row["id"]] = version
    return out


def versions_in_tree(repo, table):
    out = {}
    for row in _bundle_rows(table):
        path = Path(repo) / row["version_file"]
        if path.is_file():
            try:
                raw = path.read_text(encoding="utf-8")
            except (OSError, ValueError):
                continue
            version = _readable_version(raw, path.suffix)
            if version:
                out[row["id"]] = version
    return out


def final_tags(repo):
    result = subprocess.run(["git", "-C", str(repo), "tag", "--list", "v[0-9]*", "--sort=-v:refname"],
                            capture_output=True, text=True)
    if result.returncode != 0:
        return []
    return [tag for tag in result.stdout.split() if "-" not in tag and VERSION.match(tag)]


def history(repo, table, current, limit):
    """Die letzten <limit> Bundle-Versionen bis <current>, neueste zuerst."""
    older = [tag for tag in final_tags(repo) if compare_versions(tag, current) < 0]
    chain = [current] + older[:max(limit - 1, 0)]
    steps = []
    for index, version in enumerate(chain):
        previous = older[index] if index < len(older) else ""
        steps.append({
            "version": version,
            "previous": previous,
            "versions": versions_in_tree(repo, table) if index == 0 else versions_at(repo, version, table),
            "previous_versions": versions_at(repo, previous, table) if previous else {},
        })
    return steps


def version_document(document, step, generated_at):
    components = []
    for component in document["components"]:
        end = step["versions"].get(component["id"])
        if not end:
            continue
        start = step["previous_versions"].get(component["id"])
        components.append({
            **{key: value for key, value in component.items() if key != "releases"},
            "version": end,
            "releases": [r for r in component["releases"] if in_range(r["version"], start, end)],
        })
    return {
        "schema_version": SCHEMA_VERSION,
        "bundle_version": step["version"],
        "previous_bundle_version": step["previous"],
        "generated_at": generated_at,
        "components": components,
    }
