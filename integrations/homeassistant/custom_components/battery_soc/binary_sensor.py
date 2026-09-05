"""binary_sensor platform for battery_soc."""
from __future__ import annotations

from homeassistant.components.binary_sensor import BinarySensorDeviceClass, BinarySensorEntity
from homeassistant.config_entries import ConfigEntry
from homeassistant.core import HomeAssistant
from homeassistant.helpers.entity_platform import AddEntitiesCallback

from .battery_soc_core import entity_specs
from .const import DOMAIN
from .entity import BatterySocEntity
from .coordinator import BatterySocCoordinator


def _binary_device_class(value: str | None) -> BinarySensorDeviceClass | None:
    """Convert string to BinarySensorDeviceClass, or None if invalid/falsy."""
    if not value:
        return None
    try:
        return BinarySensorDeviceClass(value)
    except ValueError:
        return None


class BatterySocBinarySensor(BatterySocEntity, BinarySensorEntity):
    """Binary sensor entity for battery_soc."""

    def __init__(self, coordinator: BatterySocCoordinator, desc) -> None:
        """Initialize the binary sensor."""
        super().__init__(coordinator, desc)
        self._attr_device_class = _binary_device_class(desc.device_class)

    @property
    def is_on(self) -> bool:
        """Return the state of the binary sensor."""
        return bool((self.coordinator.data or {}).get(self._desc.value_key))


async def async_setup_entry(
    hass: HomeAssistant,
    entry: ConfigEntry,
    async_add_entities: AddEntitiesCallback,
) -> None:
    """Set up binary_sensor entities from a config entry."""
    coordinator: BatterySocCoordinator = hass.data[DOMAIN][entry.entry_id]
    async_add_entities(
        BatterySocBinarySensor(coordinator, d)
        for d in entity_specs(coordinator.params)
        if d.component == "binary_sensor"
    )
