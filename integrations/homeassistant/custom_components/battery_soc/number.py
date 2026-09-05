"""number platform for battery_soc."""
from __future__ import annotations

from homeassistant.components.number import NumberEntity, NumberMode
from homeassistant.config_entries import ConfigEntry
from homeassistant.core import HomeAssistant
from homeassistant.helpers.entity_platform import AddEntitiesCallback

from .battery_soc_core import entity_specs, set_state_of_charge
from .const import DOMAIN
from .entity import BatterySocEntity
from .coordinator import BatterySocCoordinator


class BatterySocManualSoc(BatterySocEntity, NumberEntity):
    """Manual SoC number entity for battery_soc."""

    def __init__(self, coordinator: BatterySocCoordinator, desc) -> None:
        """Initialize the number entity."""
        super().__init__(coordinator, desc)
        self._attr_native_min_value = desc.number_min
        self._attr_native_max_value = desc.number_max
        self._attr_native_step = desc.number_step
        self._attr_native_unit_of_measurement = desc.unit
        self._attr_mode = NumberMode.BOX

        # Derive unit_name from object_id
        if desc.object_id.startswith("manual_soc_"):
            self._unit_name = desc.object_id[len("manual_soc_"):]
        else:
            self._unit_name = None

    @property
    def native_value(self) -> float | None:
        """Return the native value of the number entity."""
        return (self.coordinator.data or {}).get(self._desc.value_key)

    async def async_set_native_value(self, value: float) -> None:
        """Set the native value."""
        set_state_of_charge(
            self.coordinator.state,
            self.coordinator.params,
            value,
            unit_name=self._unit_name,
        )
        self.coordinator._recalc()
        await self.coordinator._save()


async def async_setup_entry(
    hass: HomeAssistant,
    entry: ConfigEntry,
    async_add_entities: AddEntitiesCallback,
) -> None:
    """Set up number entities from a config entry."""
    coordinator: BatterySocCoordinator = hass.data[DOMAIN][entry.entry_id]
    async_add_entities(
        BatterySocManualSoc(coordinator, d)
        for d in entity_specs(coordinator.params)
        if d.component == "number"
    )
