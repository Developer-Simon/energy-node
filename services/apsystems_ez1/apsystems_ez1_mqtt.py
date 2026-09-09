#!/usr/bin/env python3
"""
AP Systems EZ1 Microinverter <-> MQTT Bridge

Ein einziger Python-Service fuer ALLE konfigurierten EZ1-Wechselrichter.
Die Geraete werden aus einer JSON-Datei geladen (aehnlich wie bei den
Shellys); jeder Wechselrichter behaelt dabei sein eigenes MQTT-Topic-
Prefix, seine eigenen Home-Assistant-Entitaeten und seine eigene
Erreichbarkeitsanzeige.

Benoetigte Pakete:
    pip3 install apsystems-ez1 paho-mqtt --break-system-packages

Nutzt das gemeinsame energy_node_common-Paket (Slave-Seite des Settings-Protokolls) fuer MQTT-Aufbau, Availability, Discovery-Publishing und
den asyncio-Poll-Scheduler. Dieses
Skript liefert nur noch EZ1-Fachlogik (Kernabfrage, Zusatzdiagnose,
Power-Limit-/Status-Steuerung).
"""

from __future__ import annotations

import asyncio
import logging
import math
import sys
import time
from dataclasses import dataclass, field
from types import SimpleNamespace
from typing import Any, Optional

from APsystemsEZ1 import APsystemsEZ1M
import paho.mqtt.client as mqtt

from energy_node_common import Slave
from energy_node_common import appconfig
from energy_node_common import config as common_config
from energy_node_common import mqtt as common_mqtt
from energy_node_common.discovery import (
    publish_availability_discovery as common_publish_availability_discovery,
    publish_discovery as common_publish_discovery,
)

# ---------------------------------------------------------------------------
# Konstanten
# ---------------------------------------------------------------------------

HA_DISCOVERY_PREFIX = "homeassistant"

# Der EZ1 speichert das Power-Limit im Flash-Speicher. Zu haeufiges
# Schreiben kann laut Community-Berichten den Flash-Speicher abnutzen.
# Deshalb: Mindestabstand zwischen zwei Schreibvorgaengen erzwingen.
MIN_SECONDS_BETWEEN_POWER_WRITES = 300  # 5 Minuten

# Laut Dokumentation gueltiger Bereich fuer das Power-Limit.
MAX_POWER_MIN_W = 30
MAX_POWER_MAX_W = 800

# ---------------------------------------------------------------------------

log = logging.getLogger("apsystems_ez1_mqtt")


# ---------------------------------------------------------------------------
# Geraete-Konfiguration
# ---------------------------------------------------------------------------

@dataclass
class APsystemsDeviceConfig:
    id: str  # eindeutig, wird MQTT-Topic-Root und HA device_id
    name: str  # Anzeigename in Home Assistant
    host: str  # IP oder Hostname des EZ1
    port: int = 8050  # lokaler API-Port

    @property
    def base_topic(self) -> str:
        return f"outstation/{self.id}"


def load_devices(path: str) -> list[APsystemsDeviceConfig]:
    raw = common_config.load_json(path)

    if not isinstance(raw, list):
        raise ValueError(f"Geraete-Konfiguration {path} muss eine JSON-Liste sein")

    devices: list[APsystemsDeviceConfig] = []
    for index, item in enumerate(raw):
        if not isinstance(item, dict):
            raise ValueError(f"Eintrag {index} in {path} ist kein Objekt")
        required = {"id", "name", "host"}
        missing = required - set(item.keys())
        if missing:
            raise ValueError(
                f"Eintrag {index} in {path} fehlt Pflichtfelder: {sorted(missing)}"
            )
        devices.append(
            APsystemsDeviceConfig(
                id=str(item["id"]).strip(),
                name=str(item["name"]).strip(),
                host=str(item["host"]).strip(),
                port=int(item.get("port", 8050)),
            )
        )

    ids = [d.id for d in devices]
    if len(ids) != len(set(ids)):
        raise ValueError(f"Doppelte Geraete-IDs in {path}: {ids}")
    if not devices:
        raise ValueError(f"Keine Geraete in {path} konfiguriert")

    return devices


