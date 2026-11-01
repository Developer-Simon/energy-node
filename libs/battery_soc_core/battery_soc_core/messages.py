"""Meldungskatalog der Einstellungsbefunde.

Die Texte sind die Werte aus dem Dashboard-Katalog
(dashboard/internal/webui/catalogs/de.json und en.json, Schlüssel
config.tuning.finding.<key>). Der Core rendert daraus die deutsche
Meldung im Befund selbst. Die Oberfläche übersetzt über code und
params, der Paritätstest prüft, dass beide Kataloge zusammenpassen.
"""
from __future__ import annotations

from typing import Any, Mapping

# Schlüssel: Befund-Code, bei Befunden mit Teilbereich "code_subject".
CATALOGS: dict[str, dict[str, str]] = {
    "de": {
        "anker_unzuverlaessig": "Alle {count} Voll-Kalibrierungen haben das Tail-Strom-Kriterium verfehlt. Sie belegen keinen vollen Akku, also stützt keine davon eine Schätzung. Erst full_taper_c_rate setzen, dann eine Messreihe abwarten.",
        "kein_unterer_anker": "Keine 0-%-Kalibrierung in der Historie. Ohne eine Strecke von voll bis leer ist die nutzbare Kapazität nicht bestimmbar, dafür gibt es hier grundsätzlich keinen Vorschlag. Schaltet der Wechselrichter oder das BMS vor empty_v_per_cell ab, ist calibration_tolerance_empty_v_per_cell der Hebel.",
        "schwelle_zu_tief": "{hot} von {total} Voll-Kalibrierungen lösten dicht am Tail-Strom aus (ab {threshold} A). Das Paket nahm also noch fast die volle erlaubte Ladung auf. Entweder steht full_v_per_cell zu tief, oder die Toleranz auf der Voll-Seite ist zu weit.",
        "unplausibel_efficiency": "Die Messreihe führt auf einen Wechselrichter-Wirkungsgrad von {value}, außerhalb des plausiblen Bereichs {low} bis {high}. Das deutet auf ein anderes Problem als den Wirkungsgrad hin: eine zu tief stehende Voll-Schwelle, eine falsch angesetzte Kapazität oder eine Messquelle, die zeitweise ausfällt.",
        "unplausibel_resistance": "Die Messreihe führt auf einen Innenwiderstand von {value} mOhm/Zelle, außerhalb des plausiblen Bereichs {low} bis {high}. Das deutet eher auf eine falsch sitzende Spannungsmessung oder einen schlechten Kontakt als auf einen echten Zellwert hin.",
        "zu_geringer_strom": "Kein Kalibrierereignis mit mindestens {min_current} A Strom. Der Innenwiderstand ist bei kleinerem Strom nicht vom Messrauschen zu unterscheiden.",
        "zu_wenig_daten": "{count} von {needed} benötigten Intervallen zwischen zwei belastbaren Voll-Ankern. Ein einzelner Zyklus ist eine Anekdote, keine Messreihe.",
    },
    "en": {
        "anker_unzuverlaessig": "All {count} full calibrations missed the tail current criterion. They do not prove a full battery, so none of them supports an estimate. Set full_taper_c_rate first, then wait for a series of measurements.",
        "kein_unterer_anker": "No 0 % calibration in the history. Without a full-to-empty run the usable capacity cannot be determined, so there is no suggestion for it. If the inverter or the BMS shuts down before empty_v_per_cell, calibration_tolerance_empty_v_per_cell is the lever.",
        "schwelle_zu_tief": "{hot} of {total} full calibrations triggered close to the tail current (at least {threshold} A). So the pack was still taking almost the full permitted charge. Either full_v_per_cell is set too low, or the tolerance on the full side is too wide.",
        "unplausibel_efficiency": "The series of measurements points to an inverter efficiency of {value}, outside the plausible range {low} to {high}. That points to a different problem than the efficiency: a full threshold that is set too low, a wrong capacity, or a measurement source that drops out at times.",
        "unplausibel_resistance": "The series of measurements points to an internal resistance of {value} mOhm/cell, outside the plausible range {low} to {high}. That points to a misplaced voltage measurement or a bad contact rather than a real cell value.",
        "zu_geringer_strom": "No calibration event with at least {min_current} A of current. At lower current the internal resistance cannot be told apart from measurement noise.",
        "zu_wenig_daten": "{count} of {needed} required intervals between two reliable full anchors. A single cycle is an anecdote, not a series of measurements.",
    },
}


def finding_key(code: str, params: Mapping[str, Any]) -> str:
    """Katalogschlüssel eines Befunds: Code, plus Teilbereich falls vorhanden."""
    subject = params.get("subject")
    return f"{code}_{subject}" if subject else code
