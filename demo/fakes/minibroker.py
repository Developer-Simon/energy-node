#!/usr/bin/env python3
"""Minimaler MQTT-3.1.1-Broker fuer lokale Dashboard-Smoke-Tests.

Zweck: das Dashboard beendet sich beim Start, wenn es keinen Broker erreicht.
Fuer einen Test am Entwicklungsrechner (ohne mosquitto und ohne Zugriff auf den
Pi) reicht ein Broker, der CONNECT/SUBSCRIBE/PINGREQ beantwortet und einem
neuen Abonnenten eine vorbereitete Liste von Retained-Nachrichten zustellt -
genau das, woraus die Registry ihre Geraete, Topics und Payload-Proben bildet.

Direkt nach den Retained-Nachrichten schickt der Broker jedes Nicht-Discovery-
Topic noch einmal als *nicht* retained PUBLISH. Ohne diese frische Kopie fuehrt
die Registry jeden Wert als "mqtt-replay" und das Dashboard markiert ihn als
"veraltet" - auf echter Hardware raeumt das die erste Live-Publikation der
Sensoren weg, hier gibt es keine. Discovery-Topics (homeassistant/.../config)
bleiben aussen vor: sie erneut ohne Retain-Flag zu senden kippte die
"Discovery retained"-Anzeige im Geraete-Dialog auf "nein".

BEWUSST UNVOLLSTAENDIG: kein QoS > 0, kein Wildcard-Matching (jede Nachricht geht an jede Verbindung), keine Authentifizierung.
Nichts davon wird fuer den Smoke-Test gebraucht. Nicht ausserhalb von Tests verwenden.

PUBLISH von Clients wird wie bei einem echten Broker weitergereicht und retained abgelegt. So sieht das Dashboard seine eigene Discovery und zeigt das Geraet energy_node in jedem Preset.

Discovery-Schalter der Fixture (homeassistant/switch/.../config) antworten wie
ein Geraet: ein Befehl auf ihrem command_topic setzt den Zustand auf dem
state_topic. So laesst sich z. B. Plug Balcony im Preset alle-funktionen
schalten.

    python3 minibroker.py [PORT] [FIXTURE.json] [--simulate]
                          [--service-status DEVICES_DIR] [--reject-config NAME ...]
                          [--control-port PORT]

PORT       Standard 18883.
FIXTURE    JSON-Liste aus {"topic": ..., "payload": ...}; payload darf ein
           String oder ein Objekt sein (Objekte werden als JSON gesendet).
           Ohne Angabe wird ../fixtures/battery-soc.json geladen.
--simulate Sendet zusaetzlich alle SIMULATE_INTERVAL Sekunden aktualisierte
           Werte fuer die vier Leistungs-Topics aus
           ../fixtures/energie-ueberschuss.json (PV, Netz, Batterie, Hauslast)
           an jede offene Verbindung - reine Sinuskurven um die
           Fixture-Basiswerte, keine Energiebilanz-Simulation. Damit zeigen
           die zeitbasierten Energie-Karten (energy_band/energy_day) im
           Browser eine echte Bewegung statt einer flachen Linie. Topics, die
           die geladene Fixture nicht kennt, werden stillschweigend
           uebersprungen - mit anderen Fixtures ist --simulate ein No-Op.
--service-status DEVICES_DIR
           Spielt die Dienste, die ihre Konfiguration laden: liest jede
           Sekunde die *_devices.json und automation_rules.json im Ordner
           und meldet je Dienst retained outstation/<id>/settings/status mit
           der SHA-256 der Datei als config_revision, dazu status/online "1".
           So zeigt die Konfigurationsseite ihren Dienststatus, auch nach
           einem Speichern (erst Ausstehend, dann Angewendet). Ein Herzschlag
           alle SERVICE_HEARTBEAT Sekunden haelt die Dienste "active".
--reject-config NAME
           Meldet die Konfiguration NAME (z. B. battery_soc_devices) als
           abgelehnt: runtime_status "rejected", applied_revision bleibt die
           zuletzt angenommene Fassung. Mehrfach angebbar.
--control-port PORT
           Startet den Steuerserver auf 127.0.0.1:PORT (fuer die Fake-Konsole).
           GET /state, GET /log?since=n, POST /simulate {"on"}, /device
           {"topic", "online"}, /service {"config", "mode"}, /publish
           {"topic", "payload", "retain"}, /disconnect, /reset. Ohne das Flag
           gibt es keinen Steuerserver.
"""
import hashlib
import http.server
import json
import math
import socket
import struct
import sys
import threading
import time
from pathlib import Path
from urllib.parse import parse_qs, urlsplit

