from __future__ import annotations

import dataclasses
from dataclasses import dataclass
from typing import Optional


@dataclass(frozen=True)
class SocParams:
    topology: str = "parallel"
    bank_a_cell_count: int = 8
    bank_a_capacity_ah: float = 100.0
    bank_b_cell_count: int = 8
    bank_b_capacity_ah: float = 100.0
    bank_b_enabled: bool = True
    charger_ac_dc_efficiency: float = 0.9
    inverter_dc_ac_efficiency: float = 0.9
    charge_efficiency: float = 0.98
    battery_chemistry: str = "lifepo4"
    soc_curve: str = "generic_lifepo4"
    empty_v_per_cell: float = 2.7
    full_v_per_cell: float = 3.5
    internal_resistance_mohm_per_cell: Optional[float] = None
    calibration_tolerance_v_per_cell: float = 0.08
    calibration_hold_s: float = 120.0
    voltage_soc_mismatch_warn_pct: float = 25.0
    voltage_mismatch_hold_s: float = 300.0
    imbalance_warn_v: float = 0.5
    stale_input_s: float = 120.0
    dc_max_age_s: float = 60.0
    require_fresh_inputs: bool = False

    @classmethod
    def field_names(cls):
        return {f.name for f in dataclasses.fields(cls)}

    @classmethod
    def from_dict(cls, d):
        known = cls.field_names()
        return cls(**{k: v for k, v in d.items() if k in known})

    def validate(self):
        if self.topology not in ("parallel", "series"):
            raise ValueError(
                f"Ungueltige topology: {self.topology} (erlaubt: parallel, series)"
            )
        if self.topology == "series":
            if not self.bank_b_enabled:
                raise ValueError("topology=series braucht bank_b_enabled=true")
            if self.bank_a_capacity_ah != self.bank_b_capacity_ah:
                raise ValueError(
                    "In Reihe geschaltete Baenke muessen dieselben Kapazitaeten haben: "
                    f"{self.bank_a_capacity_ah} vs {self.bank_b_capacity_ah} Ah"
                )
        elif self.bank_b_enabled and self.bank_b_cell_count != self.bank_a_cell_count:
            raise ValueError(
                "Parallel geschaltete Baenke muessen dieselbe Zellzahl haben: "
                f"{self.bank_a_cell_count} vs {self.bank_b_cell_count}"
            )
        if self.bank_a_cell_count <= 0 or (self.bank_b_enabled and self.bank_b_cell_count <= 0):
            raise ValueError("Zellzahlen muessen positiv sein")
        if self.bank_a_capacity_ah <= 0 or (self.bank_b_enabled and self.bank_b_capacity_ah <= 0):
            raise ValueError("Kapazitaeten muessen positiv sein")
        for name in ("charger_ac_dc_efficiency", "inverter_dc_ac_efficiency", "charge_efficiency"):
            value = getattr(self, name)
            if not 0 < value <= 1:
                raise ValueError(f"{name} muss zwischen 0 (exklusiv) und 1 liegen: {value}")
