"""Sensor platform for battery_soc."""
from __future__ import annotations

from homeassistant.components.sensor import SensorDeviceClass, SensorEntity, SensorStateClass
from homeassistant.config_entries import ConfigEntry
from homeassistant.core import HomeAssistant
from homeassistant.helpers.entity_platform import AddEntitiesCallback
from homeassistant.util import dt as dt_util

from .battery_soc_core import entity_specs, EntityDesc
from .battery_soc_core.messages import CATALOGS, finding_key
from .const import DOMAIN
from .entity import BatterySocEntity
from .coordinator import BatterySocCoordinator
from .energy_node_messages import render


def _sensor_device_class(value: str | None) -> SensorDeviceClass | None:
    """Convert string to SensorDeviceClass, or None if invalid/falsy."""
    if not value:
        return None
    try:
        return SensorDeviceClass(value)
    except ValueError:
        return None


def _state_class(value: str | None) -> SensorStateClass | None:
    """Convert string to SensorStateClass, or None if invalid/falsy."""
    if not value:
        return None
    try:
        return SensorStateClass(value)
    except ValueError:
        return None


class BatterySocSensor(BatterySocEntity, SensorEntity):
    """Sensor entity for battery_soc."""

    def __init__(self, coordinator: BatterySocCoordinator, desc) -> None:
        """Initialize the sensor."""
        super().__init__(coordinator, desc)
        self._attr_native_unit_of_measurement = desc.unit
        self._attr_device_class = _sensor_device_class(desc.device_class)
        self._attr_state_class = _state_class(desc.state_class)

    @property
    def native_value(self):
        """Return the native value of the sensor."""
        v = (self.coordinator.data or {}).get(self._desc.value_key)
        if self._desc.device_class == "timestamp" and v:
            return dt_util.parse_datetime(v)
        return v


_SUGG_LABELS = {"pack": " Pack", "bank_a": " Bank A", "bank_b": " Bank B"}


def _suggestions_desc(unit_name: str) -> EntityDesc:
    """Create an EntityDesc for a suggestions sensor."""
    return EntityDesc(
        "sensor", f"open_suggestions_{unit_name}",
        f"Offene Einstellungsvorschläge{_SUGG_LABELS.get(unit_name, ' ' + unit_name)}",
        "_tuning",
        entity_category="diagnostic", icon="mdi:tune", state_class="measurement",
    )


def _localized_findings(findings: list[dict], language: str) -> list[dict]:
    """Return copies of the findings with message in the HA language.

    Only findings with params are translated, and only when the catalog knows
    their key. Everything else keeps the stored message.
    """
    out = []
    for finding in findings:
        item = dict(finding)
        if "params" in finding:
            params = finding.get("params") or {}
            text = render(
                CATALOGS, language, finding_key(finding.get("code", ""), params), params
            )
            if text is not None:
                item["message"] = text
        out.append(item)
    return out


class BatterySocSuggestionsSensor(BatterySocEntity, SensorEntity):
    """Offene Einstellungsvorschlaege einer Einheit aus dem Kalibrierverhalten.

    Stammt bewusst NICHT aus entity_specs(): analyse_calibration ist eine
    Adapter-Auswertung ueber den Ereignisring, kein tick()-Output. Der native
    Wert ist die Anzahl offener Vorschlaege; der Inhalt (Vorschlaege + Befunde)
    haengt an extra_state_attributes - dieselbe Sicht, die die MQTT-Discovery-
    Entitaet in Task 6 ueber json_attributes_topic bekommt."""

    _attr_state_class = SensorStateClass.MEASUREMENT
    _attr_icon = "mdi:tune"

    def __init__(self, coordinator: BatterySocCoordinator, unit_name: str) -> None:
        """Initialize the suggestions sensor."""
        super().__init__(coordinator, _suggestions_desc(unit_name))
        self._unit_name = unit_name

    def _block(self) -> dict:
        """Get the tuning block for this unit."""
        return (self.coordinator.data or {}).get("_tuning", {}).get(self._unit_name, {})

    @property
    def native_value(self):
        """Return the number of open suggestions."""
        return len(self._block().get("suggestions", []))

    @property
    def extra_state_attributes(self):
        """Return suggestions and findings as attributes."""
        block = self._block()
        return {
            "suggestions": block.get("suggestions", []),
            "findings": _localized_findings(
                block.get("findings", []), self.hass.config.language
            ),
        }


async def async_setup_entry(
    hass: HomeAssistant,
    entry: ConfigEntry,
    async_add_entities: AddEntitiesCallback,
) -> None:
    """Set up sensor entities from a config entry."""
    coordinator: BatterySocCoordinator = hass.data[DOMAIN][entry.entry_id]
    async_add_entities(
        BatterySocSensor(coordinator, d)
        for d in entity_specs(coordinator.params, coordinator.sources)
        if d.component == "sensor"
    )
    async_add_entities(
        BatterySocSuggestionsSensor(coordinator, unit.name)
        for unit in coordinator.state.units
    )