from fakelog import Log

DEFAULT_PORT = 18883
DEFAULT_FIXTURE = Path(__file__).resolve().parent.parent / "fixtures" / "battery-soc.json"
SIMULATE_INTERVAL = 3.0
SERVICE_SCAN_INTERVAL = 1.0
SERVICE_HEARTBEAT = 30.0
REJECT_ERROR = "simulated rejection (minibroker --reject-config)"
SERVICE_MODES = ("accept", "reject", "silent")
SERVICE_MODE_LOG = {
    "accept": ("angenommen", "info"),
    "reject": ("abgelehnt (simuliert)", "warn"),
    "silent": ("schweigt", "warn"),
}

# Amplitude/Periode/Phase je Topic, um die Basiswerte aus
# fixtures/energie-ueberschuss.json herum - unabhaengige Sinuskurven statt
# eines Energiebilanz-Modells, das fuer eine Sichtpruefung der Charts nicht
# noetig ist. voltage/id/source werden vom Fixture-Grundwert uebernommen.
SIMULATED_TOPICS = {
    "pv/wechselrichter/status": (4200, 1500, 45, 0.0),
    "zaehler/netz/status": (-1250, 800, 45, 1.0),
    "batterie/wr/status": (900, 500, 60, 2.0),
    "zaehler/haus/status": (2050, 300, 30, 0.5),
    # Keller der Fixture alle-funktionen: Waermepumpe und Entfeuchter haengen
    # am Relais Keller, dessen Leistung unten als Summe beider berechnet wird.
    "waermepumpe/status": (450, 150, 40, 0.3),
    "plug/entfeuchter/status": (220, 60, 25, 1.7),
}
RELAY_SUM_TOPIC = "relais/keller/status"
RELAY_SUM_SOURCES = ("waermepumpe/status", "plug/entfeuchter/status")


def load_fixture(path):
    entries = json.loads(Path(path).read_text(encoding="utf-8"))
    # dict statt Liste: --simulate aktualisiert Eintraege anhand des Topics,
    # und ein neu verbindender Client (z. B. nach einem Dashboard-Neustart)
    # bekommt beim SUBSCRIBE den zuletzt simulierten Stand statt der
    # urspruenglichen Fixture-Werte als Retained-Nachricht zugestellt.
    messages = {}
    for entry in entries:
        payload = entry["payload"]
        if not isinstance(payload, str):
            payload = json.dumps(payload)
        messages[entry["topic"]] = payload.encode("utf-8")
    return messages


def encode_remaining(length):
    """MQTT-Laengenfeld: 7 Bit je Byte, oberstes Bit heisst 'weiter'."""
    out = b""
    while True:
        byte = length % 128
        length //= 128
        out += bytes([byte | (0x80 if length else 0)])
        if not length:
            return out


def publish_packet(topic, payload, retain=True):
    # 0x30 = PUBLISH, QoS 0; das unterste Bit ist das RETAIN-Flag.
    first = 0x31 if retain else 0x30
    body = struct.pack("!H", len(topic)) + topic.encode("utf-8") + payload
    return bytes([first]) + encode_remaining(len(body)) + body


