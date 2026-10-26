#!/usr/bin/env python3
"""Fake Home-Assistant-Companion fuer den Smoke-Test.

Spielt die Companion-Integration: koppelt sich mit einem Kopplungscode am
Companion-Port des Dashboards, schreibt das Token in eine Datei und haelt den
Pruefstrom offen. Jede Anmeldepruefung ("verify") bejaht es, wenn Benutzername
und Passwort passen, sonst verneint es. Endet mit dem Strom.

Nur Standardbibliothek. Der Aufruf:

    fake_ha_companion.py --companion https://127.0.0.1:<port> --code <code> \
        --user <name> --password <pw> --ha-user-id <id> [--admin] --token-file <pfad>
"""

import argparse
import hashlib
import hmac
import http.client
import json
import os
import secrets
import ssl
import sys
from urllib.parse import urlsplit

PROOF_PREFIX = "energy-node-pair-v1"
PAIR_TIMEOUT = 15
STREAM_TIMEOUT = 120  # das Dashboard sendet alle 25 s ein ping


def fail(message):
    print(f"FEHLER: {message}", file=sys.stderr, flush=True)
    sys.exit(1)


def normalize(code):
    """Wie companionpair.Normalize: Grossbuchstaben, ohne '-' und Leerzeichen."""
    return code.upper().replace("-", "").replace(" ", "")


def mac(code, message):
    return hmac.new(normalize(code).encode(), message.encode(), hashlib.sha256).hexdigest()


def fingerprint(der):
    return hashlib.sha256(der).hexdigest()


def connect(host, port, pin, timeout):
    """Oeffnet eine TLS-Verbindung und prueft das Zertifikat vor jedem Senden.

    Die Pruefung laeuft auf derselben Verbindung, die danach benutzt wird. Der
    Kontext verifiziert nicht selbst, deshalb vergleicht das Skript den
    Fingerabdruck des vorgelegten Blattzertifikats mit dem Pin.
    """
    conn = http.client.HTTPSConnection(
        host, port, context=ssl._create_unverified_context(), timeout=timeout
    )
    conn.connect()
    der = conn.sock.getpeercert(True)
    if der is None or fingerprint(der) != pin:
        conn.close()
        fail("Zertifikat des Companion-Ports passt nicht zum Fingerabdruck")
    return conn


def headers(token=None, json_body=False):
    values = {"X-Forwarded-Proto": "https"}
    if token:
        values["Authorization"] = f"Bearer {token}"
    if json_body:
        values["Content-Type"] = "application/json"
    return values


def pair(host, port, pin, code):
    nonce = secrets.token_hex(32)
    client_proof = mac(code, f"{PROOF_PREFIX}|client|{pin}|{nonce}")
    conn = connect(host, port, pin, PAIR_TIMEOUT)
    body = json.dumps({"nonce": nonce, "proof": client_proof}).encode()
    conn.request("POST", "/api/v1/companion/pair", body=body, headers=headers(json_body=True))
    response = conn.getresponse()
    payload = response.read()
    conn.close()
    if response.status != 200:
        fail(f"Kopplung abgelehnt (HTTP {response.status}): {payload.decode(errors='replace')}")
    answer = json.loads(payload)
    token = answer.get("token", "")
    expected = mac(code, f"{PROOF_PREFIX}|server|{pin}|{nonce}|{token}")
    if not token or not hmac.compare_digest(expected, answer.get("proof", "")):
        fail("der Server-Nachweis stimmt nicht - der Server kennt den Code nicht")
    return token


def write_token(path, token):
    tmp = f"{path}.tmp"
    fd = os.open(tmp, os.O_WRONLY | os.O_CREAT | os.O_TRUNC, 0o600)
    with os.fdopen(fd, "w", encoding="utf-8") as handle:
        handle.write(token)
    os.replace(tmp, path)


def answer_verify(args, host, port, pin, token, request):
    ok = request.get("username", "").lower() == args.user.lower() and request.get("password") == args.password
    result = {
        "ok": ok,
        "ha_user_id": args.ha_user_id if ok else "",
        "username": args.user if ok else "",
        "name": args.user if ok else "",
        "is_admin": bool(args.admin and ok),
    }
    conn = connect(host, port, pin, PAIR_TIMEOUT)
    conn.request(
        "POST",
        f"/api/v1/companion/verify/{request['id']}",
        body=json.dumps(result).encode(),
        headers=headers(token, json_body=True),
    )
    response = conn.getresponse()
    response.read()
    conn.close()
    print(f"verify {request['id']}: ok={ok} (HTTP {response.status})", flush=True)


def serve(args, host, port, pin, token):
    conn = connect(host, port, pin, STREAM_TIMEOUT)
    conn.request("GET", "/api/v1/companion/stream", headers=headers(token))
    response = conn.getresponse()
    if response.status != 200:
        fail(f"Pruefstrom abgelehnt (HTTP {response.status})")
    print("Pruefstrom offen", flush=True)
    event = None
    while True:
        raw = response.readline()
        if not raw:
            break  # der Strom endet, etwa nach DELETE /api/v1/users/companion
        line = raw.decode("utf-8", errors="replace").rstrip("\r\n")
        if line.startswith("event:"):
            event = line[len("event:"):].strip()
        elif line.startswith("data:"):
            data = line[len("data:"):].strip()
            if event == "verify":
                answer_verify(args, host, port, pin, token, json.loads(data))
            event = None
    conn.close()
    print("Pruefstrom beendet", flush=True)


def parse_args(argv):
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("--companion", required=True, help="https://127.0.0.1:<port>")
    parser.add_argument("--code", required=True, help="Kopplungscode aus dem Admin-Konto")
    parser.add_argument("--user", required=True, help="Home-Assistant-Benutzername")
    parser.add_argument("--password", required=True, help="Passwort dieses Benutzers")
    parser.add_argument("--ha-user-id", required=True, help="Home-Assistant-Benutzer-ID")
    parser.add_argument("--admin", action="store_true", help="Benutzer ist Home-Assistant-Administrator")
    parser.add_argument("--token-file", required=True, help="Datei, in die das Token geschrieben wird")
    return parser.parse_args(argv)


def main(argv):
    args = parse_args(argv)
    parts = urlsplit(args.companion)
    host, port = parts.hostname, parts.port
    if host is None or port is None:
        fail("--companion braucht Host und Port")
    # Der Fingerabdruck kommt vom Dashboard selbst, ueber dieselbe Adresse.
    pin = fingerprint(ssl.PEM_cert_to_DER_cert(ssl.get_server_certificate((host, port))))
    token = pair(host, port, pin, args.code)
    write_token(args.token_file, token)
    print("Gekoppelt, Token geschrieben", flush=True)
    serve(args, host, port, pin, token)


if __name__ == "__main__":
    main(sys.argv[1:])
