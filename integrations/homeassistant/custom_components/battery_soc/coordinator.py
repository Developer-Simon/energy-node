"""Coordinator for battery_soc integration."""
from __future__ import annotations

import logging
import time
from datetime import timedelta
from typing import Any, Callable

from homeassistant.const import STATE_UNKNOWN, STATE_UNAVAILABLE
from homeassistant.core import HomeAssistant, callback
from homeassistant.helpers.debounce import Debouncer
from homeassistant.helpers.event import async_track_state_change_event, async_track_time_interval
from homeassistant.helpers.storage import Store
from homeassistant.helpers.update_coordinator import DataUpdateCoordinator

from .battery_soc_core import SocInputs, SocState, tick
from .const import (
    CONF_BANK_A_VOLTAGE_ENTITY,
    CONF_BANK_A_VOLTAGE_SCALE,
    CONF_BANK_B_VOLTAGE_ENTITY,
    CONF_BANK_B_VOLTAGE_SCALE,
    CONF_CHARGER_DC_POWER_ENTITY,
    CONF_CHARGER_POWER_ENTITY,
    CONF_FALLBACK_INTERVAL_S,
    CONF_INVERTER_DC_POWER_ENTITY,
    CONF_INVERTER_POWER_ENTITY,
    DEFAULT_FALLBACK_INTERVAL_S,
    DEFAULT_VOLTAGE_SCALE,
    DOMAIN,
)
from .helpers import params_from_config

_LOGGER = logging.getLogger(__name__)

# Mapping of CONF_* keys to SocInputs fields
# Each entry: (conf_key, value_attr, ts_attr, configured_attr, scale_conf_key_or_None)
SOURCE_MAPPING = [
    (CONF_CHARGER_POWER_ENTITY, "charger_power_w", "charger_power_ts", "charger_power_configured", None),
    (CONF_INVERTER_POWER_ENTITY, "inverter_power_w", "inverter_power_ts", "inverter_power_configured", None),
    (CONF_CHARGER_DC_POWER_ENTITY, "charger_dc_power_w", "charger_dc_power_ts", "charger_dc_power_configured", None),
    (CONF_INVERTER_DC_POWER_ENTITY, "inverter_dc_power_w", "inverter_dc_power_ts", "inverter_dc_power_configured", None),
    (CONF_BANK_A_VOLTAGE_ENTITY, "bank_a_voltage_v", "bank_a_voltage_ts", "bank_a_voltage_configured", CONF_BANK_A_VOLTAGE_SCALE),
    (CONF_BANK_B_VOLTAGE_ENTITY, "bank_b_voltage_v", "bank_b_voltage_ts", "bank_b_voltage_configured", CONF_BANK_B_VOLTAGE_SCALE),
]


class BatterySocCoordinator(DataUpdateCoordinator[dict]):
    """Coordinator for battery_soc integration."""

    def __init__(self, hass: HomeAssistant, entry) -> None:
        """Initialize coordinator."""
        self.entry = entry
        merged_config = {**entry.data, **entry.options}
        self.params = params_from_config(merged_config)
        self.state = SocState(self.params)
        self._store = Store(hass, 1, f"{DOMAIN}.{entry.entry_id}")
        self._inputs = SocInputs()
        self._unsub: list[Callable] = []
        self._save_debounce = Debouncer(
            hass, _LOGGER, cooldown=10, immediate=False, function=self._save
        )
        self._entity_map: dict[str, tuple[str, str, str | None]] = {}
        self._merged: dict[str, Any] = {}
        self._fallback_interval = DEFAULT_FALLBACK_INTERVAL_S

        super().__init__(
            hass,
            _LOGGER,
            name=entry.title,
            update_method=None,
            update_interval=None,
            config_entry=entry,
        )

    async def async_load(self) -> None:
        """Load state from store and initialize inputs."""
        # Load stored state
        stored = await self._store.async_load()
        if stored:
            self.state.load_dict(stored)

        # Mark which sources are configured
        merged_config = {**self.entry.data, **self.entry.options}
        for conf_key, _, _, configured_attr, _ in SOURCE_MAPPING:
            is_configured = conf_key in merged_config and bool(merged_config[conf_key])
            setattr(self._inputs, configured_attr, is_configured)

        # Prime inputs from current HA state
        self._prime_from_states()

        # Do initial calculation
        self._recalc()

    def _prime_from_states(self) -> None:
        """Read current HA state for each configured source into _inputs."""
        merged_config = {**self.entry.data, **self.entry.options}

        for conf_key, value_attr, ts_attr, configured_attr, scale_conf_key in SOURCE_MAPPING:
            if not getattr(self._inputs, configured_attr):
                continue  # Source not configured

            entity_id = merged_config.get(conf_key)
            if not entity_id:
                continue

            st = self.hass.states.get(entity_id)
            if st is None or st.state in (STATE_UNKNOWN, STATE_UNAVAILABLE):
                continue

            try:
                val = float(st.state)
            except ValueError:
                continue

            # Apply scale for voltage sources
            if scale_conf_key:
                scale = merged_config.get(scale_conf_key, DEFAULT_VOLTAGE_SCALE)
                val *= scale

            setattr(self._inputs, value_attr, val)
            setattr(self._inputs, ts_attr, st.last_updated.timestamp())

    def _recalc(self) -> None:
        """Run core tick calculation and update coordinator data."""
        now = time.time()
        result = tick(self.params, self.state, self._inputs, now)
        self.async_set_updated_data(result.outputs)
        self._save_debounce.async_schedule_call()

    def async_start_listeners(self) -> None:
        """Start listening to source entity state changes and fallback timer."""
        # Compute merged config once for use in callbacks
        self._merged = {**self.entry.data, **self.entry.options}

        # Build entity_map: entity_id -> (value_attr, ts_attr, scale_conf_key)
        self._entity_map = {}
        for conf_key, value_attr, ts_attr, configured_attr, scale_conf_key in SOURCE_MAPPING:
            entity_id = self._merged.get(conf_key)
            if not entity_id:
                continue  # Source not configured
            self._entity_map[entity_id] = (value_attr, ts_attr, scale_conf_key)

        # Register state change listener
        if self._entity_map:
            self._unsub.append(
                async_track_state_change_event(
                    self.hass, list(self._entity_map), self._on_source_change
                )
            )

        # Register fallback timer
        self._fallback_interval = self._merged.get(CONF_FALLBACK_INTERVAL_S, DEFAULT_FALLBACK_INTERVAL_S)
        self._unsub.append(
            async_track_time_interval(
                self.hass, self._on_tick, timedelta(seconds=self._fallback_interval)
            )
        )

    @callback
    def _on_source_change(self, event) -> None:
        """Handle state change event from source entity."""
        entity_id = event.data["entity_id"]
        new = event.data.get("new_state")

        # Skip unavailable/unknown states
        if new is None or new.state in (STATE_UNKNOWN, STATE_UNAVAILABLE):
            return

        # Get the mapping for this entity
        value_attr, ts_attr, scale_conf_key = self._entity_map[entity_id]

        # Parse value
        try:
            val = float(new.state)
        except (ValueError, TypeError):
            _LOGGER.debug("non-numeric state %s for %s", new.state, entity_id)
            return

        # Apply scale if needed
        if scale_conf_key:
            scale = self._merged.get(scale_conf_key, DEFAULT_VOLTAGE_SCALE)
            val *= scale

        # Update inputs and recalculate
        setattr(self._inputs, value_attr, val)
        setattr(self._inputs, ts_attr, new.last_updated.timestamp())
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
