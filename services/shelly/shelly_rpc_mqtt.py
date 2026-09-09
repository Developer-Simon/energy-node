#!/usr/bin/env python3
"""
shelly_rpc_mqtt.py

Ein einziger Python-Service für ALLE Shelly-Geräte im Energy-Node-Projekt.

Ersetzt das bisherige On-Device-MQTT-Discovery-Skript
(mqtt-discovery-self.shelly.js). Statt dass jedes Shelly-Gerät selbst per
MQTT publiziert (was auf den Geräten den Zugriff auf die Shelly-Cloud
blockiert), fragt dieser Service jedes Gerät aktiv per HTTP ab:

  - Gen1-Geräte (3EM, 1, Plug S, Uni): GET /status, Schalten über
    GET /relay/<ch>?turn=on|off
  - Gen2+-Geräte (Plug S+, u. Ã¤.):     POST /rpc  {"method": "Shelly.GetStatus"}
    Schalten über POST /rpc {"method": "Switch.Set", "params": {...}}

Ein Prozess = ein Slave (Settings-Protokoll von energy_node_common), aber
JEDES physische Shelly-Gerät bekommt sein eigenes HA-Gerät (via_device zu
energy-node), eigene Discovery-Entities und eigene Verfügbarkeit.

Poll-Zyklus: alle Geräte werden pro Zyklus concurrent (asyncio) abgefragt.
Ein Fehler an einem Gerät (Timeout, falsche IP, ...) wird pro Gerät isoliert
behandelt und wirkt sich NICHT auf die anderen Geräte im selben Zyklus aus.
"""

from __future__ import annotations

import asyncio
import json
import logging
import math
import re
import sys
import time
from dataclasses import dataclass, field
from typing import Any, Optional

import requests
from requests.adapters import HTTPAdapter
from urllib3.util.retry import Retry
import paho.mqtt.client as mqtt_client

from energy_node_common import Slave, mqtt, discovery
from energy_node_common import config as common_config
from energy_node_common import appconfig

LOG = logging.getLogger("shelly_rpc_mqtt")


# ---------------------------------------------------------------------------
# Geräte-Konfiguration
# ---------------------------------------------------------------------------

@dataclass
class ShellyDeviceConfig:
    id: str  # eindeutig innerhalb des Service, z.B. "netz_meanwell"
    name: str  # Anzeigename in Home Assistant
    host: str  # IP oder Hostname
    generation: int  # 1 (Gen1 /status) oder 2 (Gen2+ /rpc)
    switch_channels: int = 0  # Anzahl schaltbarer Relais (0 = kein Schalten)
    has_power: bool = False  # Momentanleistung(en) vorhanden
    has_energy: bool = False  # Energiezähler vorhanden
    has_3phase: bool = False  # 3EM: drei Phasen statt einem Kanal
    adc_channels: int = 0  # Anzahl ADC-Kanäle (z.B. Shelly Uni)
    has_temperature: bool = False  # Gerätetemperatur vorhanden
    has_humidity: bool = False  # Luftfeuchte vorhanden (z.B. Shelly Plus H&T)
    auth_user: str = ""
    auth_password: str = ""

    @property
    def base_topic(self) -> str:
        return f"outstation/{self.id}"

    @property
    def unique_id(self) -> str:
        return self.id

    @property
    def auth(self) -> Optional[tuple]:
        if self.auth_user:
            return (self.auth_user, self.auth_password)
        return None


def load_devices(path: str) -> list[ShellyDeviceConfig]:
    raw = common_config.load_json(path)
    devices = [ShellyDeviceConfig(**item) for item in raw]
    ids = [d.id for d in devices]
    if len(ids) != len(set(ids)):
        raise ValueError(f"Doppelte Geräte-IDs in {path}: {ids}")
    return devices


# ---------------------------------------------------------------------------
# HTTP-Zugriff (synchron, per asyncio.to_thread aus dem Poll-Loop gerufen)
#
# Eine modulweite Session hält die TCP-Verbindung pro Gerät per Keep-alive
# offen - auf dem Pi 1 ist der Verbindungsaufbau der teuerste Teil eines
# Polls (siehe docs/knowledge/performance-and-resources.md 5.1). Bricht eine
# gepoolte Verbindung weg (Gerät neu gestartet, WLAN weg), verwirft urllib3
# sie und baut on demand eine neue auf; die Retry-Policy deckt zusätzlich den
# Fall ab, dass der Abbruch erst beim Senden auffällt - auch für die
# Gen2-RPC-POSTs (Status-Read bzw. idempotenter Schaltbefehl).
# ---------------------------------------------------------------------------

def _build_session() -> requests.Session:
    session = requests.Session()
    retry = Retry(
        total=2,
        connect=2,
        read=1,
        backoff_factor=0.2,
        allowed_methods=None,  # alle Methoden erneut versuchen, auch POST
        status_forcelist=(),
    )
    adapter = HTTPAdapter(max_retries=retry, pool_connections=8, pool_maxsize=8)
    session.mount("http://", adapter)  # Shelly-Geräte werden im LAN per HTTP abgefragt
    return session


