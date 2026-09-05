from __future__ import annotations

import time
from typing import List, Optional

from .params import SocParams


class BankState:
    def __init__(self, name, cell_count, capacity_ah):
        self.name = name
        self.cell_count = cell_count
        self.capacity_ah = capacity_ah
        self.coulomb_ah = capacity_ah * 0.5
        self.last_calibration_iso = None
        self.pending_low_since = None
        self.pending_high_since = None
        self.pending_mismatch_since = None
        self.voltage_mismatch = False

    @property
    def soc_pct(self):
        if self.capacity_ah <= 0:
            return None
        return round(max(0.0, min(100.0, self.coulomb_ah / self.capacity_ah * 100)), 1)


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
                                   "last_calibration_iso": u.last_calibration_iso}
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
