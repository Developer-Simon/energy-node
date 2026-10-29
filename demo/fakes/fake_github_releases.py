#!/usr/bin/env python3
"""Faelscht die GitHub-Releases-API fuer die Update-Smoke-Tests.

Zweck 1 (Update-Check): internal/updatecheck fragt normalerweise
https://api.github.com nach dem neuesten Release (siehe releasing.md). Um die
"Update verfuegbar"-Anzeige (Masthead-Pille, Button in Systemzugriff) ohne
einen echten neueren Tag auf GitHub pruefen zu koennen, beantwortet dieser
Server jede Anfrage auf GET /repos/<repo>/releases/latest immer mit demselben
festen, hoeheren Tag - der Pfad selbst wird nicht ausgewertet. main.go zeigt
den Checker per ENERGY_NODE_UPDATES_API_BASE hierher statt zur echten API.

Zweck 2 (Paketbezug): mit einem Archiv als dritten Argument liefert der Server
zusaetzlich GET /repos/<repo>/releases (die Liste, die internal/bundlefetch
liest) mit einem Release, das genau dieses Archiv als Asset traegt, und das
Archiv selbst unter /download/<Dateiname>. Der Dateiname des Archivs ist der
Asset-Name (energy-node-v<version>-<arch>.tar.gz). Der Download laeuft in zehn
Stuecken ueber FAKE_DOWNLOAD_SECONDS Sekunden (Standard 4), damit der
Vorbereiten-Bildschirm und seine Fortschrittszeilen sichtbar werden.

Steuerung (Fake-Konsole, nur fuer Tests und Demo): auf demselben Port

    GET  /_fake/state                     aktueller Zustand (JSON)
    GET  /_fake/log?since=n               Protokoll ab Nummer n
    POST /_fake/release {"tag"?, "mode"?} Antwort auf Update-Checks
    POST /_fake/download {"seconds"?, "abort"?}  Dauer und Abbruch des Downloads
    POST /_fake/reset                     Anfangszustand wiederherstellen

Modi fuer die Antwort: update (Standard, wie frueher), none (kein Update,
alter Tag, leere Liste), error (500), ratelimit (403 mit
X-RateLimit-Remaining: 0). Abbruch des Downloads: never (Standard), half (bei
der Haelfte), start (sofort nach den Kopfzeilen). Ohne Steuerung antwortet der
Server wie frueher.

    python3 fake_github_releases.py [PORT] [TAG] [ARCHIV]

PORT    Standard 18884.
TAG     Der zurueckgelieferte tag_name. Standard "v9.9.9" - bewusst weit ueber
        jeder echten dashboard/VERSION, damit der Vergleich unabhaengig vom
        gerade eingecheckten Stand zuverlaessig "verfuegbar" ergibt.
ARCHIV  Optional: ein Bundle-.tar.gz, das als Release-Asset angeboten wird.

BEWUSST UNVOLLSTAENDIG: keine Authentifizierung, keine anderen Endpunkte.
Nicht ausserhalb von Tests verwenden.
"""
import http.server
import json
import os
import re
import sys
import threading
import time
from urllib.parse import parse_qs, urlsplit

from fakelog import Log

DEFAULT_PORT = 18884
DEFAULT_TAG = "v9.9.9"
CHUNKS = 10
RELEASE_MODES = ("update", "none", "error", "ratelimit")
ABORT_MODES = ("never", "half", "start")
NO_UPDATE_TAG = "v0.0.1"
MAX_SECONDS = 120
MAX_BODY = 64 * 1024
TAG_RE = re.compile(r"v\d+\.\d+\.\d+")
MODE_TEXT = {
    "none": "Antwort: kein Update",
    "error": "Antwort: Serverfehler 500",
    "ratelimit": "Antwort: Rate-Limit",
}
ABORT_TEXT = {
    "never": "Download bricht nicht mehr ab",
    "half": "Download bricht bei der Hälfte ab",
    "start": "Download bricht sofort ab",
}