def device_block(device_id: str, name: str, node_device_id: str) -> dict[str, Any]:
    """Erstellt einen Home-Assistant-Device-Block fuer einen EZ1-Wechselrichter."""
    return {
        "identifiers": [device_id],
        "name": name,
        "manufacturer": "APsystems",
        "model": "EZ1",
        "via_device": node_device_id,
    }


# ---------------------------------------------------------------------------
# Pro-Geraet Laufzeitstatus
# ---------------------------------------------------------------------------

@dataclass
class APsystemsDevice:
    cfg: APsystemsDeviceConfig
    node_device_id: str
    inverter: APsystemsEZ1M = field(init=False)
    ha_device: dict[str, Any] = field(init=False)
    max_power_set_topic: str = field(init=False)
    power_status_set_topic: str = field(init=False)
    online_attributes_topic: str = field(init=False)
    _last_power_write_ts: float = field(default=0.0, init=False)
    _simulation_start_ts: float = field(default=0.0, init=False)
    _simulation_last_poll_ts: float = field(default=0.0, init=False)
    _simulation_max_power_limit_w: int = field(default=MAX_POWER_MAX_W, init=False)
    _simulation_power_active: bool = field(default=True, init=False)
    _simulation_energy_today_e1_kwh: float = field(default=0.0, init=False)
    _simulation_energy_today_e2_kwh: float = field(default=0.0, init=False)
    _simulation_lifetime_base_e1: float = field(default=540.0, init=False)
    _simulation_lifetime_base_e2: float = field(default=520.0, init=False)
    _initial_diagnostics_done: bool = field(default=False, init=False)

    def __post_init__(self) -> None:
        self.inverter = APsystemsEZ1M(self.cfg.host, self.cfg.port)
        self.ha_device = device_block(self.cfg.id, self.cfg.name, self.node_device_id)
        base = self.cfg.base_topic
        self.max_power_set_topic = f"{base}/set/max_power_limit_w"
        self.power_status_set_topic = f"{base}/set/power_status"
        self.online_attributes_topic = f"{base}/status/online/attributes"


# ---------------------------------------------------------------------------
# Publishing-Helfer
# ---------------------------------------------------------------------------

def _publish_discovery(
    client: mqtt.Client, device: APsystemsDevice, component: str, object_id: str, config: dict
) -> None:
    common_publish_discovery(client, device.cfg.id, component, object_id, config)
    log.debug(
        "Published Home Assistant discovery %s/%s/%s", component, device.cfg.id, object_id
    )


def _ha_entity_config(
    device: APsystemsDevice,
    component: str,
    object_id: str,
    name: str,
    state_topic: str,
    use_availability_topic: bool = True,
    **kwargs: Any,
) -> dict:
    config: dict[str, Any] = {
        "name": name,
        "unique_id": f"{device.cfg.id}_{object_id}",
        "state_topic": state_topic,
        "device": device.ha_device,
    }
    if use_availability_topic:
        config["availability_topic"] = f"{device.cfg.base_topic}/status/online"
        config["payload_available"] = "1"
        config["payload_not_available"] = "0"
    config.update(kwargs)
    if config.get("availability_topic") is None:
        config.pop("availability_topic", None)
        config.pop("payload_available", None)
        config.pop("payload_not_available", None)
    return config


def _publish(client: mqtt.Client, device: APsystemsDevice, subtopic: str, value: Any) -> None:
    common_mqtt.publish(client, f"{device.cfg.base_topic}/{subtopic}", value)
    log.debug("Published %s/%s = %s", device.cfg.base_topic, subtopic, value)


def _publish_online_status(
    client: mqtt.Client, device: APsystemsDevice, online: bool, reason: Optional[str] = None
) -> None:
    common_mqtt.publish_online_status(client, device.cfg.base_topic, online, reason)


