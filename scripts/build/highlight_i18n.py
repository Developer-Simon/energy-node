#!/usr/bin/env python3
"""Uebersetzungen der Release-Highlights.

Je PR eine Datei changelog/highlights/<PR>.json:

    {"de": {"*": "Text fuer jede Komponente", "dashboard": "nur fuer das Dashboard"}}

Nach einem Release wandern die Texte aus den PR-Dateien in zwei Arten von Dateien:
changelog/highlights/v<MAJOR>.<MINOR>.<PATCH>.json enthaelt die Texte eines Releases,
changelog/highlights/archive-v<MAJOR>.json die aller Releases einer Major-Version.
Beide haben die Form {"<PR>": {"de": {...}}}. Eine Datei <PR>.json ueberschreibt
den Eintrag aus diesen Dateien fuer die Sprachen, die sie enthaelt.

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
# Englisch steht als "en" neben den anderen Sprachen. Es ersetzt im Build den
# Text aus dem CHANGELOG (make_changelog_json.attach_translations) und gehoert
# nicht in "i18n".
ENGLISH = "en"
TRANSLATIONS_DIR = Path("changelog/highlights")
RELEASE_FILE = re.compile(r"^v(?P<major>\d+)\.\d+\.\d+$")
ARCHIVE_FILE = re.compile(r"^archive-v(?P<major>\d+)$")
DEFAULT_KEY = "*"

BLOCK_HEAD = re.compile(r"^\s*Highlights \((?P<lang>[a-z]{2})\):\s*$")
BLOCK_LINE = re.compile(r"^\s*-\s+(?P<text>.*\S)\s*$")
TARGETED = re.compile(r"^(?P<target>[a-z_:-]+):\s+(?P<text>.+)$")
LANGUAGE_KEY = re.compile(r"^[a-z]{2}$")
PR_REF = re.compile(r"\(#\d+\)\s*$")
DOC_TITLE_KEY = "doc_title"


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


def _read_map(path):
    """{pr: {lang: {key: text}}} aus einer Datei archive-v<MAJOR>.json oder
    v<MAJOR>.<MINOR>.<PATCH>.json. Schluessel sind PR-Nummern als Text, z.B.
    {"7": {"de": {"*": "..."}}}."""
    data = _read_object(path)
    for key in data:
        if not key.isdigit():
            raise ValueError(f"{path}: key {key!r} is not a PR number")
    return {int(key): entry for key, entry in data.items()}


def _read_object(path):
    """Liest eine JSON-Datei, deren oberste Ebene ein Objekt sein muss. Sonst
    ValueError mit Dateiname, bevor der Inhalt weiterverarbeitet wird."""
    try:
        data = json.loads(path.read_text(encoding="utf-8"))
    except json.JSONDecodeError as err:
        raise ValueError(f"{path}: {err}") from err
    if not isinstance(data, dict):
        raise ValueError(f"{path}: the top level must be an object")
    return data


def _version_key(stem):
    """(major, minor, patch) als Zahlen, damit v0.10.0 nach v0.9.0 kommt."""
    return tuple(int(part) for part in stem[1:].split("."))


def _archive_paths(directory):
    """Alle archive-v<MAJOR>.json, nach Major aufsteigend."""
    found = [path for path in directory.glob("*.json")
             if path.is_file() and ARCHIVE_FILE.match(path.stem)]
    return sorted(found, key=lambda path: int(ARCHIVE_FILE.match(path.stem)["major"]))


def _release_files(directory):
    """Alle v<X.Y.Z>.json, nach Version aufsteigend."""
    found = [path for path in directory.glob("*.json")
             if path.is_file() and RELEASE_FILE.match(path.stem)]
    return sorted(found, key=lambda path: _version_key(path.stem))


def _pr_files(directory):
    """Alle <PR>.json, nach PR-Nummer aufsteigend."""
    found = [path for path in directory.glob("*.json")
             if path.is_file() and path.stem.isdigit()]
    return sorted(found, key=lambda path: int(path.stem))


def _fold(target, entries):
    for pr, entry in entries.items():
        target[pr] = {**target.get(pr, {}), **entry}


def _write_map(path, data):
    ordered = {str(pr): data[pr] for pr in sorted(data)}
    path.write_text(json.dumps(ordered, indent=2, ensure_ascii=False) + "\n", encoding="utf-8")


def load_translations(directory):
    """{pr: {lang: {key: text}}} aus allen Dateien des Verzeichnisses.

    Reihenfolge: archive-v<MAJOR>.json nach Major aufsteigend, dann
    v<X.Y.Z>.json nach Version, dann <PR>.json. Spaetere Dateien ueberschreiben
    den frueheren Eintrag je Top-Level-Schluessel. Eine kaputte Datei bricht ab
    (ValueError mit Dateiname): still verlorene Uebersetzungen fallen sonst erst
    im Release auf.
    """
    out = {}
    directory = Path(directory)
    if not directory.is_dir():
        return out
    for path in _archive_paths(directory) + _release_files(directory):
        _fold(out, _read_map(path))
    for path in _pr_files(directory):
        _fold(out, {int(path.stem): _read_object(path)})
    return out


def lookup(translations, pr, target, lang):
    texts = translations.get(pr, {}).get(lang, {})
    return texts.get(target) or texts.get(DEFAULT_KEY) or None


def english_form(text):
    """Form eines "en"-Texts: grosser Anfangsbuchstabe, kein Punkt am Ende."""
    text = text.strip()
    text = text[:1].upper() + text[1:]
    return text[:-1].rstrip() if text.endswith(".") else text


def text_languages():
    """Englisch plus die Sprachen aus LANGUAGES, zur Laufzeit gelesen."""
    return (ENGLISH, *LANGUAGES)


def english(translations, pr, target):
    """Der englische Highlight-Text fuer das Ziel, None ohne "en"."""
    return lookup(translations, pr, target, ENGLISH)


def doc_title(translations, pr, target):
    """Doku-Titel des Highlights fuer das Ziel, sonst der Standard "*", sonst None."""
    value = translations.get(pr, {}).get(DOC_TITLE_KEY)
    if not isinstance(value, dict):
        return None
    return value.get(target) or value.get(DEFAULT_KEY) or None


def validate_doc_titles(translations):
    """Eine Meldung je Fehler, Form "#<pr> doc_title: <problem>". Leere Liste,
    wenn alle Doku-Titel stimmen."""
    problems = []
    for pr in sorted(translations):
        value = translations[pr].get(DOC_TITLE_KEY)
        if value is None:
            continue
        if not isinstance(value, dict):
            problems.append(f"#{pr} {DOC_TITLE_KEY}: must be an object keyed by component")
            continue
        for key, text in value.items():
            if not isinstance(text, str):
                problems.append(f"#{pr} {DOC_TITLE_KEY}: text for {key} is not a string")
            elif not text.strip():
                problems.append(f"#{pr} {DOC_TITLE_KEY}: text for {key} is empty")
            elif ";" in text:
                problems.append(f"#{pr} {DOC_TITLE_KEY}: text for {key} contains a semicolon")
            elif text.rstrip().endswith("."):
                problems.append(f"#{pr} {DOC_TITLE_KEY}: text for {key} ends with a full stop")
    return problems


def entry_i18n(translations, pr, target):
    out = {}
    for lang in sorted(k for k in translations.get(pr, {}) if LANGUAGE_KEY.match(k) and k != ENGLISH):
        text = lookup(translations, pr, target, lang)
        if text:
            out[lang] = text
    return out


def _text_problems(prefix, lang, key, text, targets):
    if key != DEFAULT_KEY and not _is_target(key, targets):
        return [prefix + "unknown component"]
    if not isinstance(text, str) or not text.strip():
        return [prefix + "text is empty"]
    if "\n" in text:
        return [prefix + "text must be one line"]
    if PR_REF.search(text):
        return [prefix + "drop the PR number, the changelog adds it"]
    if lang != ENGLISH:
        return []
    if text[:1].islower():
        return [prefix + "start with a capital letter"]
    if text.rstrip().endswith("."):
        return [prefix + "drop the full stop at the end"]
    if ";" in text:
        return [prefix + "no semicolon"]
    return []


def validate_texts(translations, targets):
    """Eine Meldung je Fehler in den Highlight-Texten aller Sprachen. Hat ein PR
    einen Text in irgendeiner Sprache, braucht er auch "en"."""
    problems = []
    for pr in sorted(translations):
        entry = translations[pr]
        languages = sorted(key for key in entry if LANGUAGE_KEY.match(key))
        if languages and ENGLISH not in languages:
            problems.append(f"#{pr} en: missing, every highlight needs an English text next to {', '.join(languages)}")
        for lang in languages:
            texts = entry[lang]
            if not isinstance(texts, dict) or not texts:
                problems.append(f"#{pr} {lang}: must be an object of texts keyed by component")
                continue
            for key in sorted(texts):
                problems.extend(_text_problems(f"#{pr} {lang} {key}: ", lang, key, texts[key], targets))
        if DOC_TITLE_KEY in entry:
            problems.append(f"#{pr} {DOC_TITLE_KEY}: replaced by en, move the text there")
    return problems


def write_pr_file(directory, pr, blocks):
    """Schreibt die Sprachen aus blocks (je Sprache komplett ersetzt) nach
    <directory>/<pr>.json, andere Sprachen bleiben. True, wenn sich die Datei
    geaendert hat."""
    directory = Path(directory)
    path = directory / f"{pr}.json"
    current = _read_object(path) if path.is_file() else {}
    merged = {**current, **blocks}
    text = json.dumps(merged, indent=2, ensure_ascii=False, sort_keys=True) + "\n"
    if path.is_file() and path.read_text(encoding="utf-8") == text:
        return False
    directory.mkdir(parents=True, exist_ok=True)
    path.write_text(text, encoding="utf-8")
    return True


def archive_files(directory, tag):
    """Faltet die <PR>.json in die Datei <tag>.json und die Release-Dateien in
    archive-v<MAJOR>.json (je Major-Version eine), und loescht die gefalteten
    Dateien. Gibt die in <tag>.json gefalteten PR-Nummern sortiert zurueck.

    tag muss ein finales Release sein (vX.Y.Z). Ohne <PR>.json passiert nichts.
    Existiert <tag>.json schon (zweiter Lauf), werden die PR-Dateien hineingefaltet.
    Alle Dateien werden vor dem ersten Schreiben gelesen: eine kaputte Datei bricht
    mit ValueError ab, bevor etwas geschrieben oder geloescht wird.
    """
    if not RELEASE_FILE.match(tag):
        raise ValueError(f"{tag!r} is not a final release tag vX.Y.Z")
    directory = Path(directory)
    if not directory.is_dir():
        return []
    pr_paths = _pr_files(directory)
    if not pr_paths:
        return []
    tag_path = directory / f"{tag}.json"
    releases = [path for path in _release_files(directory) if path.name != tag_path.name]

    pr_entries = {int(path.stem): _read_object(path) for path in pr_paths}
    release_maps = [(path, _read_map(path)) for path in releases]
    tag_map = _read_map(tag_path) if tag_path.is_file() else {}
    archive_maps = {}
    for path, entries in release_maps:
        major = int(RELEASE_FILE.match(path.stem)["major"])
        archive_path = directory / f"archive-v{major}.json"
        if archive_path not in archive_maps:
            archive_maps[archive_path] = _read_map(archive_path) if archive_path.is_file() else {}
        _fold(archive_maps[archive_path], entries)
    _fold(tag_map, pr_entries)

    for archive_path, data in archive_maps.items():
        _write_map(archive_path, data)
    _write_map(tag_path, tag_map)
    for path, _ in release_maps:
        path.unlink()
    for path in pr_paths:
        path.unlink()
    return sorted(pr_entries)
