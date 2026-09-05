#!/usr/bin/env python3
"""Minimaler MQTT-3.1.1-Broker fuer lokale Dashboard-Smoke-Tests.

Zweck: das Dashboard beendet sich beim Start, wenn es keinen Broker erreicht.
Fuer einen Test am Entwicklungsrechner (ohne mosquitto und ohne Zugriff auf den
Pi) reicht ein Broker, der CONNECT/SUBSCRIBE/PINGREQ beantwortet und einem
neuen Abonnenten eine vorbereitete Liste von Retained-Nachrichten zustellt -
genau das, woraus die Registry ihre Geraete, Topics und Payload-Proben bildet.

BEWUSST UNVOLLSTAENDIG: kein QoS > 0, kein Wildcard-Matching, keine
Weiterleitung zwischen Clients, keine Authentifizierung. Nichts davon wird fuer
den Smoke-Test gebraucht. Nicht ausserhalb von Tests verwenden.

    python3 minibroker.py [PORT] [FIXTURE.json] [--simulate]

PORT       Standard 18883.
FIXTURE    JSON-Liste aus {"topic": ..., "payload": ...}; payload darf ein
           String oder ein Objekt sein (Objekte werden als JSON gesendet).
           Ohne Angabe wird fixtures/battery-soc.json daneben geladen.
--simulate Sendet zusaetzlich alle SIMULATE_INTERVAL Sekunden aktualisierte
           Werte fuer die vier Leistungs-Topics aus
           fixtures/energie-ueberschuss.json (PV, Netz, Batterie, Hauslast)
           an jede offene Verbindung - reine Sinuskurven um die
           Fixture-Basiswerte, keine Energiebilanz-Simulation. Damit zeigen
           die zeitbasierten Energie-Karten (energy_band/energy_day) im
           Browser eine echte Bewegung statt einer flachen Linie. Topics, die
           die geladene Fixture nicht kennt, werden stillschweigend
           uebersprungen - mit anderen Fixtures ist --simulate ein No-Op.
"""
import json
import math
import socket
import struct
import sys
import threading
import time
from pathlib import Path

DEFAULT_PORT = 18883
DEFAULT_FIXTURE = Path(__file__).resolve().parent / "fixtures" / "battery-soc.json"
SIMULATE_INTERVAL = 3.0

# Amplitude/Periode/Phase je Topic, um die Basiswerte aus
# fixtures/energie-ueberschuss.json herum - unabhaengige Sinuskurven statt
# eines Energiebilanz-Modells, das fuer eine Sichtpruefung der Charts nicht
# noetig ist. voltage/id/source werden vom Fixture-Grundwert uebernommen.
SIMULATED_TOPICS = {
    "pv/wechselrichter/status": (4200, 1500, 45, 0.0),
    "zaehler/netz/status": (-1250, 800, 45, 1.0),
    "batterie/wr/status": (900, 500, 60, 2.0),
    "zaehler/haus/status": (2050, 300, 30, 0.5),
}


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


def publish_packet(topic, payload):
    # 0x31 = PUBLISH, QoS 0, RETAIN gesetzt.
    body = struct.pack("!H", len(topic)) + topic.encode("utf-8") + payload
    return bytes([0x31]) + encode_remaining(len(body)) + body


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


def handle(conn, messages, connections):
    # connections: {conn: threading.Lock()}, geteilt mit simulate_loop() - der
    # Lock serialisiert sendall() zwischen dieser Verbindungs-Handhabung
    # (CONNACK/SUBACK/PINGRESP) und den periodischen Simulate-Publishes auf
    # demselben Socket, sonst koennten sich beide Threads verschraenken.
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
            elif packet_type == 8:    # SUBSCRIBE -> SUBACK + alle Retained
                packet_id = struct.unpack("!H", body[:2])[0]
                with lock:
                    conn.sendall(b"\x90" + encode_remaining(3) + struct.pack("!H", packet_id) + b"\x00")
                    for topic, payload in messages.items():
                        conn.sendall(publish_packet(topic, payload))
            elif packet_type == 12:   # PINGREQ -> PINGRESP
                with lock:
                    conn.sendall(b"\xd0\x00")
            elif packet_type == 14:   # DISCONNECT
                return
            # PUBLISH vom Client (3) wird angenommen und verworfen.
    except OSError:
        return
    finally:
        connections.pop(conn, None)
        conn.close()


def simulate_loop(messages, connections):
    # Periodisch neue Werte fuer SIMULATED_TOPICS berechnen, in messages
    # ablegen (damit ein neu verbindender Client den aktuellen Stand als
    # Retained-Nachricht bekommt) und an jede offene Verbindung senden. Reine
    # Sinuskurven um die Fixture-Basiswerte - kein Energiebilanz-Modell, nur
    # genug Bewegung, damit die zeitbasierten Energie-Karten im Browser eine
    # echte Kurve statt einer flachen Linie zeigen.
    start = time.monotonic()
    while True:
        time.sleep(SIMULATE_INTERVAL)
        t = time.monotonic() - start
        for topic, (base, amplitude, period, phase) in SIMULATED_TOPICS.items():
            if topic not in messages:
                continue
            state = json.loads(messages[topic])
            state["apower"] = round(base + amplitude * math.sin(t / period + phase))
            payload = json.dumps(state).encode("utf-8")
            messages[topic] = payload
            for conn, lock in list(connections.items()):
                try:
                    with lock:
                        conn.sendall(publish_packet(topic, payload))
                except OSError:
                    connections.pop(conn, None)


def main():
    args = [a for a in sys.argv[1:] if a != "--simulate"]
    simulate = "--simulate" in sys.argv[1:]
    port = int(args[0]) if len(args) > 0 else DEFAULT_PORT
    fixture = args[1] if len(args) > 1 else DEFAULT_FIXTURE
    messages = load_fixture(fixture)
    connections = {}

    if simulate:
        threading.Thread(target=simulate_loop, args=(messages, connections), daemon=True).start()

    server = socket.socket()
    server.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
    server.bind(("127.0.0.1", port))
    server.listen(5)
    print(f"minibroker: port {port}, {len(messages)} retained message(s) "
          f"from {fixture}{', simulating' if simulate else ''}", flush=True)
    while True:
        conn, _ = server.accept()
        threading.Thread(target=handle, args=(conn, messages, connections), daemon=True).start()


if __name__ == "__main__":
    main()