def _publish_object_fields(
    client: mqtt.Client, device: APsystemsDevice, subtopic_prefix: str, obj: Any
) -> None:
    """
    Published alle Felder eines Antwort-Objekts der Bibliothek als einzelne
    MQTT-Werte, unabhaengig davon, ob es sich um ein Pydantic-Model, ein
    Dataclass-Objekt oder einen einfachen Wert (str/int/bool) handelt.
    """
    if hasattr(obj, "model_dump"):
        data = obj.model_dump()
    elif hasattr(obj, "dict") and callable(obj.dict):
        data = obj.dict()
    elif hasattr(obj, "__dict__"):
        data = vars(obj)
    else:
        data = {"value": obj}

    for key, value in data.items():
        _publish(client, device, f"{subtopic_prefix}/{key}", value)
        object_id = f"{subtopic_prefix.replace('/', '_')}_{key}"
        _publish_discovery(
            client,
            device,
            "sensor",
            object_id,
            _ha_entity_config(
                device,
                "sensor",
                object_id,
                f"{subtopic_prefix} {key}",
                f"{device.cfg.base_topic}/{subtopic_prefix}/{key}",
                entity_category="diagnostic",
                enabled_by_default=False,
            ),
        )


# ---------------------------------------------------------------------------
# Discovery
# ---------------------------------------------------------------------------

def publish_device_discovery(client: mqtt.Client, device: APsystemsDevice) -> None:
    base = device.cfg.base_topic

    sensors = {
        "pv1_power": ("PV1 Leistung", "pv1/power_w", "W", "power", "measurement"),
        "pv2_power": ("PV2 Leistung", "pv2/power_w", "W", "power", "measurement"),
        "total_power": ("Gesamtleistung", "total/power_w", "W", "power", "measurement"),
        "pv1_energy_today": ("PV1 Tagesertrag", "pv1/energy_today_kwh", "kWh", "energy", "measurement"),
        "pv2_energy_today": ("PV2 Tagesertrag", "pv2/energy_today_kwh", "kWh", "energy", "measurement"),
        "total_energy_today": ("Tagesertrag", "total/energy_today_kwh", "kWh", "energy", "measurement"),
        "pv1_energy_lifetime": ("PV1 Gesamtertrag", "pv1/energy_lifetime_kwh", "kWh", "energy", "total_increasing"),
        "pv2_energy_lifetime": ("PV2 Gesamtertrag", "pv2/energy_lifetime_kwh", "kWh", "energy", "total_increasing"),
        "total_energy_lifetime": ("Gesamtertrag", "total/energy_lifetime_kwh", "kWh", "energy", "total_increasing"),
    }
    for object_id, (name, subtopic, unit, device_class, state_class) in sensors.items():
        _publish_discovery(
            client,
            device,
            "sensor",
            object_id,
            _ha_entity_config(
                device,
                "sensor",
                object_id,
                name,
                f"{base}/{subtopic}",
                unit_of_measurement=unit,
                device_class=device_class,
                state_class=state_class,
            ),
        )

    _publish_discovery(
        client,
        device,
        "number",
        "max_power_limit",
        _ha_entity_config(
            device,
            "number",
            "max_power_limit",
            "Power-Limit",
            f"{base}/max_power_limit_w",
            command_topic=device.max_power_set_topic,
            unit_of_measurement="W",
            min=MAX_POWER_MIN_W,
            max=MAX_POWER_MAX_W,
            step=1,
            mode="box",
        ),
    )
    _publish_discovery(
        client,
        device,
        "switch",
        "power_status",
        _ha_entity_config(
            device,
            "switch",
            "power_status",
            "Betriebsstatus",
            f"{base}/power_status",
            command_topic=device.power_status_set_topic,
            payload_on="ON",
            payload_off="OFF",
            state_on="ON",
            state_off="OFF",
        ),
    )
    # Alte Sensor-Discovery fuer power_status entfernen, falls noch vorhanden.
    client.publish(
        f"{HA_DISCOVERY_PREFIX}/sensor/{device.cfg.id}/power_status/config",
        payload="",
        qos=0,
        retain=True,
    )
    common_publish_availability_discovery(
        client, device.cfg.id, device.cfg.base_topic, device.ha_device
    )


# ---------------------------------------------------------------------------
# Simulation
# ---------------------------------------------------------------------------

