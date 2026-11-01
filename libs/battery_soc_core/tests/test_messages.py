"""Tests fuer battery_soc_core.messages - der Meldungskatalog der Befunde."""
import string

from battery_soc_core.messages import CATALOGS, finding_key


def _placeholders(text):
    return {name for _, name, _, _ in string.Formatter().parse(text) if name}


def test_every_language_has_the_same_keys():
    assert set(CATALOGS["de"]) == set(CATALOGS["en"])


def test_every_language_has_the_same_placeholders_per_key():
    for key, de_text in CATALOGS["de"].items():
        assert _placeholders(de_text) == _placeholders(CATALOGS["en"][key]), key


def test_finding_key_appends_subject():
    assert finding_key("unplausibel", {"subject": "efficiency"}) == "unplausibel_efficiency"
    assert finding_key("zu_wenig_daten", {"count": 2, "needed": 5}) == "zu_wenig_daten"
