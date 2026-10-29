#!/usr/bin/env python3
"""Fake-Konsole fuer lokale Dashboard-Smoke-Tests.

Zweck: eine Bedienoberflaeche mit Reitern, ueber die man im Entwicklungs-
rechner Gegenstellen (Companion, Verlauf, Home Assistant) steuern und
beobachten kann, ohne Pi und ohne echte Dienste. Jeder Reiter ist eine Tab-
Klasse mit Zustand und Aktionen. Die Seite selbst ist ein eingebetteter
String, die Fragmente der Reiter kommen per /api/tabs.

Routen:
    GET  /                          Seite mit Reitern
    GET  /api/tabs                  [{"name", "title", "html", "sources"}]
    GET  /api/status                [{"name", "level", "text"}]
    POST /api/reset                 Alle Reiter auf Start, Antwort wie /api/status
    GET  /api/<tab>/state           Zustand des Reiters
    POST /api/<tab>/<action>        Aktion mit JSON-Body, Antwort = neuer Zustand
    GET  /api/log?since=<n>         {"last": n, "entries": [...]}

Ungueltige Eingaben (ValueError) antworten mit 400 und {"error": text}.
Unbekannte Tabs oder Pfade antworten mit 404 und JSON.

    python3 fake_console.py [--port PORT] [--dashboard URL] [--companion URL]

PORT       Standard aus DASHBOARD_SMOKE_CONSOLE_PORT, sonst 18102.
--dashboard  Basis-URL des Dashboards (wird in Reitern verwendet).
--companion  Basis-URL des Companions, z. B. https://127.0.0.1:8081.
"""
import argparse
import errno
import http.server
import json
import os
import sys
import urllib.parse

from console_page import PAGE_HTML
from fakelog import Log

DEFAULT_PORT = 18102


class Tab:
    """Basisklasse fuer einen Reiter. Unterklassen setzen name, title, html
    und bei Bedarf sources (Protokollquellen des Reiters)."""

    name: str = ""
    title: str = ""
    html: str = ""
    sources: tuple[str, ...] = ()

    def state(self) -> dict:
        return {}

    def act(self, action: str, body: dict) -> dict:
        raise ValueError(f"unbekannte Aktion {action}")

    def summary(self) -> dict:
        return {"level": "ok", "text": ""}

    def reset(self) -> None:
        pass


def status_list(tabs: list[Tab]) -> list[dict]:
    """Zusammenfassung je Reiter. Ein Reiter, der nicht antwortet, gilt als Fehler."""
    result = []
    for tab in tabs:
        try:
            info = tab.summary()
        except Exception:
            info = {"level": "error", "text": "nicht erreichbar"}
        result.append({"name": tab.name, "level": info["level"], "text": info["text"]})
    return result


class _Handler(http.server.BaseHTTPRequestHandler):
    server: "_Server"

    def log_message(self, format, *args):  # Zugriffsprotokoll unterdruecken
        pass

    def _send(self, status: int, ctype: str, body: bytes) -> None:
        self.send_response(status)
        self.send_header("Content-Type", ctype)
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def _json(self, status: int, data) -> None:
        self._send(status, "application/json; charset=utf-8",
                   json.dumps(data, ensure_ascii=False).encode())

    def _error(self, status: int, text: str) -> None:
        self._json(status, {"error": text})

    def _tab(self, name: str):
        for tab in self.server.tabs:
            if tab.name == name:
                return tab
        return None

    def _body(self) -> dict:
        length = int(self.headers.get("Content-Length") or 0)
        if length == 0:
            return {}
        return json.loads(self.rfile.read(length).decode("utf-8"))

    def do_GET(self) -> None:
        url = urllib.parse.urlsplit(self.path)
        segs = [p for p in url.path.split("/") if p]
        if not segs:
            self._send(200, "text/html; charset=utf-8", PAGE_HTML.encode())
            return
        if segs == ["api", "tabs"]:
            self._json(200, [{"name": t.name, "title": t.title, "html": t.html,
                              "sources": list(t.sources)}
                             for t in self.server.tabs])
            return
        if segs == ["api", "status"]:
            self._json(200, status_list(self.server.tabs))
            return
        if segs == ["api", "log"]:
            query = urllib.parse.parse_qs(url.query)
            try:
                since = int(query.get("since", ["0"])[0])
            except ValueError:
                since = 0
            last, entries = self.server.log.since(since)
            self._json(200, {"last": last, "entries": entries})
            return
        if len(segs) == 3 and segs[0] == "api" and segs[2] == "state":
            tab = self._tab(segs[1])
            if tab is None:
                self._error(404, "unbekannter Tab")
                return
            self._json(200, tab.state())
            return
        self._error(404, "nicht gefunden")

    def _reset(self) -> None:
        for tab in self.server.tabs:
            try:
                tab.reset()
            except (ValueError, OSError) as exc:
                self.server.log.add("konsole", f"{tab.title}: {exc}", "error")
        self.server.log.add("konsole", "Alles auf Start zurückgesetzt")
        self._json(200, status_list(self.server.tabs))

    def do_POST(self) -> None:
        url = urllib.parse.urlsplit(self.path)
        segs = [p for p in url.path.split("/") if p]
        if segs == ["api", "reset"]:
            self._reset()
            return
        if len(segs) != 3 or segs[0] != "api":
            self._error(404, "nicht gefunden")
            return
        tab = self._tab(segs[1])
        if tab is None:
            self._error(404, "unbekannter Tab")
            return
        try:
            body = self._body()
        except (ValueError, UnicodeDecodeError):
            self._error(400, "ungültiger JSON-Body")
            return
        try:
            self._json(200, tab.act(segs[2], body))
        except ValueError as exc:
            self._error(400, str(exc))


class _Server(http.server.ThreadingHTTPServer):
    daemon_threads = True


def make_server(tabs: list[Tab], log: Log, port: int) -> http.server.ThreadingHTTPServer:
    """Bindet an 127.0.0.1. Port 0 waehlt einen freien Port."""
    server = _Server(("127.0.0.1", port), _Handler)
    server.tabs = tabs
    server.log = log
    return server


def build_tabs(args: argparse.Namespace, log: Log) -> list[Tab]:
    tabs: list[Tab] = []
    if args.companion and args.dashboard:
        # Import erst hier: console_companion importiert Tab und Log aus diesem Modul.
        from console_companion import CompanionTab
        tabs.append(CompanionTab(log, args.companion, args.dashboard))
    return tabs


def main(argv: list[str]) -> int:
    parser = argparse.ArgumentParser(description="Fake-Konsole fuer Smoke-Tests")
    parser.add_argument("--port", type=int,
                        default=int(os.environ.get("DASHBOARD_SMOKE_CONSOLE_PORT", DEFAULT_PORT)))
    parser.add_argument("--dashboard", default="")
    parser.add_argument("--companion", default="")
    args = parser.parse_args(argv)

    log = Log()
    try:
        server = make_server(build_tabs(args, log), log, args.port)
    except OSError as exc:
        if exc.errno != errno.EADDRINUSE:
            raise
        print(f"Port {args.port} ist belegt", file=sys.stderr)
        return 2

    print(f"Fake-Konsole: http://127.0.0.1:{server.server_address[1]}", flush=True)
    try:
        server.serve_forever()
    except KeyboardInterrupt:
        pass
    finally:
        server.server_close()
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
