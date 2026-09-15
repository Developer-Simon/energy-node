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

expect_none = ["10", "20", "30", "50", "60", "70"]
for step_id in expect_none:
    got = by_id.get(step_id, {}).get("dashboard_key")
    if got:
        sys.exit("step %s: dashboard_key = %r, erwartet keinen" % (step_id, got))

print("ok")
PY
