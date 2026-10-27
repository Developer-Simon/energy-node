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
import socket
import ssl
import sys
import threading
from dataclasses import asdict, dataclass
from typing import Callable
from urllib.parse import urlsplit

PROOF_PREFIX = "energy-node-pair-v1"
PAIR_TIMEOUT = 15
STREAM_TIMEOUT = 120  # das Dashboard sendet alle 25 s ein ping
STOP_JOIN = 5  # Sekunden, die drop_stream und unpair auf den Strom-Thread warten


class PairError(Exception):
    """Text ist die Meldung des Dashboards oder der Pruefung."""


@dataclass
class HAUser:
    ha_user_id: str
    username: str
    password: str
    name: str
    admin: bool = False
    active: bool = True


def default_users():
    return [
        HAUser("ha-smoke-maria", "maria", "maria-smoke-pass-1", "Maria", admin=True),
        HAUser("ha-smoke-tom", "tom", "tom-smoke-pass-1", "Tom"),
    ]


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


def headers(token=None, json_body=False):
    values = {"X-Forwarded-Proto": "https"}
    if token:
        values["Authorization"] = f"Bearer {token}"
    if json_body:
        values["Content-Type"] = "application/json"
    return values


def write_token(path, token):
    tmp = f"{path}.tmp"
    fd = os.open(tmp, os.O_WRONLY | os.O_CREAT | os.O_TRUNC, 0o600)
    with os.fdopen(fd, "w", encoding="utf-8") as handle:
        handle.write(token)
    os.replace(tmp, path)


def _print(text):
    print(text, flush=True)