_SESSION = _build_session()


def _http_get(url: str, auth=None, timeout_s: float = 5.0) -> dict:
    resp = _SESSION.get(url, auth=auth, timeout=timeout_s)
    resp.raise_for_status()
    return resp.json()


def _http_rpc(host: str, method: str, params: Optional[dict] = None, auth=None, timeout_s: float = 5.0) -> dict:
    body = {"id": 1, "method": method}
    if params is not None:
        body["params"] = params
    resp = _SESSION.post(f"http://{host}/rpc", json=body, auth=auth, timeout=timeout_s)
    resp.raise_for_status()
    data = resp.json()
    if "error" in data:
        raise RuntimeError(f"RPC-Fehler von {host}: {data['error']}")
    return data.get("result", {})


def fetch_gen1_status(cfg: ShellyDeviceConfig, timeout_s: float) -> dict:
    return _http_get(f"http://{cfg.host}/status", auth=cfg.auth, timeout_s=timeout_s)


def fetch_gen2_status(cfg: ShellyDeviceConfig, timeout_s: float) -> dict:
    return _http_rpc(cfg.host, "Shelly.GetStatus", auth=cfg.auth, timeout_s=timeout_s)


def set_relay_gen1(cfg: ShellyDeviceConfig, channel: int, on: bool, timeout_s: float) -> None:
    turn = "on" if on else "off"
    _http_get(f"http://{cfg.host}/relay/{channel}?turn={turn}", auth=cfg.auth, timeout_s=timeout_s)


def set_relay_gen2(cfg: ShellyDeviceConfig, channel: int, on: bool, timeout_s: float) -> None:
    _http_rpc(cfg.host, "Switch.Set", params={"id": channel, "on": on}, auth=cfg.auth, timeout_s=timeout_s)


# ---------------------------------------------------------------------------
# Geraete-Stammdaten (nur im Diagnose-Zyklus abgefragt)
#
# Gen2+ liefert sie per /rpc Shelly.GetDeviceInfo, Gen1 per GET /settings.
# Sie fuellen model/sw_version/hw_version/serial_number im HA-Discovery-
# Geraeteblock, damit Home Assistant Firmware und Modell anzeigt.
# ---------------------------------------------------------------------------

# Gen1-Modellcodes (device.type) auf lesbare Namen; unbekannte Codes werden
# unveraendert durchgereicht.
_GEN1_MODEL_NAMES = {
    "SHEM-3": "Shelly 3EM",
    "SHSW-1": "Shelly 1",
    "SHSW-21": "Shelly 2.5",
    "SHSW-25": "Shelly 2.5",
    "SHSW-PM": "Shelly 1PM",
    "SHPLG-S": "Shelly Plug S",
    "SHPLG2-1": "Shelly Plug",
    "SHUNI-1": "Shelly Uni",
}


@dataclass
class ShellyDeviceInfo:
    """Normalisierte Stammdaten eines Shelly-Geraets."""

    model: str = ""
    sw_version: str = ""
    generation: int = 0
    mac: str = ""

    @property
    def hw_version(self) -> str:
        return f"Gen {self.generation}" if self.generation else ""


def _clean_gen1_version(fw: str) -> str:
    """'20230913-112003/v1.14.0-gcb84623' -> '1.14.0'.

    Master-/Entwickler-Builds ohne SemVer-Tag werden unveraendert
    zurueckgegeben.
    """
    match = re.search(r"/v?(\d+\.\d+\.\d+)", fw or "")
    return match.group(1) if match else (fw or "")


def parse_gen1_settings(raw: dict) -> ShellyDeviceInfo:
    device = raw.get("device", {}) or {}
    model_code = device.get("type", "") or ""
    return ShellyDeviceInfo(
        model=_GEN1_MODEL_NAMES.get(model_code, model_code),
        sw_version=_clean_gen1_version(raw.get("fw", "")),
        generation=1,
        mac=device.get("mac", "") or "",
    )


def parse_gen2_device_info(raw: dict) -> ShellyDeviceInfo:
    return ShellyDeviceInfo(
        model=raw.get("model") or raw.get("app", "") or "",
        sw_version=raw.get("ver", "") or "",
        generation=int(raw.get("gen", 2) or 2),
        mac=raw.get("mac", "") or "",
    )


def fetch_gen1_settings(cfg: ShellyDeviceConfig, timeout_s: float) -> dict:
    return _http_get(f"http://{cfg.host}/settings", auth=cfg.auth, timeout_s=timeout_s)


def fetch_gen2_device_info(cfg: ShellyDeviceConfig, timeout_s: float) -> dict:
    return _http_rpc(cfg.host, "Shelly.GetDeviceInfo", auth=cfg.auth, timeout_s=timeout_s)


def fetch_device_info(cfg: ShellyDeviceConfig, timeout_s: float) -> ShellyDeviceInfo:
    """Synchroner Stammdaten-Abruf; wird per asyncio.to_thread aufgerufen."""
    if cfg.generation == 1:
        return parse_gen1_settings(fetch_gen1_settings(cfg, timeout_s))
    return parse_gen2_device_info(fetch_gen2_device_info(cfg, timeout_s))


