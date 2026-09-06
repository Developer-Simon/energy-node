from __future__ import annotations

import dataclasses
import time
from dataclasses import dataclass
from typing import List, Optional

from .params import SocParams

# Jak viele Kalibrierereignisse je Einheit aufgehoben werden. Der Ring landet
# in state.json - gross genug fuer eine belastbare Auswertung (Task 4b),
# klein genug, dass die Datei eine Datei bleibt.
CALIBRATION_EVENT_LIMIT = 20


@dataclass(frozen=True)
class CalibrationEvent:
    """Was im Moment einer Kalibrierung galt - der Beleg, den ein Sprung im
    SoC-Verlauf sonst schuldig bleibt.

    residual_ah ist das Fehlersignal: wie weit der Coulomb-Zaehler daneben
    lag, positiv wenn er zu niedrig stand. charged_ah/discharged_ah sind die
    beiden Regressoren, mit denen sich dieses Residuum in Task 4b auf die
    Lade- und die Entladeseite aufteilen laesst - ohne sie ist der Sprung
    zwar sichtbar, aber nicht zuzuordnen.

    voltage_v ist bewusst die ROHE, unkorrigierte Packspannung - nicht die
    lastkorrigierte. Task 5 rechnet aus roher Spannung, Ankerschwelle und
    Strom den tatsaechlichen Innenwiderstand zurueck; mit einer bereits
    korrigierten Spannung waere das zirkulaer, weil die Korrektur selbst
    schon eine R-Annahme (oder die Tabelle) enthaelt. corrected_v_per_cell
    bleibt daneben stehen - das ist der Wert, an dem die Schwelle
    tatsaechlich gemessen hat. cell_count macht das Ereignis
    selbststaendig auswertbar, auch wenn sich die Konfiguration spaeter
    aendert."""
    iso: str
    unit: str
    side: str
    coulomb_before_ah: float
    coulomb_after_ah: float
    residual_ah: float
    voltage_v: Optional[float]
    cell_count: int
    corrected_v_per_cell: float
    current_a: float
    threshold_v_per_cell: float
    tolerance_v_per_cell: float
    hold_s: float
    taper_met: bool
    charged_ah: float
    discharged_ah: float

    def to_dict(self):
        return dataclasses.asdict(self)

    @classmethod
    def from_dict(cls, d):
        known = {f.name for f in dataclasses.fields(cls)}
        return cls(**{k: v for k, v in d.items() if k in known})


class BankState:
    def __init__(self, name, cell_count, capacity_ah):
        self.name = name
        self.cell_count = cell_count
        self.capacity_ah = capacity_ah
        self.coulomb_ah = capacity_ah * 0.5
        self.last_calibration_iso = None
        self.pending_low_since = None
        self.pending_high_since = None
        self.pending_low_broken_since = None
        self.pending_high_broken_since = None
        self.pending_mismatch_since = None
        self.voltage_mismatch = False
        self.charged_ah = 0.0
        self.discharged_ah = 0.0
        self.events = []

    @property
    def soc_pct(self):
        if self.capacity_ah <= 0:
            return None
        return round(max(0.0, min(100.0, self.coulomb_ah / self.capacity_ah * 100)), 1)

    def append_event(self, event):
        self.events.append(event)
        del self.events[:-CALIBRATION_EVENT_LIMIT]

    def reset_balance(self):
        self.charged_ah = 0.0
        self.discharged_ah = 0.0


def build_units(params: SocParams) -> List[BankState]:
    if params.topology == "series":
        return [
            BankState("bank_a", params.bank_a_cell_count, params.bank_a_capacity_ah),
            BankState("bank_b", params.bank_b_cell_count, params.bank_b_capacity_ah),
        ]
    capacity_ah = params.bank_a_capacity_ah
    if params.bank_b_enabled:
        capacity_ah += params.bank_b_capacity_ah
    return [BankState("pack", params.bank_a_cell_count, capacity_ah)]


class SocState:
    def __init__(self, params: SocParams, last_tick: Optional[float] = None):
        self.units = build_units(params)
        self.last_tick = time.time() if last_tick is None else last_tick

    def _unit(self, name):
        for unit in self.units:
            if unit.name == name:
                return unit
        return None

    def to_dict(self):
        return {"units": {u.name: {"coulomb_ah": u.coulomb_ah,
                                   "last_calibration_iso": u.last_calibration_iso,
                                   "charged_ah": u.charged_ah,
                                   "discharged_ah": u.discharged_ah,
                                   "events": [e.to_dict() for e in u.events]}
                          for u in self.units}}

    def load_dict(self, data):
        stored = data.get("units")
        if not isinstance(stored, dict):
            return
        for unit in self.units:
            entry = stored.get(unit.name)
            if isinstance(entry, dict):
                unit.coulomb_ah = entry.get("coulomb_ah", unit.coulomb_ah)
                unit.last_calibration_iso = entry.get("last_calibration_iso")
                unit.charged_ah = entry.get("charged_ah", 0.0)
                unit.discharged_ah = entry.get("discharged_ah", 0.0)
                unit.events = [CalibrationEvent.from_dict(e)
                               for e in entry.get("events", [])
                               if isinstance(e, dict)]

    def load_legacy_dict(self, data, topology):
        legacy = [
            (data.get("bank_a_coulomb_ah"), data.get("bank_a_last_calibration_iso")),
            (data.get("bank_b_coulomb_ah"), data.get("bank_b_last_calibration_iso")),
        ]
        if topology == "series":
            for unit, (coulomb_ah, iso) in zip(self.units, legacy):
                if coulomb_ah is not None:
                    unit.coulomb_ah = coulomb_ah
                unit.last_calibration_iso = iso
            return
        values = [c for c, _ in legacy if c is not None]
        if values:
            pack = self.units[0]
            pack.coulomb_ah = min(pack.capacity_ah, sum(values))
            pack.last_calibration_iso = next((iso for _c, iso in legacy if iso), None)


def set_state_of_charge(state: SocState, params: SocParams, pct: float,
                        unit_name: Optional[str] = None) -> None:
    pct = max(0.0, min(100.0, float(pct)))
    if unit_name is None:
        if params.topology == "series":
            raise ValueError("unit_name required for series topology")
        targets = list(state.units)
    else:
        target = state._unit(unit_name)
        if target is None:
            raise ValueError(f"unknown unit: {unit_name}")
        targets = [target]
    stamp = time.strftime("%Y-%m-%dT%H:%M:%S%z")
    for unit in targets:
        unit.coulomb_ah = pct / 100.0 * unit.capacity_ah
        unit.last_calibration_iso = stamp
        unit.pending_low_since = None
        unit.pending_high_since = None
        unit.pending_mismatch_since = None
