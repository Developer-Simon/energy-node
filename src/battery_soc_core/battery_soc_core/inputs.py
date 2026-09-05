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

    inverter_dc_power_w: float = 0.0
    inverter_dc_power_ts: float = 0.0
    inverter_dc_power_configured: bool = False

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
            (inputs.charger_dc_power_configured, inputs.charger_dc_power_ts),
            (inputs.charger_power_configured, inputs.charger_power_ts),
        ]),
        ("Umrichterleistung", [
            (inputs.inverter_dc_power_configured, inputs.inverter_dc_power_ts),
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
            (inputs.bank_a_voltage_configured, inputs.bank_a_voltage_ts),
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