# ---------------------------------------------------------------------------
# Normalisierung: rohe Shelly-Antwort -> einheitliches dict fürs Publishing
#
# Gemeinsame Schlüssel (je nach Fähigkeiten des Geräts vorhanden):
#   relays: list[bool]
#   power_w: list[float]           (ein Eintrag pro Kanal/Phase)
#   energy_wh: list[float]         (Summenzähler pro Kanal/Phase)
#   energy_returned_wh: list[float]  (nur 3EM)
#   voltage_v: list[float]
#   adc_v: list[float]             (Shelly Uni)
#   temperature_c: float
#   rssi: int
#   cloud_connected: bool
# ---------------------------------------------------------------------------

def normalize_gen1(cfg: ShellyDeviceConfig, raw: dict) -> dict:
    out: dict[str, Any] = {}

    if cfg.has_3phase:
        emeters = raw.get("emeters", [])
        out["power_w"] = [e.get("power", 0.0) for e in emeters]
        out["voltage_v"] = [e.get("voltage", 0.0) for e in emeters]
        # Shelly liefert "total"/"total_returned" in Wh als Summenzähler
        out["energy_wh"] = [e.get("total", 0.0) for e in emeters]
        out["energy_returned_wh"] = [e.get("total_returned", 0.0) for e in emeters]
    else:
        if cfg.switch_channels:
            relays = raw.get("relays", [])
            out["relays"] = [r.get("ison", False) for r in relays[: cfg.switch_channels]]
        if cfg.has_power:
            meters = raw.get("meters", [])
            out["power_w"] = [m.get("power", 0.0) for m in meters]
        if cfg.has_energy:
            meters = raw.get("meters", [])
            # Gen1-"meters" liefern Energie meist in Wattminuten (energy),
            # Umrechnung in Wh
            out["energy_wh"] = [m.get("total", 0.0) / 60.0 for m in meters]

    if cfg.adc_channels:
        adcs = raw.get("adcs", [])
        out["adc_v"] = [a.get("voltage", 0.0) for a in adcs[: cfg.adc_channels]]

    if cfg.has_temperature:
        temp = raw.get("temperature") or raw.get("tmp", {}).get("tC")
        if temp is not None:
            out["temperature_c"] = temp

    wifi = raw.get("wifi_sta", {})
    out["rssi"] = wifi.get("rssi")
    out["cloud_connected"] = raw.get("cloud", {}).get("connected")

    return out


def normalize_gen2(cfg: ShellyDeviceConfig, raw: dict) -> dict:
    out: dict[str, Any] = {}

    if cfg.switch_channels:
        out["relays"] = [
            bool(raw.get(f"switch:{ch}", {}).get("output", False))
            for ch in range(cfg.switch_channels)
        ]
    if cfg.has_power:
        out["power_w"] = [
            raw.get(f"switch:{ch}", {}).get("apower", 0.0)
            for ch in range(cfg.switch_channels)
        ]
    if cfg.has_energy:
        out["energy_wh"] = [
            raw.get(f"switch:{ch}", {}).get("aenergy", {}).get("total", 0.0)
            for ch in range(cfg.switch_channels)
        ]
    if cfg.has_temperature:
        if cfg.switch_channels:
            # Gen2-Geräte melden die Gerätetemperatur meist am Switch-Kanal
            temps = [
                raw.get(f"switch:{ch}", {}).get("temperature", {}).get("tC")
                for ch in range(cfg.switch_channels)
            ]
            temps = [t for t in temps if t is not None]
            temp = temps[0] if temps else None
        else:
            # Geräte ohne Schaltkanal (z.B. Shelly Plus H&T) melden die
            # Temperatur über eine eigene "temperature:0"-Komponente.
            temp = raw.get("temperature:0", {}).get("tC")
        if temp is not None:
            out["temperature_c"] = temp

    if cfg.has_humidity:
        humidity = raw.get("humidity:0", {}).get("rh")
        if humidity is not None:
            out["humidity_pct"] = humidity

    wifi = raw.get("wifi", {})
    out["rssi"] = wifi.get("rssi")
    out["cloud_connected"] = raw.get("cloud", {}).get("connected")

    return out


# ---------------------------------------------------------------------------
# Discovery
# ---------------------------------------------------------------------------

def device_block(
    cfg: ShellyDeviceConfig,
    node_device_id: str,
    info: Optional[ShellyDeviceInfo] = None,
) -> dict:
    block = {
        "name": cfg.name,
        "identifiers": [cfg.unique_id],
        "manufacturer": "Shelly",
        "via_device": node_device_id,
    }
    if info is not None:
        if info.model:
            block["model"] = info.model
        if info.sw_version:
            block["sw_version"] = info.sw_version
        if info.hw_version:
            block["hw_version"] = info.hw_version
        if info.mac:
            block["serial_number"] = info.mac
    return block


