#!/usr/bin/env python3
"""
trucki_http_mqtt.py

HTTP-Poll-Bruecke fuer Trucki-Sticks (Community-Firmware auf den
Lumentree/Growatt-WLAN-Sticks), als Ersatz fuer die eingebaute MQTT-Funktion
des Sticks. Deckt alle drei bekannten Trucki-Varianten ab:
  - T2SG: Nulleinspeisung (Zero-Export)
  - T2MG: Ueberschussladen
  - T2HG: Ueberschussladen

Diese Bruecke unterscheidet die Varianten NICHT per Sonderfall-Code: sie
veroeffentlicht JEDES gefundene Feld dynamisch (siehe FIELD_HINTS unten),
statt eine feste Feldliste je Variante zu pflegen. Dadurch funktioniert
dieselbe Implementierung fuer alle drei Varianten, auch wenn sich ihre
Feldnamen teilweise unterscheiden (z. B. hat nur T2MG/T2HG einen DC-seitigen
Batteriestrom IOUT).

HINTERGRUND
-----------
Die Trucki-Firmware bringt einen eigenen MQTT-Client inkl. HA-Discovery mit
(siehe knowhow/trucki_und_netz_shellys_checkliste.md). Der Stick published
darueber aber offenbar sehr haeufig (bei praktisch jeder Wertaenderung), was
den Broker/HA unnoetig belastet und sich am Stick selbst nicht konfigurierbar
drosseln laesst. Dieser Service ersetzt diesen Publish-Pfad durch aktives
Abfragen: er holt sich den Status der Web-UI des Sticks per HTTP GET in einem
selbst kontrollierten Intervall (siehe TRUCKI_POLL_INTERVAL_S) und
veroeffentlicht daraus GENAU EINEN Zustand pro Zyklus - analog zu
shelly_rpc_mqtt.py, das aus demselben Grund (Verdraengung der Shelly-Cloud
durch zu haeufiges MQTT) fuer Shellys entstanden ist.

HTTP-Endpunkte (per Mitschnitten in templates/ bestaetigt, siehe
knowhow/trucki-stick-funktionsweise.md)
---------------------------------------------------------------------------
Die Web-UI des Sticks ruft selbst zwei flache JSON-Endpunkte ab (kein
Klartext-Dump, keine gerenderte HTML-Seite):

  GET /jsonlive   - Live-Messwerte, von der UI sekuendlich abgefragt
  GET /jsononce   - Konfiguration/Stammdaten, von der UI einmal beim Laden
                     abgefragt (u. a. DEVICENAME, VERSION, MINPOWER/MAXPOWER,
                     ZEPCTARGET, VBATCUTOFF, ... sowie Passwortfelder)

Diese Bruecke fragt `/jsonlive` im normalen Poll-Zyklus ab (Kernwerte,
TRUCKI_POLL_INTERVAL_S) und `/jsononce` als Diagnose-Poll deutlich seltener
(TRUCKI_POLL_INTERVAL_S * TRUCKI_DIAGNOSTIC_MULTIPLIER, wie bei den anderen
Bridges auch), zusaetzlich einmal beim Verbindungsaufbau. Passwortfelder aus
`/jsononce` (ADMIN_PASS, METER_PASS, MQTT_PASS, WIFIPASS, BEARER) werden nie
veroeffentlicht.

Ausdruecklich READ-ONLY: die Firmware nimmt Schreibzugriffe ueber
`GET /?KEY=VALUE&...&save=true` (Konfiguration), `GET /?reboot=true` (Neustart)
und `GET /?zepc_enable=enable` (Nulleinspeisung ein/aus) entgegen - all das
implementiert diese Bruecke bewusst nicht, passend zum "reines Monitoring"-
Wunsch aus der Checkliste. Kann bei Bedarf spaeter als eigener, bewusst
gesicherter Schreibpfad ergaenzt werden (siehe Erweiterungsideen in
knowhow/trucki-stick-funktionsweise.md).

Verhaeltnis zu battery_soc_mqtt.py: Trucki-Stick und Batterie-SoC sind zwei
eigenstaendige Home-Assistant-Geraete (kein Discovery-Merge). Sitzen Stick und
Batteriebank physisch im selben Geraet, wird das stattdessen ueber
`via_device` abgebildet: `via_device` in trucki_devices.json auf die `id`
des zugehoerigen Eintrags in battery_soc_devices.json setzen (z. B.
`"battery_soc"`), dann erscheint der Trucki-Stick in Home Assistant als
"Verbunden ueber" das Batterie-SoC-Geraet.

Nutzt das gemeinsame energy_node_common-Paket (Slave-Seite des Settings-Protokolls) fuer MQTT-Aufbau, Availability, Discovery-Publishing und
den asyncio-Poll-Scheduler (inkl. des Diagnose-Poll-Hooks fuer /jsononce).
"""

from __future__ import annotations

import asyncio
import json
import logging
import math
import os
import re
import time
from dataclasses import dataclass, field
from typing import Optional, Union

