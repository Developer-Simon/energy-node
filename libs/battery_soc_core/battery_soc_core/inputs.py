"""SocInputs dataclass and freshness/availability helpers.

Transport-free core extracted from battery_soc_mqtt.py.
"""
from __future__ import annotations

from dataclasses import dataclass, field
from typing import Optional


@dataclass
class SocInputs:
    """Current measurements from all configured sources.

    Mutable dataclass with all timestamps and powers defaulting to 0.0,
    voltages to None, and configured flags to False.
    """
    charger_power_w: float = 0.0
    charger_power_ts: float = 0.0
    charger_power_configured: bool = False

    inverter_power_w: float = 0.0
    inverter_power_ts: float = 0.0
    inverter_power_configured: bool = False

    charger_dc_power_w: float = 0.0
    charger_dc_power_ts: float = 0.0
    charger_dc_power_configured: bool = False
    # Einheit "W" oder "A". Bei "A" steht in charger_dc_power_w ein Strom,
    # den die Engine mit der Packspannung in Watt umrechnet.
    charger_dc_power_unit: str = "W"

    inverter_dc_power_w: float = 0.0
    inverter_dc_power_ts: float = 0.0
    inverter_dc_power_configured: bool = False
    inverter_dc_power_unit: str = "W"

    bank_a_voltage_v: Optional[float] = None
    bank_a_voltage_ts: float = 0.0
    bank_a_voltage_configured: bool = False

    bank_b_voltage_v: Optional[float] = None
    bank_b_voltage_ts: float = 0.0
    bank_b_voltage_configured: bool = False


def sample_is_fresh(configured: bool, last_ts: float, now: float, max_age_s: float) -> bool:
    """Check if a sample is fresh (configured and not too old).

    An unconfigured sample (configured=False) is never fresh, even if
    max_age_s is generous. This prevents an empty placeholder with
    timestamp 0.0 from appearing as a valid source.
    """
    return configured and (now - last_ts) <= max_age_s


def _stack_mode(params) -> bool:
    return params.topology == "series" and params.bank_a_voltage_measures == "stack"


def bank_voltages(params, inputs) -> list:
    """Spannung je gefuehrter Einheit: parallel [Bus], in Reihe [A, B].

    Misst der Bank-A-Sensor den ganzen Stapel (A+ -> B-), ist Bank A der
    Stapel minus Bank B - ohne Bank B also unbekannt."""
    a, b = inputs.bank_a_voltage_v, inputs.bank_b_voltage_v
    if params.topology != "series":
        return [a]
    if _stack_mode(params):
        return [None, None] if a is None or b is None else [a - b, b]
    return [a, b]


def pack_voltage_v(params, inputs) -> Optional[float]:
    """Spannung, an der der Paketstrom haengt: parallel die Busspannung, in
    Reihe die Summe beider Baenke. None, solange eine davon fehlt."""
    voltages = bank_voltages(params, inputs)
    return None if any(v is None for v in voltages) else sum(voltages)


def _bank_a_voltage_ts(params, inputs) -> float:
    """Aus dem Stapel abgeleitet ist Bank A nur so frisch wie beide Sensoren."""
    if not _stack_mode(params):
        return inputs.bank_a_voltage_ts
    if not inputs.bank_b_voltage_configured:
        return 0.0
    return min(inputs.bank_a_voltage_ts, inputs.bank_b_voltage_ts)


def _pack_voltage_slots(params):
    if params.topology == "series":
        return ("bank_a_voltage", "bank_b_voltage")
    return ("bank_a_voltage",)


def effective_ts(params, inputs, slot: str) -> float:
    """Zeitstempel, nach dem das Alter eines Slots bemessen wird.

    Ein Strom-Slot (Einheit A) ist nur so frisch wie sein eigener Wert UND
    die Spannung(en), mit denen er in Watt umgerechnet wird. Fehlt eine davon
    ganz, gilt er als veraltet (0.0). So greift die vorhandene
    Staleness-Logik ohne Sonderfall."""
    ts = getattr(inputs, f"{slot}_ts")
    if getattr(inputs, f"{slot}_unit", "W") != "A":
        return ts
    for voltage in _pack_voltage_slots(params):
        if (not getattr(inputs, f"{voltage}_configured")
                or getattr(inputs, f"{voltage}_v") is None):
            return 0.0
        ts = min(ts, getattr(inputs, f"{voltage}_ts"))
    return ts


