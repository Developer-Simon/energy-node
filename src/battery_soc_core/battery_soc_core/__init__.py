"""battery_soc_core — transport-agnostic LiFePO4 state-of-charge core.

Stdlib only. No MQTT, no Home Assistant, no energy_node_common.
Public surface is re-exported here; adapters import from this module.
"""
from __future__ import annotations

from .calibration import (  # noqa: F401
    apply_calibration, apply_voltage_plausibility, calibration_tolerance,
    corrected_voltage_per_cell, simulated_open_circuit_v_per_cell,
    voltage_based_soc_pct,
)
from .curves import (  # noqa: F401
    CALIBRATION_BULK_C_RATE, CALIBRATION_TAPER_C_RATE, DYNESS_AR25_CURVE,
    GENERIC_LIFEPO4_CURVE, LOAD_OFFSET_TABLE_MV, MAX_TICK_HOURS,
    MIN_TIME_ESTIMATE_W, SOC_CURVES, load_offset_mv, soc_curve_for,
)
from .electrical import (  # noqa: F401
    estimate_current,
)
from .engine import (  # noqa: F401
    EffectivePower, TickResult, effective_power, integrate_coulomb,
    tick, time_estimates, unit_currents,
)
from .entities import (  # noqa: F401
    ALL_OBJECT_IDS, EntityDesc, entity_specs,
)
from .inputs import (  # noqa: F401
    AvailabilityResult, SocInputs, availability, input_groups, sample_is_fresh,
    stale_groups,
)
from .params import SocParams  # noqa: F401
from .simulation import (  # noqa: F401
    simulated_bank_voltage_v,
)
from .state import (  # noqa: F401
    BankState, build_units, SocState, set_state_of_charge,
)
from .tuning import Finding, Suggestion, analyse as analyse_calibration  # noqa: F401

__all__ = [
    "apply_calibration", "apply_voltage_plausibility", "calibration_tolerance",
    "corrected_voltage_per_cell", "simulated_open_circuit_v_per_cell",
    "voltage_based_soc_pct",
    "CALIBRATION_BULK_C_RATE", "CALIBRATION_TAPER_C_RATE", "DYNESS_AR25_CURVE",
    "GENERIC_LIFEPO4_CURVE", "LOAD_OFFSET_TABLE_MV", "MAX_TICK_HOURS",
    "MIN_TIME_ESTIMATE_W", "SOC_CURVES", "load_offset_mv", "soc_curve_for",
    "estimate_current",
    "EffectivePower", "TickResult", "effective_power", "integrate_coulomb",
    "tick", "time_estimates", "unit_currents",
    "ALL_OBJECT_IDS", "EntityDesc", "entity_specs",
    "SocParams",
    "AvailabilityResult", "SocInputs", "availability", "input_groups",
    "sample_is_fresh", "stale_groups",
    "BankState", "build_units", "SocState", "set_state_of_charge",
    "simulated_bank_voltage_v",
    "Finding", "Suggestion", "analyse_calibration",
]
