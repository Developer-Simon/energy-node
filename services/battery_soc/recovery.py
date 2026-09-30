"""Wiederherstellung des Coulomb-Zaehlers nach einem unerwarteten Ende.

state.json wird nur noch alle state_save_interval_s geschrieben. Stirbt der
Dienst dazwischen (Absturz, kill -9, Stromausfall), fehlt die Bilanz seit dem
letzten Speichern. Zwei Quellen holen sie zurueck:

1. Der eigene retained {base}/state traegt einen ungerundeten Snapshot
   (snapshot()). Ist er juenger als die Datei und aelter als dieser Start,
   wird die Differenz Snapshot minus Datei auf den aktuellen Zaehler gebucht
   (apply_snapshot()). Delta statt Ueberschreiben, damit die Integration seit
   dem Start erhalten bleibt, egal ob ein Tick vor der Retained-Nachricht lief.
2. Sonst extrapolate(): der zuletzt gespeicherte Strom ueber die Haelfte des
   ungespeicherten Fensters. Der Absturzzeitpunkt ist unbekannt, die Haelfte
   ist der Erwartungswert.
"""

from battery_soc_core.engine import integrate_coulomb

_FIELDS = ("coulomb_ah", "charged_ah", "discharged_ah")


def _is_number(value):
    return isinstance(value, (int, float)) and not isinstance(value, bool)


def snapshot(state, now):
    return {"ts": now,
            "units": {u.name: {"coulomb_ah": u.coulomb_ah, "charged_ah": u.charged_ah,
                               "discharged_ah": u.discharged_ah}
                      for u in state.units}}


def counters(state):
    """Zaehlerstaende direkt nach dem Laden der Datei - Basis fuer das Delta."""
    return {u.name: (u.coulomb_ah, u.charged_ah, u.discharged_ah) for u in state.units}


def apply_snapshot(state, baseline, snap, saved_at, started_at):
    if not isinstance(snap, dict):
        return False
    ts, units = snap.get("ts"), snap.get("units")
    if not _is_number(ts) or not isinstance(units, dict):
        return False
    if (saved_at is not None and ts <= saved_at) or ts >= started_at:
        return False
    by_name = {u.name: u for u in state.units}
    if set(units) != set(by_name) or set(baseline) != set(by_name):
        return False
    for entry in units.values():
        if not isinstance(entry, dict) or not all(_is_number(entry.get(f)) for f in _FIELDS):
            return False
    for name, entry in units.items():
        unit = by_name[name]
        base_coulomb, base_charged, base_discharged = baseline[name]
        unit.coulomb_ah = max(0.0, min(unit.capacity_ah,
                                       unit.coulomb_ah + entry["coulomb_ah"] - base_coulomb))
        unit.charged_ah += max(0.0, entry["charged_ah"] - base_charged)
        unit.discharged_ah += max(0.0, entry["discharged_ah"] - base_discharged)
    return True


def extrapolate(params, state, stored, now, interval_s):
    if stored is None or stored.clean or stored.saved_at is None:
        return 0.0
    gap_s = now - stored.saved_at
    if gap_s <= 0:
        return 0.0
    span_h = min(gap_s, interval_s) / 2.0 / 3600.0
    for unit in state.units:
        integrate_coulomb(params, unit, stored.last_current_a.get(unit.name), span_h)
    return span_h
