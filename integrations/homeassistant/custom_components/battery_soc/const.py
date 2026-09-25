"""Constants for the battery_soc integration."""
from __future__ import annotations

DOMAIN = "battery_soc"
PLATFORMS = ["sensor", "binary_sensor", "number"]

SERVICE_SET_SOC = "set_state_of_charge"
SERVICE_APPLY_SUGGESTION = "apply_suggestion"
ATTR_STATE_OF_CHARGE = "state_of_charge"
ATTR_BANK = "bank"

CONF_CHARGER_POWER_ENTITY = "charger_power_entity"
CONF_INVERTER_POWER_ENTITY = "inverter_power_entity"
CONF_CHARGER_DC_POWER_ENTITY = "charger_dc_power_entity"
CONF_INVERTER_DC_POWER_ENTITY = "inverter_dc_power_entity"
CONF_BANK_A_VOLTAGE_ENTITY = "bank_a_voltage_entity"
CONF_BANK_B_VOLTAGE_ENTITY = "bank_b_voltage_entity"
CONF_BANK_A_VOLTAGE_SCALE = "bank_a_voltage_scale"
CONF_BANK_B_VOLTAGE_SCALE = "bank_b_voltage_scale"
CONF_FALLBACK_INTERVAL_S = "fallback_interval_s"
CONF_SYSTEM_TYPE = "system_type"
# Form-only: mapped onto bank_b_enabled + topology, never stored.
CONF_BANK_LAYOUT = "bank_layout"
# Series only: what the bank A sensor measures (SocParams field).
CONF_BANK_A_VOLTAGE_MEASURES = "bank_a_voltage_measures"
BANK_A_VOLTAGE_MEASURES = ("bank_a", "stack")

DEFAULT_FALLBACK_INTERVAL_S = 30
DEFAULT_VOLTAGE_SCALE = 1.0

SYSTEM_AC_COUPLED = "ac_coupled"
SYSTEM_DC_ONLY = "dc_only"

LAYOUT_SINGLE = "single"
LAYOUT_PARALLEL = "parallel"
LAYOUT_SERIES = "series"
# bank_layout -> (bank_b_enabled, topology)
BANK_LAYOUTS = {
    LAYOUT_SINGLE: (False, "parallel"),
    LAYOUT_PARALLEL: (True, "parallel"),
    LAYOUT_SERIES: (True, "series"),
}

# core slot name -> config key of its entity picker
SLOT_ENTITY_KEYS = {
    "charger_power": CONF_CHARGER_POWER_ENTITY,
    "inverter_power": CONF_INVERTER_POWER_ENTITY,
    "charger_dc_power": CONF_CHARGER_DC_POWER_ENTITY,
    "inverter_dc_power": CONF_INVERTER_DC_POWER_ENTITY,
    "bank_a_voltage": CONF_BANK_A_VOLTAGE_ENTITY,
    "bank_b_voltage": CONF_BANK_B_VOLTAGE_ENTITY,
}
INVERT_KEYS = (
    "charger_power_invert", "inverter_power_invert",
    "charger_dc_power_invert", "inverter_dc_power_invert",
)

# entity-picker + scale keys that are NOT SocParams fields
SOURCE_KEYS = (
    CONF_CHARGER_POWER_ENTITY, CONF_INVERTER_POWER_ENTITY,
    CONF_CHARGER_DC_POWER_ENTITY, CONF_INVERTER_DC_POWER_ENTITY,
    CONF_BANK_A_VOLTAGE_ENTITY, CONF_BANK_B_VOLTAGE_ENTITY,
    CONF_BANK_A_VOLTAGE_SCALE, CONF_BANK_B_VOLTAGE_SCALE,
    CONF_FALLBACK_INTERVAL_S, CONF_SYSTEM_TYPE, *INVERT_KEYS,
)

# Ausliefer-Pfad der Lovelace-Karte. Die Dateien liegen im Paket unter www/
# und werden beim ersten Setup als statischer Pfad registriert.
FRONTEND_URL_BASE = "/battery_soc_frontend"
FRONTEND_CARD_FILES = ("battery-card-core.js", "battery-soc-card.js")
