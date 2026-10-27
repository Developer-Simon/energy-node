#!/usr/bin/env python3
"""Fake-Konsole fuer lokale Dashboard-Smoke-Tests.

Zweck: eine Bedienoberflaeche mit Reitern, ueber die man im Entwicklungs-
rechner Gegenstellen (Companion, Verlauf, Home Assistant) steuern und
beobachten kann, ohne Pi und ohne echte Dienste. Jeder Reiter ist eine Tab-
Klasse mit Zustand und Aktionen. Die Seite selbst ist ein eingebetteter
String, die Fragmente der Reiter kommen per /api/tabs.

Routen:
    GET  /                          Seite mit Reitern
    GET  /api/tabs                  [{"name", "title", "html"}]
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
import threading
import time
import urllib.parse
from collections import deque

DEFAULT_PORT = 18102
LOG_SIZE = 500

PAGE_HTML = """<!doctype html>
<html lang="de">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Fake-Konsole</title>
<style>
:root {
  --bg: #f5f5f4;
  --fg: #1c1c1a;
  --muted: #6b6b66;
  --card: #ffffff;
  --accent: #2f6fd6;
  --border: #d6d6d0;
}
@media (prefers-color-scheme: dark) {
  :root {
    --bg: #161614;
    --fg: #ecece8;
    --muted: #9a9a94;
    --card: #222220;
    --accent: #7aa7f0;
    --border: #3a3a36;
  }
}
body {
  margin: 0;
  background: var(--bg);
  color: var(--fg);
  font-family: system-ui, sans-serif;
}
header {
  padding: 16px;
  border-bottom: 1px solid var(--border);
}
header h1 {
  margin: 0;
  font-size: 1.25rem;
}
nav {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  padding: 12px 16px 0;
}
nav button {
  padding: 6px 12px;
  border: 1px solid var(--border);
  border-radius: 6px;
  background: var(--card);
  color: var(--fg);
  cursor: pointer;
}
nav button[aria-selected="true"] {
  border-color: var(--accent);
  color: var(--accent);
}
main {
  min-width: 0;
}
#layout {
  display: grid;
  gap: 16px;
  padding: 16px;
}
#log-pane {
  min-width: 0;
  border: 1px solid var(--border);
  border-radius: 8px;
  background: var(--card);
  padding: 12px;
}
#log-pane h2 {
  margin: 0 0 8px;
  font-size: 1rem;
}
#log-list {
  list-style: none;
  margin: 0;
  padding: 0;
  max-height: 70vh;
  overflow: auto;
  font-family: ui-monospace, monospace;
  font-size: 0.85rem;
}
#log-list li {
  padding: 2px 0;
  border-bottom: 1px solid var(--border);
  overflow-wrap: anywhere;
}
@media (min-width: 900px) {
  #layout {
    grid-template-columns: minmax(0, 1fr) minmax(280px, 380px);
    align-items: start;
  }
}
section {
  display: none;
}
section.active {
  display: block;
}
</style>
</head>
<body>
<header><h1>Fake-Konsole</h1></header>
<nav id="tabs" role="tablist"></nav>
<div id="layout">
<main id="panels"></main>
<aside id="log-pane"><h2>Protokoll</h2><ul id="log-list"></ul></aside>
</div>
<script>
window.tabs = window.tabs || {};
(function () {
  const nav = document.getElementById("tabs");
  const panels = document.getElementById("panels");

  function show(name) {
    for (const b of nav.querySelectorAll("button")) {
      b.setAttribute("aria-selected", String(b.dataset.tab === name));
    }
    for (const s of panels.querySelectorAll("section")) {
      s.classList.toggle("active", s.dataset.tab === name);
    }
  }

  async function refresh(name) {
    try {
      const resp = await fetch("/api/" + name + "/state");
      if (!resp.ok) return;
      const state = await resp.json();
      const tab = window.tabs[name];
      if (tab && typeof tab.render === "function") tab.render(state);
    } catch (err) {
      /* Netzwerkfehler: naechster Durchlauf versucht es erneut */
    }
  }

  let logLast = 0;
  const logList = document.getElementById("log-list");
  async function pollLog() {
    try {
      const resp = await fetch("/api/log?since=" + logLast);
      if (!resp.ok) return;
      const data = await resp.json();
      logLast = data.last;
      for (const e of data.entries) {
        const li = document.createElement("li");
        li.textContent = e.t + " " + e.source + ": " + e.text;
        logList.appendChild(li);
      }
      while (logList.children.length > 300) logList.removeChild(logList.firstChild);
      logList.scrollTop = logList.scrollHeight;
    } catch (err) {
      /* naechster Durchlauf versucht es erneut */
    }
  }

  async function load() {
    const resp = await fetch("/api/tabs");
    const list = await resp.json();
    for (const t of list) {
      const b = document.createElement("button");
      b.type = "button";
      b.textContent = t.title;
      b.dataset.tab = t.name;
      b.setAttribute("role", "tab");
      b.addEventListener("click", () => show(t.name));
      nav.appendChild(b);

      const s = document.createElement("section");
      s.dataset.tab = t.name;
      s.innerHTML = t.html;
      panels.appendChild(s);
      // innerHTML startet keine Skripte: die Skripte des Fragments neu anlegen
      for (const old of s.querySelectorAll("script")) {
        const fresh = document.createElement("script");
        fresh.textContent = old.textContent;
        old.replaceWith(fresh);
      }
    }
    if (list.length) show(list[0].name);
    for (const t of list) refresh(t.name);
    setInterval(() => { for (const t of list) refresh(t.name); }, 2000);
    pollLog();
    setInterval(pollLog, 1000);
  }

  load();
})();
</script>
</body>
</html>
"""


class Log:
    """Ringpuffer fuer Meldungen mit fortlaufender Nummer."""

    def __init__(self, size: int = LOG_SIZE) -> None:
        self._entries = deque(maxlen=size)
        self._counter = 0
        self._lock = threading.Lock()

    def add(self, source: str, text: str) -> None:
        with self._lock:
            self._counter += 1
            self._entries.append({
                "n": self._counter,
                "t": time.strftime("%H:%M:%S"),
                "source": source,
                "text": text,
            })

    def since(self, n: int) -> tuple[int, list[dict]]:
        with self._lock:
            entries = [dict(e) for e in self._entries if e["n"] > n]
            return self._counter, entries


class Tab:
    """Basisklasse fuer einen Reiter. Unterklassen setzen name, title, html."""

    name: str = ""
    title: str = ""
    html: str = ""

    def state(self) -> dict:
        return {}

    def act(self, action: str, body: dict) -> dict:
        raise ValueError(f"unbekannte Aktion {action}")


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
            self._json(200, [{"name": t.name, "title": t.title, "html": t.html}
                             for t in self.server.tabs])
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

    def do_POST(self) -> None:
        url = urllib.parse.urlsplit(self.path)
        segs = [p for p in url.path.split("/") if p]
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
