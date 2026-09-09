#!/usr/bin/env python3
"""
energy_node_mqtt.py

Veröffentlicht den Raspberry-Pi-Knoten "Energy Node" selbst als eigenes
Home-Assistant-Gerät via MQTT Discovery (System-Diagnose-Sensoren:
CPU-Temperatur, Auslastung, RAM, Speicherplatz, Unterspannung/Throttling,
Tailscale-/Mosquitto-Status, verfügbare Updates, ...).

Der wichtige Punkt: Andere Skripte (AP Systems, Tuya, ...) können in ihrem
eigenen Discovery-"device"-Block das Feld

    "via_device": "energy_node"   (Standard-Geraete-ID dieses Knotens, aus dashboard.node_device_id der zentralen Config)

ergänzen. Dann zeigt Home Assistant sie in der Geräteliste als
"Verbunden über Energy Node" an, mit einem Klick von dort navigierbar.

Nutzt paho-mqtt v2 (CallbackAPIVersion.VERSION2), analog zu den anderen
Skripten auf diesem Knoten.
"""

import json
import os
import socket
import subprocess
import sys
import time
from pathlib import Path

import paho.mqtt.client as mqtt

from energy_node_common import Slave
from energy_node_common import appconfig
from energy_node_common.discovery import (
    availability_entity_config,
    discovery_topic,
    publish_discovery as common_publish_discovery,
)

# ---------------------------------------------------------------------------
# Konfiguration (aus zentraler Datei)
# ---------------------------------------------------------------------------

class NodeTopics:
    """Die vier Topics des Node, abgeleitet aus der Geraete-ID."""

    def __init__(self, device_id: str):
        self.base = f"outstation/{device_id}"
        self.state = f"{self.base}/state"
        self.diagnostics = f"{self.base}/diagnostics"
        self.availability = f"{self.base}/status/online"


def default_bridge_settings(device_id: str):
    """Voreingestellter Anzeigename und Poll-Intervall fuer bekannte Bridges.

    Der zweite Ruckgabewert wird nicht mehr benutzt (Poll-Intervalle kommen
    jetzt aus services.<name>.poll_interval_s), ist aber fuer die Testbarkeit
    und als Dokumentation der historischen Werte nützlich.
    """
    if device_id == "apsystems":
        return "APsystems EZ1", 60.0
    defaults = {
        "tuya": ("Tuya Service", 30.0),
        "battery_soc": ("Batterie-Ladezustand", 10.0),
        "shelly": ("Shelly Service", 20.0),
    }
    return defaults.get(device_id, (device_id, 60.0))


def managed_bridges(config):
    """Die je-Bridge-Liste fuer die Einmal-Abraeumung (Plan 0.1) - abgeleitet
    aus den in der config.json gepflegten Diensten. Nach dem Wegfall von
    node.managed_bridges (Plan 0.4) gibt es keine separate Liste mehr.

    Der Anzeigename kommt aus default_bridge_settings, das Poll-Intervall
    aus services.<name>.poll_interval_s - damit steht der Wert nur noch an
    einer Stelle.
    """
    bridges = []
    for service_id in sorted(config.services):
        name, _ = default_bridge_settings(service_id)
        bridges.append((service_id, name, config.service(service_id).poll_interval_s))
    return bridges


# ---------------------------------------------------------------------------
# System-Metriken
# ---------------------------------------------------------------------------
def read_cpu_temp_c():
    try:
        raw = Path("/sys/class/thermal/thermal_zone0/temp").read_text().strip()
        return round(int(raw) / 1000.0, 1)
    except Exception:
        return None


def read_cpu_load_pct():
    try:
        load1, _, _ = os.getloadavg()
        cpu_count = os.cpu_count() or 1
        return round(min(load1 / cpu_count * 100, 100.0), 1)
    except Exception:
        return None


def read_ram_used_pct():
    try:
        meminfo = {}
        for line in Path("/proc/meminfo").read_text().splitlines():
            key, val = line.split(":", 1)
            meminfo[key] = int(val.strip().split()[0])  # kB
        total = meminfo.get("MemTotal", 0)
        available = meminfo.get("MemAvailable", 0)
        if total == 0:
            return None
        return round((total - available) / total * 100, 1)
    except Exception:
        return None


def read_disk_used_pct(path="/"):
    try:
        usage = os.statvfs(path)
        total = usage.f_blocks * usage.f_frsize
        free = usage.f_bfree * usage.f_frsize
        if total == 0:
            return None
        return round((total - free) / total * 100, 1)
    except Exception:
        return None


def read_uptime_seconds():
    try:
        raw = Path("/proc/uptime").read_text().split()[0]
        return int(float(raw))
    except Exception:
        return None