import requests
import paho.mqtt.client as mqtt_client

from energy_node_common import Slave, mqtt, discovery
from energy_node_common import config as common_config
from energy_node_common import appconfig

LOG = logging.getLogger("trucki_http_mqtt")

_KNOWN_MODELS = ("T2SG", "T2MG", "T2HG")


# ---------------------------------------------------------------------------
# Geraete-Konfiguration
# ---------------------------------------------------------------------------

@dataclass
class TruckiDeviceConfig:
    id: str  # eindeutig, wird MQTT-Topic-Root und HA device_id
    name: str  # Anzeigename in Home Assistant
    host: str  # IP oder Hostname des Sticks
    port: int = 80
    live_path: str = "/jsonlive"
    once_path: str = "/jsononce"
    auth_user: str = ""
    auth_password: str = ""
    via_device: str = "energy-node"

    @property
    def base_topic(self) -> str:
        return f"outstation/{self.id}"

    @property
    def auth(self) -> Optional[tuple]:
        if self.auth_user:
            return (self.auth_user, self.auth_password)
        return None

    def url(self, path: str) -> str:
        return f"http://{self.host}:{self.port}{path}"


def load_devices(path: str) -> list[TruckiDeviceConfig]:
    raw = common_config.load_json(path)
    if not isinstance(raw, list):
        raise ValueError(f"Geraete-Konfiguration {path} muss eine JSON-Liste sein")

    devices: list[TruckiDeviceConfig] = []
    for index, item in enumerate(raw):
        if not isinstance(item, dict):
            raise ValueError(f"Eintrag {index} in {path} ist kein Objekt")
        required = {"id", "name", "host"}
        missing = required - set(item.keys())
        if missing:
            raise ValueError(f"Eintrag {index} in {path} fehlt Pflichtfelder: {sorted(missing)}")
        devices.append(
            TruckiDeviceConfig(
                id=str(item["id"]).strip(),
                name=str(item["name"]).strip(),
                host=str(item["host"]).strip(),
                port=int(item.get("port", 80)),
                live_path=str(item.get("live_path", "/jsonlive")).strip(),
                once_path=str(item.get("once_path", "/jsononce")).strip(),
                auth_user=str(item.get("auth_user", "")).strip(),
                auth_password=str(item.get("auth_password", "")),
                via_device=str(item.get("via_device", "energy-node")).strip(),
            )
        )

    ids = [d.id for d in devices]
    if len(ids) != len(set(ids)):
        raise ValueError(f"Doppelte Geraete-IDs in {path}: {ids}")
    return devices  # leere Liste ist erlaubt, solange noch kein Stick konfiguriert ist


# ---------------------------------------------------------------------------
# HTTP-Zugriff + Parsing der JSON-Endpunkte
# ---------------------------------------------------------------------------

_INTEGER_RE = re.compile(r"^-?\d+$")
_NUMBER_WITH_UNIT_RE = re.compile(r"^(-?\d+(?:\.\d+)?)\s+\S.*$")
# Passwortfelder aus /jsononce werden nie veroeffentlicht.
_SECRET_FIELD_RE = re.compile(r"PASS$")


@dataclass
class TruckiStatus:
    """Ergebnis von parse_live(): bereits kuratierte Live-Felder (Spiegel-
    und leere Override-Felder ausgefiltert, siehe parse_live())."""
    fields: dict[str, Union[float, int, str]] = field(default_factory=dict)


@dataclass
class TruckiInfo:
    """Ergebnis von parse_once(): Stammdaten plus kuratierte Konfig-Felder
    (Passwortfelder ausgefiltert, siehe parse_once())."""
    devicename: Optional[str]
    version: Optional[str]
    fields: dict[str, Union[float, int, str]] = field(default_factory=dict)


def fetch_json(cfg: TruckiDeviceConfig, path: str, timeout_s: float) -> dict:
    resp = requests.get(cfg.url(path), auth=cfg.auth, timeout=timeout_s)
    resp.raise_for_status()
    return resp.json()


def _coerce_value(raw) -> Union[float, int, str]:
    """Wandelt einen JSON-Rohwert in Zahl oder Text.

    Die Firmware liefert manche Messwerte direkt als Zahl (VGRID: 234.2),
    manche als Zahl-in-Anfuehrungszeichen (METER: "21.95") und manche mit
    angehaengter Einheit (ACPOWER: "880.10 W", FAULT: " 0"). Reiner Text
    (WIFI: "WIFI CONNECTED", ZEPC: "(ENABLED) 1", METERURL: "http://...")
    bleibt unveraendert."""
    if isinstance(raw, (int, float)):
        return raw
    if not isinstance(raw, str):
        return raw
    text = raw.strip()
    if text == "":
        return ""
    if _INTEGER_RE.match(text):
        return int(text)
    try:
        return float(text)
    except ValueError:
        pass
    match = _NUMBER_WITH_UNIT_RE.match(text)
    if match:
        try:
            return float(match.group(1))
        except ValueError:
            pass
    return text


