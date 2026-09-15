#!/usr/bin/env bash
# Test for scripts/bootstrap/65-dashboard-config.sh
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
script="$here/../bootstrap/65-dashboard-config.sh"

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
fail() { echo "FAIL: $1"; [ -n "${2:-}" ] && printf '%s\n' "$2"; exit 1; }

bundle="$tmp/bundle"
mkdir -p "$bundle"
cat > "$bundle/manifest.json" <<'JSON'
{
  "steps": [
    {"id": "10", "optional": false},
    {"id": "40", "optional": true, "default": true, "dashboard_key": "tailscale"},
    {"id": "70", "optional": true, "default": true},
    {"id": "81", "optional": true, "default": true, "service_id": "apsystems", "dashboard_key": "apsystems"},
    {"id": "88", "optional": true, "default": true, "service_id": "automation", "dashboard_key": "automation"}
  ]
}
JSON

export EN_STATE_DIR="$tmp/state" EN_ROOT="$tmp/root" EN_BUNDLE_DIR="$bundle"
export EN_BUNDLE_VERSION=v1.0.0 EN_SUDO=""
export EN_SELECTION="$tmp/state/selection.json"

etc="$tmp/root/etc/energy-node"
mkdir -p "$etc"
printf '{"mqtt":{}}\n' > "$etc/config.json"

run() { bash "$script"; }

# --- ohne selection.json: alles an -----------------------------------------
out="$(run)"
grep -q '^##STEP 65 ok$' <<<"$out" || fail "kein ok-Marker" "$out"
python3 - "$etc/config.json" <<'PY'
import json, sys
doc = json.load(open(sys.argv[1], encoding="utf-8"))
want = {"tailscale": True, "apsystems": True, "automation": True}
got = doc.get("installed_services")
if got != want:
    sys.exit("installed_services = %r, erwartet %r" % (got, want))
if doc.get("mqtt") != {}:
    sys.exit("fremder Schluessel 'mqtt' wurde veraendert")
PY

# --- mit selection.json: automation abgewaehlt -----------------------------
mkdir -p "$tmp/state"
printf '{"steps":{"88":false}}\n' > "$EN_SELECTION"
out="$(run)"
grep -q '^##STEP 65 ok$' <<<"$out" || fail "kein ok-Marker (2. Lauf)" "$out"
python3 - "$etc/config.json" <<'PY'
import json, sys
doc = json.load(open(sys.argv[1], encoding="utf-8"))
want = {"tailscale": True, "apsystems": True, "automation": False}
got = doc.get("installed_services")
if got != want:
    sys.exit("installed_services = %r, erwartet %r" % (got, want))
PY

echo "ok"
