"""Master-Seite des Master/Slave-Polling-Protokolls (siehe knowhow/plan.md).

Der Energy-Node-Knoten instanziiert genau ein `Master`-Objekt, registriert
die bekannten Slave-Services und laesst dieses Objekt zentrale HA-Number-
Entities veroeffentlichen, Sollwert-Befehle an die Slaves weiterleiten und
bestaetigte Ist-Werte in die zentrale Anzeige spiegeln. Ein nicht
erreichbarer oder ablehnender Slave wird nicht stillschweigend als
erfolgreich konfiguriert angezeigt - die zentrale Anzeige folgt
ausschliesslich dem vom Slave bestaetigten Status.
"""

from __future__ import annotations

import json
from dataclasses import dataclass
from typing import Dict

import paho.mqtt.client as mqtt

from . import mqtt as mqtt_helpers
from .discovery import entity_config, number_entity_config, publish_discovery
from .settings import (
    DIAGNOSTIC_MULTIPLIER_SETTING,
    MAX_DIAGNOSTIC_MULTIPLIER,
    MAX_POLL_INTERVAL_S,
    MIN_DIAGNOSTIC_MULTIPLIER,
    MIN_POLL_INTERVAL_S,
    POLL_INTERVAL_SETTING,
    SIMULATION_SETTING,
    SETTINGS,
    SlaveStatus,
    settings_set_topic,
    settings_status_topic,
    master_settings_state_topic,
)


@dataclass
class SlaveDescriptor:
    device_id: str
    name: str
    default_poll_interval_s: float


