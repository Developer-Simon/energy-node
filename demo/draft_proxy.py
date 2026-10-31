#!/usr/bin/env python3
"""Entwurfs-Proxy vor der laufenden Demo.

Die Assets des Dashboards sind per go:embed eingebettet. Ein Entwurf, der als
echtes Frontend auf den echten Seiten laufen soll, kommt deshalb ueber diesen
Proxy hinein: er reicht alles an die Demo durch und fuegt in HTML-Antworten
die Dateien eines Entwurfsordners ein. Was er wohin einfuegt, beschreibt
<entwurf>/inject.json. Genau diese Stellen aendert spaeter die Umsetzung in
den Templates, die Datei ist also zugleich der Produktions-Diff.

    demo/demo.sh                                         # Terminal 1
    python3 demo/draft_proxy.py drafts/onboarding        # Terminal 2

Dann https://localhost:18300 oeffnen (eigenes selbstsigniertes Zertifikat).
Die Entwurfsdateien werden bei jedem Aufruf frisch gelesen, Neuladen genuegt.
Nur Standardbibliothek, wie die Fakes.

inject.json:

    {
      "assets": "ob",                    # Ordner, ausgeliefert unter /__draft/
      "page": "<header class=\\"masthead\\">",  # Kennzeichen einer ganzen Seite
      "head": ["x.css", "x.js"],         # vor </head> einer ganzen Seite
      "insert": [                        # Fragmente vor eine Marke setzen
        {"before": "</body>", "fragment": ["ob/a.html", "ob/b.html"]},
        {"when": "x-ref=\\"subnav\\"", "before": "<section id=\\"x\\"", "fragment": "ob/c.html"}
      ],
      "replace": [                       # Textersetzung in einer Antwort
        {"path": "/static/js/x.js", "find": "alt", "with": "neu"}
      ]
    }

"when" ist die Marke, an der eine Antwort erkannt wird (Standard: "before").
Fehlt danach die Einfuegemarke oder ein "find", meldet der Proxy das in der
Konsole: das Template hat sich geaendert und der Entwurf muss nach.
"""
from __future__ import annotations

import argparse
import http.client
import http.server
import json
import mimetypes
import ssl
import subprocess
import sys
from pathlib import Path
from urllib.parse import urlsplit

ASSET_PREFIX = "/__draft/"
HOP = {"connection", "keep-alive", "transfer-encoding", "upgrade", "proxy-connection",
       "te", "trailer", "content-length", "content-encoding", "host", "accept-encoding"}


def warn(message: str) -> None:
    print(f"WARNUNG: {message}", file=sys.stderr, flush=True)


def load_manifest(draft: Path) -> dict:
    manifest = json.loads((draft / "inject.json").read_text(encoding="utf-8"))
    manifest.setdefault("assets", ".")
    manifest.setdefault("head", [])
    manifest.setdefault("insert", [])
    manifest.setdefault("replace", [])
    return manifest


def head_tags(names: list[str]) -> str:
    tags = []
    for name in names:
        url = ASSET_PREFIX + name
        if name.endswith(".css"):
            tags.append(f'<link rel="stylesheet" href="{url}">')
        else:
            tags.append(f'<script src="{url}" defer></script>')
    return "\n".join(tags) + "\n"


def insert_before(text: str, marker: str, extra: str) -> tuple[str, bool]:
    at = text.find(marker)
    if at < 0:
        return text, False
    return text[:at] + extra + text[at:], True


def patch_html(html: str, manifest: dict, read) -> str:
    """Setzt Kopf-Dateien und Fragmente ein. read(pfad) liefert ein Fragment."""
    page = manifest.get("page")
    if page and page in html and manifest["head"]:
        html, ok = insert_before(html, "</head>", head_tags(manifest["head"]))
        if not ok:
            warn("</head> fehlt in einer ganzen Seite")
    for rule in manifest["insert"]:
        when = rule.get("when", rule["before"])
        if when not in html:
            continue
        files = rule["fragment"] if isinstance(rule["fragment"], list) else [rule["fragment"]]
        html, ok = insert_before(html, rule["before"], "".join(read(name) for name in files))
        if not ok:
            warn(f"Einfuegemarke fehlt: {rule['before'][:70]}")
    return html


def patch_text(path: str, text: str, manifest: dict) -> str:
    for rule in manifest["replace"]:
        if rule["path"] != path:
            continue
        if rule["find"] not in text:
            warn(f"{path}: Text zum Ersetzen fehlt: {rule['find'][:60]}")
            continue
        text = text.replace(rule["find"], rule["with"], 1)
    return text


def ensure_cert(directory: Path) -> tuple[Path, Path]:
    directory.mkdir(parents=True, exist_ok=True)
    crt, key = directory / "draft-proxy.pem", directory / "draft-proxy.key"
    if not crt.exists():
        subprocess.run(["openssl", "req", "-x509", "-newkey", "rsa:2048", "-nodes", "-days", "365",
                        "-subj", "/CN=localhost", "-keyout", str(key), "-out", str(crt)],
                       check=True, capture_output=True)
    return crt, key


