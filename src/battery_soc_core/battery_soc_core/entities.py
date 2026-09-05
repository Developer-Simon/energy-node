"""Declarative entity specifications for battery_soc_core.

Translates the imperative entities() builder from battery_soc_mqtt.py into
a declarative list of EntityDesc dataclasses — no MQTT wiring, just metadata.
"""
from __future__ import annotations

from dataclasses import dataclass
from typing import List, Optional

from .params import SocParams
from .state import build_units


@dataclass(frozen=True)
class EntityDesc:
    """Declarative specification of an entity published to Home Assistant."""
    component: str  # "sensor", "binary_sensor", "number"
    object_id: str
    name: str
    value_key: str
    unit: Optional[str] = None
    device_class: Optional[str] = None
    state_class: Optional[str] = None
    entity_category: Optional[str] = None
    icon: Optional[str] = None
    enabled_by_default: bool = True
    number_min: Optional[float] = None
    number_max: Optional[float] = None
    number_step: Optional[float] = None


def entity_specs(params: SocParams) -> List[EntityDesc]:
    """Generate the full list of entity specifications for the given topology.

    Reproduces exactly what battery_soc_mqtt.py::entities() yields,
    plus the new manual-SoC number entities.
    """
    series = params.topology == "series"

    items = [
        # Fixed 6 items
        EntityDesc(
            "sensor", "soc_combined",
            "SoC" if not series else "SoC Gesamt (schwaechste Bank)",
            "soc_combined_pct",
            unit="%", device_class="battery", state_class="measurement",
            icon="mdi:battery-heart-variant"
        ),
        EntityDesc(
            "sensor", "net_power", "Netto-Batterieleistung", "net_power_w",
            unit="W", device_class="power", state_class="measurement"
        ),
        EntityDesc(
            "binary_sensor", "inputs_stale", "Eingangsdaten veraltet",
            "inputs_stale",
            device_class="problem", entity_category="diagnostic"
        ),
        EntityDesc(
            "binary_sensor", "ac_fallback", "AC-Fallback aktiv",
            "ac_fallback_active",
            device_class="problem", entity_category="diagnostic",
            icon="mdi:transmission-tower"
        ),
        EntityDesc(
            "sensor", "time_to_full", "Zeit bis voll", "time_to_full_h",
            unit="h", device_class="duration", state_class="measurement",
            entity_category="diagnostic"
        ),
        EntityDesc(
            "sensor", "time_to_empty", "Zeit bis leer", "time_to_empty_h",
            unit="h", device_class="duration", state_class="measurement",
            entity_category="diagnostic"
        ),
    ]

    # Per-unit block
    labels = {"pack": "", "bank_a": " Bank A", "bank_b": " Bank B"}
    units = build_units(params)

    for unit in units:
        label = labels[unit.name]
        items.extend([
            EntityDesc(
                "sensor", f"voltage_{unit.name}",
                f"Spannung{label or ' (Bus)'}",
                f"{unit.name}_voltage_v",
                unit="V", device_class="voltage", state_class="measurement",
                entity_category="diagnostic"
            ),
            EntityDesc(
                "sensor", f"current_{unit.name}",
                f"Strom{label} (geschaetzt)",
                f"{unit.name}_current_a",
                unit="A", device_class="current", state_class="measurement",
                entity_category="diagnostic"
            ),
            EntityDesc(
                "sensor", f"remaining_ah_{unit.name}",
                f"Restkapazitaet{label}",
                f"{unit.name}_remaining_ah",
                unit="Ah", state_class="measurement",
                entity_category="diagnostic"
            ),
            EntityDesc(
                "sensor", f"corrected_v_{unit.name}",
                f"Lastkorrigierte Zellspannung{label}",
                f"{unit.name}_corrected_v_per_cell",
                unit="V", device_class="voltage", state_class="measurement",
                entity_category="diagnostic"
            ),
            EntityDesc(
                "sensor", f"last_calibration_{unit.name}",
                f"Letzte Kalibrierung{label}",
                f"last_calibration_{unit.name}",
                device_class="timestamp", entity_category="diagnostic"
            ),
            EntityDesc(
                "sensor", f"calibration_empty_v_{unit.name}",
                f"Kalibrierschwelle leer{label}",
                f"{unit.name}_calibration_empty_v_per_cell",
                unit="V", device_class="voltage", state_class="measurement",
                entity_category="diagnostic"
            ),
            EntityDesc(
                "sensor", f"calibration_full_v_{unit.name}",
                f"Kalibrierschwelle voll{label}",
                f"{unit.name}_calibration_full_v_per_cell",
                unit="V", device_class="voltage", state_class="measurement",
                entity_category="diagnostic"
            ),
            EntityDesc(
                "sensor", f"voltage_soc_{unit.name}",
                f"Spannungsbasierte SoC (unsicher){label}",
                f"{unit.name}_voltage_soc_pct",
                unit="%", device_class="battery", state_class="measurement",
                entity_category="diagnostic", icon="mdi:flash-alert"
            ),
            EntityDesc(
                "binary_sensor", f"voltage_soc_mismatch_{unit.name}",
                f"Spannungs-/Coulomb-Abweichung{label}",
                f"{unit.name}_voltage_soc_mismatch",
                device_class="problem", entity_category="diagnostic"
            ),
        ])

    # Series-only items
    if series:
        items.extend([
            EntityDesc(
                "sensor", "soc_a", "SoC Bank A", "soc_a_pct",
                unit="%", device_class="battery", state_class="measurement",
                icon="mdi:battery"
            ),
            EntityDesc(
                "sensor", "soc_b", "SoC Bank B", "soc_b_pct",
                unit="%", device_class="battery", state_class="measurement",
                icon="mdi:battery"
            ),
            EntityDesc(
                "sensor", "voltage_delta", "Spannungsdifferenz Bank A/B",
                "voltage_delta_v",
                unit="V", device_class="voltage", state_class="measurement",
                entity_category="diagnostic", icon="mdi:scale-unbalanced"
            ),
            EntityDesc(
                "binary_sensor", "imbalance_warning", "Baenke unsymmetrisch",
                "imbalance_warning",
                device_class="problem", entity_category="diagnostic",
                icon="mdi:scale-unbalanced"
            ),
        ])

    # Manual-SoC number entities
    if series:
        items.extend([
            EntityDesc(
                "number", "manual_soc_bank_a", "Manueller SoC Bank A setzen",
                "soc_a_pct",
                unit="%", number_min=0, number_max=100, number_step=1,
                icon="mdi:battery-sync", entity_category="config"
            ),
            EntityDesc(
                "number", "manual_soc_bank_b", "Manueller SoC Bank B setzen",
                "soc_b_pct",
                unit="%", number_min=0, number_max=100, number_step=1,
                icon="mdi:battery-sync", entity_category="config"
            ),
        ])
    else:
        items.append(EntityDesc(
            "number", "manual_soc", "Manueller SoC setzen", "soc_combined_pct",
            unit="%", number_min=0, number_max=100, number_step=1,
            icon="mdi:battery-sync", entity_category="config"
        ))

    return items


