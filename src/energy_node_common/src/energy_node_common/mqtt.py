"""Niedrige MQTT-Hilfsfunktionen, die von allen Energy-Node-Bridges auf
dieselbe Weise verwendet werden (Client-Aufbau, Publish-Helfer, gemeinsames
Availability-/`last_update`-Muster)."""

from __future__ import annotations

import json
import time
from typing import Optional

import paho.mqtt.client as mqtt


def build_client(
    client_id: str,
    host: str,
    port: int,
    user: str = "",
    password: str = "",
    will_topic: Optional[str] = None,
    will_payload: str = "0",
) -> mqtt.Client:
    """Baut einen paho-mqtt-Client mit der v2-Callback-API auf (identisch
    zum bisherigen Muster in allen Skripten auf diesem Knoten)."""
    client = mqtt.Client(mqtt.CallbackAPIVersion.VERSION2, client_id=client_id)
    if user:
        client.username_pw_set(user, password)
    client.reconnect_delay_set(min_delay=5, max_delay=60)
    if will_topic:
        client.will_set(will_topic, payload=will_payload, qos=0, retain=True)
    client.connect(host, port, keepalive=60)
    return client


def publish(client: mqtt.Client, topic: str, value, retain: bool = True, qos: int = 0) -> None:
    client.publish(topic, payload=str(value), qos=qos, retain=retain)


def publish_json(client: mqtt.Client, topic: str, payload: dict, retain: bool = True, qos: int = 0) -> None:
    client.publish(topic, payload=json.dumps(payload), qos=qos, retain=retain)


def publish_online_status(
    client: mqtt.Client,
    base_topic: str,
    online: bool,
    reason: Optional[str] = None,
    online_payload: str = "1",
    offline_payload: str = "0",
) -> None:
    """Gemeinsames Availability-Muster: Erreichbarkeit, Grund als Attribut
    und `last_update`, sofern online - bisher in jedem Skript einzeln
    implementiert."""
    publish(client, f"{base_topic}/status/online", online_payload if online else offline_payload)
    publish_json(client, f"{base_topic}/status/online/attributes", {"reason": reason or ""})
    if online:
        # Retain legacy last_update topic for backwards compatibility.
        publish(client, f"{base_topic}/status/last_update", int(time.time()))
