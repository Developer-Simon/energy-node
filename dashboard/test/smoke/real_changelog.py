#!/usr/bin/env python3
"""Legt den echten Changelog des Repos in einen simulierten Installer-Zustand.

    real_changelog.py REPO STATE_DIR

Baut changelog.json mit scripts/build/make_changelog_json.py, setzt die
installierten Versionen im installed-manifest.json auf den Stand des Changelogs
und den Vorgaenger-Stand des Dashboards auf die vorletzte Dashboard-Version, damit
"Neuerungen" den Bereich des aktuellen Releases zeigt. Die ersten beiden
Highlights der neuesten Dashboard-Version bekommen Beispiel-Details (eines mit
Bild, Ausschnitt und Doku-Link, eines mit unerreichbarem Bild), solange das Repo
noch keine eigenen Details traegt. Bilder aus docs/images/ werden eingebettet.
"""
import base64
import json
import subprocess
import sys
import urllib.parse
from pathlib import Path

repo, state = Path(sys.argv[1]), Path(sys.argv[2])
subprocess.run([sys.executable, str(repo / "scripts/build/make_changelog_json.py"),
                "--repo", str(repo), "--out", str(state / "changelog.json")], check=True)
changelog = json.loads((state / "changelog.json").read_text())

MANIFEST_KEYS = {"dashboard", "bootstrap", "energy_node_common", "battery_soc_core", "services"}
versions = {c["id"]: c["version"] for c in changelog["components"] if c["id"] in MANIFEST_KEYS}
previous_dashboard = None
for c in changelog["components"]:
    if c["id"] == "dashboard":
        # Vorgaenger = juengster Tag vor der neuesten Dashboard-Version im Repo.
        tags = subprocess.run(["git", "-C", str(repo), "tag", "--list", "v*", "--sort=-v:refname"],
                              capture_output=True, text=True, check=True).stdout.split()
        previous_dashboard = next((t for t in tags if t != c["version"]), "v0.0.0")

for name, previous in (("installed-manifest.json", False), ("previous-manifest.json", True)):
    path = state / name
    manifest = json.loads(path.read_text())
    manifest["components"].update(versions)
    if previous and previous_dashboard:
        manifest["components"]["dashboard"] = previous_dashboard
    manifest["version"] = changelog["bundle_version"] if not previous else previous_dashboard
    path.write_text(json.dumps(manifest, indent=2))

svg = ("<svg xmlns='http://www.w3.org/2000/svg' width='1000' height='600' viewBox='0 0 1000 600'>"
       "<rect width='1000' height='600' fill='#dfe7ee'/><rect x='500' y='120' width='500' height='240' fill='#f2a33a'/>"
       "<text x='750' y='250' font-size='40' text-anchor='middle' font-family='sans-serif'>crop area</text>"
       "<text x='250' y='300' font-size='40' text-anchor='middle' font-family='sans-serif'>whole image</text></svg>")
demo = [
    {"text": {"en": "Demo text for the first highlight. Open and close the row, switch the image between crop and whole image.",
              "de": "Beispieltext für das erste Highlight. Zeile öffnen und schließen, Bild zwischen Ausschnitt und ganzem Bild umschalten."},
     "image": {"url": "data:image/svg+xml," + urllib.parse.quote(svg), "width": 1000, "height": 600, "crop": [0.5, 0.2, 0.5, 0.4]},
     "doc": "https://developer-simon.github.io/energy-node/dashboard/settings-versions.html"},
    {"text": {"en": "Second demo row with an unreachable image. Image and toggle must disappear."},
     "image": {"url": "http://127.0.0.1:9/missing.png", "width": 1000, "height": 600, "crop": [0.1, 0.1, 0.5, 0.5]}},
]
dashboard = next(c for c in changelog["components"] if c["id"] == "dashboard")
entries = [e for r in dashboard["releases"][:1] for g in r["groups"] for e in g["entries"] if e.get("highlight")]
if not any("details" in e for e in entries):
    for entry, details in zip(entries, demo):
        entry["details"] = details

# Die Bild-URLs zeigen auf den Tag oder Commit bei GitHub, den es lokal noch nicht
# gibt. Echte Bilder aus docs/images/ werden deshalb als data-URI eingebettet.
for release in dashboard["releases"]:
    for group in release["groups"]:
        for entry in group["entries"]:
            image = (entry.get("details") or {}).get("image")
            if image and "/docs/images/" in image["url"]:
                png = repo / "docs/images" / image["url"].rsplit("/", 1)[1]
                image["url"] = "data:image/png;base64," + base64.b64encode(png.read_bytes()).decode()
(state / "changelog.json").write_text(json.dumps(changelog, ensure_ascii=False))
print(f"Echter Changelog {changelog['bundle_version']}: {len(entries)} Highlights, {sum(1 for e in entries if e.get('details'))} mit Details")
