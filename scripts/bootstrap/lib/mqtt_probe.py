#!/usr/bin/env python3
"""Prueft, ob der MQTT-Broker die Anmeldedaten annimmt (Diagnose).

Aufruf: mqtt_probe.py <host> <port> <user>
Das Passwort kommt roh (ohne strip) auf stdin.

Sendet einen MQTT-3.1.1-CONNECT (clean session, keepalive 10) und wertet
den CONNACK-Code aus. Ausgabe genau ein Wort: ok, denied oder unreachable.
Exit immer 0.
"""
import socket
import struct
import sys

TIMEOUT = 3.0
CLIENT_ID = b"energy-node-diagnose"
KEEPALIVE = 10


def remaining_length(n):
    out = bytearray()
    while True:
        byte = n % 128
        n //= 128
        if n:
            byte |= 0x80
        out.append(byte)
        if not n:
            return bytes(out)


def connect_packet(user, password):
    flags = 0x02  # clean session
    payload = struct.pack("!H", len(CLIENT_ID)) + CLIENT_ID
    if user:
        flags |= 0x80
        name = user.encode("utf-8")
        payload += struct.pack("!H", len(name)) + name
        if password:
            flags |= 0x40
            payload += struct.pack("!H", len(password)) + password
    body = (
        struct.pack("!H", 4) + b"MQTT"
        + bytes([4, flags])
        + struct.pack("!H", KEEPALIVE)
        + payload
    )
    return b"\x10" + remaining_length(len(body)) + body


def recv_exact(sock, n):
    out = b""
    while len(out) < n:
        chunk = sock.recv(n - len(out))
        if not chunk:
            raise OSError("Verbindung vorzeitig geschlossen")
        out += chunk
    return out


def probe(host, port, user, password):
    try:
        with socket.create_connection((host, port), timeout=TIMEOUT) as sock:
            sock.settimeout(TIMEOUT)
            sock.sendall(connect_packet(user, password))
            ack = recv_exact(sock, 4)
    except OSError:
        return "unreachable"
    if ack[0] != 0x20 or ack[1] != 2:
        return "unreachable"
    code = ack[3]
    if code == 0:
        return "ok"
    if code in (4, 5):
        return "denied"
    return "unreachable"


def main():
    try:
        host, port, user = sys.argv[1:4]
        password = sys.stdin.buffer.read()
        result = probe(host, int(port), user, password)
    except Exception:
        result = "unreachable"
    print(result)


if __name__ == "__main__":
    main()
