"""Coordinator for battery_soc integration."""
from __future__ import annotations

import dataclasses
import logging
import time
from dataclasses import dataclass
from datetime import timedelta
from typing import Any, Callable

from homeassistant.const import ATTR_UNIT_OF_MEASUREMENT, STATE_UNKNOWN, STATE_UNAVAILABLE
from homeassistant.core import HomeAssistant, callback
from homeassistant.helpers.debounce import Debouncer
from homeassistant.helpers.event import async_track_state_change_event, async_track_time_interval
from homeassistant.helpers.storage import Store
from homeassistant.helpers.update_coordinator import DataUpdateCoordinator

from .battery_soc_core import SocInputs, SocState, tick, analyse_calibration
from .const import (
    CONF_BANK_A_VOLTAGE_SCALE,
    CONF_BANK_B_VOLTAGE_SCALE,
    CONF_FALLBACK_INTERVAL_S,
    DEFAULT_FALLBACK_INTERVAL_S,
    DEFAULT_VOLTAGE_SCALE,
    DOMAIN,
    SLOT_ENTITY_KEYS,
)
from .helpers import params_from_config, source_config
from .units import UnitError, normalize

_LOGGER = logging.getLogger(__name__)


@dataclass(frozen=True)
class Slot:
    """One core input: its config key and where readings land in SocInputs."""

    name: str                    # core slot name, e.g. "charger_dc_power"
    kind: str                    # "ac_power" | "dc_power" | "voltage"
    scale_key: str | None = None

    @property
    def conf_key(self) -> str:
        return SLOT_ENTITY_KEYS[self.name]

    @property
    def value_attr(self) -> str:
        return f"{self.name}_v" if self.kind == "voltage" else f"{self.name}_w"


SLOTS = (
    Slot("charger_power", "ac_power"),
    Slot("inverter_power", "ac_power"),
    Slot("charger_dc_power", "dc_power"),
    Slot("inverter_dc_power", "dc_power"),
    Slot("bank_a_voltage", "voltage", CONF_BANK_A_VOLTAGE_SCALE),
    Slot("bank_b_voltage", "voltage", CONF_BANK_B_VOLTAGE_SCALE),
)