def publish_device_discovery(
    client: mqtt_client.Client,
    cfg: ShellyDeviceConfig,
    node_device_id: str,
    dev_block: Optional[dict] = None,
) -> None:
    dev = dev_block or device_block(cfg, node_device_id)
    base = cfg.base_topic

    for ch in range(cfg.switch_channels):
        object_id = f"relay_{ch}" if cfg.switch_channels > 1 else "relay"
        state_topic = f"{base}/relay/{ch}"
        command_topic = f"{base}/relay/{ch}/set"
        cfg_payload = discovery.entity_config(
            device_id=cfg.unique_id,
            base_topic=base,
            object_id=object_id,
            name=f"{cfg.name} Schalter" + (f" {ch}" if cfg.switch_channels > 1 else ""),
            device_block=dev,
            state_topic=state_topic,
            command_topic=command_topic,
            payload_on="ON",
            payload_off="OFF",
        )
        discovery.publish_discovery(client, cfg.unique_id, "switch", object_id, cfg_payload)

    if cfg.has_3phase:
        phases = ["l1", "l2", "l3"]
        for i, phase in enumerate(phases):
            _publish_sensor(client, cfg, dev, f"power_{phase}", f"{cfg.name} Leistung {phase.upper()}",
                             f"{base}/power/{i}", unit="W", device_class="power")
            _publish_sensor(client, cfg, dev, f"voltage_{phase}", f"{cfg.name} Spannung {phase.upper()}",
                             f"{base}/voltage/{i}", unit="V", device_class="voltage")
            _publish_sensor(client, cfg, dev, f"energy_{phase}", f"{cfg.name} Energie {phase.upper()}",
                             f"{base}/energy/{i}", unit="Wh", device_class="energy",
                             state_class="total_increasing")
            _publish_sensor(client, cfg, dev, f"energy_returned_{phase}",
                             f"{cfg.name} Einspeisung {phase.upper()}",
                             f"{base}/energy_returned/{i}", unit="Wh", device_class="energy",
                             state_class="total_increasing")
    else:
        if cfg.has_power:
            for ch in range(max(cfg.switch_channels, 1)):
                suffix = f" {ch}" if cfg.switch_channels > 1 else ""
                _publish_sensor(client, cfg, dev, f"power_{ch}", f"{cfg.name} Leistung{suffix}",
                                 f"{base}/power/{ch}", unit="W", device_class="power")
        if cfg.has_energy:
            for ch in range(max(cfg.switch_channels, 1)):
                suffix = f" {ch}" if cfg.switch_channels > 1 else ""
                _publish_sensor(client, cfg, dev, f"energy_{ch}", f"{cfg.name} Energie{suffix}",
                                 f"{base}/energy/{ch}", unit="Wh", device_class="energy",
                                 state_class="total_increasing")

    for ch in range(cfg.adc_channels):
        _publish_sensor(client, cfg, dev, f"adc_{ch}", f"{cfg.name} Spannung ADC {ch}",
                         f"{base}/adc/{ch}", unit="V", device_class="voltage")

    if cfg.has_temperature:
        _publish_sensor(client, cfg, dev, "temperature", f"{cfg.name} Temperatur",
                         f"{base}/temperature", unit="°C", device_class="temperature")

    if cfg.has_humidity:
        _publish_sensor(client, cfg, dev, "humidity", f"{cfg.name} Luftfeuchte",
                         f"{base}/humidity", unit="%", device_class="humidity")

    _publish_sensor(client, cfg, dev, "rssi", f"{cfg.name} WLAN-Signal",
                     f"{base}/rssi", unit="dBm", device_class="signal_strength",
                     enabled_by_default=False, entity_category="diagnostic")

    discovery.publish_availability_discovery(
        client, cfg.unique_id, cfg.base_topic, dev
    )


def publish_simulation_discovery(
    client: mqtt_client.Client,
    cfg: ShellyDeviceConfig,
    node_device_id: str,
    dev_block: Optional[dict] = None,
) -> None:
    discovery.publish_discovery(
        client,
        cfg.unique_id,
        "switch",
        "simulation_active",
        discovery.entity_config(
            cfg.unique_id,
            cfg.base_topic,
            "simulation_active",
            "Simulationsmodus",
            dev_block or device_block(cfg, node_device_id),
            state_topic=f"outstation/{cfg.unique_id}/settings/simulation_active",
            command_topic=f"outstation/{cfg.unique_id}/settings/simulation_active/set",
            payload_on="1",
            payload_off="0",
            entity_category="config",
        ),
    )


def _publish_sensor(client, cfg, dev, object_id, name, state_topic, unit=None,
                     device_class=None, state_class=None, enabled_by_default=True,
                     entity_category=None) -> None:
    kwargs: dict[str, Any] = {}
    if unit:
        kwargs["unit_of_measurement"] = unit
    if device_class:
        kwargs["device_class"] = device_class
    if state_class:
        kwargs["state_class"] = state_class
    if entity_category:
        kwargs["entity_category"] = entity_category
    kwargs["enabled_by_default"] = enabled_by_default

    cfg_payload = discovery.entity_config(
        device_id=cfg.unique_id,
        base_topic=cfg.base_topic,
        object_id=object_id,
        name=name,
        device_block=dev,
        state_topic=state_topic,
        **kwargs,
    )
    discovery.publish_discovery(client, cfg.unique_id, "sensor", object_id, cfg_payload)