class Master:
    """Verwaltet Sollwerte fuer eine Menge bekannter Slave-Services und
    veroeffentlicht dafuer zentrale HA-Number-Entities am Node-Geraet.

    Der Diagnose-Multiplikator ist fuer alle Slaves gemeinsam; Aenderungen
    an der zentralen Entity werden an jeden Slave einzeln weitergeleitet,
    jeder Slave behandelt den Wert aber weiterhin lokal unabhaengig.
    Das Poll-Intervall bleibt pro Slave individuell einstellbar."""

    def __init__(
        self,
        master_device_id: str,
        device_block: dict,
        default_diagnostic_multiplier: float = 10.0,
    ):
        self.master_device_id = master_device_id
        self.master_base_topic = f"outstation/{master_device_id}"
        self.device_block = device_block
        self._slaves: Dict[str, SlaveDescriptor] = {}
        self.default_diagnostic_multiplier = default_diagnostic_multiplier
        self.last_known_status: Dict[str, SlaveStatus] = {}

    def register_slave(
        self,
        device_id: str,
        name: str,
        default_poll_interval_s: float,
    ) -> None:
        self._slaves[device_id] = SlaveDescriptor(
            device_id,
            name,
            default_poll_interval_s,
        )

    def _central_command_topic(self, device_id: str, setting: str) -> str:
        return f"{self.master_base_topic}/settings/{device_id}/{setting}/set"

    def _central_state_topic(self, device_id: str, setting: str) -> str:
        return f"{self.master_base_topic}/settings/{device_id}/{setting}"

    def _central_status_topic(self, device_id: str) -> str:
        return f"{self.master_base_topic}/settings/{device_id}/status"

    def _global_command_topic(self, setting: str) -> str:
        return f"{self.master_base_topic}/settings/{setting}/set"

    def _global_state_topic(self, setting: str) -> str:
        return f"{self.master_base_topic}/settings/{setting}"

    def publish_discovery(self, client: mqtt.Client) -> None:
        """Veroeffentlicht je registriertem Slave eine zentrale Number-Entity
        fuer das individuelle Poll-Intervall sowie gemeinsame Entities fuer
        den Diagnose-Multiplikator und den Simulationsmodus am Node-Geraet."""
        for device_id, descriptor in self._slaves.items():
            publish_discovery(
                client,
                self.master_device_id,
                "number",
                f"{device_id}_{POLL_INTERVAL_SETTING}",
                number_entity_config(
                    self.master_device_id,
                    self.master_base_topic,
                    f"{device_id}_{POLL_INTERVAL_SETTING}",
                    f"{descriptor.name} Abfrageintervall",
                    self.device_block,
                    state_topic=self._central_state_topic(device_id, POLL_INTERVAL_SETTING),
                    command_topic=self._central_command_topic(device_id, POLL_INTERVAL_SETTING),
                    min_value=MIN_POLL_INTERVAL_S,
                    max_value=MAX_POLL_INTERVAL_S,
                    unit_of_measurement="s",
                ),
            )
        publish_discovery(
            client,
            self.master_device_id,
            "number",
            DIAGNOSTIC_MULTIPLIER_SETTING,
            number_entity_config(
                self.master_device_id,
                self.master_base_topic,
                DIAGNOSTIC_MULTIPLIER_SETTING,
                "Diagnose-Multiplikator",
                self.device_block,
                state_topic=self._global_state_topic(DIAGNOSTIC_MULTIPLIER_SETTING),
                command_topic=self._global_command_topic(DIAGNOSTIC_MULTIPLIER_SETTING),
                min_value=MIN_DIAGNOSTIC_MULTIPLIER,
                max_value=MAX_DIAGNOSTIC_MULTIPLIER,
            ),
        )
        publish_discovery(
            client,
            self.master_device_id,
            "switch",
            SIMULATION_SETTING,
            entity_config(
                self.master_device_id,
                self.master_base_topic,
                SIMULATION_SETTING,
                "Simulationsmodus",
                self.device_block,
                state_topic=self._global_state_topic(SIMULATION_SETTING),
                command_topic=self._global_command_topic(SIMULATION_SETTING),
                payload_on="1",
                payload_off="0",
                entity_category="config",
            ),
        )

    def bootstrap_defaults(self, client: mqtt.Client) -> None:
        """Sendet die konfigurierten Startwerte als retained Commands an
        jeden Slave. ENV-Werte sind Bootstrap-Fallbacks - ein bereits
        retained Slave-Ack ueberlebt Neustarts und wird hierdurch nicht
        veraendert, da der Slave denselben Wert erneut bestaetigt."""
        for device_id, descriptor in self._slaves.items():
            mqtt_helpers.publish(
                client,
                settings_set_topic(device_id, POLL_INTERVAL_SETTING),
                descriptor.default_poll_interval_s,
            )
        mqtt_helpers.publish(
            client,
            self._global_state_topic(DIAGNOSTIC_MULTIPLIER_SETTING),
            self.default_diagnostic_multiplier,
        )
        for device_id in self._slaves:
            mqtt_helpers.publish(
                client,
                settings_set_topic(device_id, DIAGNOSTIC_MULTIPLIER_SETTING),
                self.default_diagnostic_multiplier,
            )

    def subscribe(self, client: mqtt.Client) -> None:
        """In `on_connect` des Node-Services aufrufen."""
        for device_id in self._slaves:
            client.subscribe(settings_status_topic(device_id))
        client.subscribe(f"{self.master_base_topic}/settings/+/+/set")
        client.subscribe(self._global_command_topic(SIMULATION_SETTING))
        client.subscribe(self._global_command_topic(DIAGNOSTIC_MULTIPLIER_SETTING))

    def handle_message(self, client: mqtt.Client, topic: str, payload: str) -> bool:
        """Im `on_message` des Node-Services aufrufen. Gibt True zurueck,
        wenn die Nachricht zum Master/Slave-Protokoll gehoerte."""
        for device_id in self._slaves:
            if topic == settings_status_topic(device_id):
                self._mirror_status(client, device_id, payload)
                return True

        if topic == self._global_command_topic(SIMULATION_SETTING):
            mqtt_helpers.publish(
                client,
                master_settings_state_topic(self.master_device_id, SIMULATION_SETTING),
                payload,
            )
            return True

        if topic == self._global_command_topic(DIAGNOSTIC_MULTIPLIER_SETTING):
            # Ein gemeinsamer Diagnose-Multiplikator fuer alle Slaves:
            # den Sollwert global bekanntgeben und an jeden Slave einzeln
            # weiterleiten. Jeder Slave behandelt den Wert lokal unabhaengig.
            mqtt_helpers.publish(
                client, self._global_state_topic(DIAGNOSTIC_MULTIPLIER_SETTING), payload
            )
            for device_id in self._slaves:
                mqtt_helpers.publish(
                    client, settings_set_topic(device_id, DIAGNOSTIC_MULTIPLIER_SETTING), payload
                )
            return True

        prefix = f"{self.master_base_topic}/settings/"
        if topic.startswith(prefix) and topic.endswith("/set"):
            remainder = topic[len(prefix):-len("/set")]
            parts = remainder.split("/", 1)
            if len(parts) == 2:
                device_id, setting = parts
                if device_id in self._slaves and setting in SETTINGS:
                    # Weiterleitung: zentraler Befehl -> retained Command
                    # direkt an den zustaendigen Slave.
                    mqtt_helpers.publish(client, settings_set_topic(device_id, setting), payload)
                    return True
        return False

    def _mirror_status(self, client: mqtt.Client, device_id: str, payload: str) -> None:
        try:
            data = json.loads(payload)
            status = SlaveStatus.from_dict(data)
        except (TypeError, ValueError, json.JSONDecodeError):
            return
        self.last_known_status[device_id] = status
        mqtt_helpers.publish(
            client, self._central_state_topic(device_id, POLL_INTERVAL_SETTING), status.poll_interval_s
        )
        mqtt_helpers.publish(
            client,
            self._central_state_topic(device_id, DIAGNOSTIC_MULTIPLIER_SETTING),
            status.diagnostic_poll_multiplier,
        )
        mqtt_helpers.publish_json(client, self._central_status_topic(device_id), status.to_dict())