def parse_live(payload: dict) -> TruckiStatus:
    """Parst /jsonlive. Rund die Haelfte der Rohfelder (`MQTT_<X>_VALUE` /
    `MQTT_<X>OVR_VALUE`) sind reine Spiegel eines Klarnamen-Feldes
    (z. B. MQTT_VGRID_VALUE == VGRID) oder leere Schreib-Override-Eingaben -
    beide werden hier verworfen, um keine doppelten/leeren HA-Entities zu
    erzeugen. Ausnahmen ohne Klarnamen-Pendant: MQTT_STATE_VALUE/
    MQTT_ZEPC_VALUE werden als eigene Felder `state`/`zepc` uebernommen."""
    fields: dict[str, Union[float, int, str]] = {}
    for key, raw in payload.items():
        value = _coerce_value(raw)
        if value == "":
            continue
        if key == "MQTT_STATE_VALUE":
            fields["state"] = value
            continue
        if key == "MQTT_ZEPC_VALUE":
            fields["zepc"] = value
            continue
        if key.startswith("MQTT_") and key.endswith("_VALUE"):
            continue
        fields[key] = value
    return TruckiStatus(fields=fields)


def parse_once(payload: dict) -> TruckiInfo:
    """Parst /jsononce. Passwortfelder werden nie in `fields` uebernommen."""
    devicename = payload.get("DEVICENAME")
    version = payload.get("VERSION")
    fields: dict[str, Union[float, int, str]] = {}
    for key, raw in payload.items():
        if key in ("DEVICENAME", "VERSION"):
            continue
        if _SECRET_FIELD_RE.search(key) or key == "BEARER":
            continue
        value = _coerce_value(raw)
        if value == "":
            continue
        fields[key] = value
    return TruckiInfo(
        devicename=str(devicename).strip() if devicename else None,
        version=str(version).strip() if version else None,
        fields=fields,
    )


def model_from_devicename(devicename: Optional[str]) -> Optional[str]:
    if not devicename:
        return None
    upper = devicename.upper()
    for model in _KNOWN_MODELS:
        if upper.startswith(model):
            return model
    return None


# ---------------------------------------------------------------------------
# Feld-Metadaten fuer die HA-Discovery
#
# Anzeigename/Einheit/device_class stammen aus den `unit_*`-Angaben der
# Firmware-Weboberflaeche selbst (templates/T2SGA55A69.html,
# templates/T2MG81A4E9.html - Mitschnitte der echten Sticks), nicht aus einer
# Vermutung. Unbekannte/neue Felder (z. B. vom bislang unbeobachteten T2HG)
# werden als generischer, standardmaessig deaktivierter Diagnose-Sensor ohne
# Einheit veroeffentlicht - diese Tabelle bei Bedarf ergaenzen, sobald ihre
# reale Bedeutung vor Ort bestaetigt ist.
# ---------------------------------------------------------------------------

