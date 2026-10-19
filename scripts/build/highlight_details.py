#!/usr/bin/env python3
"""Details zu den Release-Highlights: Text, optional Bild und Doku-Link.

Ein Highlight in changelog/highlights/<PR>.json kann einen Schluessel "details"
haben, neben den Sprachschluesseln:

    "details": {
      "*": {"en": "...", "de": "...",
            "image": "dashboard-device-map-switch-channel.png",
            "crop": [0.535, 0.17, 0.465, 0.31],
            "doc": "dashboard/device-map.html#switches"}
    }

"*" gilt fuer jede Komponente, ein Schluessel mit dem Ziel (wie in den
Uebersetzungen) nur fuer diese Komponente und ersetzt das "*"-Objekt ganz.
image liegt in docs/images, crop (x, y, Breite, Hoehe als Anteile 0 bis 1)
gilt nur mit image, doc ist eine Seite der Doku unter docs/.

resolve() liefert daraus die Form fuer die Release-Notes. Das Bild ist an
den Tag oder Commit gepinnt, aus dem das Release gebaut wird.
"""
import struct
import subprocess
from pathlib import Path

DETAILS_KEY = "details"
DEFAULT_KEY = "*"
RAW_BASE = "https://raw.githubusercontent.com/Developer-Simon/energy-node/"
PAGES_BASE = "https://developer-simon.github.io/energy-node/"
IMAGES_DIR = Path("docs/images")
DOCS_DIR = Path("docs")
TEXT_LANGUAGES = ("en", "de")
PNG_SIGNATURE = b"\x89PNG\r\n\x1a\n"


def png_size(path):
    """(Breite, Hoehe) aus dem IHDR-Chunk einer PNG-Datei."""
    with open(path, "rb") as handle:
        head = handle.read(24)
    if len(head) < 24 or head[:8] != PNG_SIGNATURE or head[12:16] != b"IHDR":
        raise ValueError(f"{path} is not a PNG file")
    return struct.unpack(">II", head[16:24])


def lookup(translations, pr, target):
    """Das Details-Objekt fuer das Ziel: erst der Schluessel target, sonst "*"."""
    details = translations.get(pr, {}).get(DETAILS_KEY)
    if not isinstance(details, dict):
        return None
    return details.get(target) or details.get(DEFAULT_KEY) or None


def _is_number(value):
    return isinstance(value, (int, float)) and not isinstance(value, bool)


def _crop_problem(crop):
    if not isinstance(crop, list) or len(crop) != 4 or not all(_is_number(v) for v in crop):
        return "crop must be four numbers [x, y, width, height]"
    x, y, width, height = crop
    if width <= 0 or height <= 0:
        return "crop width and height must be positive"
    if x < 0 or y < 0 or x + width > 1 or y + height > 1:
        return "crop must lie inside the image"
    return None


def _doc_path(doc):
    """Pfad der Markdown-Seite unter docs/ zu einem Doku-Link wie
    "dashboard/device-map.html#switches" oder "dashboard/"."""
    page = doc.split("#", 1)[0]
    if page.startswith("/") or ".." in Path(page).parts or not page:
        return None
    if page.endswith("/"):
        page += "index.md"
    elif page.endswith(".html"):
        page = page[: -len(".html")] + ".md"
    else:
        return None
    return DOCS_DIR / page


def _entry_problems(repo, pr, key, entry):
    prefix = f"#{pr} {key}: "
    if not isinstance(entry, dict):
        return [prefix + "details must be an object"]
    problems = []
    for lang in TEXT_LANGUAGES:
        if lang not in entry:
            problems.append(prefix + f"text for {lang} is missing")
        elif not isinstance(entry[lang], str) or not entry[lang].strip():
            problems.append(prefix + f"text for {lang} is empty")
    if "image" in entry:
        name = entry["image"]
        if not isinstance(name, str) or not name or Path(name).name != name:
            problems.append(prefix + "image must be a file name in docs/images")
        elif not (Path(repo) / IMAGES_DIR / name).is_file():
            problems.append(prefix + f"image {name} not found in docs/images")
        else:
            try:
                png_size(Path(repo) / IMAGES_DIR / name)
            except ValueError:
                problems.append(prefix + f"image {name} is not a PNG")
    if "crop" in entry:
        if "image" not in entry:
            problems.append(prefix + "crop needs image")
        else:
            reason = _crop_problem(entry["crop"])
            if reason:
                problems.append(prefix + reason)
    if "doc" in entry:
        doc = entry["doc"]
        path = _doc_path(doc) if isinstance(doc, str) else None
        if path is None:
            problems.append(prefix + "doc must be a relative .html page or a folder ending in /")
        elif not (Path(repo) / path).is_file():
            problems.append(prefix + f"doc page {path.as_posix()} not found")
    return problems


def validate(repo, translations):
    """Eine Meldung je Fehler, Form "#<pr> <key>: <problem>". Leere Liste, wenn
    alle Details stimmen."""
    problems = []
    for pr in sorted(translations):
        details = translations[pr].get(DETAILS_KEY)
        if details is None:
            continue
        if not isinstance(details, dict):
            problems.append(f"#{pr} {DETAILS_KEY}: must be an object keyed by component")
            continue
        for key in sorted(details):
            problems.extend(_entry_problems(repo, pr, key, details[key]))
    return problems


def resolve(repo, translations, pr, target, image_ref):
    """Details fuer ein Highlight und eine Komponente als Dict fuer die
    Release-Notes, None ohne Details. Ungueltige Details: ValueError."""
    entry = lookup(translations, pr, target)
    if entry is None:
        return None
    key = target if translations[pr][DETAILS_KEY].get(target) else DEFAULT_KEY
    problems = _entry_problems(repo, pr, key, entry)
    if problems:
        raise ValueError(problems[0])
    result = {"text": {lang: entry[lang] for lang in TEXT_LANGUAGES}}
    if "image" in entry:
        width, height = png_size(Path(repo) / IMAGES_DIR / entry["image"])
        image = {
            "url": f"{RAW_BASE}{image_ref}/{IMAGES_DIR.as_posix()}/{entry['image']}",
            "width": width,
            "height": height,
        }
        if "crop" in entry:
            image["crop"] = entry["crop"]
        result["image"] = image
    if "doc" in entry:
        result["doc"] = PAGES_BASE + entry["doc"]
    return result


def image_ref(repo):
    """Exakter Tag auf HEAD, sonst der Commit HEAD."""
    tag = subprocess.run(
        ["git", "describe", "--tags", "--exact-match", "HEAD"],
        cwd=repo, capture_output=True, text=True,
    )
    if tag.returncode == 0 and tag.stdout.strip():
        return tag.stdout.strip()
    head = subprocess.run(
        ["git", "rev-parse", "HEAD"], cwd=repo, check=True, capture_output=True, text=True,
    )
    return head.stdout.strip()
