#!/usr/bin/env python3
"""Uebersetzungen der Release-Highlights pflegen und pruefen.

    highlight_translations.py sync  --repo <repo> --pr <N> --body-file <datei>
    highlight_translations.py check --repo <repo> --pr <N>
    highlight_translations.py archive --repo <repo>
    highlight_translations.py validate --repo <repo>

sync (Version-bump-Workflow): uebertraegt die Bloecke "Highlights (xx):" aus
der PR-Beschreibung nach changelog/highlights/<N>.json. Ohne Block bleibt die
Datei, wie sie ist.

check (CI): jeder Highlight-Eintrag des PRs (Squash-Eintrag "(#N)" vom Typ
feat/fix/perf oder Breaking, siehe make_changelog_json.mark_highlights) in
jeder CHANGELOG.md aus scripts/version/components.json braucht einen Text in
jeder Sprache aus highlight_i18n.LANGUAGES. Eintraege ohne PR-Nummer (einzelne
Commits) prueft niemand.

archive (Release-Workflow): faltet alle changelog/highlights/<N>.json in
archive.json und loescht sie.

validate (CI): prueft die Details aller Dateien in changelog/highlights, nicht
nur die des PRs (siehe highlight_details.validate). Eine Zeile je Meldung,
Exit 1 bei mindestens einer.
"""
import argparse
import json
import sys
from pathlib import Path

import highlight_details
import highlight_i18n
from make_changelog_json import mark_highlights, parse_changelog

HINT = """
Add the German text to the PR description, the Version bump workflow then
writes changelog/highlights/{pr}.json:

    Highlights (de):
    - <German text for every component>
    - <target>: <German text for one component>

scripts/dev/create-pr.sh asks for these texts when it opens the PR.
"""


def _table(repo):
    return json.loads((repo / "scripts/version/components.json").read_text(encoding="utf-8"))


def missing(repo, table, pr, translations):
    gaps = []
    for row in table["components"]:
        changelog = repo / row["changelog"]
        if not changelog.is_file():
            continue
        for release in mark_highlights(parse_changelog(changelog.read_text(encoding="utf-8"))):
            for group in release["groups"]:
                for entry in group["entries"]:
                    if entry.get("pr") != pr or not entry.get("highlight"):
                        continue
                    for lang in highlight_i18n.LANGUAGES:
                        if not highlight_i18n.lookup(translations, pr, row["target"], lang):
                            gaps.append({"component": row["label"], "target": row["target"],
                                         "version": release["version"], "text": entry["text"], "lang": lang})
    return gaps


def sync(repo, pr, body_file):
    body = Path(body_file).read_text(encoding="utf-8")
    blocks = highlight_i18n.parse_blocks(body, highlight_i18n.targets_from_table(_table(repo)))
    if not blocks:
        print("no translation block in the PR description, file left as it is")
        return 0
    directory = repo / highlight_i18n.TRANSLATIONS_DIR
    if highlight_i18n.write_pr_file(directory, pr, blocks):
        print(f"wrote {directory / f'{pr}.json'}")
    else:
        print("unchanged")
    return 0


def check(repo, pr):
    translations = highlight_i18n.load_translations(repo / highlight_i18n.TRANSLATIONS_DIR)
    gaps = missing(repo, _table(repo), pr, translations)
    if not gaps:
        print(f"PR #{pr}: every highlight has a text in {', '.join(highlight_i18n.LANGUAGES)}")
        return 0
    for gap in gaps:
        print(f"PR #{pr}, {gap['component']} {gap['version']} (target {gap['target']}): "
              f"no '{gap['lang']}' text for \"{gap['text']}\"")
    print(HINT.format(pr=pr))
    return 1


def validate(repo):
    translations = highlight_i18n.load_translations(repo / highlight_i18n.TRANSLATIONS_DIR)
    problems = highlight_details.validate(repo, translations)
    for problem in problems:
        print(problem)
    return 1 if problems else 0


def archive(repo):
    folded = highlight_i18n.archive_files(repo / highlight_i18n.TRANSLATIONS_DIR)
    if not folded:
        print("nothing to archive")
    else:
        print(f"archived {len(folded)} files")
    return 0


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__.split("\n")[0])
    sub = parser.add_subparsers(dest="command", required=True)
    for name in ("sync", "check"):
        command = sub.add_parser(name)
        command.add_argument("--repo", required=True)
        command.add_argument("--pr", required=True, type=int)
        if name == "sync":
            command.add_argument("--body-file", required=True)
    archive_command = sub.add_parser("archive")
    archive_command.add_argument("--repo", required=True)
    validate_command = sub.add_parser("validate")
    validate_command.add_argument("--repo", required=True)
    args = parser.parse_args(argv)
    repo = Path(args.repo)
    if args.command == "archive":
        return archive(repo)
    if args.command == "validate":
        return validate(repo)
    if args.command == "sync":
        return sync(repo, args.pr, args.body_file)
    return check(repo, args.pr)


if __name__ == "__main__":
    sys.exit(main())