def _get_simulation_output_data(device: APsystemsDevice) -> SimpleNamespace:
    now = time.time()
    if device._simulation_start_ts == 0.0:
        device._simulation_start_ts = now

    elapsed = now - device._simulation_start_ts
    # Leichte Phasenverschiebung pro Geraet, damit die Werte nicht alle
    # exakt gleich aussehen, falls mehrere Inverter simuliert werden.
    offset = hash(device.cfg.id) % 100 / 100.0
    raw_p1 = max(0.0, 160 + 140 * math.sin((elapsed / 300) + offset))
    raw_p2 = max(0.0, 140 + 120 * math.sin((elapsed / 260) + 2 + offset))

    if not device._simulation_power_active:
        p1 = p2 = 0
    else:
        # Wie am echten Geraet begrenzt das Power-Limit die Gesamtleistung;
        # PV1/PV2 werden dabei proportional zueinander gedrosselt.
        total_raw = raw_p1 + raw_p2
        limit = device._simulation_max_power_limit_w
        if total_raw > limit > 0:
            scale = limit / total_raw
            raw_p1 *= scale
            raw_p2 *= scale
        p1 = int(raw_p1)
        p2 = int(raw_p2)

    # Energie wird als echtes Integral ueber die seit dem letzten Poll
    # vergangene Zeit aufsummiert, statt aus der Momentanleistung
    # hochgerechnet zu werden - sonst schwankt der Tagesertrag mit der
    # Momentanleistung statt monoton zu steigen.
    dt_hours = 0.0
    if device._simulation_last_poll_ts:
        dt_hours = max(0.0, now - device._simulation_last_poll_ts) / 3600.0
    device._simulation_last_poll_ts = now

    device._simulation_energy_today_e1_kwh += p1 * dt_hours / 1000.0
    device._simulation_energy_today_e2_kwh += p2 * dt_hours / 1000.0
    e1_today = round(device._simulation_energy_today_e1_kwh, 3)
    e2_today = round(device._simulation_energy_today_e2_kwh, 3)
    te1 = round(device._simulation_lifetime_base_e1 + e1_today, 3)
    te2 = round(device._simulation_lifetime_base_e2 + e2_today, 3)

    return SimpleNamespace(p1=p1, p2=p2, e1=e1_today, e2=e2_today, te1=te1, te2=te2)


# ---------------------------------------------------------------------------
# Poll- und Steuerlogik
# ---------------------------------------------------------------------------

async def poll_once(
    device: APsystemsDevice, mqtt_client: mqtt.Client, simulation_active: bool
) -> None:
    """Fragt die Kernwerte fuer ein Geraet ab und published sie."""
    if simulation_active:
        output_data = _get_simulation_output_data(device)
        log.info("[%s] Simulationswerte werden publiziert.", device.cfg.id)
    else:
        output_data = await device.inverter.get_output_data()

    p1 = output_data.p1
    p2 = output_data.p2
    e1_today = output_data.e1
    e2_today = output_data.e2
    te1_lifetime = output_data.te1
    te2_lifetime = output_data.te2

    total_power = p1 + p2
    total_energy_today = round(e1_today + e2_today, 3)
    total_energy_lifetime = round(te1_lifetime + te2_lifetime, 3)

    _publish(mqtt_client, device, "pv1/power_w", p1)
    _publish(mqtt_client, device, "pv2/power_w", p2)
    _publish(mqtt_client, device, "pv1/energy_today_kwh", round(e1_today, 3))
    _publish(mqtt_client, device, "pv2/energy_today_kwh", round(e2_today, 3))
    _publish(mqtt_client, device, "pv1/energy_lifetime_kwh", round(te1_lifetime, 3))
    _publish(mqtt_client, device, "pv2/energy_lifetime_kwh", round(te2_lifetime, 3))
    _publish(mqtt_client, device, "total/power_w", total_power)
    _publish(mqtt_client, device, "total/energy_today_kwh", total_energy_today)
    _publish(mqtt_client, device, "total/energy_lifetime_kwh", total_energy_lifetime)
    _publish_online_status(mqtt_client, device, True)

    log.info(
        "[%s] PV1=%sW PV2=%sW Gesamt=%sW Ertrag heute=%skWh Lifetime=%skWh",
        device.cfg.id,
        p1,
        p2,
        total_power,
        total_energy_today,
        total_energy_lifetime,
    )
    log.debug(
        "Hinweis: Firmware-Bug bekannt, der te1/te2 bei ca. 540kWh auf 0 "
        "zurücksetzen kann (Integer-Overflow)."
    )


