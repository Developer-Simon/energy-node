"""Slave-Seite des Master/Slave-Polling-Protokolls (siehe knowhow/plan.md).

Jeder Geraete-Service instanziiert genau ein `Slave`-Objekt und liefert nur
`poll_core()` (und optional `poll_diagnostics()`/`on_setting_changed()`).
Diese Klasse uebernimmt das Settings-Protokoll, den Scheduler, Ack-/Status-
Publishing und `last_update` - keine geraetespezifische Fachlogik.
"""

from __future__ import annotations

import time
from typing import Any, Callable, Optional

import paho.mqtt.client as mqtt

from . import mqtt as mqtt_helpers
from .discovery import entity_config, number_entity_config
from .discovery import publish_discovery as _publish_discovery
from .mqtt import publish as _publish
from .scheduler import AsyncScheduler, Scheduler
from .settings import (
    DIAGNOSTIC_MULTIPLIER_SETTING,
    MAX_DIAGNOSTIC_MULTIPLIER,
    MAX_POLL_INTERVAL_S,
    MIN_DIAGNOSTIC_MULTIPLIER,
    MIN_POLL_INTERVAL_S,
    POLL_INTERVAL_SETTING,
    SIMULATION_SETTING,
    SlaveStatus,
    settings_set_topic,
    settings_state_topic,
    settings_status_topic,
    master_settings_set_topic,
    config_reload_topic,
    validate_diagnostic_multiplier,
    validate_poll_interval_s,
    parse_bool,
)