class BatterySocCoordinator(DataUpdateCoordinator[dict]):
    """Coordinator for battery_soc integration."""

    def __init__(self, hass: HomeAssistant, entry) -> None:
        """Initialize coordinator."""
        self.entry = entry
        self._merged: dict[str, Any] = {**entry.data, **entry.options}
        self.params = params_from_config(self._merged)
        self.sources = source_config(self._merged)
        self.state = SocState(self.params)
        self._store = Store(hass, 1, f"{DOMAIN}.{entry.entry_id}")
        self._inputs = SocInputs()
        self._unsub: list[Callable] = []
        self._save_debounce = Debouncer(
            hass, _LOGGER, cooldown=10, immediate=False, function=self._save
        )
        self._entity_map: dict[str, list[Slot]] = {}
        # (entity_id, slot) -> last warned reason; one warning per reason
        self._unit_warnings: dict[tuple[str, str], str] = {}
        self._fallback_interval = DEFAULT_FALLBACK_INTERVAL_S

        super().__init__(
            hass,
            _LOGGER,
            name=entry.title,
            update_method=None,
            update_interval=None,
            config_entry=entry,
        )

    def _configured_slots(self) -> list[Slot]:
        return [slot for slot in SLOTS if self._merged.get(slot.conf_key)]

    async def async_load(self) -> None:
        """Load state from store and initialize inputs."""
        stored = await self._store.async_load()
        if stored:
            self.state.load_dict(stored)

        for slot in SLOTS:
            setattr(self._inputs, f"{slot.name}_configured",
                    bool(self._merged.get(slot.conf_key)))

        self._prime_from_states()
        self._recalc()

    def _prime_from_states(self) -> None:
        """Read current HA state for each configured slot into _inputs."""
        for slot in self._configured_slots():
            self._apply_state(slot, self.hass.states.get(self._merged[slot.conf_key]))

    def _apply_state(self, slot: Slot, st) -> bool:
        """Write one reading into its slot; False when the state is unusable.

        Power readings are scaled to W (or A on a DC slot) and then inverted
        if the slot asks for it, so one signed sensor can feed the charge
        slot as-is and the discharge slot inverted."""
        if st is None or st.state in (STATE_UNKNOWN, STATE_UNAVAILABLE):
            return False
        try:
            val = float(st.state)
        except (ValueError, TypeError):
            _LOGGER.debug("non-numeric state %s for %s", st.state, st.entity_id)
            return False

        if slot.kind == "voltage":
            val *= self._merged.get(slot.scale_key, DEFAULT_VOLTAGE_SCALE)
        else:
            try:
                val, unit = normalize(
                    val, st.attributes.get(ATTR_UNIT_OF_MEASUREMENT),
                    allow_current=slot.kind == "dc_power",
                )
            except UnitError as err:
                self._warn_unit(st.entity_id, slot, str(err))
                return False
            self._unit_warnings.pop((st.entity_id, slot.name), None)
            if self._merged.get(f"{slot.name}_invert"):
                val = -val
            if slot.kind == "dc_power":
                setattr(self._inputs, f"{slot.name}_unit", unit)

        setattr(self._inputs, slot.value_attr, val)
        setattr(self._inputs, f"{slot.name}_ts", st.last_updated.timestamp())
        return True

    def _warn_unit(self, entity_id: str, slot: Slot, reason: str) -> None:
        key = (entity_id, slot.name)
        if self._unit_warnings.get(key) == reason:
            return
        self._unit_warnings[key] = reason
        _LOGGER.warning("Ignoring %s as %s: %s", entity_id, slot.name, reason)

    def _recalc(self) -> None:
        """Run core tick calculation and update coordinator data."""
        now = time.time()
        result = tick(self.params, self.state, self._inputs, now)
        tuning = {}
        for unit in self.state.units:
            suggestions, findings = analyse_calibration(self.params, unit.events)
            tuning[unit.name] = {
                "suggestions": [dataclasses.asdict(s) for s in suggestions],
                "findings": [dataclasses.asdict(f) for f in findings],
            }
        data = dict(result.outputs)
        data["_tuning"] = tuning
        self.async_set_updated_data(data)
        self._save_debounce.async_schedule_call()

    def async_start_listeners(self) -> None:
        """Start listening to source entity state changes and fallback timer."""
        # One entity may feed several slots (e.g. a signed sensor in both
        # DC slots, one of them inverted).
        self._entity_map = {}
        for slot in self._configured_slots():
            self._entity_map.setdefault(self._merged[slot.conf_key], []).append(slot)

        if self._entity_map:
            self._unsub.append(
                async_track_state_change_event(
                    self.hass, list(self._entity_map), self._on_source_change
                )
            )

        self._fallback_interval = self._merged.get(CONF_FALLBACK_INTERVAL_S, DEFAULT_FALLBACK_INTERVAL_S)
        self._unsub.append(
            async_track_time_interval(
                self.hass, self._on_tick, timedelta(seconds=self._fallback_interval)
            )
        )

    @callback
    def _on_source_change(self, event) -> None:
        """Handle state change event from source entity."""
        new = event.data.get("new_state")
        changed = False
        for slot in self._entity_map.get(event.data["entity_id"], ()):
            changed = self._apply_state(slot, new) or changed
        if changed:
            self._recalc()

    @callback
    def _on_tick(self, now) -> None:
        """Handle fallback timer tick."""
        self._recalc()

    async def _save(self) -> None:
        """Save current state to store."""
        await self._store.async_save(self.state.to_dict())

    async def async_shutdown(self) -> None:
        """Shutdown coordinator."""
        # Unsub all listeners (Tasks 6/7), make idempotent by clearing after
        while self._unsub:
            unsub = self._unsub.pop()
            unsub()

        # Shutdown debouncer and save
        self._save_debounce.async_shutdown()
        await self._save()
