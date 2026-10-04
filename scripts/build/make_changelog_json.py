#!/usr/bin/env python3
"""Baut changelog.json fuer das Bundle aus den CHANGELOG.md-Dateien.

Quelle sind die Markdown-Dateien, nicht die Commit-Historie: sie enthalten
eingefrorene Abschnitte aus dem Vorgaenger-Repo und von Hand nachbearbeitete
Eintraege, die Git nicht mehr hergibt. changelog.json ist nur ihre
maschinenlesbare Fassung.

    scripts/build/make_changelog_json.py --repo <repo> --out <datei> \\
        [--bundle-version vX.Y.Z] [--max-releases N]

Welche Komponenten hineinkommen (bundle: true), wie sie heissen und welcher Art
sie sind, steht in scripts/version/components.json.
"""
import argparse
import datetime
import json
import re
import sys
from pathlib import Path

SCHEMA_VERSION = 1
DEFAULT_MAX_RELEASES = 20

# Umkehrung von LABELS in scripts/generate_changelog.sh.
TYPE_BY_LABEL = {
    "Features": "feat", "Fixes": "fix", "Refactors": "refactor",
    "Performance": "perf", "Documentation": "docs", "Tests": "test",
    "Style": "style", "Chores": "chore", "Dev": "dev", "Build": "build",
    "CI": "ci", "Other": "other",
}

HEADING = re.compile(r"^## (?P<version>v\d+\.\d+\.\d+)(?: \((?P<date>\d{4}-\d{2}-\d{2})\))?\s*$")
ENTRY = re.compile(
    r"^- (?:\*\*⚠ Breaking(?: — (?P<bscope>[^*]+?))?:\*\* |\*\*(?P<scope>[^*]+?):\*\* )?"
    r"(?P<text>.*?)"
    r"(?: \(#(?P<pr>\d+)\))?"
    r"(?: \((?P<hash>[0-9a-f]{7,40})\))?$"
)


def _entry(line):
    match = ENTRY.match(line)
    if not match:
        # Nichts faellt still weg: was sich nicht zerlegen laesst, bleibt Rohtext.
        return {"text": line[2:]}
    entry = {"text": match["text"], "breaking": line.startswith("- **⚠ Breaking")}
    scope = match["bscope"] or match["scope"]
    if scope:
        entry["scope"] = scope
    if match["pr"]:
        entry["pr"] = int(match["pr"])
    if match["hash"]:
        entry["hash"] = match["hash"]
    return entry


def parse_changelog(text):
    """Zerlegt eine CHANGELOG.md in Releases (neueste zuerst, wie in der Datei).

    Uebersprungen werden "## [Unreleased]" und "## Unversioniert ..." - beides
    ist fuer einen Betreiber keine Version, zu der er "seit" fragen koennte.
    """
    releases = []
    release = group = None
    for line in text.splitlines():
        if line.startswith("## "):
            match = HEADING.match(line)
            release = group = None
            if match:
                release = {"version": match["version"], "date": match["date"] or "", "groups": []}
                releases.append(release)
        elif line.startswith("### ") and release is not None:
            label = line[4:].strip()
            group = {"type": TYPE_BY_LABEL.get(label, "other"), "label": label, "entries": []}
            release["groups"].append(group)
        elif line.startswith("- ") and group is not None:
            group["entries"].append(_entry(line.rstrip()))
    for item in releases:
        item["groups"] = [g for g in item["groups"] if g["entries"]]
    return releases


HIGHLIGHT_TYPES = {"feat", "fix", "perf"}


def mark_highlights(releases):
    """Setzt an jedem Eintrag "highlight" (bool) und gibt releases zurueck.

    Highlights sind, was ein Betreiber lesen soll; der Rest sind Details.
    Traegt ein Release Eintraege mit PR-Nummer, sind das seine Squash-Merges
    (bzw. die eigene Highlight-Zeile aus der PR-Nachricht, siehe
    scripts/generate_changelog.sh): Highlight ist ein PR-Eintrag vom Typ
    feat/fix/perf. Ohne jede PR-Nummer (Abschnitte aus dem Vorgaenger-Repo)
    zaehlt jeder feat/fix/perf-Eintrag. Ein Breaking Change ist immer eins.
    """
    for release in releases:
        entries = [e for g in release["groups"] for e in g["entries"]]
        has_pr = any("pr" in e for e in entries)
        for group in release["groups"]:
            for entry in group["entries"]:
                wanted = group["type"] in HIGHLIGHT_TYPES and ("pr" in entry or not has_pr)
                entry["highlight"] = bool(entry.get("breaking")) or wanted
    return releases


def read_version(path):
    """Version einer Komponente als "vX.Y.Z" - aus VERSION oder manifest.json."""
    raw = path.read_text(encoding="utf-8")
    if path.suffix == ".json":
        return "v" + str(json.loads(raw)["version"]).lstrip("v")
    return raw.strip()


def build_document(repo, table, *, bundle_version, max_releases, generated_at):
    components = []
    for row in table["components"]:
        if not row["bundle"]:
            continue
        changelog = repo / row["changelog"]
        releases = mark_highlights(parse_changelog(changelog.read_text(encoding="utf-8"))) if changelog.is_file() else []
        components.append({
            "id": row["id"],
            "label": row["label"],
            "kind": row["kind"],
            "version": read_version(repo / row["version_file"]),
            "releases": releases[:max_releases],
        })
    return {
        "schema_version": SCHEMA_VERSION,
        "generated_at": generated_at,
        "bundle_version": bundle_version,
        "components": components,
    }


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__.split("\n")[0])
    parser.add_argument("--repo", required=True)
    parser.add_argument("--out", required=True)
    parser.add_argument("--bundle-version")
    parser.add_argument("--max-releases", type=int, default=DEFAULT_MAX_RELEASES)
    args = parser.parse_args(argv)
    repo = Path(args.repo)
    table = json.loads((repo / "scripts/version/components.json").read_text(encoding="utf-8"))
    bundle_version = args.bundle_version or read_version(repo / "dashboard/VERSION")
    generated_at = datetime.datetime.now().astimezone().isoformat(timespec="seconds")
    document = build_document(
        repo, table, bundle_version=bundle_version,
        max_releases=args.max_releases, generated_at=generated_at,
    )
    Path(args.out).write_text(json.dumps(document, indent=2, ensure_ascii=False) + "\n", encoding="utf-8")
    entries = sum(len(g["entries"]) for c in document["components"] for r in c["releases"] for g in r["groups"])
    print(f"changelog.json: {len(document['components'])} Komponenten, {entries} Eintraege")
    return 0


if __name__ == "__main__":
    sys.exit(main())