def make_handler(draft: Path, upstream: str):
    target = urlsplit(upstream)
    # Die Demo hat ein selbstsigniertes Zertifikat.
    insecure = ssl.create_default_context()
    insecure.check_hostname = False
    insecure.verify_mode = ssl.CERT_NONE

    class Handler(http.server.BaseHTTPRequestHandler):
        protocol_version = "HTTP/1.1"

        def log_message(self, fmt, *args):  # nur Fehler und Warnungen
            pass

        def serve_asset(self) -> None:
            manifest = load_manifest(draft)
            root = (draft / manifest["assets"]).resolve()
            file = (root / self.path[len(ASSET_PREFIX):].split("?")[0]).resolve()
            if root not in file.parents or not file.is_file():
                self.send_error(404)
                return
            body = file.read_bytes()
            self.send_response(200)
            self.send_header("Content-Type", mimetypes.guess_type(file.name)[0] or "application/octet-stream")
            self.send_header("Content-Length", str(len(body)))
            self.send_header("Cache-Control", "no-store")
            self.end_headers()
            self.wfile.write(body)

        def proxy(self) -> None:
            if self.path.startswith(ASSET_PREFIX):
                self.serve_asset()
                return
            length = int(self.headers.get("Content-Length") or 0)
            body = self.rfile.read(length) if length else None
            headers = {k: v for k, v in self.headers.items() if k.lower() not in HOP}
            own = f"https://{self.headers.get('Host', '')}"
            for key in ("Origin", "Referer"):
                if key in headers:
                    headers[key] = headers[key].replace(own, upstream)
            headers["Host"] = target.netloc
            headers["Accept-Encoding"] = "identity"
            conn = http.client.HTTPSConnection(target.hostname, target.port or 443, context=insecure, timeout=None)
            try:
                conn.request(self.command, self.path, body=body, headers=headers)
                up = conn.getresponse()
                self.relay(up)
            except (BrokenPipeError, ConnectionResetError):
                pass
            finally:
                conn.close()

        def relay(self, up: http.client.HTTPResponse) -> None:
            ctype = up.getheader("Content-Type", "")
            path = self.path.split("?")[0]
            passthrough = [(k, v) for k, v in up.getheaders() if k.lower() not in HOP]
            manifest = load_manifest(draft)
            touches = ctype.startswith("text/html") or any(r["path"] == path for r in manifest["replace"])
            if touches and self.command != "HEAD":
                text = up.read().decode("utf-8")
                if ctype.startswith("text/html"):
                    text = patch_html(text, manifest, lambda name: (draft / name).read_text(encoding="utf-8"))
                text = patch_text(path, text, manifest)
                data = text.encode("utf-8")
                self.send_response(up.status, up.reason)
                for k, v in passthrough:
                    if k.lower() != "cache-control":
                        self.send_header(k, v)
                self.send_header("Content-Length", str(len(data)))
                self.send_header("Cache-Control", "no-store")
                self.end_headers()
                self.wfile.write(data)
                return
            # Alles andere, auch der Ereignisstrom (SSE), laeuft stueckweise durch.
            self.send_response(up.status, up.reason)
            for k, v in passthrough:
                self.send_header(k, v)
            if self.command == "HEAD" or up.status in (204, 304):
                self.send_header("Content-Length", "0")
                self.end_headers()
                return
            self.send_header("Transfer-Encoding", "chunked")
            self.end_headers()
            while chunk := up.read1(65536):
                self.wfile.write(b"%x\r\n%s\r\n" % (len(chunk), chunk))
                self.wfile.flush()
            self.wfile.write(b"0\r\n\r\n")

        do_GET = do_POST = do_PUT = do_PATCH = do_DELETE = do_HEAD = do_OPTIONS = proxy

    return Handler


def main() -> None:
    parser = argparse.ArgumentParser(description="Entwurfs-Proxy vor der Demo")
    parser.add_argument("draft", type=Path, help="Entwurfsordner mit inject.json")
    parser.add_argument("--port", type=int, default=18300)
    parser.add_argument("--upstream", default="https://localhost:18200")
    args = parser.parse_args()
    draft = args.draft.resolve()
    load_manifest(draft)

    server = http.server.ThreadingHTTPServer(("127.0.0.1", args.port), make_handler(draft, args.upstream))
    server.daemon_threads = True
    crt, key = ensure_cert(Path(__file__).resolve().parent / ".run" / "draft-proxy")
    ctx = ssl.create_default_context(ssl.Purpose.CLIENT_AUTH)
    ctx.load_cert_chain(crt, key)
    server.socket = ctx.wrap_socket(server.socket, server_side=True)
    print(f"Entwurf {draft.name}: https://localhost:{args.port}  (vor {args.upstream})", flush=True)
    try:
        server.serve_forever()
    except KeyboardInterrupt:
        pass


if __name__ == "__main__":
    main()
