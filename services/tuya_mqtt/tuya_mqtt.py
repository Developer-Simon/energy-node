#!/usr/bin/env python3
"""Multi-device Tuya MQTT bridge."""

from __future__ import annotations

import logging
import os
import sys
import time
from dataclasses import dataclass, field
from threading import RLock
from typing import Any

import paho.mqtt.client as mqtt
import tinytuya

from energy_node_common import Slave
from energy_node_common import appconfig
from energy_node_common import config as common_config
from energy_node_common import mqtt as common_mqtt
from energy_node_common.discovery import (
    publish_availability_discovery as common_publish_availability_discovery,
    publish_discovery as common_publish_discovery,
)

log = logging.getLogger("tuya_mqtt")


@dataclass(frozen=True)
class TuyaDeviceConfig:
    id: str
    name: str
    device_id: str
    local_key: str
    ip: str
    version: float = 3.3
    device_type: str = "valve"
    datapoints: dict[str, str] = field(default_factory=lambda: {"switch": "1"})

    @property
    def switch_dp(self) -> str:
        return str(self.datapoints.get("switch", "1"))

    @property
    def base_topic(self) -> str:
        return f"outstation/{self.id}"

    @property
    def switch_set_topic(self) -> str:
        return f"{self.base_topic}/set/switch"


@dataclass
class TuyaDevice:
    cfg: TuyaDeviceConfig
    device: Any = field(default=None, init=False)
    simulated_switch_state: bool = field(default=False, init=False)

    @property
    def online_topic(self) -> str:
        return f"{self.cfg.base_topic}/status/online"

    def ha_device(self, node_device_id: str) -> dict[str, Any]:
        return {
            "identifiers": [self.cfg.id],
            "name": self.cfg.name,
            "manufacturer": "Tuya",
            "model": f"Tuya {self.cfg.device_type}",
            "via_device": node_device_id,
        }


def load_devices(path: str) -> list[TuyaDeviceConfig]:
    raw = common_config.load_json(path)

    if not isinstance(raw, list) or not raw:
        raise ValueError(f"Tuya-Konfiguration {path} muss eine nichtleere Liste sein")

    configs: list[TuyaDeviceConfig] = []
    for index, item in enumerate(raw):
        if not isinstance(item, dict):
            raise ValueError(f"Eintrag {index} in {path} ist kein Objekt")
        missing = {"id", "name", "device_id", "local_key", "ip"} - set(item)
        if missing:
            raise ValueError(
                f"Eintrag {index} in {path} fehlt Pflichtfelder: {sorted(missing)}"
            )
        values = dict(item)
        values["id"] = str(values["id"]).strip()
        values["name"] = str(values["name"]).strip()
        values["device_id"] = str(values["device_id"]).strip()
        values["local_key"] = str(values["local_key"]).strip()
        values["ip"] = str(values["ip"]).strip()
        if not isinstance(values.get("datapoints"), dict):
            raise ValueError(f"Eintrag {index} in {path} braucht eine Objekt-DP-Map")
        values["datapoints"] = {
            str(key): str(value) for key, value in values.get("datapoints", {}).items()
        }
        if not values["datapoints"].get("switch"):
            raise ValueError(f"Eintrag {index} in {path} braucht datapoints.switch")
        if any(not values[key] for key in ("id", "name", "device_id", "local_key", "ip")):
            raise ValueError(f"Eintrag {index} in {path} enthält leere Pflichtfelder")
        try:
            configs.append(TuyaDeviceConfig(**values))
        except (TypeError, ValueError) as exc:
            raise ValueError(f"Ungültige Tuya-Konfiguration in Eintrag {index}: {exc}") from exc

    ids = [config.id for config in configs]
    if len(ids) != len(set(ids)):
        raise ValueError(f"Doppelte Tuya-Geräte-IDs in {path}: {ids}")
    return configs


def publish(client: mqtt.Client, device: TuyaDevice, subtopic: str, value: Any) -> None:
    common_mqtt.publish(client, f"{device.cfg.base_topic}/{subtopic}", value)


def publish_online_status(
    client: mqtt.Client, device: TuyaDevice, online: bool, reason: str | None = None
) -> None:
    common_mqtt.publish_online_status(client, device.cfg.base_topic, online, reason)


