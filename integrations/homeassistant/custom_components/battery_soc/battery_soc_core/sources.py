"""sources.py — welche Quellen-Kombinationen gueltig sind.

Eine Regelbasis fuer beide Adapter: die HA-Integration ruft sie im
Config-Flow auf, der MQTT-Dienst beim Laden. Sie liefert stabile
Fehlercodes, keinen Fliesstext - uebersetzen tun die Adapter.

Der Anlagentyp (system_type) steuert sonst nur die Formulare; die Engine
liest ihn nie. Hier dient er allein als Sicherheitsnetz gegen AC-Quellen,
die in einer reinen DC-Anlage stehen geblieben sind.
"""
from __future__ import annotations

from dataclasses import dataclass, field
from typing import Dict, FrozenSet, List

SYSTEM_TYPES = ("ac_coupled", "dc_only")

AC_POWER_SLOTS = ("charger_power", "inverter_power")
DC_POWER_SLOTS = ("charger_dc_power", "inverter_dc_power")
POWER_SLOTS = AC_POWER_SLOTS + DC_POWER_SLOTS
VOLTAGE_SLOTS = ("bank_a_voltage", "bank_b_voltage")

# (AC-Slot, DC-Slot) je Seite der Leistungsbilanz
_SIDES = (("charger_power", "charger_dc_power"),
          ("inverter_power", "inverter_dc_power"))


@dataclass(frozen=True)
class SourceConfig:
    """Schmale Sicht auf die Quellen einer Anlage.

    configured: Slot-Namen (POWER_SLOTS, VOLTAGE_SLOTS), fuer die eine Quelle
    eingetragen ist. units: Slot -> "W" / "A", nur fuer bekannte Einheiten."""
    configured: FrozenSet[str] = frozenset()
    units: Dict[str, str] = field(default_factory=dict)
    topology: str = "parallel"
    bank_b_enabled: bool = True
    system_type: str = "ac_coupled"

    def fallback_possible(self) -> bool:
        """Ein AC-Rueckfall ist nur moeglich, wo eine Seite AC UND DC hat."""
        return any(ac in self.configured and dc in self.configured for ac, dc in _SIDES)


def validate_sources(config: SourceConfig) -> List[str]:
    """Fehlercodes in fester Reihenfolge; leer heisst gueltig."""
    configured = config.configured
    codes = []
    if "bank_a_voltage" not in configured:
        codes.append("bank_a_voltage_required")
    if config.topology == "series" and "bank_b_voltage" not in configured:
        codes.append("bank_b_voltage_required")
    if "charger_power" not in configured and "charger_dc_power" not in configured:
        codes.append("charge_source_required")
    if "inverter_power" not in configured and "inverter_dc_power" not in configured:
        codes.append("discharge_source_required")
    if any(config.units.get(slot) == "A" for slot in AC_POWER_SLOTS):
        codes.append("current_only_on_dc")
    if config.system_type == "dc_only" and any(slot in configured for slot in AC_POWER_SLOTS):
        codes.append("ac_source_in_dc_system")
    return codes
