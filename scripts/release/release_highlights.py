#!/usr/bin/env python3
"""Highlights block of the GitHub release notes, English and German.

    release_highlights.py --repo <repo> [--previous-tag vX.Y.Z]

Reads the same data as the dashboard and the installer: the CHANGELOG.md
files with their highlight flag and changelog/highlights/<PR>.json (see
scripts/build/make_changelog_json.py). Per bundle component it lists the
highlights of every release newer than the component's version at the previous
tag, up to its current version. A component the previous tag does not know, and
every component without a previous tag, shows its current release only. Prints
nothing when there is no highlight.
"""
import argparse
import datetime
import json
import re
import subprocess
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "build"))
from make_changelog_json import build_document, parse_version  # noqa: E402

GERMAN_SUMMARY = "Highlights auf Deutsch"
VERSION = re.compile(r"^v?(\d+)\.(\d+)\.(\d+)")


def _parts(version):
    match = VERSION.match(version or "")
    return tuple(int(n) for n in match.groups()) if match else (0, 0, 0)


def _compare(a, b):
    pa, pb = _parts(a), _parts(b)
    return (pa > pb) - (pa < pb)


def versions_at(repo, ref, table):
    out = {}
    for row in table["components"]:
        result = subprocess.run(["git", "-C", str(repo), "show", f"{ref}:{row['version_file']}"],
                                capture_output=True, text=True)
        if result.returncode == 0:
            out[row["id"]] = parse_version(result.stdout, Path(row["version_file"]).suffix)
    return out


def highlight_rows(document, previous):
    rows = []
    for component in document["components"]:
        start = previous.get(component["id"])
        for release in component["releases"]:
            if start:
                in_range = _compare(release["version"], start) > 0 and _compare(release["version"], component["version"]) <= 0
            else:
                in_range = _compare(release["version"], component["version"]) == 0
            entries = [e for g in release["groups"] for e in g["entries"] if e.get("highlight")]
            if in_range and entries:
                rows.append((component["label"], release["version"], entries))
    return rows


def _section(rows, lang):
    lines = []
    for label, version, entries in rows:
        lines += [f"### {label} {version}", ""]
        for entry in entries:
            text = (entry.get("i18n") or {}).get(lang) if lang else None
            breaking = "**⚠ Breaking** " if entry.get("breaking") else ""
            ref = f" (#{entry['pr']})" if entry.get("pr") else ""
            lines.append(f"- {breaking}{text or entry['text']}{ref}")
        lines.append("")
    return lines


def render(rows):
    if not rows:
        return ""
    lines = ["## Highlights", ""] + _section(rows, None)
    lines += ["<details>", f"<summary>{GERMAN_SUMMARY}</summary>", ""] + _section(rows, "de") + ["</details>", ""]
    return "\n".join(lines)


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__.split("\n")[0])
    parser.add_argument("--repo", required=True)
    parser.add_argument("--previous-tag", default="")
    args = parser.parse_args(argv)
    repo = Path(args.repo)
    table = json.loads((repo / "scripts/version/components.json").read_text(encoding="utf-8"))
    document = build_document(repo, table, bundle_version="", max_releases=50,
                              generated_at=datetime.datetime.now().isoformat(timespec="seconds"))
    previous = versions_at(repo, args.previous_tag, table) if args.previous_tag else {}
    sys.stdout.write(render(highlight_rows(document, previous)))
    return 0


if __name__ == "__main__":
    sys.exit(main())