FIELD_HINTS: dict[str, tuple[str, Optional[str], Optional[str], Optional[str]]] = {
    # Live-Messwerte, beide Varianten (/jsonlive)
    "VGRID": ("Netzspannung", "V", "voltage", "measurement"),
    "VBAT": ("Batteriespannung", "V", "voltage", "measurement"),
    "TEMP": ("Temperatur", "°C", "temperature", "measurement"),
    "SETACPOWER": ("AC-Sollwert", "W", "power", "measurement"),
    "ACPOWER": ("AC-Leistung", "W", "power", "measurement"),
    "POWERLIMIT": ("Leistungsgrenze", "W", "power", "measurement"),
    "ZEPCPOWER": ("Nulleinspeisungs-Leistung", "W", "power", "measurement"),
    "ZEPCTARGET": ("ZEPC-Zielwert", "W", "power", "measurement"),
    "METERPOWER": ("Zaehlerleistung", "W", "power", "measurement"),
    "DAYENERGY": ("Tagesenergie", "kWh", "energy", "total_increasing"),
    "TOTALENERGY": ("Gesamtenergie", "kWh", "energy", "total_increasing"),
    "METERDAYENERGY": ("Zaehler-Tagesenergie", "kWh", "energy", "total_increasing"),
    "METERREADOUT": ("Zaehler-Auslesezeit", "ms", "duration", "measurement"),
    "SUN2ROUNDTRIP": ("Roundtrip-Zeit (SUN2)", "ms", "duration", "measurement"),
    "SUN3ROUNDTRIP": ("Roundtrip-Zeit (SUN3)", "ms", "duration", "measurement"),
    "SUN2SETPOINT": ("AC-Sollwert (SUN2)", "W", "power", "measurement"),
    "SUN3SETPOINT": ("AC-Sollwert (SUN3)", "W", "power", "measurement"),
    "SUN2POWERLIMIT": ("Leistungsgrenze (SUN2)", "W", "power", "measurement"),
    "SUN3POWERLIMIT": ("Leistungsgrenze (SUN3)", "W", "power", "measurement"),
    "SUN2MINPOWER": ("Minimalleistung (SUN2)", "W", "power", "measurement"),
    "SUN3MINPOWER": ("Minimalleistung (SUN3)", "W", "power", "measurement"),
    # Nur T2MG/T2HG (Ueberschussladen)
    "DCPOWER": ("DC-Leistung", "W", "power", "measurement"),
    "SETDCPOWER": ("DC-Sollwert", "W", "power", "measurement"),
    "IOUT": ("Batteriestrom", "A", "current", "measurement"),
    "IOUTSET": ("Batteriestrom (Soll)", "A", "current", "measurement"),
    "VOUTSET": ("Batteriespannung (Soll)", "V", "voltage", "measurement"),
    "IOUTOFFLINE": ("Offline-Strom", "A", "current", "measurement"),
    "VOUTOFFLINE": ("Offline-Spannung", "V", "voltage", "measurement"),
    "MWEFFICIENCY": ("Wirkungsgrad", "%", None, "measurement"),
    "NEXTUPDATE": ("Naechstes Update", "s", "duration", "measurement"),
    # Diagnose-Felder ohne Einheit, aber mit bekannter Bedeutung (siehe
    # _DIAGNOSTIC_KNOWN_FIELDS - landen in der Diagnostics-Kategorie statt als
    # Haupt-Sensor; die laufend nuetzlichen bleiben aktiviert, die selten
    # gebrauchten (_DIAGNOSTIC_HIDDEN_FIELDS) werden standardmaessig deaktiviert)
    "UPTIME": ("Betriebszeit", None, None, None),
    "TIME": ("Uhrzeit", None, None, None),
    "WIFI": ("WLAN-Status", None, None, None),
    "RSSI": ("WLAN-Signalqualitaet", None, None, None),
    "LOCALIP": ("Lokale IP", None, None, None),
    "BSSID": ("Access-Point MAC", None, None, None),
    "FAULT": ("Fehlercode", None, None, None),
    "CHGSTATUS": ("Ladestatus-Code", None, None, None),
    "SYSSTATUS": ("Systemstatus-Code", None, None, None),
    "SYSCONFIG": ("Systemkonfiguration", None, None, None),
    "CURVECFG": ("Kurvenkonfiguration", None, None, None),
    "MWPCSTATE": ("MW-Regelzustand", None, None, None),
    "DAC": ("DAC-Wert", None, None, None),
    "CALSTEP": ("Kalibrierschritt", None, None, None),
    "state": ("Betriebsstatus", None, None, None),
    "zepc": ("Nulleinspeisung Status", None, None, None),
    # Konfiguration/Stammdaten (/jsononce): immer entity_category diagnostic
    # UND standardmaessig deaktiviert - die Konfig-Spiegel (~40 Stueck je
    # Stick) haben die Dashboard-Diagnose zugemuellt, ohne im Alltag gebraucht
    # zu werden. Nutzer aktiviert in Home Assistant gezielt, was er braucht.
    # Gilt fuer bekannte wie unbekannte /jsononce-Felder, siehe
    # publish_field_discovery().
    "MINPOWER": ("Minimalleistung", "W", "power", None),
    "MAXPOWER": ("Maximalleistung", "W", "power", None),
    "NIGHT_MAXPOWER": ("Maximalleistung (Nacht)", "W", "power", None),
    "VBATCUTOFF": ("Batteriespannung Abschaltung", "V", "voltage", None),
    "VBATREBOOT": ("Batteriespannung Neustart", "V", "voltage", None),
    "VBATLOW": ("Batteriespannung niedrig", "V", "voltage", None),
    "VBATNORMAL": ("Batteriespannung normal", "V", "voltage", None),
    "VBATFULL": ("Batteriespannung voll", "V", "voltage", None),
    "TARGETFULL": ("Zielwert bei Vollladung", "W", "power", None),
    "ZEPCTARGETMIN": ("ZEPC-Zielwert Minimum", "W", "power", None),
    "ZEPCTARGETMAX": ("ZEPC-Zielwert Maximum", "W", "power", None),
    "ZEPCSTARTDELAY": ("ZEPC-Startverzoegerung", "s", "duration", None),
    "STANDBY": ("Standby-Zeit", "s", "duration", None),
    "MWINTERVAL": ("Update-Intervall", "s", "duration", None),
    "FULLDELAY": ("Vollladungs-Verzoegerung", "min", None, None),
    "METERINTERVAL": ("Zaehler-Abfrageintervall", "ms", "duration", None),
    "METERURL": ("Zaehler-URL", None, None, None),
    "ZEPCAVERAGE": ("ZEPC-Mittelungsfenster", None, None, None),
    "HADISCOVERY": ("HA-Discovery (am Stick)", None, None, None),
    "MQTTREADONLY": ("MQTT Readonly (am Stick)", None, None, None),
}

