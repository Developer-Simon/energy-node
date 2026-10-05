#!/usr/bin/env bash
# Test for the manifest "steps" assembly in scripts/build/make_bundle.sh:
# every dashboard-relevant optional step must carry dashboard_key.
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo="$(cd "$here/../.." && pwd)"

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
fail() { echo "FAIL: $1"; [ -n "${2:-}" ] && printf '%s\n' "$2"; exit 1; }

out="$("$repo/scripts/build/make_bundle.sh" --arch amd64 --python-minor 3.11 \
  --user energynode --base /home/energynode --out "$tmp" --skip-wheels \
  --dashboard-binary <(printf '#!/bin/sh\n') 2>&1)" \
  || fail "make_bundle.sh ist fehlgeschlagen" "$out"

bundle_tar="$(ls "$tmp"/energy-node-*-amd64.tar.gz)"
extract="$tmp/extract"
mkdir -p "$extract"
tar -xzf "$bundle_tar" -C "$extract"

python3 - "$extract/manifest.json" <<'PY'
import json, sys

manifest = json.loads(open(sys.argv[1], encoding="utf-8").read())
by_id = {s["id"]: s for s in manifest["steps"]}

expect_keyed = {
    "40": "tailscale",
    "81": "apsystems",
    "82": "battery_soc",
    "83": "shelly",
    "84": "trucki",
    "85": "tuya",
    "88": "automation",
}
for step_id, want in expect_keyed.items():
    got = by_id.get(step_id, {}).get("dashboard_key")
    if got != want:
        sys.exit("step %s: dashboard_key = %r, erwartet %r" % (step_id, got, want))

expect_none = ["10", "15", "20", "30", "35", "50", "60", "70"]
for step_id in expect_none:
    got = by_id.get(step_id, {}).get("dashboard_key")
    if got:
        sys.exit("step %s: dashboard_key = %r, erwartet keinen" % (step_id, got))

# Opt-in: die Firewall-Freigabe fuer den Shelly-Wake-Webhook ist optional
# und standardmaessig aus; alle anderen optionalen Schritte bleiben an (E7).
step35 = by_id.get("35")
if not step35 or step35.get("optional") is not True or step35.get("default") is not False:
    sys.exit("step 35: erwartet optional mit default false, gefunden %r" % step35)
# Der Webhook-Port ergibt nur mit dem Shelly-Dienst Sinn.
if step35.get("requires") != "83" or by_id.get("83", {}).get("service_id") != "shelly":
    sys.exit("step 35: erwartet requires 83 (shelly), gefunden %r" % step35.get("requires"))
if [s["id"] for s in manifest["steps"] if s.get("requires")] != ["35"]:
    sys.exit("requires steht an anderen Schritten als 35")
for step in manifest["steps"]:
    if step["optional"] and step["id"] != "35" and step.get("default") is not True:
        sys.exit("step %s: optionaler Schritt ohne default true" % step["id"])
# 15 aktualisiert die Systempakete: optional, Vorgabe an, direkt nach 10.
step15 = by_id.get("15")
if not step15 or step15.get("optional") is not True or step15.get("default") is not True:
    sys.exit("step 15: erwartet optional mit default true, gefunden %r" % step15)
ids = [s["id"] for s in manifest["steps"]]
if ids.index("15") != ids.index("10") + 1:
    sys.exit("step 15 steht nicht direkt nach 10: %r" % ids)
if by_id.get("30", {}).get("optional"):
    sys.exit("step 30 ist nicht mehr Kern")

print("ok")
PY

# changelog.json faehrt mit, ist von manifest.files gedeckt, und seine
# Versionen stimmen mit denen im Manifest ueberein - sonst zeigte die
# Oberflaeche einen Changelog zu einer anderen Version als der installierten.
python3 - "$extract" <<'PY'
import json, pathlib, sys

root = pathlib.Path(sys.argv[1])
manifest = json.loads((root / "manifest.json").read_text(encoding="utf-8"))
document = json.loads((root / "changelog.json").read_text(encoding="utf-8"))

if "changelog.json" not in manifest["files"]:
    sys.exit("changelog.json fehlt in manifest.files (kein SHA-256, nicht von der Signatur gedeckt)")
if document["bundle_version"] != manifest["version"]:
    sys.exit("bundle_version %r != manifest.version %r" % (document["bundle_version"], manifest["version"]))

versions = {c["id"]: c["version"] for c in document["components"]}
for name, version in manifest["components"].items():
    if versions.get(name) != version:
        sys.exit("components[%s] = %r, changelog.json sagt %r" % (name, version, versions.get(name)))
for step in manifest["steps"]:
    if step.get("dir") and versions.get("service:" + step["dir"]) != step.get("version"):
        sys.exit("Dienst %s: steps[].version = %r, changelog.json sagt %r"
                 % (step["dir"], step.get("version"), versions.get("service:" + step["dir"])))
print("ok changelog.json")
PY