# All object_ids ever assigned by this service. Entries not in the current
# topology are actively deleted during discovery publication.
ALL_OBJECT_IDS = {
    "sensor": [
        "soc_a", "soc_b", "soc_combined",
        "voltage_a", "voltage_b", "voltage_pack", "voltage_bank_a", "voltage_bank_b",
        "voltage_delta", "net_power",
        "last_calibration_a", "last_calibration_b",
        "last_calibration_pack", "last_calibration_bank_a", "last_calibration_bank_b",
        "current_a", "current_b", "current_pack", "current_bank_a", "current_bank_b",
        "remaining_ah_a", "remaining_ah_b",
        "remaining_ah_pack", "remaining_ah_bank_a", "remaining_ah_bank_b",
        "corrected_v_a", "corrected_v_b",
        "corrected_v_pack", "corrected_v_bank_a", "corrected_v_bank_b",
        "voltage_soc_pack", "voltage_soc_bank_a", "voltage_soc_bank_b",
        "time_to_full", "time_to_empty",
    ],
    "binary_sensor": [
        "inputs_stale", "ac_fallback", "imbalance_warning",
        "voltage_soc_mismatch_pack", "voltage_soc_mismatch_bank_a",
        "voltage_soc_mismatch_bank_b",
    ],
    "number": [
        "manual_soc", "manual_soc_bank_a", "manual_soc_bank_b",
    ],
}