# Bekannte Felder ohne Einheit, die trotzdem in die Diagnostics-Kategorie
# gehoeren statt als Haupt-Sensor. Standardmaessig aktiviert, ausser den in
# _DIAGNOSTIC_HIDDEN_FIELDS gelisteten.
_DIAGNOSTIC_KNOWN_FIELDS = frozenset(
    {
        "UPTIME", "TIME", "WIFI", "RSSI", "LOCALIP", "BSSID",
        "FAULT", "CHGSTATUS", "SYSSTATUS", "SYSCONFIG", "CURVECFG",
        "MWPCSTATE", "DAC", "CALSTEP", "state", "zepc",
    }
)

# Teilmenge von _DIAGNOSTIC_KNOWN_FIELDS: bekannt, aber im Alltag selten
# gebraucht - als Entity angelegt, in Home Assistant aber standardmaessig
# deaktiviert. Sichtbar bleiben nur die laufend nuetzlichen Statusfelder
# (state, zepc, UPTIME, TIME, WIFI, RSSI).
_DIAGNOSTIC_HIDDEN_FIELDS = frozenset(
    {
        "LOCALIP", "BSSID", "FAULT", "CHGSTATUS", "SYSSTATUS",
        "SYSCONFIG", "CURVECFG", "MWPCSTATE", "DAC", "CALSTEP",
    }
)


def field_hint(key: str) -> tuple[str, Optional[str], Optional[str], Optional[str]]:
    return FIELD_HINTS.get(key, (key, None, None, None))


def is_known_field(key: str) -> bool:
    return key in FIELD_HINTS


# ---------------------------------------------------------------------------
# Discovery
# ---------------------------------------------------------------------------

def device_block(
    cfg: TruckiDeviceConfig,
    model: Optional[str] = None,
    serial: Optional[str] = None,
    sw_version: Optional[str] = None,
) -> dict:
    block = {
        "name": cfg.name,
        "identifiers": [cfg.id],
        "manufacturer": "Trucki (Community-Firmware)",
        "model": model or "T2SG/T2MG/T2HG",
        "via_device": cfg.via_device,
    }
    if serial:
        block["serial_number"] = serial
    if sw_version:
        block["sw_version"] = sw_version
    return block


def publish_base_discovery(
    client: mqtt_client.Client, cfg: TruckiDeviceConfig, dev_block: Optional[dict] = None
) -> None:
    """Veroeffentlicht die Verfuegbarkeits-Entity. Wird nach jedem
    erfolgreichen /jsononce-Poll erneut mit aktuellem Geraeteblock (Modell/
    Seriennummer/Firmware) aufgerufen, damit HA das Geraet aktuell haelt."""
    discovery.publish_availability_discovery(
        client, cfg.id, cfg.base_topic, dev_block or device_block(cfg)
    )


def publish_field_discovery(
    client: mqtt_client.Client,
    cfg: TruckiDeviceConfig,
    dev_block: dict,
    key: str,
    config_field: bool = False,
) -> None:
    object_id = f"cfg_{key.lower()}" if config_field else key.lower()
    name, unit, device_class, state_class = field_hint(key)
    kwargs: dict = {}
    if unit:
        kwargs["unit_of_measurement"] = unit
    if device_class:
        kwargs["device_class"] = device_class
    if state_class:
        kwargs["state_class"] = state_class
    if config_field:
        kwargs["entity_category"] = "diagnostic"
        kwargs["enabled_by_default"] = False
    elif key in _DIAGNOSTIC_KNOWN_FIELDS:
        kwargs["entity_category"] = "diagnostic"
        if key in _DIAGNOSTIC_HIDDEN_FIELDS:
            kwargs["enabled_by_default"] = False
    elif not is_known_field(key):
        kwargs["entity_category"] = "diagnostic"
        kwargs["enabled_by_default"] = False

    topic_namespace = "config" if config_field else "field"
    cfg_payload = discovery.entity_config(
        device_id=cfg.id,
        base_topic=cfg.base_topic,
        object_id=object_id,
        name=f"{cfg.name} {name}",
        device_block=dev_block,
        state_topic=f"{cfg.base_topic}/{topic_namespace}/{key.lower()}",
        **kwargs,
    )
    discovery.publish_discovery(client, cfg.id, "sensor", object_id, cfg_payload)


# ---------------------------------------------------------------------------
# Simulation
#
# Variante wird an der Geraete-id festgemacht (die realen Sticks heissen
# nach ihrer Seriennummer, z. B. "t2sga55a69"/"t2mg81a4e9" - siehe
# trucki_devices.json), damit auch ohne vorherigen /jsononce-Poll ein
# realistischer Feldsatz simuliert wird.
# ---------------------------------------------------------------------------

def _simulated_variant(cfg: TruckiDeviceConfig) -> str:
    lowered = cfg.id.lower()
    if "t2mg" in lowered or "t2hg" in lowered:
        return "T2MG"
    return "T2SG"


