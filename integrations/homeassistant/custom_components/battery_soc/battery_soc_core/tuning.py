"""Einstellungsvorschlaege aus dem beobachteten Kalibrierverhalten.

WAS HIER BESTIMMBAR IST - und was nicht:

Zwischen zwei aufeinanderfolgenden VOLL-Ankern war das Paket an beiden Enden
voll. Die wahre Nettoladung ueber das Intervall ist also null, waehrend der
Zaehler charged_ah hinein- und discharged_ah herausgerechnet hat:

    a * charged_ah - b * discharged_ah = 0

Zwei Unbekannte, eine Gleichung je Intervall: nur das VERHAELTNIS a/b ist
bestimmbar. Das ist keine Schwaeche der Auswertung, sondern eine Eigenschaft
einer Anlage mit nur einem Anker.

Die Normierung a := 1 ist nicht willkuerlich. Die Ladeseite wird beim
konfigurierten DC-Pfad direkt auf dem Gleichstrombus gemessen und traegt
keine Wirkungsgradannahme. Die Entladeseite hat keine DC-Messung und wird
ueber inverter_dc_ac_efficiency aus der Netzseite hochgerechnet - dort sitzt
die einzige freie Annahme, also gehoert die Korrektur dorthin.

Die KAPAZITAET ist aus Voll->Voll-Intervallen grundsaetzlich nicht
bestimmbar; dafuer braucht es eine Voll->Leer-Strecke. Fehlt sie, gibt es
hier einen Hinweis und keinen Vorschlag.

Die QUALITAET DER SCHWELLE kommt nicht aus dem Residuum, sondern aus dem
Zustand im Moment des Ausloesens: eine Kalibrierung bei hohem Strom ist
verdaechtig, egal wie klein das Residuum war.

Diese Funktionen SCHLAGEN VOR. Sie aendern nichts. Der Dienst bleibt
fail-closed, das Anwenden ist eine bewusste Handlung im Dashboard."""
from __future__ import annotations

import statistics
from dataclasses import dataclass, field
from typing import Optional

from .messages import CATALOGS, finding_key

# Unter dieser Zahl brauchbarer Intervalle wird nichts vorgeschlagen. Ein
# einzelner Zyklus ist eine Anekdote, keine Messreihe.
MIN_SAMPLES_FOR_SUGGESTION = 5

# Physikalische Grenzen fuer den vorgeschlagenen Wandler-Wirkungsgrad.
EFFICIENCY_BOUNDS = (0.70, 0.99)

# Ab dieser relativen Streuung der Einzelschaetzungen sinkt das Vertrauen.
SPREAD_MEDIUM = 0.02
SPREAD_LOW = 0.05

# Ab diesem Anteil der Taper-Grenze gilt der Strom bei der Kalibrierung als
# zu hoch - die Schwelle steht dann vermutlich zu tief.
SUSPICIOUS_TAPER_SHARE = 0.8

# Unter diesem Betrag ist der Strom als Nenner zu klein, um das
# Spannungsrauschen (bei einem Shelly Uni realistisch ein paar mV) von
# einem echten R-Signal zu trennen. Siehe Modul-Docstring: eine Simulation
# mit 3 mV Rauschen zeigt bei < 2 A Streuungen von 50-90 %.
MIN_CURRENT_FOR_RESISTANCE_A = 2.0

# Plausibler Bereich fuer den Innenwiderstand einer LiFePO4-Zelle dieser
# Groessenordnung (100 Ah). Die Bin-Tabelle LOAD_OFFSET_TABLE_MV entspricht
# umgerechnet ca. 1.2 mOhm/Zelle (siehe deren Modulkommentar in curves.py) -
# das liegt mittig im Fenster.
RESISTANCE_BOUNDS = (0.1, 5.0)


@dataclass(frozen=True)
class Suggestion:
    key: str
    current_value: Optional[float]  # None, wenn das Feld unkonfiguriert ist
                                     # (z.B. internal_resistance_mohm_per_cell
                                     # im Tabellenpfad, siehe Task 5)
    suggested_value: float
    confidence: str
    sample_count: int
    spread: float
    reason: str


@dataclass(frozen=True)
class Finding:
    code: str
    severity: str
    message: str  # Deutsche Meldung aus dem Katalog; Oberflaechen uebersetzen ueber code + params
    params: dict = field(default_factory=dict)


def _finding(code, severity, **params):
    """Baut einen Befund: die Meldung kommt aus dem Katalog, nicht aus dem Code."""
    text = CATALOGS["de"][finding_key(code, params)].format(**params)
    return Finding(code, severity, text, params)


def _confidence(spread):
    if spread <= SPREAD_MEDIUM:
        return "hoch"
    if spread <= SPREAD_LOW:
        return "mittel"
    return "gering"


def _pack_capacity_ah(params):
    capacity = params.bank_a_capacity_ah
    if params.topology == "parallel" and params.bank_b_enabled:
        capacity += params.bank_b_capacity_ah
    return capacity