def read_last_boot_iso():
    uptime_s = read_uptime_seconds()
    if uptime_s is None:
        return None
    boot_ts = time.time() - uptime_s
    return time.strftime("%Y-%m-%dT%H:%M:%S%z", time.localtime(boot_ts))


def read_throttled_flags():
    """vcgencmd get_throttled -> Bitmaske interpretieren.
    Besonders auf einem Pi 1 an einem Außenstandort mit ggf. langem
    Stromkabel/schwachem Netzteil sehr aufschlussreich."""
    try:
        out = subprocess.run(
            ["vcgencmd", "get_throttled"], capture_output=True, text=True, timeout=5
        ).stdout.strip()
        # Format: "throttled=0x50005"
        value = int(out.split("=")[1], 16)
        return {
            "undervoltage_now": bool(value & 0x1),
            "freq_capped_now": bool(value & 0x2),
            "throttled_now": bool(value & 0x4),
            "undervoltage_occurred": bool(value & 0x10000),
            "freq_capped_occurred": bool(value & 0x20000),
            "throttled_occurred": bool(value & 0x40000),
        }
    except Exception:
        return None


def read_wifi_signal_dbm(interface="wlan0"):
    try:
        for line in Path("/proc/net/wireless").read_text().splitlines()[2:]:
            parts = line.split()
            if parts and parts[0].rstrip(":") == interface:
                return float(parts[3])
        return None
    except Exception:
        return None


def read_ip_address():
    try:
        s = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
        s.connect(("1.1.1.1", 80))
        ip = s.getsockname()[0]
        s.close()
        return ip
    except Exception:
        return None


def read_systemd_active(unit):
    try:
        result = subprocess.run(
            ["systemctl", "is-active", unit], capture_output=True, text=True, timeout=5
        )
        return result.stdout.strip() == "active"
    except Exception:
        return None


def read_tailscale_connected():
    try:
        result = subprocess.run(
            ["tailscale", "status", "--json"], capture_output=True, text=True, timeout=5
        )
        data = json.loads(result.stdout)
        return bool(data.get("Self", {}).get("Online", False))
    except Exception:
        return None


# `apt list --upgradable` parst jedes Mal den kompletten APT-Paket-Cache und
# ist auf dem Pi 1 der teuerste Diagnose-Aufruf (siehe
# docs/knowledge/performance-and-resources.md 5.2). Der Diagnose-Zyklus ruft
# read_apt_updates_pending() weiterhin regelmaessig, echt ausgefuehrt wird der
# Aufruf aber hoechstens 1x/Tag - dazwischen kommt der zuletzt ermittelte Wert.
APT_UPDATES_TTL_S = 86_400

_apt_updates_cache = {"ts": 0.0, "value": None}


def _run_apt_updates_pending():
    try:
        result = subprocess.run(
            ["apt", "list", "--upgradable"], capture_output=True, text=True, timeout=30
        )
        lines = [l for l in result.stdout.splitlines() if "/" in l]
        return len(lines)
    except Exception:
        return None


def read_apt_updates_pending(now=None):
    now = time.time() if now is None else now
    cached = _apt_updates_cache["value"]
    if cached is not None and (now - _apt_updates_cache["ts"]) < APT_UPDATES_TTL_S:
        return cached

    value = _run_apt_updates_pending()
    if value is not None:
        _apt_updates_cache["ts"] = now
        _apt_updates_cache["value"] = value
        return value

    # Fehlgeschlagener Aufruf wird nicht gecacht: der naechste Zyklus versucht
    # es erneut. Gibt es schon einen guten Wert, wird der weitergereicht statt
    # auf None zu flackern.
    return cached


# ---------------------------------------------------------------------------
# Home Assistant MQTT Discovery
# ---------------------------------------------------------------------------
def device_block(dashboard):
    return {
        "identifiers": [dashboard.node_device_id],
        "name": dashboard.node_device_name,
        "manufacturer": "Raspberry Pi Foundation",
        "model": "Raspberry Pi 1 (ARMv6)",
        "sw_version": "Raspberry Pi OS Legacy (32-bit) Lite",
    }


def make_config_payload(component, object_id, name, dashboard, topics, **extra):
    payload = {
        "name": name,
        "unique_id": f"{dashboard.node_device_id}_{object_id}",
        "availability_topic": topics.availability,
        "payload_available": "1",
        "payload_not_available": "0",
        "device": device_block(dashboard),
    }
    payload.update(extra)
    return component, object_id, payload


