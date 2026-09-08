"""Service-Adapter: baut die Home-Assistant-MQTT-Discovery-Payloads aus der
deklarativen Entity-Spec des Cores (battery_soc_core.entities).

Ziel ist bit-genaue Paritaet mit dem, was battery_soc_mqtt.py::entities() /
make_config() / publish_discovery() bisher publiziert haben - siehe
services/battery_soc/tests/golden/*.discovery.json (vor der Extraktion aus dem
laufenden Dienst aufgenommen).
"""
from __future__ import annotations

from energy_node_common import discovery
from energy_node_common.discovery import (
    publish_availability_discovery as common_publish_availability_discovery,
    publish_discovery as common_publish_discovery,
)

from battery_soc_core.entities import ALL_OBJECT_IDS, EntityDesc, entity_specs
from battery_soc_core.state import build_units

# Kommando-Topic-Ende einer manual-SoC-number unter config.base_topic. In
# Reihenschaltung haengt zusaetzlich /bank_a bzw. /bank_b daran.
MANUAL_SOC_COMMAND_SUFFIX = "cmd/manual_soc"

_UNIT_LABELS = {"pack": "", "bank_a": " Bank A", "bank_b": " Bank B"}

_MANUAL_SOC_BANK_SUFFIX = {
    "manual_soc_bank_a": "/bank_a",
    "manual_soc_bank_b": "/bank_b",
    "manual_soc": "",
}


def _open_suggestions_discovery(config, unit_name):
    """Baut die Discovery-Config fuer einen open_suggestions_<unit>-Diagnose-Sensor."""
    base_topic = config.base_topic
    return {
        "name": f"Offene Einstellungsvorschlaege{_UNIT_LABELS.get(unit_name, '')}",
        "unique_id": f"{config.id}_open_suggestions_{unit_name}",
        "device": device_block(config),
        "availability_topic": f"{base_topic}/status/online",
        "payload_available": "1",
        "payload_not_available": "0",
        "state_topic": f"{base_topic}/tuning",
        "value_template": "{{ value_json.units." + unit_name + ".suggestions | default([]) | length }}",
        "state_class": "measurement",
        "entity_category": "diagnostic",
        "icon": "mdi:tune",
        "json_attributes_topic": f"{base_topic}/tuning",
        "json_attributes_template": (
            "{{ {'suggestions': value_json.units." + unit_name
            + ".suggestions | default([]), 'findings': value_json.units." + unit_name
            + ".findings | default([])} | tojson }}"
        ),
    }


def device_block(config) -> dict:
    """HA-device-Block, wortgleich uebernommen aus
    battery_soc_mqtt.py::device_block() (nimmt jetzt config statt runtime)."""
    return {
        "identifiers": [config.id],
        "name": config.name,
        "manufacturer": "DIY",
        "model": "LiFePO4 Dual-Bank Coulomb-Counter",
        "via_device": config.via_device,
    }


def desc_to_discovery(config, desc: EntityDesc, state_topic: str):
    """Uebersetzt einen EntityDesc in (component, object_id, payload). Der
    payload entspricht dem, was make_config()/entities() bisher gebaut haben."""
    base_topic = config.base_topic
    effective_state_topic = (
        f"{base_topic}/{desc.state_topic_suffix}"
        if desc.state_topic_suffix else state_topic
    )
    payload = {
        "name": desc.name,
        "unique_id": f"{config.id}_{desc.object_id}",
        "device": device_block(config),
        "availability_topic": f"{base_topic}/status/online",
        "payload_available": "1",
        "payload_not_available": "0",
        "state_topic": effective_state_topic,
    }

    if desc.component == "binary_sensor":
        payload["value_template"] = (
            "{{ 'ON' if value_json." + desc.value_key + " else 'OFF' }}"
        )
    elif desc.component == "number":
        payload["value_template"] = f"{{{{ value_json.{desc.value_key} }}}}"
        suffix = _MANUAL_SOC_BANK_SUFFIX.get(desc.object_id, "")
        payload["command_topic"] = (
            f"{base_topic}/{MANUAL_SOC_COMMAND_SUFFIX}{suffix}"
        )
        payload["min"] = desc.number_min
        payload["max"] = desc.number_max
        payload["step"] = desc.number_step
        payload["mode"] = "box"
    else:  # sensor
        payload["value_template"] = f"{{{{ value_json.{desc.value_key} }}}}"

    # Optionale Keys nur, wenn im EntityDesc gesetzt (nicht None) - ein
    # ausgelassener Key ist die Semantik von "nicht konfiguriert".
    if desc.unit is not None:
        payload["unit_of_measurement"] = desc.unit
    if desc.device_class is not None:
        payload["device_class"] = desc.device_class
    if desc.state_class is not None:
        payload["state_class"] = desc.state_class
    if desc.entity_category is not None:
        payload["entity_category"] = desc.entity_category
    if desc.icon is not None:
        payload["icon"] = desc.icon
    if desc.enabled_by_default is False:
        payload["enabled_by_default"] = False

    return desc.component, desc.object_id, payload


def publish_discovery(client, config, state) -> None:
    """Publiziert die Discovery-Config jeder aktiven Entity, loescht per leerem
    retained Payload jede (component, object_id)-Kombination aus ALL_OBJECT_IDS,
    die zur aktuellen Topologie nicht gehoert, und haengt die Availability-
    Entity an. `state` wird nicht gebraucht, bleibt fuer Signatur-Paritaet mit
    den uebrigen publish_*-Funktionen erhalten."""
    state_topic = f"{config.base_topic}/state"
    active = set()
    for desc in entity_specs(config.soc_params()):
        component, object_id, payload = desc_to_discovery(config, desc, state_topic)
        common_publish_discovery(client, config.id, component, object_id, payload)
        active.add((component, object_id))

    # Publiziere Diagnose-Sensoren fuer Kalibrier-Vorschlaege je Einheit
    units = build_units(config.soc_params())
    for unit in units:
        payload = _open_suggestions_discovery(config, unit.name)
        common_publish_discovery(client, config.id, "sensor", f"open_suggestions_{unit.name}", payload)
        active.add(("sensor", f"open_suggestions_{unit.name}"))

    for component, object_ids in ALL_OBJECT_IDS.items():
        for object_id in object_ids:
            if (component, object_id) not in active:
                client.publish(
                    discovery.discovery_topic(component, config.id, object_id),
                    payload="", qos=0, retain=True,
                )
    common_publish_availability_discovery(
        client, config.id, config.base_topic, device_block(config)
    )


def publish_simulation_discovery(client, config, slave) -> None:
    """Wortgleicher Wrapper um slave.publish_simulation_discovery() (frueher
    battery_soc_mqtt.py::publish_simulation_discovery). Das Brief schweigt zur
    Herkunft von `slave`; statt einen Modul-Global aus battery_soc_mqtt zu
    importieren (Zyklus) wird der Slave explizit uebergeben - Task 15 reicht
    ihn beim Verdrahten durch."""
    slave.publish_simulation_discovery(
        client,
        config.id,
        device_block(config),
        config.base_topic,
    )