def simulated_status(cfg: TruckiDeviceConfig) -> TruckiStatus:
    now = time.time()
    offset = hash(cfg.id) % 100 / 100.0
    vgrid = round(230 + 5 * math.sin(now / 120 + offset), 1)
    vbat = round(26.5 + 0.3 * math.sin(now / 600 + offset), 2)
    temp = round(45 + 5 * math.sin(now / 300 + offset), 1)
    setacpower = 200.0
    acpower = round(setacpower + 5 * math.sin(now / 90 + offset), 2)
    fields: dict[str, Union[float, int, str]] = {
        "VGRID": vgrid,
        "VBAT": vbat,
        "TEMP": temp,
        "SETACPOWER": setacpower,
        "ACPOWER": acpower,
        "POWERLIMIT": 1000,
        "ZEPCPOWER": round(setacpower + 0.3 * math.sin(now / 90 + offset), 2),
        "DAYENERGY": round(0.4 + offset, 2),
        "TOTALENERGY": round(1000 + offset * 100, 1),
        "METERPOWER": round(20 * math.sin(now / 200 + offset), 2),
        "state": "ON",
        "zepc": "(ENABLED) 1",
    }
    if _simulated_variant(cfg) == "T2MG":
        fields.update(
            {
                "DCPOWER": round(acpower * 0.95, 2),
                "SETDCPOWER": round(setacpower * 0.95, 2),
                "IOUT": round(12 + 0.5 * math.sin(now / 150 + offset), 2),
                "IOUTSET": 12.5,
                "VOUTSET": 28.8,
                "MWEFFICIENCY": 95,
                "FAULT": 0,
            }
        )
    return TruckiStatus(fields=fields)


def simulated_info(cfg: TruckiDeviceConfig) -> TruckiInfo:
    variant = _simulated_variant(cfg)
    fields: dict[str, Union[float, int, str]] = {"MINPOWER": 0, "MAXPOWER": 1000}
    if variant == "T2MG":
        fields.update({"ZEPCTARGETMIN": 20, "ZEPCTARGETMAX": -50})
    else:
        fields.update({"ZEPCTARGET": 10, "NIGHT_MAXPOWER": 300})
    return TruckiInfo(
        devicename=f"SIM-{variant}-{cfg.id}", version="SIM 1.0", fields=fields
    )


# ---------------------------------------------------------------------------
# Poll-Logik pro Geraet
# ---------------------------------------------------------------------------

async def fetch_and_publish_status(
    client: mqtt_client.Client,
    cfg: TruckiDeviceConfig,
    known_fields: set,
    timeout_s: float,
    simulation_active: bool = False,
    dev_block: Optional[dict] = None,
) -> None:
    """Fragt /jsonlive fuer ein einzelnes Geraet ab (oder simuliert es) und
    veroeffentlicht den Zustand. Neue Felder bekommen dabei automatisch eine
    Discovery-Entity; Fehler werden pro Geraet isoliert als offline
    markiert."""
    dev_block = dev_block or device_block(cfg)
    try:
        if simulation_active:
            status = simulated_status(cfg)
            reason = "Simulation aktiv"
        else:
            payload = await asyncio.to_thread(fetch_json, cfg, cfg.live_path, timeout_s)
            status = parse_live(payload)
            reason = "connected"

        new_keys = sorted(set(status.fields) - known_fields)
        for key in new_keys:
            publish_field_discovery(client, cfg, dev_block, key)
        known_fields.update(new_keys)

        for key, value in status.fields.items():
            mqtt.publish(client, f"{cfg.base_topic}/field/{key.lower()}", value)

        mqtt.publish_online_status(client, cfg.base_topic, online=True, reason=reason)

    except Exception as exc:
        LOG.warning("Trucki '%s' (%s) nicht erreichbar: %s", cfg.id, cfg.host, exc)
        mqtt.publish_online_status(client, cfg.base_topic, online=False, reason=str(exc))


