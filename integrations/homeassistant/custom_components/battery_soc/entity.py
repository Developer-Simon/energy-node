"""Shared entity base class for battery_soc integration."""
from __future__ import annotations

from homeassistant.const import EntityCategory
from homeassistant.helpers.device_registry import DeviceInfo
from homeassistant.helpers.update_coordinator import CoordinatorEntity

from .battery_soc_core import EntityDesc
from .const import DOMAIN
from .coordinator import BatterySocCoordinator


def _category(value: str | None) -> EntityCategory | None:
    """Convert string to EntityCategory, or None if invalid/falsy."""
    if not value:
        return None
    try:
        return EntityCategory(value)
    except ValueError:
        return None


class BatterySocEntity(CoordinatorEntity[BatterySocCoordinator]):
    """Base class for battery_soc entities."""

    def __init__(self, coordinator: BatterySocCoordinator, desc: EntityDesc) -> None:
        """Initialize the entity."""
        super().__init__(coordinator)
        self._desc = desc
        self._attr_unique_id = f"{coordinator.entry.entry_id}_{desc.object_id}"
        self._attr_has_entity_name = True
        self._attr_name = desc.name
        self._attr_entity_category = _category(desc.entity_category)
        self._attr_entity_registry_enabled_default = desc.enabled_by_default
        self._attr_icon = desc.icon
        self._attr_device_info = DeviceInfo(
            identifiers={(DOMAIN, coordinator.entry.entry_id)},
            name=coordinator.entry.title,
            manufacturer="DIY",
            model="LiFePO4 Dual-Bank Coulomb-Counter",
        )

    @property
    def available(self) -> bool:
        """Check if entity is available."""
        return super().available and self._desc.value_key in (self.coordinator.data or {})
