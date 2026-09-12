#!/usr/bin/env bash
# Test for scripts/build/lib/manifests.sh
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
lib="$here/../build/lib/manifests.sh"
repo="$(cd "$here/../.." && pwd)"

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
fail() { echo "FAIL: $1"; [ -n "${2:-}" ] && printf '%s\n' "$2"; exit 1; }

write_manifest() {
  mkdir -p "$tmp/services/$1"
  printf '{"service_id":"%s","unit":"%s","schema":"config.schema.json","bootstrap_step":"%s","required":["service_id"]}\n' \
    "$2" "$3" "$4" > "$tmp/services/$1/manifest.json"
  touch "$tmp/services/$1/$3"
}

# --- zwei Dienste ergeben zwei Zeilen in bekannter Form -------------------
write_manifest apsystems_ez1 apsystems apsystems-ez1.service 81
write_manifest tuya_mqtt     tuya      tuya.service          85

out="$(bash -c '
  source "$1"
  load_service_table "$2"
  printf "%s\n" "${SERVICE_TABLE[@]}"
  echo ---
  printf "%s\n" "${SERVICE_STEPS[@]}"
' _ "$lib" "$tmp/services")"

grep -qx 'apsystems-ez1.service:services/apsystems_ez1/apsystems-ez1.service:apsystems_ez1:1' <<<"$out" \
  || fail "Eintrag fuer apsystems fehlt oder hat ein anderes Format" "$out"
grep -qx 'tuya.service:services/tuya_mqtt/tuya.service:tuya_mqtt:1' <<<"$out" \
  || fail "Eintrag fuer tuya fehlt" "$out"
grep -qx 'apsystems_ez1:81' <<<"$out" || fail "SERVICE_STEPS fehlt 81" "$out"
grep -qx 'tuya_mqtt:85' <<<"$out" || fail "SERVICE_STEPS fehlt 85" "$out"

# --- fehlendes bootstrap_step ist ein Fehler ------------------------------
printf '{"service_id":"x","unit":"x.service","schema":"config.schema.json","required":["service_id"]}\n' \
  > "$tmp/services/tuya_mqtt/manifest.json"
set +e
bash -c 'source "$1"; load_service_table "$2"' _ "$lib" "$tmp/services" 2>"$tmp/err"
rc=$?
set -e
[ "$rc" -ne 0 ] || fail "fehlendes bootstrap_step nicht bemaengelt"
grep -q 'bootstrap_step' "$tmp/err" || fail "Meldung nennt das Feld nicht" "$(cat "$tmp/err")"

# --- doppelte Schritt-ID ist ein Fehler -----------------------------------
write_manifest tuya_mqtt tuya tuya.service 81
set +e
bash -c 'source "$1"; load_service_table "$2"' _ "$lib" "$tmp/services" 2>"$tmp/err"
rc=$?
set -e
[ "$rc" -ne 0 ] || fail "doppelte Schritt-ID nicht bemaengelt"

# --- leeres Verzeichnis ist ein Fehler ------------------------------------
set +e
bash -c 'source "$1"; load_service_table "$2"' _ "$lib" "$tmp/leer" 2>/dev/null
rc=$?
set -e
[ "$rc" -ne 0 ] || fail "leeres Verzeichnis nicht bemaengelt"

# --- gegen das echte Repo: genau die sechs bekannten Dienste -------------
# Das ist der Drift-Waechter zwischen den Manifesten und den 8x-Skripten.
out="$(bash -c '
  source "$1"
  load_service_table "$2/services"
  printf "%s\n" "${SERVICE_STEPS[@]}" | sort
' _ "$lib" "$repo")"
expected="apsystems_ez1:81
automation:88
battery_soc:82
shelly:83
trucki:84
tuya_mqtt:85"
[ "$out" = "$expected" ] || fail "Repo-Manifeste weichen von den 8x-Skripten ab" "$out"

for entry in $expected; do
  dir="${entry%%:*}"; id="${entry##*:}"
  ls "$repo/scripts/bootstrap/${id}-"*.sh >/dev/null 2>&1 \
    || fail "kein Bootstrap-Skript fuer Schritt $id ($dir)"
done

echo "OK: $(basename "$0")"