class ReleaseState:
    """Veraenderlicher Zustand des Fakes: Antwort, Download und Protokoll."""

    def __init__(self, tag: str, archive: str, seconds: float) -> None:
        self.log = Log()
        self.archive = archive
        self.initial_tag = tag
        self.initial_seconds = seconds
        self._lock = threading.Lock()
        self.tag = tag
        self.mode = "update"
        self.seconds = seconds
        self.abort = "never"

    def set_release(self, tag: str | None = None, mode: str | None = None) -> None:
        if tag is not None and (not isinstance(tag, str) or not TAG_RE.fullmatch(tag)):
            raise ValueError("Version muss die Form v1.2.3 haben")
        if mode is not None and mode not in RELEASE_MODES:
            raise ValueError("unbekannte Antwort")
        with self._lock:
            if tag is not None and tag != self.tag:
                self.tag = tag
                self.log.add("github", f"Bietet jetzt {tag} an")
            if mode is not None:
                self.mode = mode
                text = MODE_TEXT.get(mode, f"Antwort: Update {self.tag}")
                self.log.add("github", text)

    def set_download(self, seconds: float | None = None, abort: str | None = None) -> None:
        if seconds is not None and (
            isinstance(seconds, bool)
            or not isinstance(seconds, (int, float))
            or not 0 <= seconds <= MAX_SECONDS
        ):
            raise ValueError("Dauer muss zwischen 0 und 120 s liegen")
        if abort is not None and abort not in ABORT_MODES:
            raise ValueError("unbekannter Abbruch")
        with self._lock:
            if seconds is not None:
                self.seconds = float(seconds)
                self.log.add("github", f"Download-Dauer auf {self.seconds:g} s gesetzt")
            if abort is not None:
                self.abort = abort
                self.log.add("github", ABORT_TEXT[abort])

    def reset(self) -> None:
        with self._lock:
            self.tag = self.initial_tag
            self.mode = "update"
            self.seconds = self.initial_seconds
            self.abort = "never"
            self.log.add("github", "Auf den Anfangszustand zurückgesetzt")

    def state(self) -> dict:
        with self._lock:
            return {
                "tag": self.tag,
                "mode": self.mode,
                "seconds": self.seconds,
                "abort": self.abort,
                "archive": self.archive,
                "initial_tag": self.initial_tag,
                "initial_seconds": self.initial_seconds,
            }


def _latest_object(tag: str) -> dict:
    return {
        "tag_name": tag,
        "html_url": f"https://github.com/example/energy-node/releases/tag/{tag}",
        "published_at": "2026-01-01T00:00:00Z",
    }


