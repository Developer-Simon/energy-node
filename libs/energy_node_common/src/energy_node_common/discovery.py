"""Generische Home-Assistant-MQTT-Discovery-Helfer, unabhaengig von der
konkreten Geraete-Entitaet."""

from __future__ import annotations

import json
from typing import Optional

import paho.mqtt.client as mqtt

HA_DISCOVERY_PREFIX = "homeassistant"


def discovery_topic(component: str, device_id: str, object_id: str) -> str:
    return f"{HA_DISCOVERY_PREFIX}/{component}/{device_id}/{object_id}/config"


def publish_discovery(client: mqtt.Client, device_id: str, component: str, object_id: str, config: dict) -> None:
    client.publish(discovery_topic(component, device_id, object_id), payload=json.dumps(config), qos=0, retain=True)


def entity_config(
    device_id: str,
    base_topic: str,
    object_id: str,
    name: str,
    device_block: dict,
    state_topic: Optional[str] = None,
    use_availability_topic: bool = True,
    **kwargs,
) -> dict:
    """Baut eine HA-Discovery-Konfiguration nach dem gemeinsamen Muster auf
    (unique_id, availability_topic, device-Block)."""
    config = {
        "name": name,
        "unique_id": f"{device_id}_{object_id}",
        "device": device_block,
    }
    if state_topic:
        config["state_topic"] = state_topic
    if use_availability_topic:
        config["availability_topic"] = f"{base_topic}/status/online"
        config["payload_available"] = "1"
        config["payload_not_available"] = "0"
    config.update(kwargs)
    return config


def availability_entity_config(
    device_id: str,
    base_topic: str,
    device_block: dict,
    object_id: str = "online",
    name: str = "Erreichbarkeit",
    state_topic: Optional[str] = None,
    attributes_topic: Optional[str] = None,
) -> dict:
    """Baut die HA-Discovery-Konfiguration fuer die Erreichbarkeit
    (Binary Sensor, Diagnostics-Kategorie) auf."""
    if state_topic is None:
        state_topic = f"{base_topic}/status/online"
    config = entity_config(
        device_id,
        base_topic,
        object_id,
        name,
        device_block,
        state_topic=state_topic,
        use_availability_topic=False,
        device_class="connectivity",
        payload_on="1",
        payload_off="0",
        entity_category="diagnostic",
    )
    if attributes_topic is None:
        attributes_topic = f"{base_topic}/status/online/attributes"
    config["json_attributes_topic"] = attributes_topic
    return config


def publish_availability_discovery(
    client: mqtt.Client,
    device_id: str,
    base_topic: str,
    device_block: dict,
) -> None:
    """Veroeffentlicht die Erreichbarkeits-Entity fuer ein Geraet."""
    publish_discovery(
        client,
        device_id,
        "binary_sensor",
        "online",
        availability_entity_config(device_id, base_topic, device_block),
    )