def analyse(params, events):
    """Wertet den Ereignisring aus. Liefert (Vorschlaege, Befunde).

    Befunde erklaeren, warum ein Vorschlag ausbleibt - das ist der
    haeufigere und der wichtigere Fall."""
    suggestions = []
    findings = []

    full_events = [e for e in events if e.side == "full"]
    trustworthy = [e for e in full_events if e.taper_met]

    if full_events and not trustworthy:
        findings.append(_finding(
            "anker_unzuverlaessig", "warnung", count=len(full_events)))
        return suggestions, findings

    if not any(e.side == "empty" for e in events):
        findings.append(_finding("kein_unterer_anker", "info"))

    usable = [e for e in trustworthy if e.charged_ah > 0 and e.discharged_ah > 0]
    if len(usable) < MIN_SAMPLES_FOR_SUGGESTION:
        findings.append(_finding(
            "zu_wenig_daten", "info",
            count=len(usable), needed=MIN_SAMPLES_FOR_SUGGESTION))
        return suggestions, findings

    # b = charged/discharged je Intervall; Median statt Mittelwert, damit ein
    # einzelner Ausreisser die Empfehlung nicht traegt.
    ratios = [e.charged_ah / e.discharged_ah for e in usable]
    b = statistics.median(ratios)
    spread = (statistics.pstdev(ratios) / b) if b else float("inf")
    proposed = params.inverter_dc_ac_efficiency / b

    low, high = EFFICIENCY_BOUNDS
    if not low <= proposed <= high:
        findings.append(_finding(
            "unplausibel", "warnung", subject="efficiency",
            value=f"{proposed:.3f}", low=low, high=high))
    else:
        direction = "hoeher" if proposed > params.inverter_dc_ac_efficiency else "niedriger"
        suggestions.append(Suggestion(
            key="inverter_dc_ac_efficiency",
            current_value=params.inverter_dc_ac_efficiency,
            suggested_value=round(proposed, 3),
            confidence=_confidence(spread),
            sample_count=len(usable),
            spread=round(spread, 4),
            reason=(
                f"Ueber {len(usable)} Intervalle zwischen zwei belastbaren "
                f"Voll-Ankern wurden im Median {b:.3f} Ah geladen je Ah "
                f"entladen. Da beide Enden voll waren, muss dieses Verhaeltnis "
                f"1 sein; die Abweichung sitzt auf der Entladeseite, weil nur "
                f"sie eine geschaetzte Wandlung enthaelt. Der wahre "
                f"Wirkungsgrad liegt damit {direction} als der eingestellte.")))

    taper_limit = params.full_taper_c_rate
    if taper_limit is not None:
        capacity = _pack_capacity_ah(params)
        threshold_a = taper_limit * capacity * SUSPICIOUS_TAPER_SHARE
        hot = [e for e in trustworthy if abs(e.current_a) >= threshold_a]
        if len(hot) >= len(trustworthy) / 2:
            findings.append(_finding(
                "schwelle_zu_tief", "warnung",
                hot=len(hot), total=len(trustworthy),
                threshold=f"{threshold_a:.1f}"))

    resistance_candidates = [
        e for e in trustworthy + [ev for ev in events if ev.side == "empty"]
        if abs(e.current_a) >= MIN_CURRENT_FOR_RESISTANCE_A
    ]
    # trustworthy enthaelt nur Voll-Ereignisse (siehe oben); Leer-Ereignisse
    # haben kein Taper-Kriterium und werden hier direkt aufgenommen. Ein
    # Ereignis kann nicht in beiden Listen stehen (side ist eindeutig).
    if not resistance_candidates:
        if full_events or any(e.side == "empty" for e in events):
            findings.append(_finding(
                "zu_geringer_strom", "info",
                min_current=f"{MIN_CURRENT_FOR_RESISTANCE_A:.1f}"))
    else:
        estimates = []
        for e in resistance_candidates:
            anchor = (params.full_v_per_cell if e.side == "full"
                     else params.empty_v_per_cell)
            raw_v_per_cell = e.voltage_v / e.cell_count
            estimates.append(
                (raw_v_per_cell - anchor) / e.current_a * 1000.0)
        r_median = statistics.median(estimates)
        r_spread = (statistics.pstdev(estimates) / r_median
                   if r_median else float("inf"))
        low, high = RESISTANCE_BOUNDS
        if not low <= r_median <= high:
            findings.append(_finding(
                "unplausibel", "warnung", subject="resistance",
                value=f"{r_median:.2f}", low=low, high=high))
        else:
            suggestions.append(Suggestion(
                key="internal_resistance_mohm_per_cell",
                current_value=params.internal_resistance_mohm_per_cell,
                suggested_value=round(r_median, 2),
                confidence=_confidence(r_spread),
                sample_count=len(resistance_candidates),
                spread=round(r_spread, 4),
                reason=(
                    f"Ueber {len(resistance_candidates)} Kalibrierereignisse "
                    f"mit mindestens {MIN_CURRENT_FOR_RESISTANCE_A:.1f} A "
                    "ergibt die rohe Spannung minus Ankerschwelle, geteilt "
                    f"durch den Strom, im Median {r_median:.2f} mOhm/Zelle. "
                    "Voll- und Leer-Seite zusammen, weil offset=I*R "
                    "vorzeichenunabhaengig gilt und die Leer-Seite die "
                    "groesseren, rauschaermeren Stroeme liefert.")))

    return suggestions, findings
