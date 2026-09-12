#!/usr/bin/env bash
# Test for scripts/bootstrap/plan.sh
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
script="$here/../bootstrap/plan.sh"

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
fail() { echo "FAIL: $1"; [ -n "${2:-}" ] && printf '%s\n' "$2"; exit 1; }

bundle="$tmp/bundle"
mkdir -p "$bundle"
cat > "$bundle/manifest.json" <<'JSON'
{
  "version": "v0.2.0",
  "components": { "dashboard": "v0.6.1", "services": "v1.4.0" },
  "steps": [
    { "id": "10", "optional": false },
    { "id": "40", "optional": true, "default": true },
    { "id": "50", "optional": false },
    { "id": "81", "optional": true, "default": true, "service_id": "apsystems",
      "dir": "apsystems_ez1", "unit": "apsystems-ez1.service" }
  ]
}
JSON

export EN_STATE_DIR="$tmp/state" EN_BUNDLE_DIR="$bundle"
export EN_BUNDLE_VERSION=v0.2.0 EN_SELECTION="$tmp/selection.json"

mkdir -p "$EN_STATE_DIR/steps"
printf 'bundle=v0.2.0\n' > "$EN_STATE_DIR/steps/10"   # erledigt
printf 'bundle=v0.1.0\n' > "$EN_STATE_DIR/steps/50"   # altes Bundle -> offen
printf '{"steps":{"40":false}}\n' > "$EN_SELECTION"
printf '{"version":"v0.1.0","components":{"dashboard":"v0.6.0"}}\n' \
  > "$EN_STATE_DIR/installed-manifest.json"

out="$(bash "$script")"

# Nicht auf Formatierung pruefen, sondern auf Inhalt.
get() { python3 -c 'import json,sys; d=json.load(sys.stdin); print(eval(sys.argv[1], {"d": d}))' "$1" <<<"$out"; }

[ "$(get 'd["bundle_version"]')" = "v0.2.0" ] || fail "bundle_version falsch" "$out"
[ "$(get 'd["steps"][0]["state"]')" = "done" ] || fail "10 nicht done" "$out"
[ "$(get 'd["steps"][1]["state"]')" = "deselected" ] || fail "40 nicht deselected" "$out"
[ "$(get 'd["steps"][1]["selected"]')" = "False" ] || fail "40 gilt als gewaehlt" "$out"
[ "$(get 'd["steps"][2]["state"]')" = "pending" ] || fail "50 mit altem Stempel nicht pending" "$out"
[ "$(get 'd["steps"][3]["state"]')" = "pending" ] || fail "81 nicht pending" "$out"
[ "$(get 'd["steps"][3]["selected"]')" = "True" ] || fail "81 nicht gewaehlt" "$out"
[ "$(get 'd["components"]["dashboard"]["von"]')" = "v0.6.0" ] || fail "von falsch" "$out"
[ "$(get 'd["components"]["dashboard"]["nach"]')" = "v0.6.1" ] || fail "nach falsch" "$out"
[ "$(get 'd["components"]["services"]["von"]')" = "None" ] || fail "unbekanntes von nicht null" "$out"

# --- ohne installed-manifest.json ist jedes von null ----------------------
rm -f "$EN_STATE_DIR/installed-manifest.json"
out="$(bash "$script")"
[ "$(get 'd["components"]["dashboard"]["von"]')" = "None" ] || fail "von ohne Kopie nicht null" "$out"

# --- ohne selection.json ist alles gewaehlt -------------------------------
rm -f "$EN_SELECTION"
out="$(bash "$script")"
[ "$(get 'd["steps"][1]["selected"]')" = "True" ] || fail "ohne Auswahl nicht gewaehlt" "$out"
[ "$(get 'd["steps"][1]["state"]')" = "pending" ] || fail "ohne Auswahl nicht pending" "$out"

# --- keine ##STEP-Marker --------------------------------------------------
grep -q '^##STEP' <<<"$out" && fail "plan.sh gibt Schritt-Marker aus" "$out"

# --- fehlendes Manifest ---------------------------------------------------
rm -f "$bundle/manifest.json"
set +e
out="$(bash "$script")"
rc=$?
set -e
[ "$rc" -eq 1 ] || fail "fehlendes Manifest nicht gemeldet" "$rc"
[ "${out##*$'\n'}" = "FEHLER BUNDLE_MANIFEST_MISSING" ] || fail "falscher Code" "$out"

echo "OK: $(basename "$0")"