def _build_entities(dashboard, topics):
    """Erstellt die Basis-Entity-Konfigurationen."""
    return [
        make_config_payload(
            "sensor", "cpu_temp", "CPU-Temperatur",
            dashboard, topics,
            state_topic=topics.state, value_template="{{ value_json.cpu_temp_c }}",
            unit_of_measurement="°C", device_class="temperature",
            state_class="measurement", entity_category="diagnostic",
        ),
        make_config_payload(
            "sensor", "cpu_load", "CPU-Auslastung",
            dashboard, topics,
            state_topic=topics.state, value_template="{{ value_json.cpu_load_pct }}",
            unit_of_measurement="%", state_class="measurement",
            icon="mdi:chip", entity_category="diagnostic",
        ),
        make_config_payload(
            "sensor", "ram_used", "RAM-Auslastung",
            dashboard, topics,
            state_topic=topics.state, value_template="{{ value_json.ram_used_pct }}",
            unit_of_measurement="%", state_class="measurement",
            icon="mdi:memory", entity_category="diagnostic",
        ),
        make_config_payload(
            "sensor", "disk_used", "Speicherplatz belegt",
            dashboard, topics,
            state_topic=topics.state, value_template="{{ value_json.disk_used_pct }}",
            unit_of_measurement="%", state_class="measurement",
            icon="mdi:harddisk", entity_category="diagnostic",
        ),
        make_config_payload(
            "sensor", "wifi_signal", "WLAN-Signalstärke",
            dashboard, topics,
            state_topic=topics.state, value_template="{{ value_json.wifi_signal_dbm }}",
            unit_of_measurement="dBm", device_class="signal_strength",
            state_class="measurement", entity_category="diagnostic",
            enabled_by_default=False,  # nur relevant, falls der Pi tatsächlich per WLAN angebunden ist
        ),
        make_config_payload(
            "binary_sensor", "undervoltage_now", "Unterspannung aktuell",
            dashboard, topics,
            state_topic=topics.state, value_template="{{ 'ON' if value_json.undervoltage_now else 'OFF' }}",
            device_class="problem", entity_category="diagnostic",
        ),
        make_config_payload(
            "binary_sensor", "undervoltage_occurred", "Unterspannung seit letztem Neustart aufgetreten",
            dashboard, topics,
            state_topic=topics.state, value_template="{{ 'ON' if value_json.undervoltage_occurred else 'OFF' }}",
            device_class="problem", entity_category="diagnostic",
        ),
        make_config_payload(
            "binary_sensor", "throttled_now", "CPU aktuell gedrosselt",
            dashboard, topics,
            state_topic=topics.state, value_template="{{ 'ON' if value_json.throttled_now else 'OFF' }}",
            device_class="problem", entity_category="diagnostic",
        ),
        make_config_payload(
            "sensor", "last_boot", "Letzter Neustart",
            dashboard, topics,
            state_topic=topics.state, value_template="{{ value_json.last_boot }}",
            device_class="timestamp", entity_category="diagnostic",
        ),
        make_config_payload(
            "sensor", "ip_address", "IP-Adresse",
            dashboard, topics,
            state_topic=topics.diagnostics, value_template="{{ value_json.ip_address }}",
            icon="mdi:ip-network", entity_category="diagnostic",
        ),
        make_config_payload(
            "binary_sensor", "mosquitto_running", "Mosquitto läuft",
            dashboard, topics,
            state_topic=topics.diagnostics, value_template="{{ 'ON' if value_json.mosquitto_active else 'OFF' }}",
            device_class="running", entity_category="diagnostic",
        ),
        make_config_payload(
            "binary_sensor", "tailscale_connected", "Tailscale verbunden",
            dashboard, topics,
            state_topic=topics.diagnostics, value_template="{{ 'ON' if value_json.tailscale_connected else 'OFF' }}",
            device_class="connectivity", entity_category="diagnostic",
        ),
        make_config_payload(
            "sensor", "apt_updates", "Verfügbare Paket-Updates",
            dashboard, topics,
            state_topic=topics.diagnostics, value_template="{{ value_json.apt_updates_pending }}",
            icon="mdi:package-up", state_class="measurement", entity_category="diagnostic",
        ),
        (
            "binary_sensor",
            "online",
            availability_entity_config(
                dashboard.node_device_id,
                topics.base,
                device_block(dashboard),
            ),
        ),
    ]


def publish_discovery(client, dashboard, topics):
    entities = _build_entities(dashboard, topics)
    for component, object_id, payload in entities:
        common_publish_discovery(client, dashboard.node_device_id, component, object_id, payload)