def read_remaining(sock):
    multiplier, value = 1, 0
    while True:
        byte = sock.recv(1)
        if not byte:
            return None
        value += (byte[0] & 127) * multiplier
        if not byte[0] & 0x80:
            return value
        multiplier *= 128


def parse_publish(flags, body):
    """PUBLISH-Rumpf eines Clients: (topic, payload, retain) oder None.

    Nur QoS 0 - der Broker schickt kein PUBACK, eine QoS-1-Nachricht wuerde
    der Client ohnehin wiederholen. Das Dashboard publiziert nur QoS 0."""
    if (flags >> 1) & 0x03:
        return None
    length = struct.unpack("!H", body[:2])[0]
    topic = body[2:2 + length].decode("utf-8")
    return topic, body[2 + length:], bool(flags & 0x01)


def relay(messages, connections, topic, payload, retain):
    """Client-PUBLISH weiterreichen wie ein echter Broker: retained ablegen
    (leere Payload loescht, HA-Konvention), an jede offene Verbindung als
    Live-Nachricht senden - auch an den Absender, denn das Dashboard
    abonniert die eigene Discovery und liest so seinen Knoten energy_node."""
    if retain:
        if payload:
            messages[topic] = payload
        else:
            messages.pop(topic, None)
    for conn, lock in list(connections.items()):
        try:
            with lock:
                conn.sendall(publish_packet(topic, payload, retain=False))
        except OSError:
            connections.pop(conn, None)
    echo_switch(messages, connections, topic, payload)


def echo_switch(messages, connections, topic, payload):
    """Spielt das Geraet hinter einem Discovery-Schalter der Fixture: ein
    Befehl auf dessen command_topic kommt als neuer Zustand retained auf dem
    state_topic zurueck. Ohne das bliebe ein Schaltbefehl aus dem Dashboard
    folgenlos, die Geraetekarte zeigte die Steckdose nie eingeschaltet."""
    for config_topic, raw in list(messages.items()):
        if not (config_topic.startswith("homeassistant/switch/") and config_topic.endswith("/config")):
            continue
        try:
            config = json.loads(raw)
        except ValueError:
            continue
        if config.get("command_topic") != topic or not config.get("state_topic"):
            continue
        states = {
            config.get("payload_on", "ON"): config.get("state_on", config.get("payload_on", "ON")),
            config.get("payload_off", "OFF"): config.get("state_off", config.get("payload_off", "OFF")),
        }
        state = states.get(payload.decode("utf-8", "replace"))
        if state is not None:
            # broadcast() legt retained ab, sendet aber mit Retain-Flag 0 wie
            # ein echter Broker an schon verbundene Clients. Nur eine solche
            # Live-Nachricht bestaetigt im Dashboard den offenen Befehl.
            broadcast(messages, connections, config["state_topic"], state.encode("utf-8"), False)


def parse_client_id(body):
    """Client-ID aus dem CONNECT-Rumpf: Protokollname, Level, Flags, Keepalive, dann die ID. Kaputt -> ""."""
    try:
        name_length = struct.unpack("!H", body[:2])[0]
        offset = 2 + name_length + 4
        length = struct.unpack("!H", body[offset:offset + 2])[0]
        return body[offset + 2:offset + 2 + length].decode("utf-8")
    except (struct.error, UnicodeDecodeError):
        return ""


