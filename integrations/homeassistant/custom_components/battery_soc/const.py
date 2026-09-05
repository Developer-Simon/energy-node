"""Constants for the battery_soc integration."""
from __future__ import annotations

DOMAIN = "battery_soc"
PLATFORMS = ["sensor", "binary_sensor", "number"]

SERVICE_SET_SOC = "set_state_of_charge"
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

DEFAULT_FALLBACK_INTERVAL_S = 30
DEFAULT_VOLTAGE_SCALE = 1.0

# entity-picker + scale keys that are NOT SocParams fields
SOURCE_KEYS = (
    CONF_CHARGER_POWER_ENTITY, CONF_INVERTER_POWER_ENTITY,
    CONF_CHARGER_DC_POWER_ENTITY, CONF_INVERTER_DC_POWER_ENTITY,
    CONF_BANK_A_VOLTAGE_ENTITY, CONF_BANK_B_VOLTAGE_ENTITY,
    CONF_BANK_A_VOLTAGE_SCALE, CONF_BANK_B_VOLTAGE_SCALE,
    CONF_FALLBACK_INTERVAL_S,
)

# Ausliefer-Pfad der Lovelace-Karte. Die Dateien liegen im Paket unter www/
# und werden beim ersten Setup als statischer Pfad registriert.
FRONTEND_URL_BASE = "/battery_soc_frontend"
FRONTEND_CARD_FILES = ("battery-card-core.js", "battery-soc-card.js")