class HACompanion:
    """Gekoppelter Companion mit Benutzertabelle, der den Pruefstrom haelt.

    Ereignisse und Meldungen gehen an den Log-Hook `log`. Fehler beim Koppeln
    werden als PairError geworfen, Stromenden und 401 stehen in state().
    """

    def __init__(self, companion_url: str, log: Callable[[str], None], users: list[HAUser] | None = None) -> None:
        parts = urlsplit(companion_url)
        if parts.hostname is None or parts.port is None:
            raise ValueError("companion_url braucht Host und Port")
        self._host = parts.hostname
        self._port = parts.port
        self._log = log
        self.users: dict[str, HAUser] = {}
        for user in users if users is not None else default_users():
            self.users[user.ha_user_id] = user
        self.manual = False
        self._lock = threading.Lock()
        self._pin = None
        self._token = None
        self._paired = False
        self._connected = False
        self._error = ""
        self._refusal = ""
        self._stopping = False
        self._sock = None  # Socket des Stroms, bleibt nach getresponse() erhalten
        self._pending: dict[str, dict] = {}  # offene Prüfungen im manuellen Modus
        self._thread = None

    # Verbindung

    def _fetch_pin(self) -> str:
        der = ssl.PEM_cert_to_DER_cert(ssl.get_server_certificate((self._host, self._port)))
        return fingerprint(der)

    def _connect(self, timeout):
        """Oeffnet eine TLS-Verbindung und prueft das Zertifikat vor jedem Senden.

        Die Pruefung laeuft auf derselben Verbindung, die danach benutzt wird. Der
        Kontext verifiziert nicht selbst, deshalb vergleicht die Klasse den
        Fingerabdruck des vorgelegten Blattzertifikats mit dem Pin.
        """
        conn = http.client.HTTPSConnection(
            self._host, self._port, context=ssl._create_unverified_context(), timeout=timeout
        )
        conn.connect()
        der = conn.sock.getpeercert(True)
        if der is None or fingerprint(der) != self._pin:
            conn.close()
            raise PairError("Zertifikat des Companion-Ports passt nicht zum Fingerabdruck")
        return conn

    def _post_pair(self, nonce, proof):
        conn = self._connect(PAIR_TIMEOUT)
        body = json.dumps({"nonce": nonce, "proof": proof}).encode()
        conn.request("POST", "/api/v1/companion/pair", body=body, headers=headers(json_body=True))
        response = conn.getresponse()
        payload = response.read().decode(errors="replace")
        conn.close()
        return response.status, payload

    # Kopplung und Strom

    def pair(self, code: str, on_paired: Callable[[str], None] | None = None) -> None:
        """Koppelt mit dem Code und startet den Pruefstrom. on_paired bekommt das Token vor dem Strom."""
        self._pin = self._fetch_pin()
        nonce = secrets.token_hex(32)
        client_proof = mac(code, f"{PROOF_PREFIX}|client|{self._pin}|{nonce}")
        status, payload = self._post_pair(nonce, client_proof)
        if status != 200:
            raise PairError(f"Kopplung abgelehnt (HTTP {status}): {payload}")
        answer = json.loads(payload)
        token = answer.get("token", "")
        expected = mac(code, f"{PROOF_PREFIX}|server|{self._pin}|{nonce}|{token}")
        if not token or not hmac.compare_digest(expected, answer.get("proof", "")):
            raise PairError("der Server-Nachweis stimmt nicht - der Server kennt den Code nicht")
        with self._lock:
            self._token = token
            self._paired = True
            self._error = ""
        if on_paired is not None:
            on_paired(token)
        self._start_stream()

    def pair_wrong_code(self) -> str:
        """Sendet einen zufaelligen Code und gibt die Ablehnung des Dashboards zurueck."""
        self._pin = self._fetch_pin()
        code = secrets.token_hex(6).upper()
        nonce = secrets.token_hex(32)
        status, payload = self._post_pair(nonce, mac(code, f"{PROOF_PREFIX}|client|{self._pin}|{nonce}"))
        if status == 200:
            raise PairError("das Dashboard hat einen falschen Code angenommen")
        return f"Kopplung abgelehnt (HTTP {status}): {payload}"

    def unpair(self) -> None:
        """Vergisst das Token und schliesst den Strom, ohne das Dashboard zu rufen."""
        self._stop_stream()
        with self._lock:
            self._token = None
            self._paired = False
            self._error = ""
            self._pending.clear()

    def drop_stream(self) -> None:
        """Schliesst den Strom, die Kopplung bleibt."""
        self._stop_stream()

    def open_stream(self) -> None:
        with self._lock:
            if not self._paired:
                raise PairError("nicht gekoppelt")
            if self._thread is not None and self._thread.is_alive():
                return
        self._start_stream()

    def wait(self) -> str:
        """Wartet, bis der Strom-Thread endet. Gibt die Ablehnung zurueck (401 oder anderer Status), sonst ""."""
        thread = self._thread
        if thread is not None:
            thread.join()
        with self._lock:
            return self._refusal

    def token(self) -> str:
        with self._lock:
            return self._token or ""

    def state(self) -> dict:
        with self._lock:
            return {
                "paired": self._paired,
                "connected": self._connected,
                "error": self._error,
                "manual": self.manual,
                "users": [asdict(user) for user in self.users.values()],
                "pending": [{"id": request["id"], "username": request.get("username", "")} for request in self._pending.values()],
            }

    def _start_stream(self) -> None:
        with self._lock:
            token = self._token
            self._stopping = False
            self._refusal = ""
            self._error = ""
            thread = threading.Thread(target=self._stream, args=(token,), daemon=True)
            self._thread = thread
        thread.start()

    def _stop_stream(self) -> None:
        with self._lock:
            self._stopping = True
            sock = self._sock
            thread = self._thread
        if sock is not None:
            try:
                sock.shutdown(socket.SHUT_RDWR)
            except OSError:
                pass
        if thread is not None and thread is not threading.current_thread():
            thread.join(STOP_JOIN)
        with self._lock:
            self._connected = False

    def _forget_token(self, error: str) -> None:
        """Vergisst das Token. Der Aufrufer haelt den Lock."""
        self._token = None
        self._paired = False
        self._error = error
        self._pending.clear()

    def _stream(self, token) -> None:
        opened = False
        refusal = ""
        error = ""
        conn = None
        try:
            conn = self._connect(STREAM_TIMEOUT)
            with self._lock:
                self._sock = conn.sock
                if self._stopping:
                    return
            conn.request("GET", "/api/v1/companion/stream", headers=headers(token))
            response = conn.getresponse()
            if response.status != 200:
                refusal = f"Pruefstrom abgelehnt (HTTP {response.status})"
                return
            with self._lock:
                self._connected = True
            opened = True
            self._log("Pruefstrom offen")
            self._read_events(response, token)
        except PairError as failure:
            error = str(failure)
        except (OSError, ValueError, http.client.HTTPException) as failure:
            error = f"Pruefstrom unterbrochen: {failure}"
        finally:
            if conn is not None:
                conn.close()
            with self._lock:
                stopping = self._stopping
                self._sock = None
                self._connected = False
                self._refusal = refusal
                if refusal:
                    self._forget_token(refusal)
                elif not stopping:
                    self._forget_token(error or "Pruefstrom vom Dashboard beendet")
            if opened:
                self._log("Pruefstrom beendet")

    def _read_events(self, response, token) -> None:
        event = None
        while True:
            raw = response.readline()
            if not raw:
                return  # der Strom endet, etwa nach DELETE /api/v1/users/companion
            line = raw.decode("utf-8", errors="replace").rstrip("\r\n")
            if line.startswith("event:"):
                event = line[len("event:"):].strip()
            elif line.startswith("data:"):
                data = line[len("data:"):].strip()
                if event == "verify":
                    self._verify(json.loads(data), token)
                event = None

    # Prüfungen

    def _find_user(self, username: str) -> HAUser | None:
        for user in self.users.values():
            if user.username.lower() == username.lower():
                return user
        return None

    def _verify(self, request, token) -> None:
        if self.manual:
            with self._lock:
                self._pending[request["id"]] = request
            return
        user = self._find_user(request.get("username", ""))
        ok = user is not None and user.active and request.get("password") == user.password
        if ok:
            result = {
                "ok": True,
                "ha_user_id": user.ha_user_id,
                "username": user.username,
                "name": user.name,
                "is_admin": user.admin,
            }
        else:
            result = {"ok": False, "ha_user_id": "", "username": "", "name": "", "is_admin": False}
        self._send_answer(request["id"], result, ok, token)

    def _send_answer(self, request_id, result, ok, token) -> None:
        status = self._post_verify(request_id, result, token)
        late = ", Pruefung abgelaufen" if status in (404, 410) else ""
        self._log(f"verify {request_id}: ok={ok} (HTTP {status}){late}")

    def _post_verify(self, request_id, result, token) -> int:
        conn = self._connect(PAIR_TIMEOUT)
        conn.request(
            "POST",
            f"/api/v1/companion/verify/{request_id}",
            body=json.dumps(result).encode(),
            headers=headers(token, json_body=True),
        )
        response = conn.getresponse()
        response.read()
        conn.close()
        return response.status

    def resolve(self, request_id: str, decision: str) -> None:
        """Beantwortet eine offene Prüfung im manuellen Modus: "yes", "no" oder "timeout"."""
        if decision not in ("yes", "no", "timeout"):
            raise ValueError(f"unbekannte Entscheidung: {decision}")
        with self._lock:
            request = self._pending.pop(request_id, None)
            token = self._token
        if request is None:
            raise ValueError(f"keine offene Prüfung {request_id}")
        if decision == "timeout":
            self._log(f"verify {request_id}: Zeitüberschreitung, keine Antwort")
            return
        if decision == "no":
            result = {"ok": False, "ha_user_id": "", "username": "", "name": "", "is_admin": False}
            self._send_answer(request_id, result, False, token)
            return
        username = request.get("username", "")
        user = self._find_user(username)
        if user is not None:
            result = {
                "ok": True,
                "ha_user_id": user.ha_user_id,
                "username": user.username,
                "name": user.name,
                "is_admin": user.admin,
            }
        else:
            result = {
                "ok": True,
                "ha_user_id": "ha-" + username,
                "username": username,
                "name": username,
                "is_admin": False,
            }
        self._send_answer(request_id, result, True, token)

    def set_manual(self, on: bool) -> None:
        self.manual = bool(on)

    # Benutzertabelle

    def save_user(self, user: HAUser) -> None:
        """Legt einen Benutzer an oder ändert ihn und meldet die Änderung, wenn gekoppelt."""
        with self._lock:
            self.users[user.ha_user_id] = user
            token = self._token
        if token is not None:
            body = {"ha_user_id": user.ha_user_id, "name": user.name, "admin": user.admin, "active": user.active}
            self._post_user_changed(body, token)

    def remove_user(self, ha_user_id: str) -> None:
        """Löscht einen Benutzer und meldet das Löschen ohne Benutzernamen, wenn gekoppelt."""
        with self._lock:
            self.users.pop(ha_user_id, None)
            token = self._token
        if token is not None:
            self._post_user_changed({"ha_user_id": ha_user_id, "removed": True}, token)

    def _post_user_changed(self, body, token) -> None:
        conn = self._connect(PAIR_TIMEOUT)
        conn.request(
            "POST",
            "/api/v1/companion/user-changed",
            body=json.dumps(body).encode(),
            headers=headers(token, json_body=True),
        )
        response = conn.getresponse()
        response.read()
        conn.close()


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
    if parts.hostname is None or parts.port is None:
        fail("--companion braucht Host und Port")
    user = HAUser(args.ha_user_id, args.user, args.password, args.user, admin=args.admin)
    companion = HACompanion(args.companion, _print, users=[user])

    def store(token):
        write_token(args.token_file, token)
        _print("Gekoppelt, Token geschrieben")

    try:
        companion.pair(args.code, on_paired=store)
    except PairError as error:
        fail(str(error))
    refusal = companion.wait()
    if refusal:
        fail(refusal)


if __name__ == "__main__":
    main(sys.argv[1:])