class BrokerState:
    """Zustand des Brokers, geteilt von Verbindungen, Schleifen und Steuerserver."""

    def __init__(self, messages, port, simulate, rejected):
        self.messages = messages
        self.port = port
        self.initial_simulate = simulate
        self.simulate = simulate
        self.initial_modes = {name: "reject" for name in rejected}
        self.modes = dict(self.initial_modes)
        self.connections = {}  # conn -> threading.Lock()
        self.clients = {}      # conn -> {"client_id", "since"}
        self.log = Log()
        self.services = {}     # config -> "ok" | "rejected" | "silent", sobald der Dienst-Scan sie sah
        self.stale = set()     # Konfigurationen, die der naechste Scan neu meldet
        self.seen = {}         # config -> zuletzt gemeldete Pruefsumme
        self.applied = {}      # config -> zuletzt angenommene Pruefsumme

    def mode_of(self, config):
        return self.modes.get(config, "accept")

    def _device_map(self):
        # availability_topic -> Name und Nutzlasten; die erste Discovery-Konfiguration je Topic gewinnt.
        found = {}
        for config_topic in sorted(list(self.messages)):
            if not (config_topic.startswith("homeassistant/") and config_topic.endswith("/config")):
                continue
            try:
                config = json.loads(self.messages.get(config_topic) or b"null")
            except ValueError:
                continue
            if not isinstance(config, dict) or not config.get("availability_topic"):
                continue
            topic = config["availability_topic"]
            if topic in found:
                continue
            device = config.get("device")
            name = device.get("name") if isinstance(device, dict) else None
            found[topic] = {
                "name": name or topic,
                "available": str(config.get("payload_available", "online")),
                "not_available": str(config.get("payload_not_available", "offline")),
            }
        return found

    def devices(self):
        out = []
        for topic, info in self._device_map().items():
            online = self.messages.get(topic) != info["not_available"].encode("utf-8")
            out.append({"topic": topic, "name": info["name"], "online": online})
        return sorted(out, key=lambda device: device["name"])

    def set_device(self, topic, online):
        info = self._device_map().get(topic)
        if info is None:
            raise ValueError("unbekanntes Gerät")
        payload = info["available" if online else "not_available"].encode("utf-8")
        broadcast(self.messages, self.connections, topic, payload, False)
        if online:
            self.log.add("mqtt", f"{info['name']} online")
        else:
            self.log.add("mqtt", f"{info['name']} offline gemeldet", "warn")

    def set_simulate(self, on):
        self.simulate = bool(on)

    def set_service(self, config, mode):
        if mode not in SERVICE_MODES:
            raise ValueError("unbekannter Modus")
        self.modes[config] = mode
        self.stale.add(config)
        text, level = SERVICE_MODE_LOG[mode]
        self.log.add("mqtt", f"{config} {text}", level)

    def publish(self, topic, payload, retain):
        if not topic:
            raise ValueError("Topic fehlt")
        relay(self.messages, self.connections, topic, payload.encode("utf-8"), retain)
        self.log.add("konsole", f"Konsole sendet {topic}")

    def disconnect_all(self):
        conns = list(self.connections)
        for conn in conns:
            try:
                conn.shutdown(socket.SHUT_RDWR)
            except OSError:
                pass
        self.log.add("mqtt", f"{len(conns)} Verbindungen getrennt", "warn")
        return len(conns)

    def reset(self):
        self.simulate = self.initial_simulate
        self.modes = dict(self.initial_modes)
        for device in self.devices():
            if not device["online"]:
                self.set_device(device["topic"], True)
        self.stale.update(self.services)
        self.log.add("konsole", "Broker zurückgesetzt")

    def state(self):
        services = [
            {
                "config": config,
                "mode": self.mode_of(config),
                "initial_mode": self.initial_modes.get(config, "accept"),
                "status": status,
            }
            for config, status in sorted(self.services.items())
        ]
        return {
            "port": self.port,
            "simulate": self.simulate,
            "initial_simulate": self.initial_simulate,
            "topics": len(self.messages),
            "topic_names": sorted(name for name in list(self.messages) if not name.startswith("homeassistant/")),
            "connections": [{"client_id": c["client_id"], "since": c["since"]} for c in list(self.clients.values())],
            "devices": self.devices(),
            "services": services,
        }


