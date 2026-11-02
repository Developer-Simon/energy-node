#!/usr/bin/env python3
"""Touren zu den Release-Highlights: Schritte durch die Web-UI.

Ein Highlight in changelog/highlights/<PR>.json kann einen Schluessel "tour"
haben, neben den Sprachschluesseln und "details":

    "tour": {
      "*": {"steps": [{"panel": "devices-panel", "page": "users",
                       "target": "favorites", "device": "any",
                       "need": "view_devices",
                       "title": {"en": "...", "de": "..."},
                       "text": {"en": "...", "de": "..."}}]}
    }

Hat das Highlight nichts zu zeigen, steht dort eine Ausnahme mit Begruendung:

    "tour": {"*": {"exempt": "Only the backend changed"}}

"*" gilt fuer jede Komponente, ein Schluessel mit dem Ziel nur fuer diese
Komponente und ersetzt das "*"-Objekt ganz (wie bei highlight_details).
target ist eine data-tour-ID der Web-UI und Pflicht. panel, page und need
sind optional, device ist any, phone oder desktop (Standard any). title und
text brauchen Englisch und jede Sprache aus highlight_i18n.LANGUAGES.
open ist optional und eine nicht leere Liste aus data-tour-IDs oder @Aktionen,
die vor dem Schritt geöffnet werden. replaced_by ist optional und nennt als
{"pr": N, "step": N} den PR und Schritt, die diese Tour ersetzen.

    highlight_tours.py check --repo <repo> --pr <N>

check (CI): jedes Highlight des PRs in einer Komponente aus WEB_UI_TARGETS
braucht eine Tour oder eine Ausnahme. PRs vor TOUR_REQUIRED_FROM_PR und die
Archive v*.json und archive-v*.json prueft niemand.
"""
import argparse
import json
import re
import sys
from pathlib import Path

import highlight_i18n as hi

TOUR_KEY = "tour"
DEFAULT_KEY = "*"
DEVICES = ("any", "phone", "desktop")
STEP_KEYS = {"panel", "page", "target", "device", "need", "open", "title", "text"}
OPTIONAL_KEYS = ("panel", "page", "need")
OPEN_ENTRY = re.compile(r"^@?[a-z0-9_-]+$")
REPLACED_BY_PROBLEM = 'replaced_by must be {"pr": <number>, "step": <number from 1>}'
# Erster PR des Onboardings (#186): aeltere PR-Dateien brauchen keine Tour.
TOUR_REQUIRED_FROM_PR = 186
# Komponenten, deren Highlights die Web-UI betreffen.
WEB_UI_TARGETS = ("dashboard",)

HINT = """
Add a tour to changelog/highlights/{pr}.json, next to the language keys:

    "tour": {{"*": {{"steps": [{{"target": "<data-tour id>",
      "title": {{"en": "...", "de": "..."}}, "text": {{"en": "...", "de": "..."}}}}]}}}}

A highlight with nothing to show gets "tour": {{"*": {{"exempt": "<reason>"}}}},
only after the user agreed to the exemption. See docs/developing/releasing.md.
"""


def _text_languages():
    """Englisch plus die Sprachen aus highlight_i18n, zur Laufzeit gelesen."""
    return ("en", *hi.LANGUAGES)


def _select(tours, target):
    """(Schluessel, Eintrag) fuer das Ziel: der Schluessel target, sonst "*".
    (None, None), wenn keiner der beiden da ist."""
    if target in tours:
        return target, tours[target]
    if DEFAULT_KEY in tours:
        return DEFAULT_KEY, tours[DEFAULT_KEY]
    return None, None


def _tours_problem(pr):
    return f"#{pr} {TOUR_KEY}: must be an object keyed by component"


def _texts_problems(prefix, name, texts):
    if not isinstance(texts, dict):
        return [prefix + f"{name} must be an object keyed by language"]
    problems = []
    for lang in _text_languages():
        if lang not in texts:
            problems.append(prefix + f"{name} for {lang} is missing")
        elif not isinstance(texts[lang], str) or not texts[lang].strip():
            problems.append(prefix + f"{name} for {lang} is empty")
    return problems


def _open_is_valid(value):
    return (isinstance(value, list) and len(value) > 0
            and all(isinstance(item, str) and OPEN_ENTRY.fullmatch(item) for item in value))


def _replaced_by_is_valid(value):
    if not isinstance(value, dict) or set(value) != {"pr", "step"}:
        return False
    return all(type(value[key]) is int and value[key] >= 1 for key in ("pr", "step"))


def _step_problems(prefix, step):
    if not isinstance(step, dict):
        return [prefix + "step must be an object"]
    problems = [prefix + f"unknown key {key}" for key in sorted(set(step) - STEP_KEYS)]
    target = step.get("target")
    if not isinstance(target, str) or not target.strip():
        problems.append(prefix + "target must be a data-tour id")
    for key in OPTIONAL_KEYS:
        if key in step and (not isinstance(step[key], str) or not step[key].strip()):
            problems.append(prefix + f"{key} must be a non-empty string")
    if "device" in step and step["device"] not in DEVICES:
        problems.append(prefix + f"device must be one of {', '.join(DEVICES)}")
    if "open" in step and not _open_is_valid(step["open"]):
        problems.append(prefix + "open must be a non-empty list of data-tour ids or @actions")
    problems.extend(_texts_problems(prefix, "title", step.get("title")))
    problems.extend(_texts_problems(prefix, "text", step.get("text")))
    return problems