def publish_device_discovery(client: mqtt.Client, device: TuyaDevice, node_device_id: str) -> None:
    ha_device = device.ha_device(node_device_id)
    switch_config = {
        "name": device.cfg.name,
        "unique_id": f"{device.cfg.id}_switch",
        "state_topic": f"{device.cfg.base_topic}/switch",
        "command_topic": device.cfg.switch_set_topic,
        "payload_on": "ON",
        "payload_off": "OFF",
        "state_on": "ON",
        "state_off": "OFF",
        "availability_topic": device.online_topic,
        "payload_available": "1",
        "payload_not_available": "0",
        "device": ha_device,
    }
    common_publish_discovery(client, device.cfg.id, "switch", "switch", switch_config)
    common_publish_availability_discovery(
        client, device.cfg.id, device.cfg.base_topic, ha_device
    )


def poll_one(device: TuyaDevice, client: mqtt.Client, simulation_active: bool) -> None:
    if simulation_active:
        publish(client, device, "switch", "ON" if device.simulated_switch_state else "OFF")
        publish_online_status(client, device, True, "Simulation aktiv")
        return

    if device.device is None:
        device.device = tinytuya.Device(
            device.cfg.device_id, device.cfg.ip, device.cfg.local_key
        )
        device.device.set_version(device.cfg.version)
        device.device.set_socketPersistent(True)
        log.info("[%s] Tuya-Gerät initialisiert: %s", device.cfg.id, device.cfg.device_id)

    status = device.device.status()
    if "Error" in status:
        raise RuntimeError(f"Statusfehler: {status}")

    dps = status.get("dps", {})
    log.info("[%s] Roher Gerätestatus (dps): %s", device.cfg.id, dps)
    switch_value = dps.get(device.cfg.switch_dp)
    if switch_value is None:
        publish(client, device, "switch", "UNKNOWN")
        publish_online_status(client, device, True, "DP nicht gefunden")
        log.warning(
            "[%s] DP '%s' nicht im Status enthalten; verfügbar: %s",
            device.cfg.id, device.cfg.switch_dp, list(dps.keys()),
        )
        return

    publish(client, device, "switch", "ON" if switch_value else "OFF")
    publish_online_status(client, device, True)


def set_switch(device: TuyaDevice, client: mqtt.Client, active: bool, simulation: bool) -> None:
    if simulation:
        device.simulated_switch_state = active
    else:
        if device.device is None:
            device.device = tinytuya.Device(
                device.cfg.device_id, device.cfg.ip, device.cfg.local_key
            )
            device.device.set_version(device.cfg.version)
            device.device.set_socketPersistent(True)
        result = device.device.set_value(device.cfg.switch_dp, active)
        log.info("[%s] Tuya-Antwort: %s", device.cfg.id, result)
    publish(client, device, "switch", "ON" if active else "OFF")
    publish_online_status(client, device, True, "Simulation aktiv" if simulation else None)