def handle(conn, state):
    # state.connections: {conn: threading.Lock()}, geteilt mit simulate_loop() - der
    # Lock serialisiert sendall() zwischen dieser Verbindungs-Handhabung
    # (CONNACK/SUBACK/PINGRESP) und den periodischen Simulate-Publishes auf
    # demselben Socket, sonst koennten sich beide Threads verschraenken.
    messages, connections = state.messages, state.connections
    lock = threading.Lock()
    try:
        while True:
            header = conn.recv(1)
            if not header:
                return
            packet_type = header[0] >> 4
            length = read_remaining(conn)
            if length is None:
                return
            body = b""
            while len(body) < length:
                chunk = conn.recv(length - len(body))
                if not chunk:
                    return
                body += chunk

            if packet_type == 1:      # CONNECT -> CONNACK, akzeptiert immer
                with lock:
                    conn.sendall(b"\x20\x02\x00\x00")
                connections[conn] = lock
                client_id = parse_client_id(body)
                state.clients[conn] = {"client_id": client_id, "since": time.time()}
                state.log.add("mqtt", f"Client {client_id} verbunden")
            elif packet_type == 8:    # SUBSCRIBE -> SUBACK + alle Retained + Live-Refresh
                packet_id = struct.unpack("!H", body[:2])[0]
                with lock:
                    conn.sendall(b"\x90" + encode_remaining(3) + struct.pack("!H", packet_id) + b"\x00")
                    for topic, payload in list(messages.items()):
                        conn.sendall(publish_packet(topic, payload))
                    # Jedes State-/Verfuegbarkeits-Topic noch einmal als nicht
                    # retained PUBLISH: so fuehrt die Registry den Wert als
                    # "live" statt "mqtt-replay" und das Dashboard zeigt ihn
                    # nicht als "veraltet". Discovery bleibt retained (s. o.).
                    for topic, payload in list(messages.items()):
                        if topic.startswith("homeassistant/"):
                            continue
                        conn.sendall(publish_packet(topic, payload, retain=False))
            elif packet_type == 3:    # PUBLISH vom Client -> ablegen und weiterreichen
                parsed = parse_publish(header[0] & 0x0F, body)
                if parsed:
                    relay(messages, connections, *parsed)
                    client = state.clients.get(conn, {}).get("client_id", "?")
                    state.log.add("mqtt", f"{client} sendet {parsed[0]}")
            elif packet_type == 12:   # PINGREQ -> PINGRESP
                with lock:
                    conn.sendall(b"\xd0\x00")
            elif packet_type == 14:   # DISCONNECT
                return
    except OSError:
        return
    finally:
        connections.pop(conn, None)
        client = state.clients.pop(conn, None)
        if client:
            state.log.add("mqtt", f"Client {client['client_id']} getrennt")
        conn.close()


def simulate_tick(state, t):
    # Ein Tick der Simulation: neue Werte fuer SIMULATED_TOPICS in messages
    # ablegen (damit ein neu verbindender Client den aktuellen Stand als
    # Retained-Nachricht bekommt) und an jede offene Verbindung senden - als
    # nicht retained PUBLISH, wie eine echte Live-Publikation, sonst faerbte
    # jeder Tick die Werte wieder auf "veraltet". Ticks schreiben nichts ins
    # Protokoll. Ist die Simulation aus, passiert nichts.
    if not state.simulate:
        return
    messages, connections = state.messages, state.connections
    for topic, (base, amplitude, period, phase) in SIMULATED_TOPICS.items():
        if topic not in messages:
            continue
        values = json.loads(messages[topic])
        values["apower"] = round(base + amplitude * math.sin(t / period + phase))
        messages[topic] = json.dumps(values).encode("utf-8")
        broadcast(messages, connections, topic, messages[topic], False)
    if RELAY_SUM_TOPIC in messages and all(src in messages for src in RELAY_SUM_SOURCES):
        relay_state = json.loads(messages[RELAY_SUM_TOPIC])
        relay_state["apower"] = sum(json.loads(messages[src])["apower"] for src in RELAY_SUM_SOURCES)
        messages[RELAY_SUM_TOPIC] = json.dumps(relay_state).encode("utf-8")
        broadcast(messages, connections, RELAY_SUM_TOPIC, messages[RELAY_SUM_TOPIC], False)


