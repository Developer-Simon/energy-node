"""Unit normalisation for power and current source entities.

The core expects watts, or amps on a DC slot (converted there with the pack
voltage). Home Assistant sensors report whatever unit their integration picked,
so every reading is scaled to W or A here, before it reaches SocInputs.
"""
from __future__ import annotations

POWER_UNITS = {"W": 1.0, "kW": 1000.0, "mW": 0.001}
CURRENT_UNITS = {"A": 1.0, "mA": 0.001}


class UnitError(ValueError):
    """The reading cannot be used on this slot; the message is the reason."""


def base_unit(uom: str | None) -> str | None:
    """"W" or "A" for a supported unit, None for anything else."""
    if uom in POWER_UNITS:
        return "W"
    if uom in CURRENT_UNITS:
        return "A"
    return None


def normalize(value: float, uom: str | None, *, allow_current: bool) -> tuple[float, str]:
    """Scale a reading to W or A. Current is only allowed on DC slots."""
    if uom in POWER_UNITS:
        return value * POWER_UNITS[uom], "W"
    if uom in CURRENT_UNITS:
        if not allow_current:
            raise UnitError(f"current ({uom}) is only accepted on DC slots")
        return value * CURRENT_UNITS[uom], "A"
    if uom is None:
        raise UnitError("no unit_of_measurement")
    raise UnitError(f"unsupported unit {uom!r}")
