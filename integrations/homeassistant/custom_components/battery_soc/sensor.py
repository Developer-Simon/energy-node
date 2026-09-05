"""Sensor platform for battery_soc."""
from __future__ import annotations

from homeassistant.components.sensor import SensorDeviceClass, SensorEntity, SensorStateClass
from homeassistant.config_entries import ConfigEntry
from homeassistant.core import HomeAssistant
from homeassistant.helpers.entity_platform import AddEntitiesCallback
from homeassistant.util import dt as dt_util

from .battery_soc_core import entity_specs
from .const import DOMAIN
from .entity import BatterySocEntity
from .coordinator import BatterySocCoordinator


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


async def async_setup_entry(
    hass: HomeAssistant,
    entry: ConfigEntry,
    async_add_entities: AddEntitiesCallback,
) -> None:
    """Set up sensor entities from a config entry."""
    coordinator: BatterySocCoordinator = hass.data[DOMAIN][entry.entry_id]
    async_add_entities(
        BatterySocSensor(coordinator, d)
        for d in entity_specs(coordinator.params)
        if d.component == "sensor"
    )
