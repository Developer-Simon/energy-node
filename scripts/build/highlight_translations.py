#!/usr/bin/env python3
"""Uebersetzungen der Release-Highlights pflegen und pruefen.

    highlight_translations.py sync  --repo <repo> --pr <N> --body-file <datei>
    highlight_translations.py check --repo <repo> --pr <N>
    highlight_translations.py archive --repo <repo> --tag <vX.Y.Z>
    highlight_translations.py validate --repo <repo>

sync (Version-bump-Workflow): uebertraegt die Bloecke "Highlights (xx):" und die
Tour aus der PR-Beschreibung nach changelog/highlights/<N>.json. Fehlt "en",
kopiert sync den Text der CHANGELOG-Eintraege des PRs (highlight_i18n.english_form).
Ein Block "Highlights (en):" ersetzt ihn.

check (CI): jeder Highlight-Eintrag des PRs (Squash-Eintrag "(#N)" vom Typ
feat/fix/perf oder Breaking, siehe make_changelog_json.mark_highlights) in
jeder CHANGELOG.md aus scripts/version/components.json braucht einen Text in
Englisch und jeder Sprache aus highlight_i18n.LANGUAGES. Eintraege ohne
PR-Nummer (einzelne Commits) prueft niemand.

archive --tag (Release-Workflow): faltet alle changelog/highlights/<N>.json in
changelog/highlights/<vX.Y.Z>.json, die Datei des vorigen Releases in
archive-v<MAJOR>.json, und loescht die gefalteten Dateien.

validate (CI): prueft die Texte (highlight_i18n.validate_texts), ihre Abdeckung je
Komponente, die Details und Touren aller Dateien in changelog/highlights, nicht nur
die des PRs (siehe highlight_details.validate und highlight_tours.validate). Eine
Zeile je Meldung, Exit 1 bei mindestens einer.
"""
import argparse
import json
import sys
from pathlib import Path

import highlight_details
import highlight_i18n
import highlight_tours
from make_changelog_json import mark_highlights, parse_changelog

HINT = """
Add the German text to the PR description, the Version bump workflow then
writes changelog/highlights/{pr}.json:

    Highlights (de):
    - <German text for every component>
    - <target>: <German text for one component>

scripts/dev/create-pr.sh asks for these texts when it opens the PR.
"""

HINT_EN = """
The Version bump workflow copies the English text of the changelog entry into
changelog/highlights/{pr}.json when it has none. Edit the PR description to run
it again, or set the text in the description:

    Highlights (en):
    - <English text for every component>
"""


def _table(repo):
    return json.loads((repo / "scripts/version/components.json").read_text(encoding="utf-8"))


def _highlights(repo, table):
    """(Zeile aus components.json, Version, Eintrag) fuer jeden Highlight-Eintrag
    mit PR-Nummer in allen CHANGELOG.md, in der Reihenfolge der Tabelle."""
    out = []
    for row in table["components"]:
        changelog = repo / row["changelog"]
        if not changelog.is_file():
            continue
        for release in mark_highlights(parse_changelog(changelog.read_text(encoding="utf-8"))):
            for group in release["groups"]:
                for entry in group["entries"]:
                    if entry.get("pr") and entry.get("highlight"):
                        out.append((row, release["version"], entry))
    return out


def _gaps(highlights, pr, translations):
    gaps = []
    for row, version, entry in highlights:
        if entry["pr"] != pr:
            continue
        for lang in highlight_i18n.text_languages():
            if not highlight_i18n.lookup(translations, pr, row["target"], lang):
                gaps.append({"component": row["label"], "target": row["target"],
                             "version": version, "text": entry["text"], "lang": lang})
    return gaps


def missing(repo, table, pr, translations):
    return _gaps(_highlights(repo, table), pr, translations)