class Slave:
    """Gemeinsame Slave-Logik fuer einen Geraete-Service."""

    def __init__(
        self,
        device_id: str,
        poll_core: Callable,
        poll_diagnostics: Optional[Callable] = None,
        default_poll_interval_s: float = 60,
        default_diagnostic_multiplier: float = 10,
        master_device_id: Optional[str] = None,
        on_setting_changed: Optional[Callable[[str, Any], None]] = None,
        on_simulation_changed: Optional[Callable[[str, bool], None]] = None,
        on_config_reload: Optional[Callable[[], None]] = None,
        on_poll_error: Optional[Callable[[Exception], None]] = None,
        async_loop=None,
    ):
        self.device_id = device_id
        self.base_topic = f"outstation/{device_id}"

        self.poll_interval_s = default_poll_interval_s
        self.diagnostic_poll_multiplier = default_diagnostic_multiplier
        # Merkt sich, ob ein Wert zur Laufzeit ueber das Settings-Topic
        # gesetzt wurde. apply_config_defaults laesst solche Werte in Ruhe.
        self._poll_interval_overridden = False
        self._diagnostic_multiplier_overridden = False
        self._simulation_active: dict[str, bool] = {}
        self._master_simulation_set_topic = (
            master_settings_set_topic(master_device_id, SIMULATION_SETTING)
            if master_device_id
            else None
        )
        self._on_setting_changed = on_setting_changed
        self._on_simulation_changed = on_simulation_changed
        self._on_config_reload = on_config_reload
        self._last_update_ts: Optional[int] = None
        self._async_loop = async_loop
        self._scheduler_started = False

        self._poll_interval_set_topic = settings_set_topic(device_id, POLL_INTERVAL_SETTING)
        self._poll_interval_state_topic = settings_state_topic(device_id, POLL_INTERVAL_SETTING)
        self._diag_multiplier_set_topic = settings_set_topic(device_id, DIAGNOSTIC_MULTIPLIER_SETTING)
        self._diag_multiplier_state_topic = settings_state_topic(device_id, DIAGNOSTIC_MULTIPLIER_SETTING)
        self._status_topic = settings_status_topic(device_id)
        self._config_reload_topic = config_reload_topic(device_id)

        if async_loop is not None:
            self._scheduler = AsyncScheduler(
                poll_core,
                lambda: self.poll_interval_s,
                lambda: self.diagnostic_poll_multiplier,
                poll_diagnostics,
                on_error=on_poll_error,
            )
        else:
            self._scheduler = Scheduler(
                poll_core,
                lambda: self.poll_interval_s,
                lambda: self.diagnostic_poll_multiplier,
                poll_diagnostics,
                on_error=on_poll_error,
                name=f"{device_id}-slave-scheduler",
            )

    @property
    def settings_topics(self) -> tuple:
        """Topics, die der Service in seinem `on_connect` abonnieren muss."""
        topics = [self._poll_interval_set_topic, self._diag_multiplier_set_topic, self._config_reload_topic]
        if self._master_simulation_set_topic:
            topics.append(self._master_simulation_set_topic)
        topics.extend(
            topic
            for device_id in self._simulation_active
            for topic in (
                settings_set_topic(device_id, SIMULATION_SETTING),
                settings_state_topic(device_id, SIMULATION_SETTING),
            )
        )
        return tuple(topics)

    def register_devices(
        self, device_ids: list[str] | tuple[str, ...], client: Optional[mqtt.Client] = None
    ) -> None:
        """Registriert die physischen Geraete fuer geräteweise Simulation.

        Bestehende Zustände bleiben bei einem Config-Reload erhalten; neue
        Geräte starten aus Sicherheitsgruenden mit deaktivierter Simulation.
        """
        active_ids = {str(device_id) for device_id in device_ids}
        new_ids = active_ids - self._simulation_active.keys()
        self._simulation_active = {
            device_id: self._simulation_active.get(device_id, False)
            for device_id in active_ids
        }
        if client:
            for device_id in new_ids:
                client.subscribe(self._simulation_set_topic(device_id))
                client.subscribe(self._simulation_state_topic(device_id))

    def simulation_active_for(self, device_id: str) -> bool:
        return self._simulation_active.get(device_id, False)

    @property
    def simulation_active(self) -> bool:
        """Kompatibilitaetswert: true, wenn alle registrierten Geräte simulieren."""
        return bool(self._simulation_active) and all(self._simulation_active.values())

    def _simulation_state_topic(self, device_id: str) -> str:
        return settings_state_topic(device_id, SIMULATION_SETTING)

    def _simulation_set_topic(self, device_id: str) -> str:
        return settings_set_topic(device_id, SIMULATION_SETTING)

    def start(self, client: mqtt.Client) -> None:
        """In `on_connect` aufrufen: abonniert die Settings-Topics,
        bestaetigt die aktuellen Werte retained und startet den Scheduler
        (nur beim allerersten Aufruf; erneutes `start()` nach Reconnect
        startet den Scheduler nicht doppelt)."""
        client.subscribe(self._poll_interval_set_topic)
        client.subscribe(self._diag_multiplier_set_topic)
        client.subscribe(self._config_reload_topic)
        if self._master_simulation_set_topic:
            client.subscribe(self._master_simulation_set_topic)
        for device_id in self._simulation_active:
            client.subscribe(self._simulation_set_topic(device_id))
            client.subscribe(self._simulation_state_topic(device_id))
        self._publish_ack(client, POLL_INTERVAL_SETTING, self.poll_interval_s)
        self._publish_ack(client, DIAGNOSTIC_MULTIPLIER_SETTING, self.diagnostic_poll_multiplier)
        for device_id, active in self._simulation_active.items():
            self._publish_simulation_ack(client, device_id, active)
        self._publish_status(client)

        if not self._scheduler_started:
            if isinstance(self._scheduler, AsyncScheduler):
                self._scheduler.start(self._async_loop)
            else:
                self._scheduler.start()
            self._scheduler_started = True

    def handle_message(self, client: mqtt.Client, topic: str, payload: str) -> bool:
        """Im `on_message` des Services aufrufen. Gibt True zurueck, wenn
        die Nachricht zum Settings-Protokoll gehoerte (und damit bereits
        verarbeitet wurde)."""
        if topic == self._poll_interval_set_topic:
            self._apply_setting(client, POLL_INTERVAL_SETTING, payload)
            return True
        if topic == self._diag_multiplier_set_topic:
            self._apply_setting(client, DIAGNOSTIC_MULTIPLIER_SETTING, payload)
            return True
        if topic == self._master_simulation_set_topic:
            self._apply_global_simulation(client, payload)
            return True
        for device_id in self._simulation_active:
            if topic == self._simulation_set_topic(device_id):
                self._apply_device_simulation(client, device_id, payload)
                return True
            if topic == self._simulation_state_topic(device_id):
                self._restore_device_simulation(device_id, payload)
                return True
        if topic == self._config_reload_topic:
            self._reload_config(client)
            return True
        return False

    def _apply_global_simulation(self, client: mqtt.Client, payload: str) -> None:
        simulation_value, error = parse_bool(payload)
        if error:
            self._publish_status(client, runtime_status="rejected", error=error)
            return
        for device_id in self._simulation_active:
            self._set_device_simulation(client, device_id, simulation_value)

    def _apply_device_simulation(self, client: mqtt.Client, device_id: str, payload: str) -> None:
        simulation_value, error = parse_bool(payload)
        if error:
            self._publish_status(client, runtime_status="rejected", error=error)
            return
        self._set_device_simulation(client, device_id, simulation_value)

    def _restore_device_simulation(self, device_id: str, payload: str) -> None:
        simulation_value, error = parse_bool(payload)
        if error:
            return
        self._simulation_active[device_id] = simulation_value
        if self._on_simulation_changed:
            self._on_simulation_changed(device_id, simulation_value)

    def _set_device_simulation(self, client: mqtt.Client, device_id: str, active: bool) -> None:
        self._simulation_active[device_id] = active
        self._publish_simulation_ack(client, device_id, active)
        if self._on_simulation_changed:
            self._on_simulation_changed(device_id, active)
        self._publish_status(client)

    def _reload_config(self, client: mqtt.Client) -> None:
        if self._on_config_reload is None:
            self._publish_status(client, runtime_status="rejected", error="config_reload_not_supported")
            return
        try:
            self._on_config_reload()
        except Exception as exc:
            self._publish_status(client, runtime_status="rejected", error=str(exc))
            return
        self._publish_status(client)

    def apply_config_defaults(
        self,
        poll_interval_s: Optional[float] = None,
        diagnostic_multiplier: Optional[float] = None,
    ) -> None:
        """Neue Startwerte aus config.json uebernehmen.

        Ein zur Laufzeit ueber outstation/<id>/settings/<name>/set
        gesetzter Wert bleibt unangetastet: die Konfigurationsdatei liefert
        den Standard, nicht den Sollwert.
        """
        if poll_interval_s is not None and not self._poll_interval_overridden:
            self.poll_interval_s = poll_interval_s
        if diagnostic_multiplier is not None and not self._diagnostic_multiplier_overridden:
            self.diagnostic_poll_multiplier = diagnostic_multiplier

    def _apply_setting(self, client: mqtt.Client, setting: str, payload: str) -> None:
        if setting == SIMULATION_SETTING:
            self._apply_global_simulation(client, payload)
            return

        try:
            value = float(payload)
        except (TypeError, ValueError):
            return

        error = (
            validate_poll_interval_s(value)
            if setting == POLL_INTERVAL_SETTING
            else validate_diagnostic_multiplier(value)
        )
        if error:
            self._publish_status(client, runtime_status="rejected", error=error)
            return

        if setting == POLL_INTERVAL_SETTING:
            self.poll_interval_s = value
            self._poll_interval_overridden = True
        else:
            self.diagnostic_poll_multiplier = value
            self._diagnostic_multiplier_overridden = True

        self._publish_ack(client, setting, value)
        self._publish_status(client)
        if self._on_setting_changed:
            self._on_setting_changed(setting, value)

    def _publish_ack(self, client: mqtt.Client, setting: str, value: float | int) -> None:
        if setting == POLL_INTERVAL_SETTING:
            topic = self._poll_interval_state_topic
        elif setting == DIAGNOSTIC_MULTIPLIER_SETTING:
            topic = self._diag_multiplier_state_topic
        else:
            return
        mqtt_helpers.publish(client, topic, value)

    def _publish_simulation_ack(self, client: mqtt.Client, device_id: str, active: bool) -> None:
        mqtt_helpers.publish(client, self._simulation_state_topic(device_id), int(active))

    def _publish_status(self, client: mqtt.Client, runtime_status: str = "ok", error: str = "") -> None:
        status = SlaveStatus(
            poll_interval_s=self.poll_interval_s,
            diagnostic_poll_multiplier=self.diagnostic_poll_multiplier,
            actual_poll_interval_s=self.poll_interval_s,
            simulation_active=self.simulation_active,
            last_update=self._last_update_ts,
            runtime_status=runtime_status,
            error=error,
        )
        mqtt_helpers.publish_json(client, self._status_topic, status.to_dict())

    def note_update(self, client: mqtt.Client, ts: Optional[int] = None) -> None:
        """Nach jeder erfolgreichen Kernabfrage aufrufen: aktualisiert
        `last_update` und spiegelt es in der Statusstruktur sowie als
        separaten Timestamp auf dem vom Discovery-Eintrag erwarteten
        Topic `.../status/last_update`."""
        self._last_update_ts = ts if ts is not None else int(time.time())
        self._publish_status(client)
        _publish(client, f"{self.base_topic}/status/last_update", self._last_update_ts)

    def publish_discovery(
        self,
        client: mqtt.Client,
        device_block: dict,
        local_entities_enabled_by_default: bool = False,
    ) -> None:
        """Veroeffentlicht lokale Rate-/Diagnose-Entities.

        Geraete-Services rufen diese Methode nicht mehr auf; damit entstehen
        dort keine kuenstlichen Service- oder `last_update`-Entities.
        """
        _publish_discovery(
            client, self.device_id, "number", POLL_INTERVAL_SETTING,
            number_entity_config(
                self.device_id, self.base_topic, POLL_INTERVAL_SETTING, "Abfrageintervall (lokal)",
                device_block,
                state_topic=self._poll_interval_state_topic,
                command_topic=self._poll_interval_set_topic,
                min_value=MIN_POLL_INTERVAL_S,
                max_value=MAX_POLL_INTERVAL_S,
                unit_of_measurement="s",
                enabled_by_default=local_entities_enabled_by_default,
            ),
        )
        _publish_discovery(
            client, self.device_id, "number", DIAGNOSTIC_MULTIPLIER_SETTING,
            number_entity_config(
                self.device_id, self.base_topic, DIAGNOSTIC_MULTIPLIER_SETTING, "Diagnose-Multiplikator (lokal)",
                device_block,
                state_topic=self._diag_multiplier_state_topic,
                command_topic=self._diag_multiplier_set_topic,
                min_value=MIN_DIAGNOSTIC_MULTIPLIER,
                max_value=MAX_DIAGNOSTIC_MULTIPLIER,
                enabled_by_default=local_entities_enabled_by_default,
            ),
        )

    def publish_simulation_discovery(
        self, client: mqtt.Client, device_id: str, device_block: dict, base_topic: str
    ) -> None:
        """Veröffentlicht den Simulationsschalter für ein physisches Gerät."""
        _publish_discovery(
            client,
            device_id,
            "switch",
            SIMULATION_SETTING,
            entity_config(
                device_id,
                base_topic,
                SIMULATION_SETTING,
                "Simulationsmodus",
                device_block,
                state_topic=self._simulation_state_topic(device_id),
                command_topic=self._simulation_set_topic(device_id),
                payload_on="1",
                payload_off="0",
                entity_category="config",
            ),
        )

    def stop(self) -> None:
        self._scheduler.stop()
