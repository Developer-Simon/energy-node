#!/usr/bin/env python3
"""Uebersetzungen der Release-Highlights.

Je PR eine Datei changelog/highlights/<PR>.json:

    {"de": {"*": "Text fuer jede Komponente", "dashboard": "nur fuer das Dashboard"}}

Texte alter, bereits veroeffentlichter Highlights koennen in changelog/highlights/
archive.json stehen ({"<PR>": {"de": {...}}}). Eine Datei <PR>.json ueberschreibt
den Archiv-Eintrag fuer die Sprachen, die sie enthaelt.

"*" gilt fuer jede Komponente ohne eigene Zeile, wie eine Zeile ohne Ziel im
Block "Highlights:" der Squash-Nachricht (scripts/generate_changelog.sh). Jeder
andere Schluessel ist ein Ziel von generate_changelog.sh (Spalte "target" in
scripts/version/components.json).

Solange der PR offen ist, ist seine Beschreibung die Quelle: ein Block
"Highlights (de):" mit "- "-Zeilen, den der Version-bump-Workflow hierher
uebertraegt (scripts/build/highlight_translations.py sync). Nach dem Merge darf
man die Datei von Hand pflegen.
"""
import json
import re
from pathlib import Path

# Sprachen ausser Englisch, die jedes Highlight braucht (CI-Pruefung).
LANGUAGES = ("de",)
TRANSLATIONS_DIR = Path("changelog/highlights")
ARCHIVE_NAME = "archive.json"
DEFAULT_KEY = "*"

BLOCK_HEAD = re.compile(r"^\s*Highlights \((?P<lang>[a-z]{2})\):\s*$")
BLOCK_LINE = re.compile(r"^\s*-\s+(?P<text>.*\S)\s*$")
TARGETED = re.compile(r"^(?P<target>[a-z_:-]+):\s+(?P<text>.+)$")


def targets_from_table(table):
    return {row["target"] for row in table["components"]}


def _is_target(name, targets):
    return name.startswith("service:") or name in targets


def parse_blocks(body, targets):
    """{lang: {key: text}} aus allen Bloecken "Highlights (xx):" eines PR-Texts.

    Ein Block endet an der ersten Zeile, die nicht mit "- " beginnt. Wie in
    generate_changelog.sh gewinnt je Schluessel die erste Zeile; ein Praefix,
    das kein Ziel ist, bleibt Teil des Texts.
    """
    out = {}
    lang = None
    for line in body.splitlines():
        head = BLOCK_HEAD.match(line)
        if head:
            lang = head["lang"]
            continue
        if lang is None:
            continue
        item = BLOCK_LINE.match(line)
        if not item:
            lang = None
            continue
        key, text = DEFAULT_KEY, item["text"]
        targeted = TARGETED.match(text)
        if targeted and _is_target(targeted["target"], targets):
            key, text = targeted["target"], targeted["text"]
        out.setdefault(lang, {}).setdefault(key, text)
    return out


def _read_archive(path):
    """{pr: {lang: {key: text}}} aus archive.json. Schluessel sind PR-Nummern
    als Text, z.B. {"7": {"de": {"*": "..."}}}."""
    try:
        data = json.loads(path.read_text(encoding="utf-8"))
    except json.JSONDecodeError as err:
        raise ValueError(f"{path}: {err}") from err
    if not isinstance(data, dict):
        raise ValueError(f"{path}: the top level must be an object")
    for key in data:
        if not key.isdigit():
            raise ValueError(f"{path}: key {key!r} is not a PR number")
    return {int(key): entry for key, entry in data.items()}


def load_translations(directory):
    """{pr: {lang: {key: text}}} aus <directory>/archive.json und <PR>.json.

    Eine Datei <PR>.json ueberschreibt den Archiv-Eintrag je Sprache. Eine
    kaputte Datei bricht ab (ValueError mit Dateiname): still verlorene
    Uebersetzungen fallen sonst erst im Release auf.
    """
    out = {}
    directory = Path(directory)
    if not directory.is_dir():
        return out
    archive = directory / ARCHIVE_NAME
    if archive.is_file():
        out = _read_archive(archive)
    for path in sorted(directory.glob("*.json")):
        if not path.stem.isdigit():
            continue
        try:
            entry = json.loads(path.read_text(encoding="utf-8"))
        except json.JSONDecodeError as err:
            raise ValueError(f"{path}: {err}") from err
        pr = int(path.stem)
        out[pr] = {**out.get(pr, {}), **entry}
    return out


def lookup(translations, pr, target, lang):
    texts = translations.get(pr, {}).get(lang, {})
    return texts.get(target) or texts.get(DEFAULT_KEY) or None


def entry_i18n(translations, pr, target):
    out = {}
    for lang in sorted(translations.get(pr, {})):
        text = lookup(translations, pr, target, lang)
        if text:
            out[lang] = text
    return out


def write_pr_file(directory, pr, blocks):
    """Schreibt die Sprachen aus blocks (je Sprache komplett ersetzt) nach
    <directory>/<pr>.json, andere Sprachen bleiben. True, wenn sich die Datei
    geaendert hat."""
    directory = Path(directory)
    path = directory / f"{pr}.json"
    current = json.loads(path.read_text(encoding="utf-8")) if path.is_file() else {}
    merged = {**current, **blocks}
    text = json.dumps(merged, indent=2, ensure_ascii=False, sort_keys=True) + "\n"
    if path.is_file() and path.read_text(encoding="utf-8") == text:
        return False
    directory.mkdir(parents=True, exist_ok=True)
    path.write_text(text, encoding="utf-8")
    return True
