#!/usr/bin/env python3
"""Body of the GitHub release notes: links to the docs, highlights folded per language.

    release_highlights.py --repo <repo> --tag vX.Y.Z [--previous-tag vX.Y.Z]

Reads the same data as the dashboard and the installer: the CHANGELOG.md
files with their highlight flag and changelog/highlights/<PR>.json (see
scripts/build/make_changelog_json.py). Per bundle component it lists the
highlights of every release newer than the component's version at the previous
tag, up to its current version. A component the previous tag does not know, and
every component without a previous tag, shows its current release only. Without a
highlight the two folded blocks are left out.
"""
import argparse
import datetime
import json
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "build"))
from changelog_history import in_range, versions_at  # noqa: E402
from make_changelog_json import build_document  # noqa: E402

ENGLISH_SUMMARY = "Highlights in English"
GERMAN_SUMMARY = "Highlights auf Deutsch"
DOCS_URL = "https://developer-simon.github.io/energy-node"
REPO_URL = "https://github.com/Developer-Simon/energy-node"


def links(tag):
    return " · ".join([
        f"[What's new in {tag}]({DOCS_URL}/whats-new.html#{tag})",
        f"[Component versions]({DOCS_URL}/developing/versions.html#{tag})",
        f"[Full changelog]({REPO_URL}/blob/{tag}/dashboard/CHANGELOG.md)",
    ])


def highlight_rows(document, previous):
    rows = []
    for component in document["components"]:
        start = previous.get(component["id"])
        for release in component["releases"]:
            release_in_range = in_range(release["version"], start, component["version"])
            entries = [e for g in release["groups"] for e in g["entries"] if e.get("highlight")]
            if release_in_range and entries:
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


def _fold(summary, rows, lang):
    return ["<details>", f"<summary>{summary}</summary>", ""] + _section(rows, lang) + ["</details>", ""]


def render(rows, tag):
    lines = [links(tag), ""]
    if rows:
        lines += _fold(ENGLISH_SUMMARY, rows, None) + _fold(GERMAN_SUMMARY, rows, "de")
    return "\n".join(lines)


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__.split("\n")[0])
    parser.add_argument("--repo", required=True)
    parser.add_argument("--tag", required=True)
    parser.add_argument("--previous-tag", default="")
    args = parser.parse_args(argv)
    repo = Path(args.repo)
    table = json.loads((repo / "scripts/version/components.json").read_text(encoding="utf-8"))
    document = build_document(repo, table, bundle_version="", max_releases=50,
                              generated_at=datetime.datetime.now().isoformat(timespec="seconds"))
    previous = versions_at(repo, args.previous_tag, table) if args.previous_tag else {}
    sys.stdout.write(render(highlight_rows(document, previous), args.tag))
    return 0


if __name__ == "__main__":
    sys.exit(main())