def slot_power_w(params, inputs, slot: str) -> float:
    """Leistung eines Leistungs-Slots in W; ein Strom-Slot wird mit der
    Packspannung umgerechnet (ohne Wirkungsgrad, er misst schon am Bus)."""
    value = getattr(inputs, f"{slot}_w")
    if getattr(inputs, f"{slot}_unit", "W") != "A":
        return value
    voltage_v = pack_voltage_v(params, inputs)
    return 0.0 if voltage_v is None else value * voltage_v


def input_groups(params, inputs) -> tuple[tuple[str, list[tuple[bool, float]]], ...]:
    """Group inputs by their measurement source, with German names.

    Returns tuples of (group_name, [(configured, last_ts), ...]).
    Each group lists its sources in priority order (DC before AC).
    Unconfigured sources are filtered out, but empty groups are kept
    (the availability logic needs to distinguish "not configured" from "stale").

    Topology determines the voltage group names:
    - parallel: "Busspannung" (single bank_a)
    - series: "Spannung Bank A" and "Spannung Bank B" (if enabled)
    """
    groups = [
        ("Ladeleistung", [
            (inputs.charger_dc_power_configured,
             effective_ts(params, inputs, "charger_dc_power")),
            (inputs.charger_power_configured, inputs.charger_power_ts),
        ]),
        ("Umrichterleistung", [
            (inputs.inverter_dc_power_configured,
             effective_ts(params, inputs, "inverter_dc_power")),
            (inputs.inverter_power_configured, inputs.inverter_power_ts),
        ]),
    ]

    # Voltage groups depend on topology
    if params.topology == "parallel":
        groups.append(("Busspannung", [
            (inputs.bank_a_voltage_configured, inputs.bank_a_voltage_ts),
        ]))
    else:  # series
        groups.append(("Spannung Bank A", [
            (inputs.bank_a_voltage_configured, _bank_a_voltage_ts(params, inputs)),
        ]))
        if params.bank_b_enabled:
            groups.append(("Spannung Bank B", [
                (inputs.bank_b_voltage_configured, inputs.bank_b_voltage_ts),
            ]))

    # Filter out unconfigured sources but keep empty groups
    return tuple(
        (name, [(c, ts) for c, ts in entries if c])
        for name, entries in groups
    )


def stale_groups(params, inputs, now: float) -> dict[str, bool]:
    """Determine which input groups have no fresh data.

    Returns a dict mapping group name to True (all sources stale) or False (at least
    one source fresh). Unconfigured groups (empty entries) are omitted from the result.
    """
    limit = params.stale_input_s
    return {
        name: all(now - ts > limit for _c, ts in entries)
        for name, entries in input_groups(params, inputs)
        if entries
    }


@dataclass
class AvailabilityResult:
    """Result of availability analysis.

    available: True if at least one input group has a fresh source.
    missing: List of group names that are either unconfigured or all-stale.
    any_configured: True if any group has at least one configured source.
    """
    available: bool = False
    missing: list[str] = field(default_factory=list)
    any_configured: bool = False


def availability(params, inputs, now: float) -> AvailabilityResult:
    """Transport-free core of publish_online_status.

    Determines system availability based on input freshness:
    - available: True if any group has at least one fresh source
    - missing: Groups that are unconfigured or all-stale
    - any_configured: True if any group has configured sources

    The logic:
    1. A group is "available" if it has entries (configured) AND at least one is fresh.
    2. A group is "missing" if it has no entries (unconfigured) OR all entries are stale.
    3. any_configured is True if input_groups produces any non-empty group.
    """
    groups = input_groups(params, inputs)
    missing = []
    stale = stale_groups(params, inputs, now)

    for name, entries in groups:
        if not entries:
            # Unconfigured group
            missing.append(name)
        elif stale.get(name, False):
            # All sources in this group are stale
            missing.append(name)

    # available if any group has at least one fresh source
    available = any(
        any(now - ts <= params.stale_input_s for _c, ts in entries)
        for _name, entries in groups
    )

    # any_configured if any group has entries
    any_configured = any(entries for _name, entries in groups)

    return AvailabilityResult(
        available=available,
        missing=missing,
        any_configured=any_configured,
    )
