#!/usr/bin/env python3
"""Baut changelog.json fuer das Bundle aus den CHANGELOG.md-Dateien.

Quelle sind die Markdown-Dateien, nicht die Commit-Historie: sie enthalten
eingefrorene Abschnitte aus dem Vorgaenger-Repo und von Hand nachbearbeitete
Eintraege, die Git nicht mehr hergibt. changelog.json ist nur ihre
maschinenlesbare Fassung.

    scripts/build/make_changelog_json.py --repo <repo> --out <datei> \\
        [--out-dir <verzeichnis>] [--bundle-version vX.Y.Z] [--history N]

--out bekommt nur die neueste Bundle-Version (Datei fuer aeltere Leser), --out-dir
je Bundle-Version eine Datei <version>.json (siehe changelog_history.py).

Welche Komponenten hineinkommen (bundle: true), wie sie heissen und welcher Art
sie sind, steht in scripts/version/components.json.
"""
import argparse
import datetime
import json
import re
import sys
from pathlib import Path

import changelog_history
import highlight_details
import highlight_i18n
from changelog_history import parse_version  # noqa: F401  (re-exported)

SCHEMA_VERSION = 1

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
    return parse_version(path.read_text(encoding="utf-8"), path.suffix)


def attach_translations(releases, translations, target):
    """Setzt "i18n": {lang: text} an jeden PR-Eintrag mit Uebersetzung
    (changelog/highlights/<PR>.json, siehe highlight_i18n.py)."""
    for release in releases:
        for group in release["groups"]:
            for entry in group["entries"]:
                if "pr" not in entry:
                    continue
                texts = highlight_i18n.entry_i18n(translations, entry["pr"], target)
                if texts:
                    entry["i18n"] = texts
    return releases


def attach_details(releases, repo, translations, target, image_ref):
    """Setzt "details" an jeden PR-Eintrag mit Details (Schluessel "details" in
    changelog/highlights/<PR>.json, siehe highlight_details.py).

    image_ref None: der Tag oder Commit von repo wird erst gebraucht, wenn ein
    Eintrag tatsaechlich ein Bild hat. Repos ohne Git und ohne Bild-Details
    bleiben damit lauffaehig.
    """
    ref = image_ref
    for release in releases:
        for group in release["groups"]:
            for entry in group["entries"]:
                if "pr" not in entry:
                    continue
                details = highlight_details.lookup(translations, entry["pr"], target)
                if details is None:
                    continue
                if ref is None and isinstance(details, dict) and "image" in details:
                    ref = highlight_details.image_ref(repo)
                entry["details"] = highlight_details.resolve(repo, translations, entry["pr"], target, ref)
    return releases


def build_document(repo, table, *, bundle_version, max_releases, generated_at, image_ref=None):
    translations = highlight_i18n.load_translations(repo / highlight_i18n.TRANSLATIONS_DIR)
    components = []
    for row in table["components"]:
        if not row["bundle"]:
            continue
        changelog = repo / row["changelog"]
        releases = mark_highlights(parse_changelog(changelog.read_text(encoding="utf-8"))) if changelog.is_file() else []
        target = row.get("target", row["id"])
        attach_translations(releases, translations, target)
        attach_details(releases, repo, translations, target, image_ref)
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
    parser.add_argument("--out", required=True, help="changelog.json: nur die neueste Bundle-Version (fuer aeltere Leser)")
    parser.add_argument("--out-dir", help="Verzeichnis fuer <version>.json je Bundle-Version")
    parser.add_argument("--bundle-version")
    parser.add_argument("--history", type=int, default=changelog_history.DEFAULT_HISTORY)
    parser.add_argument("--image-ref", help="Tag oder Commit fuer Bild-URLs der Details (Standard: Tag oder HEAD von --repo)")
    args = parser.parse_args(argv)
    repo = Path(args.repo)
    table = json.loads((repo / "scripts/version/components.json").read_text(encoding="utf-8"))
    bundle_version = args.bundle_version or read_version(repo / "dashboard/VERSION")
    generated_at = datetime.datetime.now().astimezone().isoformat(timespec="seconds")
    # Alle Releases: die Bereiche je Bundle-Version schneiden erst die Schritte.
    full = build_document(repo, table, bundle_version=bundle_version, max_releases=10_000,
                          generated_at=generated_at, image_ref=args.image_ref)
    steps = changelog_history.history(repo, table, bundle_version, max(args.history, 1))
    documents = [changelog_history.version_document(full, step, generated_at) for step in steps]
    write_json(Path(args.out), documents[0])
    if args.out_dir:
        out_dir = Path(args.out_dir)
        out_dir.mkdir(parents=True, exist_ok=True)
        for document in documents:
            write_json(out_dir / f"{document['bundle_version']}.json", document)
    entries = sum(len(g["entries"]) for c in documents[0]["components"] for r in c["releases"] for g in r["groups"])
    print(f"changelog: {len(documents)} Bundle-Versionen, neueste mit {entries} Eintraegen")
    return 0


def write_json(path, document):
    path.write_text(json.dumps(document, indent=2, ensure_ascii=False) + "\n", encoding="utf-8")


if __name__ == "__main__":
    sys.exit(main())
