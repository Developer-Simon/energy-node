import io, json, os, sys, tarfile, urllib.parse

archive, arch = sys.argv[1:3]
manifest = {
    "version": "v9.9.9",
    "arch": arch,
    "components": {"bootstrap": "1.2.0", "dashboard": "9.9.9", "services": "3.4.0"},
    "steps": [
        {"id": "10", "optional": False}, {"id": "20", "optional": False},
        {"id": "30", "optional": False}, {"id": "40", "optional": True},
        {"id": "50", "optional": False}, {"id": "60", "optional": False},
        {"id": "65", "optional": False}, {"id": "70", "optional": True},
        # Zwei Dienstschritte, damit die Vorschau etwas zum Neustart-Vergleich
        # hat: battery_soc geaendert (Neustart), apsystems unveraendert.
        {"id": "81", "optional": True, "service_id": "apsystems", "dir": "apsystems_ez1", "unit": "apsystems-ez1.service", "version": "v0.4.0"},
        {"id": "82", "optional": True, "service_id": "battery_soc", "dir": "battery_soc", "unit": "battery-soc.service", "version": "v0.5.0"},
    ],
}
def entry(text, scope, pr, breaking=False, details=None):
    e = {"text": text, "breaking": breaking, "scope": scope, "pr": pr, "highlight": True}
    if details:
        e["details"] = details
    return e
# Beispiel-Details fuer die Neuerungen-Zeilen: Bild mit Ausschnitt und Doku-Link,
# dazu ein unerreichbares Bild (Bild und Umschalter muessen verschwinden).
SHOT = ("<svg xmlns='http://www.w3.org/2000/svg' width='1000' height='600' viewBox='0 0 1000 600'>"
        "<rect width='1000' height='600' fill='#dfe7ee'/><rect x='500' y='120' width='500' height='240' fill='#f2a33a'/>"
        "<text x='750' y='250' font-size='40' text-anchor='middle' font-family='sans-serif'>crop area</text>"
        "<text x='250' y='300' font-size='40' text-anchor='middle' font-family='sans-serif'>whole image</text></svg>")
DETAILS_SHOT = {
    "text": {"en": "Open a row to see what changed. The image starts on the crop and can show the whole picture.",
             "de": "Zeile öffnen, um zu sehen, was sich geändert hat. Das Bild zeigt zuerst den Ausschnitt, auf Wunsch das ganze Bild."},
    "image": {"url": "data:image/svg+xml," + urllib.parse.quote(SHOT), "width": 1000, "height": 600, "crop": [0.5, 0.2, 0.5, 0.4]},
    "doc": "https://developer-simon.github.io/energy-node/dashboard/settings-versions.html",
}
DETAILS_BROKEN = {
    "text": {"en": "This row has an unreachable image. Image and toggle must disappear."},
    "image": {"url": "http://127.0.0.1:9/missing.png", "width": 1000, "height": 600, "crop": [0.1, 0.1, 0.5, 0.5]},
}
# Ein Commit innerhalb eines PRs: Detail, kein Highlight (make_changelog_json.py).
def detail(text, scope, sha):
    return {"text": text, "breaking": False, "scope": scope, "hash": sha, "highlight": False}

# installiert ist demo/fixtures/installed-state (v0.7.5, battery_soc v0.4.0, apsystems v0.4.0)
# Was ist neu: die Seite zeigt nur, was darueber liegt, mit dem Breaking Change von 9.9.8 oben.
changelog = {
    "schema_version": 1,
    "generated_at": "2026-10-01T12:00:00+02:00",
    "bundle_version": "v9.9.9",
    "components": [
        {"id": "dashboard", "label": "Dashboard", "kind": "app", "version": "9.9.9", "releases": [
            {"version": "9.9.9", "date": "2026-10-01", "groups": [
                {"type": "feat", "label": "Features", "entries": [
                    entry("show what is new before an update", "redeploy", 82, details=DETAILS_SHOT),
                    detail("count the changes per type in the preview card", "redeploy", "a1b2c3d")]},
                {"type": "fix", "label": "Fixes", "entries": [
                    entry("rest the battery flow icon on its fill level at 0 W", "energy", 81, details=DETAILS_BROKEN),
                    detail("keep the changelog filter when the language switches", "redeploy", "b2c3d4e")]},
                {"type": "refactor", "label": "Refactors", "entries": [
                    detail("share the changelog model between installer and dashboard", "redeploy", "c3d4e5f")]}]},
            {"version": "9.9.8", "date": "2026-09-28", "groups": [
                {"type": "feat", "label": "Features", "entries": [
                    entry("add a versions page", "settings", 71),
                    entry("fold the config.json node block into dashboard.node_*", "config", 70, True)]}]},
            {"version": "0.7.5", "date": "2026-09-20", "groups": [
                {"type": "feat", "label": "Features", "entries": [
                    entry("already installed, must not appear", "settings", 60)]}]}]},
        {"id": "service:battery_soc", "label": "Batterie-SoC", "kind": "service", "version": "v0.5.0", "releases": [
            {"version": "v0.5.0", "date": "2026-09-30", "groups": [
                {"type": "feat", "label": "Features", "entries": [
                    entry("save the state on an interval and recover after a crash", "battery_soc", 78)]},
                {"type": "test", "label": "Tests", "entries": [
                    detail("cover a crash between two saves", "battery_soc", "d4e5f6a")]}]}]},
        {"id": "bootstrap", "label": "Bootstrap", "kind": "tool", "version": "1.2.0", "releases": [
            {"version": "1.2.0", "date": "2026-09-29", "groups": [
                {"type": "fix", "label": "Fixes", "entries": [
                    entry("keep the changed service selection in the update preview", "installer", 80)]}]}]},
    ],
}
files = {
    "./manifest.json": json.dumps(manifest).encode(),
    "./changelog.json": json.dumps(changelog).encode(),
    f"./changelog/{manifest['version']}.json": json.dumps({**changelog, "bundle_version": manifest["version"]}).encode(),
    "./manifest.json.sig": b"smoke-test-not-a-real-signature",
    "./bootstrap/10-apt.sh": b"#!/bin/sh\n",
    # Nicht komprimierbar, damit der Download ein paar Sekunden dauert und der
    # Fortschritt (20 %-Schritte) auf dem Bildschirm zu sehen ist.
    "./payload/wheels.bin": os.urandom(6 * 1024 * 1024),
}
with tarfile.open(archive, "w:gz") as tar:
    for name, body in files.items():
        info = tarfile.TarInfo(name)
        info.size = len(body)
        info.mode = 0o755 if name.endswith(".sh") else 0o644
        tar.addfile(info, io.BytesIO(body))
