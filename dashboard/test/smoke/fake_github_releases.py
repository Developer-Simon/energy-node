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

BEWUSST UNVOLLSTAENDIG: kein Rate-Limit, keine anderen Endpunkte, keine
Authentifizierung, keine Fehlerpfade. Nicht ausserhalb von Tests verwenden.

    python3 fake_github_releases.py [PORT] [TAG] [ARCHIV]

PORT    Standard 18884.
TAG     Der zurueckgelieferte tag_name. Standard "v9.9.9" - bewusst weit ueber
        jeder echten dashboard/VERSION, damit der Vergleich unabhaengig vom
        gerade eingecheckten Stand zuverlaessig "verfuegbar" ergibt.
ARCHIV  Optional: ein Bundle-.tar.gz, das als Release-Asset angeboten wird.
"""
import http.server
import json
import os
import sys
import time

DEFAULT_PORT = 18884
DEFAULT_TAG = "v9.9.9"
CHUNKS = 10


def make_handler(tag, archive):
    latest = json.dumps({
        "tag_name": tag,
        "html_url": f"https://github.com/example/energy-node/releases/tag/{tag}",
        "published_at": "2026-01-01T00:00:00Z",
    }).encode("utf-8")
    seconds = float(os.environ.get("FAKE_DOWNLOAD_SECONDS", "4"))

    class Handler(http.server.BaseHTTPRequestHandler):
        def _send(self, body, content_type="application/json"):
            self.send_response(200)
            self.send_header("Content-Type", content_type)
            self.send_header("Content-Length", str(len(body)))
            self.end_headers()
            self.wfile.write(body)

        def _releases(self):
            name = os.path.basename(archive)
            host = self.headers.get("Host", "127.0.0.1")
            self._send(json.dumps([{
                "tag_name": tag,
                "draft": False,
                "prerelease": False,
                "assets": [{
                    "name": name,
                    "browser_download_url": f"http://{host}/download/{name}",
                    "size": os.path.getsize(archive),
                }],
            }]).encode("utf-8"))

        def _download(self):
            with open(archive, "rb") as handle:
                data = handle.read()
            self.send_response(200)
            self.send_header("Content-Type", "application/gzip")
            self.send_header("Content-Length", str(len(data)))
            self.end_headers()
            step = max(1, len(data) // CHUNKS)
            for start in range(0, len(data), step):
                self.wfile.write(data[start:start + step])
                self.wfile.flush()
                time.sleep(seconds / CHUNKS)

        def do_GET(self):
            path = self.path.split("?", 1)[0]
            if archive and path.startswith("/download/"):
                self._download()
            elif archive and path.endswith("/releases"):
                self._releases()
            else:
                self._send(latest)

        def log_message(self, format, *args):
            pass  # ruhig - der Smoke-Test hat schon genug Logs

    return Handler


def main():
    port = int(sys.argv[1]) if len(sys.argv) > 1 else DEFAULT_PORT
    tag = sys.argv[2] if len(sys.argv) > 2 else DEFAULT_TAG
    archive = sys.argv[3] if len(sys.argv) > 3 else ""
    server = http.server.ThreadingHTTPServer(("127.0.0.1", port), make_handler(tag, archive))
    server.serve_forever()


if __name__ == "__main__":
    main()