async def poll_extended_info(
    device: APsystemsDevice, mqtt_client: mqtt.Client, simulation_active: bool
) -> None:
    """
    Fragt die selten wechselnden Zusatzwerte fuer ein Geraet ab.
    Jeder Teilschritt wird einzeln abgesichert, da einzelne Endpunkte
    bekanntermassen Fehler werfen koennen, wenn der Wechselrichter gerade
    offline/im Sleep-Modus ist.
    """
    if simulation_active:
        _publish_object_fields(
            mqtt_client,
            device,
            "device_info",
            SimpleNamespace(
                model="EZ1",
                firmware="simulated",
                serial_number="SIM-0001",
                manufacturer="APsystems",
            ),
        )
        _publish_object_fields(
            mqtt_client,
            device,
            "alarm",
            SimpleNamespace(code=0, description="Keine Alarme", active=False),
        )
        _publish(mqtt_client, device, "max_power_limit_w", device._simulation_max_power_limit_w)
        _publish(
            mqtt_client,
            device,
            "power_status",
            "ON" if device._simulation_power_active else "OFF",
        )
        log.info("[%s] Simulierte erweiterte Geraeteinfo aktualisiert.", device.cfg.id)
        return

    try:
        device_info = await device.inverter.get_device_info()
        _publish_object_fields(mqtt_client, device, "device_info", device_info)
    except Exception as exc:
        log.warning("[%s] Geraeteinfo-Abfrage fehlgeschlagen: %s", device.cfg.id, exc)

    try:
        alarm_info = await device.inverter.get_alarm_info()
        _publish_object_fields(mqtt_client, device, "alarm", alarm_info)
    except Exception as exc:
        log.warning("[%s] Alarm-Abfrage fehlgeschlagen: %s", device.cfg.id, exc)

    try:
        max_power = await device.inverter.get_max_power()
        _publish(mqtt_client, device, "max_power_limit_w", max_power)
    except Exception as exc:
        log.warning("[%s] Power-Limit-Abfrage fehlgeschlagen: %s", device.cfg.id, exc)

    try:
        power_status = await device.inverter.get_device_power_status()
        _publish(mqtt_client, device, "power_status", "ON" if power_status else "OFF")
    except Exception as exc:
        log.warning("[%s] Status-Abfrage fehlgeschlagen: %s", device.cfg.id, exc)

    log.info("[%s] Erweiterte Geraeteinfo aktualisiert.", device.cfg.id)


async def set_max_power_safe(
    device: APsystemsDevice, mqtt_client: mqtt.Client, new_limit: int, simulation_active: bool
) -> None:
    if simulation_active:
        device._simulation_max_power_limit_w = new_limit
        log.info(
            "[%s] Simulationsmodus: simuliertes Power-Limit auf %sW gesetzt.",
            device.cfg.id,
            new_limit,
        )
        _publish(mqtt_client, device, "max_power_limit_w", device._simulation_max_power_limit_w)
        return

    try:
        current = await device.inverter.get_max_power()
    except Exception as exc:
        log.warning("[%s] Power-Limit-Abfrage vor dem Setzen fehlgeschlagen: %s", device.cfg.id, exc)
        current = None

    if current is not None and current == new_limit:
        log.info(
            "[%s] Power-Limit bleibt unveraendert: aktuelles Limit ist bereits %sW.",
            device.cfg.id,
            new_limit,
        )
        _publish(mqtt_client, device, "max_power_limit_w", current)
        return

    now = time.time()
    seconds_since_last_write = now - device._last_power_write_ts
    if seconds_since_last_write < MIN_SECONDS_BETWEEN_POWER_WRITES:
        wait_left = int(MIN_SECONDS_BETWEEN_POWER_WRITES - seconds_since_last_write)
        log.warning(
            "[%s] Power-Limit-Aenderung abgelehnt: erst in %ss wieder moeglich "
            "(Schutz des Flash-Speichers).",
            device.cfg.id,
            wait_left,
        )
        _publish(mqtt_client, device, "set/rejected_reason", f"rate_limited_{wait_left}s")
        return

    if not (MAX_POWER_MIN_W <= new_limit <= MAX_POWER_MAX_W):
        log.warning(
            "[%s] Power-Limit-Aenderung abgelehnt: %sW ausserhalb des gueltigen "
            "Bereichs (%s-%sW).",
            device.cfg.id,
            new_limit,
            MAX_POWER_MIN_W,
            MAX_POWER_MAX_W,
        )
        _publish(mqtt_client, device, "set/rejected_reason", "out_of_range")
        return

    try:
        response = await device.inverter.set_max_power(new_limit)
        device._last_power_write_ts = now
        log.info("[%s] Power-Limit gesetzt auf %sW: %s", device.cfg.id, new_limit, response)

        await asyncio.sleep(2)
        current = await device.inverter.get_max_power()
        _publish(mqtt_client, device, "max_power_limit_w", current)
    except Exception as exc:
        log.warning("[%s] Setzen des Power-Limits fehlgeschlagen: %s", device.cfg.id, exc)