async def fetch_and_publish_info(
    client: mqtt_client.Client,
    cfg: TruckiDeviceConfig,
    known_config_fields: set,
    timeout_s: float,
    simulation_active: bool = False,
) -> Optional[TruckiInfo]:
    """Diagnose-Poll gegen /jsononce: Stammdaten (fuer den Geraeteblock) plus
    Konfig-Felder als Diagnose-Entities. Gibt bei Erfolg die geparsten Infos
    zurueck (Aufrufer aktualisiert damit den zwischengespeicherten
    Geraeteblock fuer die naechsten /jsonlive-Zyklen), bei Fehler None.

    Die Verfuegbarkeits-Entity wird hier bewusst NICHT mehr angemeldet: das
    lief bislang in jedem Diagnose-Zyklus, und da der Broker Live-Publishes
    ohne Retain-Flag ausliefert, sah das Dashboard die Discovery dauerhaft
    als "nicht retained veroeffentlicht". Basis-Discovery laeuft jetzt nur
    noch beim Verbindungsaufbau und bei Geraeteblock-Aenderungen
    (TruckiService._sync_device_block)."""
    try:
        if simulation_active:
            info = simulated_info(cfg)
        else:
            payload = await asyncio.to_thread(fetch_json, cfg, cfg.once_path, timeout_s)
            info = parse_once(payload)

        dev_block = device_block(
            cfg,
            model=model_from_devicename(info.devicename),
            serial=info.devicename,
            sw_version=info.version,
        )

        new_keys = sorted(set(info.fields) - known_config_fields)
        for key in new_keys:
            publish_field_discovery(client, cfg, dev_block, key, config_field=True)
        known_config_fields.update(new_keys)

        for key, value in info.fields.items():
            mqtt.publish(client, f"{cfg.base_topic}/config/{key.lower()}", value)

        return info
    except Exception as exc:
        LOG.warning("Trucki '%s' (%s): /jsononce nicht abrufbar: %s", cfg.id, cfg.host, exc)
        return None


# ---------------------------------------------------------------------------
# Service-Zusammenbau
# ---------------------------------------------------------------------------

