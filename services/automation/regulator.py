"""Regler-Rechnung fuer Automations-Publish-Aktionen.

Berechnet aus dem Netzsaldo einen Sollwert und quantisiert ihn so, dass
dieselbe Formatierung wie bei einfachen Publish-Aktionen entsteht.
"""

import math
from dataclasses import dataclass
from typing import Optional


def quantize(
    value: float,
    *,
    step: Optional[float] = None,
    minimum: Optional[float] = None,
    maximum: Optional[float] = None,
    decimals: Optional[int] = None,
) -> tuple[float, str]:
    if step:
        # Floor, not round-to-nearest: a surplus-driven setpoint (e.g. wallbox
        # charge power from grid_export) must never round up past what is
        # actually available.
        value = math.floor(value / step) * step
    if minimum is not None:
        value = max(value, minimum)
    if maximum is not None:
        value = min(value, maximum)
    if decimals is not None:
        value = round(value, decimals)
        return value, f"{value:.{decimals}f}"
    return value, str(value)


@dataclass(frozen=True)
class RegulatorStep:
    error: float
    value: Optional[float]
    payload: Optional[str]
    reason: Optional[str]


def compute_step(regulator: dict, *, net: float, base: float, last_value: Optional[float]) -> RegulatorStep:
    error = net - regulator["target_w"]
    if abs(error) <= regulator["deadband_w"]:
        return RegulatorStep(error, None, None, "deadband")
    sign = 1 if regulator["direction"] == "producer" else -1
    value = base + sign * regulator["gain"] * error  # producer: Einspeisung (net<0) senkt
    max_change = regulator.get("max_change_w")
    if max_change is not None and last_value is not None:
        value = min(max(value, last_value - max_change), last_value + max_change)
    out = regulator["output"]
    value, payload = quantize(
        value,
        step=out.get("step"),
        minimum=out["min"],
        maximum=out["max"],
        decimals=out.get("decimals"),
    )
    if last_value is not None and value == last_value:
        return RegulatorStep(error, None, None, "unchanged")
    return RegulatorStep(error, value, payload, None)