async def set_power_status(
    device: APsystemsDevice, mqtt_client: mqtt.Client, active: bool, simulation_active: bool
) -> None:
    if simulation_active:
        device._simulation_power_active = active
        _publish(mqtt_client, device, "power_status", "ON" if active else "OFF")
        log.info(
            "[%s] Simulationsmodus: simulierter Betriebsstatus auf %s gesetzt.",
            device.cfg.id,
            "ON" if active else "OFF",
        )
        return

    try:
        status = await device.inverter.set_device_power_status(active)
        if status is None:
            status = await device.inverter.get_device_power_status()
        _publish(mqtt_client, device, "power_status", "ON" if status else "OFF")
        log.info("[%s] Betriebsstatus auf %s gesetzt.", device.cfg.id, "ON" if status else "OFF")
    except Exception as exc:
        log.warning("[%s] Setzen des Betriebsstatus fehlgeschlagen: %s", device.cfg.id, exc)


# ---------------------------------------------------------------------------
# Service-Zusammenbau
# ---------------------------------------------------------------------------

class APsystemsService:
    def __init__(self, devices: list[APsystemsDevice], config_store, config: appconfig.AppConfig, service_name: str = "apsystems"):
        self.devices = devices
        self.by_id = {d.cfg.id: d for d in devices}
        self.config_store = config_store
        self.config = config
        self.service_name = service_name
        self.service_config = config.service(service_name)
        self.node_device_id = config.dashboard.node_device_id
        self.mqtt_config = config.mqtt
        self.base_topic = f"outstation/{self.service_config.service_id}"
        self.client: Optional[mqtt.Client] = None
        self.loop: Optional[asyncio.AbstractEventLoop] = None
        self.slave: Optional[Slave] = None
        self._discovery_done = False
        self._mqtt_was_connected = False

    def reload_config(self) -> None:
        """config.json und die Geraetedatei gemeinsam neu laden.

        Zuerst beide Kandidaten laden, dann beide uebernehmen: ein Fehler
        in einer der beiden Dateien laesst beide unveraendert, und der
        Slave meldet runtime_status: rejected mit dem Grund.
        """
        if self.config_store is None:
            raise RuntimeError("configuration reload is not configured")

        new_app_config = appconfig.load(self.config.path)
        new_service_config = new_app_config.service(self.service_name)
        new_configs = self.config_store.load_candidate()
        new_node_device_id = new_app_config.dashboard.node_device_id

        self.config = new_app_config
        self.service_config = new_service_config
        self.node_device_id = new_node_device_id
        self.mqtt_config = new_app_config.mqtt
        self.config_store.commit(new_configs)
        logging.getLogger().setLevel(new_app_config.log_level)
        if self.slave is not None:
            self.slave.apply_config_defaults(
                poll_interval_s=new_service_config.poll_interval_s,
                diagnostic_multiplier=new_service_config.diagnostic_poll_multiplier,
            )
        new_devices = [APsystemsDevice(config, self.node_device_id) for config in new_configs]
        if self.loop is not None and self.loop.is_running():
            try:
                running_loop = asyncio.get_running_loop()
            except RuntimeError:
                running_loop = None
            if running_loop is self.loop:
                self._replace_devices(new_devices)
            else:
                self.loop.call_soon_threadsafe(self._replace_devices, new_devices)
        else:
            raise RuntimeError("service event loop is not running")

    def _replace_devices(self, new_devices: list[APsystemsDevice]) -> None:
        self.devices = new_devices
        self.by_id = {device.cfg.id: device for device in new_devices}
        self.slave.register_devices([device.cfg.id for device in new_devices], self.client)
        if self.client is None:
            return
        for device in new_devices:
            publish_device_discovery(self.client, device)
            self.slave.publish_simulation_discovery(
                self.client, device.cfg.id, device.ha_device, device.cfg.base_topic
            )
            self.client.subscribe(device.max_power_set_topic)
            self.client.subscribe(device.power_status_set_topic)

    async def _poll_one_device(self, device: APsystemsDevice) -> None:
        """Pollt ein einzelnes Geraet isoliert; Fehler markieren nur
        dieses Geraet als offline."""
        simulation_active = self.slave.simulation_active_for(device.cfg.id)
        try:
            await poll_once(device, self.client, simulation_active)
        except Exception as exc:
            log.warning(
                "[%s] Kernabfrage fehlgeschlagen (evtl. Nacht-/Sleep-Modus): %s",
                device.cfg.id,
                exc,
            )
            _publish_online_status(
                self.client, device, False, f"Abfragefehler: {exc}"
            )

    async def poll_core(self) -> None:
        await asyncio.gather(*(self._poll_one_device(d) for d in self.devices))
        self.slave.note_update(self.client)

    async def poll_diagnostics(self) -> None:
        await asyncio.gather(
            *(
                poll_extended_info(
                    d, self.client, self.slave.simulation_active_for(d.cfg.id)
                )
                for d in self.devices
            )
        )

    def on_connect(self, client, userdata, flags, reason_code, properties=None):
        # Laeuft im MQTT-Netzwerk-Thread; alles Slave-relevante muss auf den
        # asyncio-Loop-Thread uebergeben werden.
        if reason_code == 0:
            log.info("MQTT verbunden")
            self.loop.call_soon_threadsafe(self._handle_connect, client)
        else:
            log.warning("MQTT-Verbindung fehlgeschlagen: %s", reason_code)

    def _handle_connect(self, client: mqtt.Client) -> None:
        try:
            if self._mqtt_was_connected:
                log.info("MQTT wieder verbunden")
            self._mqtt_was_connected = True

            common_mqtt.publish_online_status(
                client, self.base_topic, True, reason="connected"
            )

            self.slave.start(client)
            log.info("Slave gestartet")

            if not self._discovery_done:
                for device in self.devices:
                    publish_device_discovery(client, device)
                    self.slave.publish_simulation_discovery(
                        client, device.cfg.id, device.ha_device, device.cfg.base_topic
                    )
                self._discovery_done = True
                log.info("Discovery fuer %d EZ1-Geraete veroeffentlicht", len(self.devices))

            for device in self.devices:
                client.subscribe(device.max_power_set_topic)
                client.subscribe(device.power_status_set_topic)

            for device in self.devices:
                if not device._initial_diagnostics_done:
                    asyncio.ensure_future(
                        poll_extended_info(
                            device, client, self.slave.simulation_active_for(device.cfg.id)
                        )
                    )
                    device._initial_diagnostics_done = True
        except Exception:
            log.exception("Fehler in _handle_connect")

    def on_disconnect(self, client, userdata, flags, rc, properties=None):
        if rc == 0:
            log.info("MQTT sauber getrennt.")
        else:
            log.warning("MQTT getrennt, rc=%s; erneuter Verbindungsversuch laeuft.", rc)
        self._mqtt_was_connected = False

    def on_message(self, client, userdata, msg):
        topic = msg.topic
        payload = msg.payload.decode(errors="ignore")
        self.loop.call_soon_threadsafe(self._handle_message, client, topic, payload)

    def _handle_message(self, client: mqtt.Client, topic: str, payload: str) -> None:
        try:
            if self.slave.handle_message(client, topic, payload):
                return

            for device in self.devices:
                if topic == device.max_power_set_topic:
                    try:
                        new_limit = int(payload.strip())
                    except ValueError:
                        log.warning("[%s] Ungueltiger max_power-Wert: %r", device.cfg.id, payload)
                        return
                    log.info(
                        "[%s] MQTT-Befehl: Power-Limit auf %sW setzen",
                        device.cfg.id,
                        new_limit,
                    )
                    asyncio.ensure_future(
                        set_max_power_safe(
                            device,
                            client,
                            new_limit,
                            self.slave.simulation_active_for(device.cfg.id),
                        )
                    )
                    return

                if topic == device.power_status_set_topic:
                    normalized = payload.strip().lower()
                    if normalized in ("1", "on", "true", "yes"):
                        active = True
                    elif normalized in ("0", "off", "false", "no"):
                        active = False
                    else:
                        log.warning(
                            "[%s] Ungueltiger Betriebsstatus: %r", device.cfg.id, payload
                        )
                        return
                    asyncio.ensure_future(
                        set_power_status(
                            device, client, active, self.slave.simulation_active_for(device.cfg.id)
                        )
                    )
                    return

            log.debug("Unbekanntes MQTT-Topic empfangen: %s", topic)
        except Exception:
            log.exception("Fehler in _handle_message (topic=%s)", topic)

    def on_poll_error(self, exc: Exception) -> None:
        log.error("Scheduler-Fehler im APsystems-Service: %s", exc)

    def run(self) -> None:
        self.loop = asyncio.new_event_loop()
        asyncio.set_event_loop(self.loop)

        self.slave = Slave(
            service_id=self.service_config.service_id,
            poll_core=self.poll_core,
            poll_diagnostics=self.poll_diagnostics,
            default_poll_interval_s=self.service_config.poll_interval_s,
            default_diagnostic_multiplier=self.service_config.diagnostic_poll_multiplier,
            node_device_id=self.node_device_id,
            on_config_reload=self.reload_config,
            on_poll_error=self.on_poll_error,
            async_loop=self.loop,
        )
        self.slave.register_devices([device.cfg.id for device in self.devices])

        self.client = common_mqtt.build_client(
            client_id=f"{self.service_config.service_id}-bridge",
            host=self.mqtt_config.host,
            port=self.mqtt_config.port,
            user=self.mqtt_config.username,
            password=self.mqtt_config.password(),
            will_topic=f"{self.base_topic}/status/online",
        )
        # Ohne aktiven Logger verschluckt paho-mqtt Exceptions aus Callbacks.
        self.client.enable_logger(log)
        self.client.on_connect = self.on_connect
        self.client.on_disconnect = self.on_disconnect
        self.client.on_message = self.on_message

        self.client.loop_start()

        log.info(
            "Starte APsystems EZ1 Service fuer %d Inverter (Intervall: %ss, Multiplikator: %s)",
            len(self.devices),
            self.service_config.poll_interval_s,
            self.service_config.diagnostic_poll_multiplier,
        )
        for device in self.devices:
            log.info(
                "- %s @ %s:%s (Topics: %s)",
                device.cfg.id,
                device.cfg.host,
                device.cfg.port,
                device.cfg.base_topic,
            )
        log.info(
            "Power-Limit pro Inverter ueber .../set/max_power_limit_w (%s-%sW)",
            MAX_POWER_MIN_W,
            MAX_POWER_MAX_W,
        )

        try:
            self.loop.run_forever()
        finally:
            self.slave.stop()
            common_mqtt.publish_online_status(
                self.client, self.base_topic, False, reason="shutdown"
            )
            self.client.loop_stop()
            self.client.disconnect()


def main() -> None:
    try:
        config = appconfig.load(appconfig.config_path_from_argv())
    except appconfig.ConfigError as exc:
        print(f"Konfigurationsfehler: {exc}", file=sys.stderr)
        raise SystemExit(1)

    logging.basicConfig(
        level=config.log_level,
        format="%(asctime)s %(levelname)s %(name)s: %(message)s",
    )
    devices_path = config.devices_config("apsystems")
    config_store = common_config.ReloadableConfig(devices_path, load_devices)
    devices = [APsystemsDevice(cfg, config.dashboard.node_device_id) for cfg in config_store.load()]
    log.info("Geladen: %d EZ1-Geraete aus %s", len(devices), devices_path)
    APsystemsService(devices, config_store, config, "apsystems").run()


if __name__ == "__main__":
    try:
        main()
    except KeyboardInterrupt:
        log.info("Beendet durch Benutzer.")