def english_texts(highlights, pr):
    """{key: text} fuer "en" aus den CHANGELOG-Eintraegen von #pr, in der Form
    von highlight_i18n.english_form. "*" traegt den haeufigsten Text (bei
    Gleichstand den der ersten Komponente der Tabelle), eine Komponente mit
    anderem Text bekommt eine eigene Zeile. {} ohne Highlight. ValueError, wenn
    eine Komponente zwei verschiedene Texte fuer denselben PR hat."""
    per_target = {}
    for row, _, entry in highlights:
        if entry["pr"] != pr:
            continue
        text = highlight_i18n.english_form(entry["text"])
        known = per_target.setdefault(row["target"], text)
        if known != text:
            raise ValueError(f"#{pr} {row['target']}: two different texts in {row['changelog']}")
    if not per_target:
        return {}
    counts = {}
    for text in per_target.values():
        counts[text] = counts.get(text, 0) + 1
    default = max(counts, key=lambda text: counts[text])
    out = {highlight_i18n.DEFAULT_KEY: default}
    for target, text in per_target.items():
        if text != default:
            out[target] = text
    return out


def coverage(repo, table, translations):
    """Wie check, fuer jeden PR mit Eintrag in changelog/highlights."""
    highlights = _highlights(repo, table)
    problems = []
    for pr in sorted(translations):
        for gap in _gaps(highlights, pr, translations):
            problems.append(f"#{pr} {gap['lang']}: no text for {gap['component']} {gap['version']} \"{gap['text']}\"")
    return problems


def sync(repo, pr, body_file):
    table = _table(repo)
    body = Path(body_file).read_text(encoding="utf-8")
    blocks = highlight_i18n.parse_blocks(body, highlight_i18n.targets_from_table(table))
    try:
        tour = highlight_tours.parse_body(body)
    except ValueError as err:
        print(f"::error::Tour section of the PR description: {err}")
        return 1
    if tour is not None:
        blocks[highlight_tours.TOUR_KEY] = tour
    directory = repo / highlight_i18n.TRANSLATIONS_DIR
    english = highlight_i18n.ENGLISH
    if english in blocks:
        # Ein Block "Highlights (en):" ersetzt den englischen Text.
        blocks[english] = {key: highlight_i18n.english_form(text) for key, text in blocks[english].items()}
    elif english not in highlight_i18n.load_translations(directory).get(pr, {}):
        # Sonst nur fuellen, wenn er fehlt: aus den CHANGELOG-Eintraegen, die
        # generate_changelog.sh im selben Lauf gerade geschrieben hat. Ein
        # gekuerzter Text bleibt so erhalten.
        texts = english_texts(_highlights(repo, table), pr)
        if texts:
            blocks[english] = texts
    if not blocks:
        print("no highlight and no translation block, file left as it is")
        return 0
    if highlight_i18n.write_pr_file(directory, pr, blocks):
        print(f"wrote {directory / f'{pr}.json'}")
    else:
        print("unchanged")
    return 0


def check(repo, pr):
    translations = highlight_i18n.load_translations(repo / highlight_i18n.TRANSLATIONS_DIR)
    gaps = missing(repo, _table(repo), pr, translations)
    if not gaps:
        print(f"PR #{pr}: every highlight has a text in {', '.join(highlight_i18n.text_languages())}")
        return 0
    for gap in gaps:
        print(f"PR #{pr}, {gap['component']} {gap['version']} (target {gap['target']}): "
              f"no '{gap['lang']}' text for \"{gap['text']}\"")
    languages = {gap["lang"] for gap in gaps}
    if highlight_i18n.ENGLISH in languages:
        print(HINT_EN.format(pr=pr))
    if languages - {highlight_i18n.ENGLISH}:
        print(HINT.format(pr=pr))
    return 1


def validate(repo):
    table = _table(repo)
    translations = highlight_i18n.load_translations(repo / highlight_i18n.TRANSLATIONS_DIR)
    problems = (highlight_i18n.validate_texts(translations, highlight_i18n.targets_from_table(table))
                + coverage(repo, table, translations)
                + highlight_details.validate(repo, translations)
                + highlight_tours.validate(translations))
    for problem in problems:
        print(problem)
    return 1 if problems else 0


def archive(repo, tag):
    folded = highlight_i18n.archive_files(repo / highlight_i18n.TRANSLATIONS_DIR, tag)
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
    archive_command.add_argument("--tag", required=True)
    validate_command = sub.add_parser("validate")
    validate_command.add_argument("--repo", required=True)
    args = parser.parse_args(argv)
    repo = Path(args.repo)
    if args.command == "archive":
        return archive(repo, args.tag)
    if args.command == "validate":
        return validate(repo)
    if args.command == "sync":
        return sync(repo, args.pr, args.body_file)
    return check(repo, args.pr)


if __name__ == "__main__":
    sys.exit(main())
