"""Paritaetstest: der Meldungskatalog des Cores (battery_soc_core.messages)
und die Dashboard-Kataloge muessen dieselben Befund-Texte fuehren.

Schluessel-Regel: CATALOGS[sprache][key] entspricht
config.tuning.finding.<key> im Dashboard-Katalog derselben Sprache."""
import json
import string
from pathlib import Path

import pytest

from battery_soc_core.messages import CATALOGS

REPO_ROOT = Path(__file__).parents[3]
CATALOG_DIR = REPO_ROOT / "dashboard" / "internal" / "webui" / "catalogs"
PREFIX = "config.tuning.finding."
LANGS = ("de", "en")


def _placeholders(text):
    return {name for _, name, _, _ in string.Formatter().parse(text) if name}


def _load_dashboard(lang):
    data = json.loads((CATALOG_DIR / f"{lang}.json").read_text(encoding="utf-8"))
    return {k[len(PREFIX):]: v for k, v in data.items() if k.startswith(PREFIX)}


def parity_problems(python_catalog, dashboard_catalog):
    """Liefert die Abweichungen zwischen Core-Katalog und Dashboard-Katalog
    einer Sprache. Leere Liste heisst: paritaetisch."""
    problems = []
    for key, text in python_catalog.items():
        if key not in dashboard_catalog:
            problems.append(f"missing in dashboard: {key}")
            continue
        if _placeholders(text) != _placeholders(dashboard_catalog[key]):
            problems.append(f"placeholders differ: {key}")
    for key in dashboard_catalog:
        if key not in python_catalog:
            problems.append(f"missing in core: {key}")
    return problems


@pytest.mark.parametrize("lang", LANGS)
def test_core_catalog_matches_dashboard_catalog(lang):
    assert parity_problems(CATALOGS[lang], _load_dashboard(lang)) == []


def test_parity_check_detects_drift():
    """Der Vergleich selbst muss scheitern, wenn er es soll - sonst waere
    der Paritaetstest oben wertlos."""
    core = {"zu_wenig_daten": "{count} von {needed}", "kein_unterer_anker": "Text"}
    dashboard = {"zu_wenig_daten": "{count} von {total}", "anker_unzuverlaessig": "x"}
    problems = parity_problems(core, dashboard)
    assert "placeholders differ: zu_wenig_daten" in problems
    assert "missing in dashboard: kein_unterer_anker" in problems
    assert "missing in core: anker_unzuverlaessig" in problems