def simulate_loop(state):
    # Reine Sinuskurven um die Fixture-Basiswerte - kein Energiebilanz-Modell,
    # nur genug Bewegung, damit die zeitbasierten Energie-Karten im Browser
    # eine echte Kurve statt einer flachen Linie zeigen. Die Schleife laeuft
    # immer; solange die Simulation aus ist, ueberspringt simulate_tick jeden Tick.
    start = time.monotonic()
    while True:
        time.sleep(SIMULATE_INTERVAL)
        simulate_tick(state, time.monotonic() - start)


def broadcast(messages, connections, topic, payload, retain):
    """Ablegen fuer kuenftige Abonnenten und an jede offene Verbindung senden."""
    messages[topic] = payload
    for conn, lock in list(connections.items()):
        try:
            with lock:
                conn.sendall(publish_packet(topic, payload, retain=retain))
        except OSError:
            connections.pop(conn, None)


def service_id_for_config(name):
    # Spiegel von config.ServiceIDForConfig (internal/config/config.go).
    if name == "automation_rules":
        return "automation"
    if name.endswith("_devices"):
        return name[: -len("_devices")]
    return None


def service_status_scan(devices_dir, state, beat):
    # Ein Durchlauf von service_status_loop. Ein Dienst liest seine Datei,
    # meldet das Ergebnis retained auf settings/status und nennt dabei die
    # Pruefsumme der Datei. Genau diese vergleicht die Konfigurationsseite mit
    # der Datei, die sie geschrieben hat - deshalb hier die echte SHA-256 der
    # Bytes auf der Platte. Der Modus kommt aus dem Zustand (accept, reject,
    # silent); silent meldet weder Status noch Herzschlag.
    for path in sorted(Path(devices_dir).glob("*.json")):
        name = path.stem
        if name.endswith(".schema"):
            continue
        service_id = service_id_for_config(name)
        if not service_id:
            continue
        try:
            revision = hashlib.sha256(path.read_bytes()).hexdigest()
        except OSError:
            continue
        mode = state.mode_of(name)
        if mode == "silent":
            state.services[name] = "silent"
            continue
        if state.seen.get(name) == revision and not beat and name not in state.stale:
            continue
        state.stale.discard(name)
        state.seen[name] = revision
        status = {"last_update": int(time.time()), "config_revision": revision}
        if mode == "reject":
            state.services[name] = "rejected"
            status.update(runtime_status="rejected", error=REJECT_ERROR,
                          applied_revision=state.applied.get(name, ""))
        else:
            state.applied[name] = revision
            state.services[name] = "ok"
            status.update(runtime_status="ok", error="", applied_revision=revision)
        base = f"outstation/{service_id}"
        broadcast(state.messages, state.connections, f"{base}/status/online", b"1", True)
        broadcast(state.messages, state.connections, f"{base}/settings/status",
                  json.dumps(status).encode("utf-8"), True)


def service_status_loop(devices_dir, state):
    last_beat = 0.0
    while True:
        now = time.time()
        beat = now - last_beat >= SERVICE_HEARTBEAT
        service_status_scan(devices_dir, state, beat)
        if beat:
            last_beat = now
        time.sleep(SERVICE_SCAN_INTERVAL)


CONTROL_POST_PATHS = ("/simulate", "/device", "/service", "/publish", "/disconnect", "/reset")


def _field(body, key):
    if key not in body:
        raise ValueError(f"{key} fehlt")
    return body[key]