def _entry_problems(pr, key, entry):
    prefix = f"#{pr} {TOUR_KEY} {key}: "
    if not isinstance(entry, dict):
        return [prefix + "must be an object with steps or exempt"]
    if "exempt" in entry:
        if set(entry) != {"exempt"}:
            return [prefix + "exempt stands alone, without steps"]
        reason = entry["exempt"]
        if not isinstance(reason, str) or not reason.strip():
            return [prefix + "exempt needs a reason"]
        return []
    problems = [prefix + f"unknown key {name}" for name in sorted(set(entry) - {"steps", "replaced_by"})]
    if "replaced_by" in entry and not _replaced_by_is_valid(entry["replaced_by"]):
        problems.append(prefix + REPLACED_BY_PROBLEM)
    steps = entry.get("steps")
    if not isinstance(steps, list) or not steps:
        return problems + [prefix + "steps must be a non-empty list, or set exempt with a reason"]
    for index, step in enumerate(steps, start=1):
        problems.extend(_step_problems(prefix + f"step {index}: ", step))
    return problems


def validate(translations):
    """Eine Meldung je Fehler, Form "#<pr> tour <key>: <problem>". Leere Liste,
    wenn alle Touren stimmen."""
    problems = []
    for pr in sorted(translations):
        if TOUR_KEY not in translations[pr]:
            continue
        tours = translations[pr][TOUR_KEY]
        if not isinstance(tours, dict):
            problems.append(_tours_problem(pr))
            continue
        for key in sorted(tours):
            problems.extend(_entry_problems(pr, key, tours[key]))
    return problems


def resolve(translations, pr, target):
    """Tour fuer ein Highlight und eine Komponente in der Form fuer
    changelog.json, None ohne Tour. device ist immer gesetzt, eine Ausnahme
    hat leere steps. Kaputte Tour: ValueError mit der Meldung aus validate."""
    if TOUR_KEY not in translations.get(pr, {}):
        return None
    tours = translations[pr][TOUR_KEY]
    if not isinstance(tours, dict):
        raise ValueError(_tours_problem(pr))
    key, entry = _select(tours, target)
    if key is None:
        return None
    problems = _entry_problems(pr, key, entry)
    if problems:
        raise ValueError(problems[0])
    if "exempt" in entry:
        return {"steps": [], "exempt": entry["exempt"]}
    steps = []
    for step in entry["steps"]:
        out = {"target": step["target"], "device": step.get("device", "any")}
        for name in OPTIONAL_KEYS:
            if name in step:
                out[name] = step[name]
        if "open" in step:
            out["open"] = step["open"]
        out["title"] = {lang: step["title"][lang] for lang in _text_languages()}
        out["text"] = {lang: step["text"][lang] for lang in _text_languages()}
        steps.append(out)
    result = {"steps": steps}
    if "replaced_by" in entry:
        result["replaced_by"] = entry["replaced_by"]
    return result


def missing(repo, table, pr, translations):
    """(Komponente, Version, Text) je Highlight des PRs in WEB_UI_TARGETS
    ohne Tour und ohne Ausnahme."""
    # Spaet importiert: make_changelog_json importiert dieses Modul.
    from make_changelog_json import mark_highlights, parse_changelog

    tours = translations.get(pr, {}).get(TOUR_KEY)
    gaps = []
    for row in table["components"]:
        if row["target"] not in WEB_UI_TARGETS:
            continue
        changelog = Path(repo) / row["changelog"]
        if not changelog.is_file():
            continue
        if isinstance(tours, dict) and _select(tours, row["target"])[0] is not None:
            continue
        for release in mark_highlights(parse_changelog(changelog.read_text(encoding="utf-8"))):
            for group in release["groups"]:
                for entry in group["entries"]:
                    if entry.get("pr") == pr and entry.get("highlight"):
                        gaps.append((row["label"], release["version"], entry["text"]))
    return gaps


def check(repo, pr):
    if pr < TOUR_REQUIRED_FROM_PR:
        print(f"PR #{pr}: before #{TOUR_REQUIRED_FROM_PR}, no tour required")
        return 0
    translations = hi.load_translations(repo / hi.TRANSLATIONS_DIR)
    table = json.loads((repo / "scripts/version/components.json").read_text(encoding="utf-8"))
    gaps = missing(repo, table, pr, translations)
    if not gaps:
        print(f"PR #{pr}: every web UI highlight has a tour or an exemption")
        return 0
    for component, version, text in gaps:
        print(f"PR #{pr}, {component} {version}: no tour for \"{text}\"")
    print(HINT.format(pr=pr))
    return 1


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__.split("\n")[0])
    sub = parser.add_subparsers(dest="command", required=True)
    command = sub.add_parser("check")
    command.add_argument("--repo", required=True)
    command.add_argument("--pr", required=True, type=int)
    args = parser.parse_args(argv)
    return check(Path(args.repo), args.pr)


if __name__ == "__main__":
    sys.exit(main())
