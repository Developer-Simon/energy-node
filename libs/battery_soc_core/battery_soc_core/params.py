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
    # Tail-Strom-Kriterium der Voll-Kalibrierung: das Pack gilt nur dann als
    # voll, wenn es bei Vollspannung nicht mehr als diese C-Rate aufnimmt
    # ODER abgibt. None = aus (Verhalten vor Einfuehrung).
    #
    # Das ist der Gegenspieler zu calibration_tolerance_v_per_cell. Die
    # Toleranz weicht die Schwelle bei kleinem Strom auf, weil die Messung
    # dann als Ruhespannung durchgeht. In einer Anlage mit Ueberschussladen
    # ist kleiner Strom aber kein Vollstands-Signal, sondern ein
    # Sonnenstands-Signal: der Laderegler stellt die Klemmenspannung nach
    # verfuegbarer Leistung ein. Erst zusammen mit dem Taper-Kriterium wird
    # die Toleranz wieder sicher - kleine Stroeme oeffnen das Fenster dann
    # nur noch, wenn das Pack die Ladung auch wirklich verweigert.
    #
    # Richtwert ist der Tail-Strom aus dem Zell-Datenblatt (Dyness AR2.5:
    # 5 A je 100-Ah-Pack = 0.05 C).
    full_taper_c_rate: Optional[float] = None
    # Seitenweise Uebersteuerung von calibration_tolerance_v_per_cell.
    # None = gemeinsamen Wert benutzen, 0.0 = ausdruecklich keine Toleranz.
    #
    # Warum ueberhaupt getrennt: die beiden Enden haben verschiedene Gegner.
    # Oben steht ein Laderegler, der die Vollspannung erreicht - dort ist
    # jede Aufweichung ein Risiko. Unten steht ein BMS, das lange vor der
    # Leerspannung der Kurve abschaltet - dort ist ohne Aufweichung gar kein
    # Anker zu bekommen. Ein symmetrischer Wert muss einen der beiden Faelle
    # falsch machen.
    calibration_tolerance_empty_v_per_cell: Optional[float] = None
    calibration_tolerance_full_v_per_cell: Optional[float] = None
    # Wie lange ein einzelner Aussetzer die laufende Haltezeit ueberleben
    # darf. 0 = Hard-Reset wie bisher.
    #
    # Ohne Karenz ist calibration_hold_s in einer Anlage mit
    # Ueberschussladen nicht nach oben skalierbar: der Laderegler pausiert
    # regelmaessig fuer eine halbe Minute, und jede Pause wirft den Timer auf
    # null. Genau die lange Haltezeit waere aber die beste Absicherung.
    # Die Karenz gilt NICHT fuer eine veraltete Spannung - dort setzt die
    # Engine None ein, und ein blinder Sensor darf keine Haltezeit fuellen.
    calibration_grace_s: float = 0.0
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

    def tolerance_for(self, side):
        """Maximale Schwellen-Aufweichung dieser Seite ('empty' / 'full')."""
        override = (self.calibration_tolerance_empty_v_per_cell if side == "empty"
                    else self.calibration_tolerance_full_v_per_cell)
        return self.calibration_tolerance_v_per_cell if override is None else override

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
        if self.full_taper_c_rate is not None and not 0 < self.full_taper_c_rate <= 1:
            raise ValueError(
                "full_taper_c_rate muss zwischen 0 (exklusiv) und 1 liegen: "
                f"{self.full_taper_c_rate}"
            )
        for name in ("calibration_tolerance_empty_v_per_cell",
                     "calibration_tolerance_full_v_per_cell"):
            value = getattr(self, name)
            if value is not None and value < 0:
                raise ValueError(f"{name} darf nicht negativ sein: {value}")
        if self.calibration_grace_s < 0:
            raise ValueError(
                f"calibration_grace_s darf nicht negativ sein: {self.calibration_grace_s}"
            )
        for name in ("charger_ac_dc_efficiency", "inverter_dc_ac_efficiency", "charge_efficiency"):
            value = getattr(self, name)
            if not 0 < value <= 1:
                raise ValueError(f"{name} muss zwischen 0 (exklusiv) und 1 liegen: {value}")