class TruckiService:
    def __init__(
        self,
        devices: list[TruckiDeviceConfig],
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
        self.known_fields: dict[str, set] = {device.id: set() for device in devices}
        self.known_config_fields: dict[str, set] = {device.id: set() for device in devices}
        self.device_info: dict[str, TruckiInfo] = {}
        self._initial_diagnostics_done: set[str] = set()
        # Hash des Geraeteblocks, mit dem die Discovery eines Geraets zuletzt
        # veroeffentlicht wurde - siehe _sync_device_block().
        self._published_block_hash: dict[str, str] = {}

    def _current_device_block(self, cfg: TruckiDeviceConfig) -> dict:
        info = self.device_info.get(cfg.id)
        if info is None:
            return device_block(cfg)
        return device_block(
            cfg,
            model=model_from_devicename(info.devicename),
            serial=info.devicename,
            sw_version=info.version,
        )

    @staticmethod
    def _block_hash(dev_block: dict) -> str:
        return json.dumps(dev_block, sort_keys=True, ensure_ascii=False)

    def _republish_discovery_for_block(
        self, cfg: TruckiDeviceConfig, dev_block: dict
    ) -> None:
        """Alle bereits bekannten Discovery-Entities eines Geraets mit
        aktualisiertem Geraeteblock neu veroeffentlichen. Modell/Seriennummer/
        Firmware stehen erst nach dem ersten /jsononce-Poll fest und muessen
        dann auch an die frueher (mit Platzhalter-Block) angelegten
        /jsonlive-Entities durchgereicht werden - sonst widersprechen sich
        Discovery-Payload und normalisiertes Entitaetsmodell im Dashboard."""
        if self.client is None:
            return
        publish_base_discovery(self.client, cfg, dev_block)
        self.slave.publish_simulation_discovery(
            self.client, cfg.id, dev_block, cfg.base_topic
        )
        for key in sorted(self.known_fields.get(cfg.id, set())):
            publish_field_discovery(self.client, cfg, dev_block, key)
        for key in sorted(self.known_config_fields.get(cfg.id, set())):
            publish_field_discovery(self.client, cfg, dev_block, key, config_field=True)

    def _sync_device_block(self, cfg: TruckiDeviceConfig) -> dict:
        """Aktuellen Geraeteblock bestimmen und - falls er sich seit der
        letzten Discovery geaendert hat - alle bekannten Entities damit neu
        anmelden. Gibt den Block fuer den weiteren Poll-Zyklus zurueck."""
        dev_block = self._current_device_block(cfg)
        new_hash = self._block_hash(dev_block)
        previous = self._published_block_hash.get(cfg.id)
        if previous is not None and previous != new_hash:
            self._republish_discovery_for_block(cfg, dev_block)
        self._published_block_hash[cfg.id] = new_hash
        return dev_block

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

    async def _replace_devices(self, new_configs: list[TruckiDeviceConfig]) -> None:
        self.devices = new_configs
        self.by_id = {device.id: device for device in new_configs}
        previous_fields = self.known_fields
        previous_config_fields = self.known_config_fields
        self.known_fields = {
            device.id: previous_fields.get(device.id, set()) for device in new_configs
        }
        self.known_config_fields = {
            device.id: previous_config_fields.get(device.id, set()) for device in new_configs
        }
        previous_block_hash = self._published_block_hash
        self._published_block_hash = {
            device.id: previous_block_hash[device.id]
            for device in new_configs
            if device.id in previous_block_hash
        }
        self.slave.register_devices([device.id for device in new_configs], self.client)
        if self.client is None:
            return
        for cfg in new_configs:
            dev_block = self._current_device_block(cfg)
            publish_base_discovery(self.client, cfg, dev_block)
            self.slave.publish_simulation_discovery(
                self.client, cfg.id, dev_block, cfg.base_topic
            )
            self._published_block_hash[cfg.id] = self._block_hash(dev_block)
            if cfg.id not in self._initial_diagnostics_done:
                asyncio.ensure_future(self._run_initial_diagnostics(cfg))
                self._initial_diagnostics_done.add(cfg.id)

    async def _run_initial_diagnostics(self, cfg: TruckiDeviceConfig) -> None:
        info = await fetch_and_publish_info(
            self.client,
            cfg,
            self.known_config_fields[cfg.id],
            self.service_config.http_timeout_s,
            self.slave.simulation_active_for(cfg.id),
        )
        if info is not None:
            self.device_info[cfg.id] = info
            self._sync_device_block(cfg)

    async def poll_core(self) -> None:
        await asyncio.gather(
            *(
                fetch_and_publish_status(
                    self.client,
                    device,
                    self.known_fields[device.id],
                    self.service_config.http_timeout_s,
                    self.slave.simulation_active_for(device.id),
                    dev_block=self._sync_device_block(device),
                )
                for device in self.devices
            )
        )
        self.slave.note_update(self.client)

    async def poll_diagnostics(self) -> None:
        results = await asyncio.gather(
            *(
                fetch_and_publish_info(
                    self.client,
                    device,
                    self.known_config_fields[device.id],
                    self.service_config.http_timeout_s,
                    self.slave.simulation_active_for(device.id),
                )
                for device in self.devices
            )
        )
        for device, info in zip(self.devices, results):
            if info is not None:
                self.device_info[device.id] = info
                self._sync_device_block(device)

    def on_connect(self, client, userdata, flags, reason_code, properties=None):
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
                    publish_base_discovery(client, cfg, dev_block)
                    self.slave.publish_simulation_discovery(
                        client, cfg.id, dev_block, cfg.base_topic
                    )
                    self._published_block_hash[cfg.id] = self._block_hash(dev_block)
                self._discovery_done = True
                LOG.info("Basis-Discovery fuer %d Trucki-Geraete veroeffentlicht", len(self.devices))

            for cfg in self.devices:
                if cfg.id not in self._initial_diagnostics_done:
                    asyncio.ensure_future(self._run_initial_diagnostics(cfg))
                    self._initial_diagnostics_done.add(cfg.id)
        except Exception:
            LOG.exception("Fehler in _handle_connect")

    def on_message(self, client, userdata, msg):
        topic = msg.topic
        payload = msg.payload.decode(errors="replace")
        self.loop.call_soon_threadsafe(self._handle_message, client, topic, payload)

    def _handle_message(self, client, topic: str, payload: str) -> None:
        try:
            self.slave.handle_message(client, topic, payload)
        except Exception:
            LOG.exception("Fehler in _handle_message (topic=%s)", topic)

    def on_poll_error(self, exc: Exception) -> None:
        LOG.error("Scheduler-Fehler im Trucki-Service: %s", exc)

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
        self.slave.register_devices([device.id for device in self.devices])

        self.client = mqtt.build_client(
            client_id=f"{self.service_config.service_id}-service",
            host=self.mqtt_config.host,
            port=self.mqtt_config.port,
            user=self.mqtt_config.username,
            password=self.mqtt_config.password(),
            will_topic=f"{self.base_topic}/status/online",
        )
        # Ohne aktiven Logger verschluckt paho-mqtt Exceptions aus
        # on_connect/on_message stillschweigend.
        self.client.enable_logger(LOG)
        self.client.on_connect = self.on_connect
        self.client.on_message = self.on_message

        self.client.connect(self.mqtt_config.host, self.mqtt_config.port)
        self.client.loop_start()

        LOG.info(
            "Starte Trucki-HTTP-Poll-Service fuer %d Geraet(e) (Intervall: %ss)",
            len(self.devices),
            self.service_config.poll_interval_s,
        )
        for cfg in self.devices:
            LOG.info("- %s @ %s:%s%s", cfg.id, cfg.host, cfg.port, cfg.live_path)

        try:
            self.loop.run_forever()
        finally:
            self.slave.stop()
            mqtt.publish_online_status(self.client, self.base_topic, online=False, reason="shutdown")
            self.client.loop_stop()
            self.client.disconnect()


def main() -> None:
    import sys
    try:
        config = appconfig.load(appconfig.config_path_from_argv())
    except appconfig.ConfigError as exc:
        print(f"Konfigurationsfehler: {exc}", file=sys.stderr)
        raise SystemExit(1)

    logging.basicConfig(level=config.log_level, format="%(asctime)s %(levelname)s %(name)s: %(message)s")
    devices_path = config.devices_config("trucki")
    config_store = common_config.ReloadableConfig(devices_path, load_devices)
    devices = config_store.load()
    LOG.info("Geladen: %d Trucki-Geraet(e) aus %s", len(devices), devices_path)
    TruckiService(devices, config_store, config, "trucki").run()


if __name__ == "__main__":
    main()