# ---------------------------------------------------------------------------
# Poll- und Control-Logik pro Gerät
# ---------------------------------------------------------------------------

@dataclass
class SimulatedDeviceState:
    """Laufzeitzustand eines simulierten Shelly-Geräts.

    relays: aktueller Schaltzustand je Relaiskanal.
    energy_wh: aufsummierte Energie je Leistungskanal/-phase, wird bei
        jedem simulierten Poll um power_w * vergangene Zeit erhöht.
    last_update_ts: Zeitstempel des letzten simulierten Polls, um die
        vergangene Zeit für die Energie-Akkumulation zu bestimmen.
    """

    relays: list[bool] = field(default_factory=list)
    energy_wh: list[float] = field(default_factory=list)
    last_update_ts: Optional[float] = None


def _simulated_power_channel_count(cfg: ShellyDeviceConfig) -> int:
    if cfg.has_3phase:
        return 3
    if cfg.has_power or cfg.has_energy:
        return max(cfg.switch_channels, 1)
    return 0


def new_simulated_state(cfg: ShellyDeviceConfig) -> SimulatedDeviceState:
    return SimulatedDeviceState(
        relays=[False] * cfg.switch_channels,
        energy_wh=[0.0] * _simulated_power_channel_count(cfg),
    )


def _simulated_channel_power_w(cfg: ShellyDeviceConfig, channel: int, now: float) -> float:
    """Plausible, sanft schwankende Leistung für einen Kanal/eine Phase.

    Hauptzähler (3EM) simulieren einen kontinuierlichen Hausverbrauch,
    Plugs simulieren die Last eines angeschlossenen Verbrauchers.
    """
    if cfg.has_3phase:
        base, amplitude, period = 300.0, 150.0, 240.0
    else:
        base, amplitude, period = 45.0, 25.0, 180.0
    offset = (hash(f"{cfg.id}-{channel}") % 1000) / 1000.0 * 2 * math.pi
    value = base + amplitude * math.sin(now / period + offset)
    return round(max(0.0, value), 1)


def simulated_state(
    cfg: ShellyDeviceConfig, state: Optional[SimulatedDeviceState] = None
) -> dict:
    if state is None:
        state = new_simulated_state(cfg)

    data: dict[str, Any] = {}
    if cfg.switch_channels:
        data["relays"] = list(state.relays)

    now = time.time()
    dt_hours = 0.0
    if state.last_update_ts is not None:
        dt_hours = max(0.0, now - state.last_update_ts) / 3600.0
    state.last_update_ts = now

    channels = _simulated_power_channel_count(cfg)
    if channels:
        power = [_simulated_channel_power_w(cfg, ch, now) for ch in range(channels)]
        # Plugs (Relais + Leistung, keine 3-Phasen-Messung): ausgeschaltete
        # Kanäle liefern keine Leistung.
        if cfg.switch_channels and not cfg.has_3phase:
            power = [
                watt if (ch < len(state.relays) and state.relays[ch]) else 0.0
                for ch, watt in enumerate(power)
            ]

        if cfg.has_3phase or cfg.has_power:
            data["power_w"] = power
        if cfg.has_3phase:
            data["voltage_v"] = [230.0] * channels
        if cfg.has_3phase or cfg.has_energy:
            for ch, watt in enumerate(power):
                state.energy_wh[ch] += watt * dt_hours
            data["energy_wh"] = list(state.energy_wh)
        if cfg.has_3phase:
            data["energy_returned_wh"] = [0.0] * channels

    if cfg.adc_channels:
        data["adc_v"] = [0.0] * cfg.adc_channels
    if cfg.has_temperature:
        data["temperature_c"] = 20.0
    if cfg.has_humidity:
        data["humidity_pct"] = 45.0
    data["rssi"] = -50
    return data


async def fetch_and_publish_state(
    client: mqtt_client.Client,
    cfg: ShellyDeviceConfig,
    timeout_s: float,
    simulation_active: bool = False,
    simulated_device_state: Optional[SimulatedDeviceState] = None,
) -> None:
    """Fragt ein einzelnes Gerät ab und veröffentlicht den Zustand.

    Fehler werden gefangen, geloggt und als offline markiert.
    """
    try:
        if simulation_active:
            data = simulated_state(cfg, simulated_device_state)
            reason = "Simulation aktiv"
        elif cfg.generation == 1:
            raw = await asyncio.to_thread(fetch_gen1_status, cfg, timeout_s)
            data = normalize_gen1(cfg, raw)
            reason = "connected"
        else:
            raw = await asyncio.to_thread(fetch_gen2_status, cfg, timeout_s)
            data = normalize_gen2(cfg, raw)
            reason = "connected"

        _publish_state(client, cfg, data)
        mqtt.publish_online_status(client, cfg.base_topic, online=True, reason=reason)

    except Exception as exc:
        LOG.warning("Shelly '%s' (%s) nicht erreichbar: %s", cfg.id, cfg.host, exc)
        mqtt.publish_online_status(client, cfg.base_topic, online=False, reason=str(exc))