class TuyaService:
    def __init__(self, devices: list[TuyaDevice], config_store=None, app_config=None, service_name: str = "tuya"):
        self.devices = devices
        self.by_topic = {device.cfg.switch_set_topic: device for device in devices}
        self.config_store = config_store
        self._devices_lock = RLock()
        self.client: mqtt.Client | None = None
        self.slave: Slave | None = None

        # Zentrale Konfiguration
        self._app_config = app_config
        self._service_name = service_name
        self.service_name = service_name
        if app_config:
            self.service_config = app_config.service(service_name)
            self.node_device_id = app_config.node.device_id
            self.mqtt_config = app_config.mqtt
        else:
            self.service_config = None
            self.node_device_id = None
            self.mqtt_config = None

    @property
    def base_topic(self) -> str:
        if self.service_config:
            return f"outstation/{self.service_config.service_id}"
        return "outstation/tuya"

    def reload_config(self) -> None:
        """config.json und die Geraetedatei gemeinsam neu laden.

        Zuerst beide Kandidaten laden, dann beide uebernehmen: ein Fehler
        in einer der beiden Dateien laesst beide unveraendert, und der
        Slave meldet runtime_status: rejected mit dem Grund.
        """
        if self.config_store is None:
            raise RuntimeError("configuration reload is not configured")

        new_app_config = appconfig.load(self._app_config.path)
        new_service_config = new_app_config.service(self.service_name)
        new_configs = self.config_store.load_candidate()

        self._app_config = new_app_config
        self.service_config = new_service_config
        self.node_device_id = new_app_config.node.device_id
        self.mqtt_config = new_app_config.mqtt
        self.config_store.commit(new_configs)
        logging.getLogger().setLevel(new_app_config.log_level)
        if self.slave is not None:
            self.slave.apply_config_defaults(
                poll_interval_s=new_service_config.poll_interval_s,
                diagnostic_multiplier=new_service_config.diagnostic_poll_multiplier,
            )
        with self._devices_lock:
            previous = {device.cfg.id: device for device in self.devices}
            new_devices = []
            for config in new_configs:
                device = TuyaDevice(config)
                old = previous.get(config.id)
                if old is not None:
                    device.device = old.device
                    device.simulated_switch_state = old.simulated_switch_state
                new_devices.append(device)
            old_topics = set(self.by_topic)
            self.devices = new_devices
            self.by_topic = {device.cfg.switch_set_topic: device for device in new_devices}
            self.slave.register_devices([device.cfg.id for device in new_devices], self.client)
            if self.client is not None:
                for topic in old_topics - set(self.by_topic):
                    self.client.unsubscribe(topic)
                for device in new_devices:
                    self.client.subscribe(device.cfg.switch_set_topic)
                    publish_device_discovery(self.client, device, self.node_device_id)
                    self.slave.publish_simulation_discovery(
                        self.client, device.cfg.id, device.ha_device(self.node_device_id), device.cfg.base_topic
                    )

    def poll_core(self) -> None:
        with self._devices_lock:
            devices = list(self.devices)
        for device in devices:
            try:
                poll_one(device, self.client, self.slave.simulation_active_for(device.cfg.id))
            except Exception as exc:
                log.warning("[%s] Abfrage fehlgeschlagen: %s", device.cfg.id, exc)
                device.device = None
                publish_online_status(self.client, device, False, f"Abfragefehler: {exc}")
        self.slave.note_update(self.client)

    def on_connect(self, client, userdata, flags, reason_code, properties=None):
        if reason_code != 0:
            log.warning("MQTT-Verbindung fehlgeschlagen: %s", reason_code)
            return
        common_mqtt.publish_online_status(
            client, self.base_topic, True, reason="connected"
        )
        self.slave.start(client)
        for device in self.devices:
            client.subscribe(device.cfg.switch_set_topic)
            publish_device_discovery(client, device, self.node_device_id)
            self.slave.publish_simulation_discovery(
                client, device.cfg.id, device.ha_device(self.node_device_id), device.cfg.base_topic
            )

    def on_message(self, client, userdata, message):
        payload = message.payload.decode(errors="ignore").strip().upper()
        if self.slave.handle_message(client, message.topic, payload):
            return
        device = self.by_topic.get(message.topic)
        if device is None or payload not in ("ON", "OFF"):
            log.warning("Ungültiger oder unbekannter Tuya-Befehl: %s = %s", message.topic, payload)
            return
        try:
            set_switch(
                device,
                client,
                payload == "ON",
                self.slave.simulation_active_for(device.cfg.id),
            )
        except Exception as exc:
            device.device = None
            publish_online_status(client, device, False, f"Set-Fehler: {exc}")
            log.warning("[%s] Schalten fehlgeschlagen: %s", device.cfg.id, exc)

    def run(self) -> None:
        self.slave = Slave(
            service_id=self.service_config.service_id,
            poll_core=self.poll_core,
            default_poll_interval_s=self.service_config.poll_interval_s,
            default_diagnostic_multiplier=self.service_config.diagnostic_poll_multiplier,
            node_device_id=self.node_device_id,
            on_config_reload=self.reload_config,
            on_poll_error=lambda exc: log.warning("Scheduler-Fehler: %s", exc),
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
        self.client.on_connect = self.on_connect
        self.client.on_message = self.on_message
        self.client.loop_start()
        log.info("Starte Tuya-Service für %d Geräte", len(self.devices))
        try:
            while True:
                time.sleep(3600)
        except KeyboardInterrupt:
            self.slave.stop()


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
    devices_path = config.devices_config("tuya")
    config_store = common_config.ReloadableConfig(devices_path, load_devices)
    devices = [TuyaDevice(cfg) for cfg in config_store.load()]
    log.info("Geladen: %d Tuya-Geraete aus %s", len(devices), devices_path)
    TuyaService(devices, config_store, config, "tuya").run()


if __name__ == "__main__":
    main()
