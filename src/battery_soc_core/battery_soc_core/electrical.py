from __future__ import annotations


def estimate_current(power_w, voltage_v):
    """Bank-Strom aus Bank-Leistung; None wenn keine Spannung vorliegt."""
    if voltage_v is None or voltage_v <= 0:
        return None
    return power_w / voltage_v
