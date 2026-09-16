#!/usr/bin/env python3
"""Faelscht die GitHub-Releases-API fuer den Update-Check-Smoke-Test.

Zweck: internal/updatecheck fragt normalerweise https://api.github.com nach
dem neuesten Release (siehe releasing.md). Um die "Update verfuegbar"-Anzeige
(Masthead-Pille, Button in Systemzugriff) ohne einen echten neueren Tag auf
GitHub pruefen zu koennen, beantwortet dieser Server jede Anfrage auf
GET /repos/<repo>/releases/latest immer mit demselben festen, hoeheren Tag -
der Pfad selbst wird nicht ausgewertet. main.go zeigt den Checker per
ENERGY_NODE_UPDATES_API_BASE hierher statt zur echten API.

BEWUSST UNVOLLSTAENDIG: kein Rate-Limit, keine anderen Endpunkte, keine
Authentifizierung, keine Fehlerpfade. Nicht ausserhalb von Tests verwenden.

    python3 fake_github_releases.py [PORT] [TAG]

PORT   Standard 18884.
TAG    Der zurueckgelieferte tag_name. Standard "v9.9.9" - bewusst weit ueber
       jeder echten dashboard/VERSION, damit der Vergleich unabhaengig vom
       gerade eingecheckten Stand zuverlaessig "verfuegbar" ergibt.
"""
import http.server
import json
import sys

DEFAULT_PORT = 18884
DEFAULT_TAG = "v9.9.9"


def make_handler(tag):
    body = json.dumps({
        "tag_name": tag,
        "html_url": f"https://github.com/example/energy-node/releases/tag/{tag}",
        "published_at": "2026-01-01T00:00:00Z",
    }).encode("utf-8")

    class Handler(http.server.BaseHTTPRequestHandler):
        def do_GET(self):
            self.send_response(200)
            self.send_header("Content-Type", "application/json")
            self.send_header("Content-Length", str(len(body)))
            self.end_headers()
            self.wfile.write(body)

        def log_message(self, format, *args):
            pass  # ruhig - der Smoke-Test hat schon genug Logs

    return Handler


def main():
    port = int(sys.argv[1]) if len(sys.argv) > 1 else DEFAULT_PORT
    tag = sys.argv[2] if len(sys.argv) > 2 else DEFAULT_TAG
    server = http.server.HTTPServer(("127.0.0.1", port), make_handler(tag))
    server.serve_forever()


if __name__ == "__main__":
    main()