def make_handler(state: ReleaseState):
    class Handler(http.server.BaseHTTPRequestHandler):
        def _send(self, body, content_type="application/json", status=200, headers=()):
            self.send_response(status)
            self.send_header("Content-Type", content_type)
            self.send_header("Content-Length", str(len(body)))
            for name, value in headers:
                self.send_header(name, value)
            self.end_headers()
            self.wfile.write(body)

        def _json(self, obj, status=200, headers=()):
            self._send(json.dumps(obj).encode("utf-8"), status=status, headers=headers)

        def _check(self, releases):
            snap = state.state()
            if snap["mode"] == "error":
                state.log.add("github", "Update-Check beantwortet mit Fehler 500", "warn")
                self._json({"message": "simulated server error"}, status=500)
                return
            if snap["mode"] == "ratelimit":
                state.log.add("github", "Update-Check beantwortet mit Rate-Limit", "warn")
                self._json({"message": "API rate limit exceeded"}, status=403,
                           headers=[("X-RateLimit-Remaining", "0")])
                return
            tag = NO_UPDATE_TAG if snap["mode"] == "none" else snap["tag"]
            state.log.add("github", f"Update-Check beantwortet mit {tag}")
            if releases and snap["mode"] == "none":
                self._json([])
            elif releases and snap["archive"]:
                self._releases(tag, snap["archive"])
            else:
                self._json(_latest_object(tag))

        def _releases(self, tag, archive):
            name = os.path.basename(archive)
            host = self.headers.get("Host", "127.0.0.1")
            self._json([{
                "tag_name": tag,
                "draft": False,
                "prerelease": False,
                "assets": [{
                    "name": name,
                    "browser_download_url": f"http://{host}/download/{name}",
                    "size": os.path.getsize(archive),
                }],
            }])

        def _download(self, snap):
            archive = snap["archive"]
            abort = snap["abort"]
            with open(archive, "rb") as handle:
                data = handle.read()
            self.send_response(200)
            self.send_header("Content-Type", "application/gzip")
            self.send_header("Content-Length", str(len(data)))
            self.end_headers()
            state.log.add("github", "Download gestartet")
            if abort == "start":
                self.close_connection = True
                state.log.add("github", "Download sofort abgebrochen", "warn")
                return
            limit = len(data) // 2 if abort == "half" else len(data)
            step = max(1, len(data) // CHUNKS)
            for start in range(0, limit, step):
                end = min(start + step, limit)
                self.wfile.write(data[start:end])
                self.wfile.flush()
                time.sleep(snap["seconds"] / CHUNKS)
            if abort == "half":
                self.close_connection = True
                state.log.add("github", "Download abgebrochen bei 50 %", "warn")
            else:
                state.log.add("github", "Download fertig")

        def _control_get(self, url):
            if url.path == "/_fake/state":
                self._json(state.state())
                return
            if url.path == "/_fake/log":
                try:
                    since = int(parse_qs(url.query).get("since", ["0"])[0])
                    if since < 0:
                        raise ValueError
                except ValueError:
                    self._json({"error": "since muss eine Zahl ab 0 sein"}, status=400)
                    return
                last, entries = state.log.since(since)
                self._json({"last": last, "entries": entries})
                return
            self._json({"error": "nicht gefunden"}, status=404)

        def _body(self):
            try:
                length = int(self.headers.get("Content-Length") or 0)
            except ValueError:
                raise ValueError("Content-Length ist ungültig")
            if length < 0 or length > MAX_BODY:
                raise ValueError("Anfrage ist zu groß oder ungültig")
            raw = self.rfile.read(length) if length else b""
            if not raw.strip():
                return {}
            try:
                data = json.loads(raw.decode("utf-8"))
            except (UnicodeDecodeError, json.JSONDecodeError):
                raise ValueError("Ungültiger JSON-Text")
            if not isinstance(data, dict):
                raise ValueError("Erwartet ein JSON-Objekt")
            return data

        def do_GET(self):
            url = urlsplit(self.path)
            if url.path.startswith("/_fake/"):
                self._control_get(url)
                return
            snap = state.state()
            if snap["archive"] and url.path.startswith("/download/"):
                self._download(snap)
            else:
                self._check(url.path.endswith("/releases"))

        def do_POST(self):
            url = urlsplit(self.path)
            try:
                body = self._body()
                if url.path == "/_fake/release":
                    state.set_release(body.get("tag"), body.get("mode"))
                elif url.path == "/_fake/download":
                    state.set_download(body.get("seconds"), body.get("abort"))
                elif url.path == "/_fake/reset":
                    state.reset()
                else:
                    self._json({"error": "nicht gefunden"}, status=404)
                    return
            except ValueError as exc:
                self._json({"error": str(exc)}, status=400)
                return
            self._json(state.state())

        def log_message(self, format, *args):
            pass  # ruhig - der Smoke-Test hat schon genug Logs

    return Handler


def main():
    port = int(sys.argv[1]) if len(sys.argv) > 1 else DEFAULT_PORT
    tag = sys.argv[2] if len(sys.argv) > 2 else DEFAULT_TAG
    archive = sys.argv[3] if len(sys.argv) > 3 else ""
    seconds = float(os.environ.get("FAKE_DOWNLOAD_SECONDS", "4"))
    state = ReleaseState(tag, archive, seconds)
    server = http.server.ThreadingHTTPServer(("127.0.0.1", port), make_handler(state))
    server.serve_forever()


if __name__ == "__main__":
    main()