def publish_fast_state(client, topics):
    throttled = read_throttled_flags() or {}
    payload = {
        "cpu_temp_c": read_cpu_temp_c(),
        "cpu_load_pct": read_cpu_load_pct(),
        "ram_used_pct": read_ram_used_pct(),
        "disk_used_pct": read_disk_used_pct(),
        "wifi_signal_dbm": read_wifi_signal_dbm(),
        "last_boot": read_last_boot_iso(),
        **throttled,
    }
    client.publish(topics.state, json.dumps(payload), retain=True, qos=0)


def publish_slow_diagnostics(client, topics):
    payload = {
        "ip_address": read_ip_address(),
        "mosquitto_active": read_systemd_active("mosquitto"),
        "tailscale_connected": read_tailscale_connected(),
        "apt_updates_pending": read_apt_updates_pending(),
    }
    client.publish(topics.diagnostics, json.dumps(payload), retain=True, qos=0)


def cleanup_legacy_master_entities(client, node_device_id, bridge_ids):
    """Einmalige Abraeumung nach dem Master-Rueckbau (Plan 0.1).

    Die zentralen Master-Number-/Switch-Entities, die lokalen Node-Poll-
    Entities und die je-Bridge-`last_update`-Sensoren am Node-Geraet gibt
    es nicht mehr. Ein leerer retained Payload auf dem jeweiligen
    Discovery-Topic laesst Home Assistant die Entity entfernen; die
    gespiegelten State-Topics werden gleich mit geleert. Das aktive
    `.../settings/simulation_active/set` bleibt unberuehrt - darauf hoeren
    die Slaves weiterhin.
    """
    base = f"outstation/{node_device_id}"
    topics = [
        discovery_topic("number", node_device_id, "diagnostic_poll_multiplier"),
        discovery_topic("number", node_device_id, "poll_interval_s"),
        discovery_topic("switch", node_device_id, "simulation_active"),
        f"{base}/settings/diagnostic_poll_multiplier",
        f"{base}/settings/simulation_active",
    ]
    for device_id in bridge_ids:
        topics.append(discovery_topic("number", node_device_id, f"{device_id}_poll_interval_s"))
        topics.append(discovery_topic("sensor", node_device_id, f"{device_id}_last_update"))
        topics.append(f"{base}/settings/{device_id}/poll_interval_s")
        topics.append(f"{base}/settings/{device_id}/diagnostic_poll_multiplier")
        topics.append(f"{base}/settings/{device_id}/status")
    for topic in topics:
        client.publish(topic, payload="", qos=0, retain=True)


def main():
    try:
        config = appconfig.load(appconfig.config_path_from_argv())
    except appconfig.ConfigError as exc:
        print(f"Konfigurationsfehler: {exc}", file=sys.stderr)
        raise SystemExit(1)

    dashboard = config.dashboard
    topics = NodeTopics(dashboard.node_device_id)

    client = mqtt.Client(
        callback_api_version=mqtt.CallbackAPIVersion.VERSION2,
        client_id=f"energy-node-bridge-{dashboard.node_device_id}",
    )
    if config.mqtt.username:
        client.username_pw_set(config.mqtt.username, config.mqtt.password())
    client.will_set(topics.availability, "0", retain=True, qos=1)

    def poll_core():
        publish_fast_state(client, topics)
        slave.note_update(client)

    def poll_diagnostics():
        publish_slow_diagnostics(client, topics)

    slave = Slave(
        service_id=dashboard.node_device_id,
        poll_core=poll_core,
        poll_diagnostics=poll_diagnostics,
        default_poll_interval_s=dashboard.node_poll_interval_s,
        default_diagnostic_multiplier=dashboard.node_diagnostic_poll_multiplier,
        node_device_id=dashboard.node_device_id,
        on_poll_error=lambda exc: print(f"Scheduler-Fehler: {exc}"),
    )

    def on_connect(client, userdata, flags, reason_code, properties=None):
        client.publish(topics.availability, "1", retain=True, qos=1)
        cleanup_legacy_master_entities(
            client,
            dashboard.node_device_id,
            [bridge_id for bridge_id, _, _ in managed_bridges(config)],
        )
        publish_discovery(client, dashboard, topics)
        slave.start(client)

    def on_message(client, userdata, msg):
        payload_str = msg.payload.decode(errors="ignore")
        slave.handle_message(client, msg.topic, payload_str)

    client.on_connect = on_connect
    client.on_message = on_message

    client.connect(config.mqtt.host, config.mqtt.port, keepalive=60)
    client.loop_start()

    try:
        while True:
            time.sleep(3600)
    except KeyboardInterrupt:
        pass
    finally:
        slave.stop()
        client.publish(topics.availability, "0", retain=True, qos=1)
        client.loop_stop()
        client.disconnect()


if __name__ == "__main__":
    main()
