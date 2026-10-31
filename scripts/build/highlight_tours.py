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
"""
import highlight_i18n as hi

TOUR_KEY = "tour"
DEFAULT_KEY = "*"
DEVICES = ("any", "phone", "desktop")
STEP_KEYS = {"panel", "page", "target", "device", "need", "title", "text"}
OPTIONAL_KEYS = ("panel", "page", "need")


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
    problems.extend(_texts_problems(prefix, "title", step.get("title")))
    problems.extend(_texts_problems(prefix, "text", step.get("text")))
    return problems


def _entry_problems(pr, key, entry):
    prefix = f"#{pr} {TOUR_KEY} {key}: "
    if not isinstance(entry, dict):
        return [prefix + "tour must be an object"]
    if "exempt" in entry:
        if set(entry) != {"exempt"}:
            return [prefix + "exempt stands alone, without steps"]
        reason = entry["exempt"]
        if not isinstance(reason, str) or not reason.strip():
            return [prefix + "exempt needs a reason"]
        return []
    problems = [prefix + f"unknown key {name}" for name in sorted(set(entry) - {"steps"})]
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
        tours = translations[pr].get(TOUR_KEY)
        if tours is None:
            continue
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
        out["title"] = {lang: step["title"][lang] for lang in _text_languages()}
        out["text"] = {lang: step["text"][lang] for lang in _text_languages()}
        steps.append(out)
    return {"steps": steps}