async def poll_one_device(
    client: mqtt_client.Client,
    cfg: ShellyDeviceConfig,
    timeout_s: float,
    simulation_active: bool = False,
    simulated_device_state: Optional[SimulatedDeviceState] = None,
) -> None:
    """Wrapper für fetch_and_publish_state im Poll-Zyklus."""
    await fetch_and_publish_state(client, cfg, timeout_s, simulation_active, simulated_device_state)


def _publish_state(client, cfg: ShellyDeviceConfig, data: dict) -> None:
    base = cfg.base_topic

    if "relays" in data:
        for ch, state in enumerate(data["relays"]):
            mqtt.publish(client, f"{base}/relay/{ch}", "ON" if state else "OFF")

    if cfg.has_3phase:
        for i, key in enumerate(("power_w", "voltage_v", "energy_wh", "energy_returned_wh")):
            topic_name = ["power", "voltage", "energy", "energy_returned"][i]
            for ph, value in enumerate(data.get(key, [])):
                mqtt.publish(client, f"{base}/{topic_name}/{ph}", round(value, 2))
    else:
        for ch, value in enumerate(data.get("power_w", [])):
            mqtt.publish(client, f"{base}/power/{ch}", round(value, 2))
        for ch, value in enumerate(data.get("energy_wh", [])):
            mqtt.publish(client, f"{base}/energy/{ch}", round(value, 2))

    for ch, value in enumerate(data.get("adc_v", [])):
        mqtt.publish(client, f"{base}/adc/{ch}", round(value, 3))

    if "temperature_c" in data:
        mqtt.publish(client, f"{base}/temperature", round(data["temperature_c"], 1))

    if "humidity_pct" in data:
        mqtt.publish(client, f"{base}/humidity", round(data["humidity_pct"], 1))

    if data.get("rssi") is not None:
        mqtt.publish(client, f"{base}/rssi", data["rssi"])


async def apply_relay_command(
    client: mqtt_client.Client,
    cfg: ShellyDeviceConfig,
    channel: int,
    on: bool,
    timeout_s: float,
    simulation_active: bool = False,
    simulated_device_state: Optional[SimulatedDeviceState] = None,
) -> None:
    try:
        if simulation_active:
            if simulated_device_state is not None:
                simulated_device_state.relays[channel] = on
            await fetch_and_publish_state(client, cfg, timeout_s, True, simulated_device_state)
            return
        if cfg.generation == 1:
            await asyncio.to_thread(set_relay_gen1, cfg, channel, on, timeout_s)
        else:
            await asyncio.to_thread(set_relay_gen2, cfg, channel, on, timeout_s)
        LOG.info("Shelly '%s' Kanal %d -> %s", cfg.id, channel, "ON" if on else "OFF")
        # Direkte Rücklesung: nach dem Schaltbefehl den aktuellen Zustand abfragen
        # und per MQTT veröffentlichen, damit HA sofort den tatsächlichen Zustand sieht.
        await fetch_and_publish_state(client, cfg, timeout_s)
    except Exception as exc:
        LOG.error("Schalten von '%s' Kanal %d fehlgeschlagen: %s", cfg.id, channel, exc)


# ---------------------------------------------------------------------------
# Service-Zusammenbau
# ---------------------------------------------------------------------------