def make_control_server(state, port):
    """Steuerserver auf 127.0.0.1: Zustand lesen, Schalter setzen. Fehler kommen als JSON."""

    class Handler(http.server.BaseHTTPRequestHandler):
        def log_message(self, *args):
            pass

        def _send(self, status, body):
            data = json.dumps(body).encode("utf-8")
            self.send_response(status)
            self.send_header("Content-Type", "application/json")
            self.send_header("Content-Length", str(len(data)))
            self.end_headers()
            self.wfile.write(data)

        def _body(self):
            length = int(self.headers.get("Content-Length", 0))
            raw = self.rfile.read(length) if length else b""
            body = json.loads(raw) if raw.strip() else {}
            if not isinstance(body, dict):
                raise ValueError("JSON-Objekt erwartet")
            return body

        def _dispatch(self, url):
            if self.command == "GET":
                if url.path == "/state":
                    return 200, state.state()
                if url.path == "/log":
                    since = int(parse_qs(url.query).get("since", ["0"])[0])
                    last, entries = state.log.since(since)
                    return 200, {"last": last, "entries": entries}
                return 404, {"error": "nicht gefunden"}
            if url.path not in CONTROL_POST_PATHS:
                return 404, {"error": "nicht gefunden"}
            body = self._body()
            if url.path == "/simulate":
                state.set_simulate(_field(body, "on"))
            elif url.path == "/device":
                state.set_device(_field(body, "topic"), _field(body, "online"))
            elif url.path == "/service":
                state.set_service(_field(body, "config"), _field(body, "mode"))
            elif url.path == "/publish":
                state.publish(body.get("topic", ""), body.get("payload", ""), bool(body.get("retain", False)))
            elif url.path == "/disconnect":
                state.disconnect_all()
            elif url.path == "/reset":
                state.reset()
            return 200, state.state()

        def _respond(self):
            try:
                status, body = self._dispatch(urlsplit(self.path))
            except ValueError as error:
                status, body = 400, {"error": str(error)}
            self._send(status, body)

        do_GET = _respond
        do_POST = _respond

    return http.server.ThreadingHTTPServer(("127.0.0.1", port), Handler)


def parse_args(argv):
    opts = {"simulate": False, "service_status": None, "rejected": set(), "control_port": None, "positional": []}
    i = 0
    while i < len(argv):
        arg = argv[i]
        if arg == "--simulate":
            opts["simulate"] = True
        elif arg == "--service-status":
            i += 1
            opts["service_status"] = argv[i]
        elif arg == "--reject-config":
            i += 1
            opts["rejected"].add(argv[i])
        elif arg == "--control-port":
            i += 1
            opts["control_port"] = int(argv[i])
        else:
            opts["positional"].append(arg)
        i += 1
    return opts


def main():
    opts = parse_args(sys.argv[1:])
    args = opts["positional"]
    simulate = opts["simulate"]
    port = int(args[0]) if len(args) > 0 else DEFAULT_PORT
    fixture = args[1] if len(args) > 1 else DEFAULT_FIXTURE
    state = BrokerState(load_fixture(fixture), port, simulate, opts["rejected"])

    threading.Thread(target=simulate_loop, args=(state,), daemon=True).start()
    if opts["service_status"]:
        threading.Thread(target=service_status_loop,
                         args=(opts["service_status"], state),
                         daemon=True).start()
    if opts["control_port"] is not None:
        control = make_control_server(state, opts["control_port"])
        threading.Thread(target=control.serve_forever, daemon=True).start()

    server = socket.socket()
    server.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
    server.bind(("127.0.0.1", port))
    server.listen(5)
    print(f"minibroker: port {port}, {len(state.messages)} retained message(s) "
          f"from {fixture}{', simulating' if simulate else ''}"
          f"{', service status from ' + opts['service_status'] if opts['service_status'] else ''}", flush=True)
    while True:
        conn, _ = server.accept()
        threading.Thread(target=handle, args=(conn, state), daemon=True).start()


if __name__ == "__main__":
    main()