class ShellyService:
    def __init__(
        self,
        devices: list[ShellyDeviceConfig],
        config_store,
        app_config: appconfig.AppConfig,
        service_name: str,
    ):
        self.devices = devices
        self.by_id = {d.id: d for d in devices}
        self.config_store = config_store
        self.app_config = app_config
        self.service_name = service_name
        self.service_config = app_config.service(service_name)
        self.mqtt_config = app_config.mqtt
        self.node_device_id = app_config.dashboard.node_device_id
        self.base_topic = f"outstation/{self.service_config.service_id}"
        self.client: Optional[mqtt_client.Client] = None
        self.loop: Optional[asyncio.AbstractEventLoop] = None
        self.slave: Optional[Slave] = None
        self._discovery_done = False
        self.simulated_state = {
            device.unique_id: new_simulated_state(device) for device in devices
        }
        # Stammdaten aus dem Diagnose-Poll (Shelly.GetDeviceInfo bzw. Gen1
        # /settings) und der Hash des zuletzt veroeffentlichten Geraeteblocks
        # pro Geraet - nur bei Aenderung wird die Discovery erneut publiziert.
        self.device_info: dict[str, ShellyDeviceInfo] = {}
        self._published_block_hash: dict[str, str] = {}

    def _current_device_block(self, cfg: ShellyDeviceConfig) -> dict:
        return device_block(cfg, self.node_device_id, self.device_info.get(cfg.unique_id))

    @staticmethod
    def _block_hash(dev_block: dict) -> str:
        return json.dumps(dev_block, sort_keys=True, ensure_ascii=False)

    def _publish_all_discovery(self, cfg: ShellyDeviceConfig, dev_block: dict) -> None:
        publish_device_discovery(self.client, cfg, self.node_device_id, dev_block)
        publish_simulation_discovery(self.client, cfg, self.node_device_id, dev_block)

    def _sync_device_block(self, cfg: ShellyDeviceConfig) -> dict:
        """Aktuellen Geraeteblock bestimmen und - falls er sich seit der
        letzten Discovery geaendert hat - alle Entities des Geraets damit neu
        anmelden. Gibt den Block zurueck."""
        dev_block = self._current_device_block(cfg)
        new_hash = self._block_hash(dev_block)
        previous = self._published_block_hash.get(cfg.unique_id)
        if previous is not None and previous != new_hash and self.client is not None:
            self._publish_all_discovery(cfg, dev_block)
        self._published_block_hash[cfg.unique_id] = new_hash
        return dev_block

    async def poll_diagnostics(self) -> None:
        targets = [
            cfg for cfg in self.devices
            if not self.slave.simulation_active_for(cfg.unique_id)
        ]
        results = await asyncio.gather(
            *(
                asyncio.to_thread(
                    fetch_device_info, cfg, self.service_config.http_timeout_s
                )
                for cfg in targets
            ),
            return_exceptions=True,
        )
        for cfg, result in zip(targets, results):
            if isinstance(result, Exception):
                LOG.warning(
                    "Shelly '%s' (%s): Stammdaten nicht abrufbar: %s",
                    cfg.id, cfg.host, result,
                )
                continue
            self.device_info[cfg.unique_id] = result
            self._sync_device_block(cfg)

    def reload_config(self) -> None:
        """config.json und die Geraetedatei gemeinsam neu laden.

        Zuerst beide Kandidaten laden, dann beide uebernehmen: ein Fehler
        in einer der beiden Dateien laesst beide unveraendert, und der
        Slave meldet runtime_status: rejected mit dem Grund.
        """
        if self.config_store is None:
            raise RuntimeError("configuration reload is not configured")

        new_app_config = appconfig.load(self.app_config.path)
        new_service_config = new_app_config.service(self.service_name)
        new_configs = self.config_store.load_candidate()

        self.app_config = new_app_config
        self.service_config = new_service_config
        self.mqtt_config = new_app_config.mqtt
        self.node_device_id = new_app_config.dashboard.node_device_id
        self.config_store.commit(new_configs)
        logging.getLogger().setLevel(new_app_config.log_level)
        if self.slave is not None:
            self.slave.apply_config_defaults(
                poll_interval_s=new_service_config.poll_interval_s,
                diagnostic_multiplier=new_service_config.diagnostic_poll_multiplier,
            )
        if self.loop is None or not self.loop.is_running():
            raise RuntimeError("service event loop is not running")
        future = asyncio.run_coroutine_threadsafe(
            self._replace_devices(new_configs), self.loop
        )
        future.result(timeout=10)

    async def _replace_devices(self, new_configs: list[ShellyDeviceConfig]) -> None:
        old_topics = {
            f"{device.base_topic}/relay/{channel}/set"
            for device in self.devices
            for channel in range(device.switch_channels)
        }
        new_topics = {
            f"{device.base_topic}/relay/{channel}/set"
            for device in new_configs
            for channel in range(device.switch_channels)
        }
        self.devices = new_configs
        self.by_id = {device.id: device for device in new_configs}
        previous_state = self.simulated_state
        self.simulated_state = {}
        for cfg in new_configs:
            new_state = new_simulated_state(cfg)
            old_state = previous_state.get(cfg.unique_id)
            if old_state is not None:
                for ch in range(min(len(old_state.relays), len(new_state.relays))):
                    new_state.relays[ch] = old_state.relays[ch]
                for ch in range(min(len(old_state.energy_wh), len(new_state.energy_wh))):
                    new_state.energy_wh[ch] = old_state.energy_wh[ch]
                new_state.last_update_ts = old_state.last_update_ts
            self.simulated_state[cfg.unique_id] = new_state
        new_ids = {cfg.unique_id for cfg in new_configs}
        self.device_info = {
            uid: info for uid, info in self.device_info.items() if uid in new_ids
        }
        self._published_block_hash = {}
        self.slave.register_devices(
            [device.unique_id for device in new_configs], self.client
        )
        if self.client is None:
            return
        for topic in old_topics - new_topics:
            self.client.unsubscribe(topic)
        for cfg in new_configs:
            dev_block = self._current_device_block(cfg)
            publish_device_discovery(self.client, cfg, self.node_device_id, dev_block)
            publish_simulation_discovery(self.client, cfg, self.node_device_id, dev_block)
            self._published_block_hash[cfg.unique_id] = self._block_hash(dev_block)
            for channel in range(cfg.switch_channels):
                self.client.subscribe(f"{cfg.base_topic}/relay/{channel}/set")

    async def poll_core(self) -> None:
        await asyncio.gather(
            *(
                poll_one_device(
                    self.client,
                    device,
                    self.service_config.http_timeout_s,
                    self.slave.simulation_active_for(device.unique_id),
                    self.simulated_state[device.unique_id],
                )
                for device in self.devices
            )
        )
        self.slave.note_update(self.client)

    def on_connect(self, client, userdata, flags, reason_code, properties=None):
        # Läuft im MQTT-Netzwerk-Thread (paho loop_start()), NICHT im Thread
        # der asyncio-Loop (self.loop läuft per run_forever() im Hauptthread).
        # Alles, was den Slave/AsyncScheduler anfasst, muss deshalb
        # thread-sicher auf die Loop geplant werden - sonst können intern
        # RuntimeErrors auftreten, die paho ohne aktiven Logger schluckt.
        LOG.info("MQTT verbunden (rc=%s)", reason_code)
        self.loop.call_soon_threadsafe(self._handle_connect, client)

    def _handle_connect(self, client) -> None:
        try:
            mqtt.publish_online_status(client, self.base_topic, online=True, reason="connected")

            self.slave.start(client)
            LOG.info("Slave gestartet")

            if not self._discovery_done:
                for cfg in self.devices:
                    dev_block = self._current_device_block(cfg)
                    publish_device_discovery(client, cfg, self.node_device_id, dev_block)
                    publish_simulation_discovery(client, cfg, self.node_device_id, dev_block)
                    self._published_block_hash[cfg.unique_id] = self._block_hash(dev_block)
                self._discovery_done = True
                LOG.info("Discovery für %d Shelly-Geräte veröffentlicht", len(self.devices))

            for cfg in self.devices:
                for ch in range(cfg.switch_channels):
                    client.subscribe(f"{cfg.base_topic}/relay/{ch}/set")
        except Exception:
            LOG.exception("Fehler in _handle_connect")

    def on_message(self, client, userdata, msg):
        topic = msg.topic
        payload = msg.payload.decode(errors="replace")
        self.loop.call_soon_threadsafe(self._handle_message, client, topic, payload)

    def _handle_message(self, client, topic: str, payload: str) -> None:
        try:
            if self.slave.handle_message(client, topic, payload):
                return

            for cfg in self.devices:
                for ch in range(cfg.switch_channels):
                    if topic == f"{cfg.base_topic}/relay/{ch}/set":
                        on = payload.strip().upper() in ("ON", "1", "TRUE")
                        # wir sind hier bereits im Loop-Thread (call_soon_threadsafe),
                        # daher reicht ensure_future statt run_coroutine_threadsafe
                        asyncio.ensure_future(
                            apply_relay_command(
                                client,
                                cfg,
                                ch,
                                on,
                                self.service_config.http_timeout_s,
                                self.slave.simulation_active_for(cfg.unique_id),
                                self.simulated_state[cfg.unique_id],
                            )
                        )
                        return
        except Exception:
            LOG.exception("Fehler in _handle_message (topic=%s)", topic)

    def on_poll_error(self, exc: Exception) -> None:
        LOG.error("Scheduler-Fehler im Shelly-Service: %s", exc)

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
        self.slave.register_devices([device.unique_id for device in self.devices])

        self.client = mqtt.build_client(
            client_id=f"{self.service_config.service_id}-service",
            host=self.mqtt_config.host,
            port=self.mqtt_config.port,
            user=self.mqtt_config.username,
            password=self.mqtt_config.password(),
            will_topic=f"{self.base_topic}/status/online",
        )
        # WICHTIG: ohne aktiven Logger verschluckt paho-mqtt Exceptions aus
        # on_connect/on_message stillschweigend (nur intern geloggt, sonst
        # nirgends sichtbar). Das hat den ursprünglichen Bug maskiert.
        self.client.enable_logger(LOG)
        self.client.on_connect = self.on_connect
        self.client.on_message = self.on_message

        self.client.connect(self.mqtt_config.host, self.mqtt_config.port)
        self.client.loop_start()

        try:
            self.loop.run_forever()
        finally:
            self.slave.stop()
            mqtt.publish_online_status(self.client, self.base_topic, online=False, reason="shutdown")
            self.client.loop_stop()
            self.client.disconnect()


def main() -> None:
    try:
        config = appconfig.load(appconfig.config_path_from_argv())
    except appconfig.ConfigError as exc:
        print(f"Konfigurationsfehler: {exc}", file=sys.stderr)
        raise SystemExit(1)

    logging.basicConfig(level=config.log_level, format="%(asctime)s %(levelname)s %(name)s: %(message)s")
    devices_path = config.devices_config("shelly")
    config_store = common_config.ReloadableConfig(devices_path, load_devices)
    devices = config_store.load()
    LOG.info("Geladen: %d Shelly-Geräte aus %s", len(devices), devices_path)
    ShellyService(devices, config_store, config, "shelly").run()


if __name__ == "__main__":
    main()