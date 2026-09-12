#!/usr/bin/env bash
# Test for scripts/bootstrap/lib/service_step.sh
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
lib="$here/../bootstrap/lib/service_step.sh"

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
fail() { echo "FAIL: $1"; [ -n "${2:-}" ] && printf '%s\n' "$2"; exit 1; }

mkdir -p "$tmp/bin"
cat > "$tmp/bin/systemctl" <<'SH'
#!/usr/bin/env bash
printf 'systemctl %s\n' "$*" >> "$SYSTEMCTL_LOG"
SH
chmod +x "$tmp/bin/systemctl"
export PATH="$tmp/bin:$PATH"
export SYSTEMCTL_LOG="$tmp/systemctl.log"

# Ein Bundle mit genau einem Dienst "demo" - beide Klassen von
# Geraetedateien sind vertreten.
bundle="$tmp/bundle"
mkdir -p "$bundle/services/demo/devices"
printf 'print("demo")\n'  > "$bundle/services/demo/demo_mqtt.py"
printf '[Unit]\n'         > "$bundle/services/demo/demo.service"
printf '{"schema":1}\n'   > "$bundle/services/demo/devices/demo_devices.schema.json"
printf '{"presets":1}\n'  > "$bundle/services/demo/devices/demo_presets.json"
printf '{"geraete":[]}\n' > "$bundle/services/demo/devices/demo_devices.json"

export EN_STATE_DIR="$tmp/state" EN_ROOT="$tmp/root" EN_BUNDLE_DIR="$bundle"
export EN_BUNDLE_VERSION=v1.0.0 EN_SUDO="" EN_TARGET_BASE=/home/pruef
export EN_SELECTION="$tmp/selection.json"

base="$tmp/root/home/pruef"
run() { bash -c 'source "$1"; service_step 81 demo demo.service' _ "$lib"; }

# --- erster Lauf legt alles an --------------------------------------------
out="$(run)"
grep -q '^##STEP 81 ok$' <<<"$out" || fail "kein ok-Marker" "$out"
[ -f "$base/demo/demo_mqtt.py" ] || fail "Quelle nicht kopiert"
[ -f "$base/devices/demo_devices.schema.json" ] || fail "Schema nicht kopiert"
[ -f "$base/devices/demo_presets.json" ] || fail "Presets nicht kopiert"
[ -f "$base/devices/demo_devices.json" ] || fail "Geraetevorlage nicht angelegt"
[ -f "$tmp/root/etc/systemd/system/demo.service" ] || fail "Unit nicht installiert"
grep -q 'systemctl enable --now demo.service' "$SYSTEMCTL_LOG" \
  || fail "Dienst nicht gestartet" "$(cat "$SYSTEMCTL_LOG")"

# --- zweiter Lauf ueberspringt --------------------------------------------
: > "$SYSTEMCTL_LOG"
out="$(run)"
grep -q '^##STEP 81 skip bereits erledigt$' <<<"$out" || fail "nicht uebersprungen" "$out"
[ -s "$SYSTEMCTL_LOG" ] && fail "zweiter Lauf hat systemctl aufgerufen"

# --- Betreiberdaten bleiben, Artefakte werden erneuert --------------------
# Das ist der Kern des Tasks: die Geraetedatei bearbeitet das Dashboard live,
# das Schema liefern wir aus. Ein Bundle-Wechsel darf genau eine der beiden
# anfassen.
printf '{"geraete":["meins"]}\n' > "$base/devices/demo_devices.json"
printf 'veraltet\n'              > "$base/devices/demo_devices.schema.json"
rm -rf "$tmp/state"
out="$(run)"
grep -q '^##STEP 81 ok$' <<<"$out" || fail "dritter Lauf nicht ok" "$out"
grep -q 'meins' "$base/devices/demo_devices.json" \
  || fail "Betreiberdaten ueberschrieben" "$(cat "$base/devices/demo_devices.json")"
grep -q 'schema' "$base/devices/demo_devices.schema.json" \
  || fail "Schema nicht erneuert" "$(cat "$base/devices/demo_devices.schema.json")"

# --- abgewaehlt: nichts passiert, kein Stempel ----------------------------
rm -rf "$tmp/state" "$tmp/root"
: > "$SYSTEMCTL_LOG"
printf '{"steps":{"81":false}}\n' > "$EN_SELECTION"
out="$(run)"
grep -q '^##STEP 81 skip nicht ausgewaehlt$' <<<"$out" || fail "nicht abgewaehlt" "$out"
[ -e "$tmp/root" ] && fail "abgewaehlter Dienst hat Dateien angelegt"
[ -e "$tmp/state/steps/81" ] && fail "abgewaehlter Dienst hat einen Stempel hinterlassen"
[ -s "$SYSTEMCTL_LOG" ] && fail "abgewaehlter Dienst hat systemctl aufgerufen"
rm -f "$EN_SELECTION"

# --- fehlendes Quellverzeichnis -------------------------------------------
rm -rf "$tmp/state"
set +e
out="$(bash -c 'source "$1"; service_step 81 fehlt fehlt.service' _ "$lib")"
rc=$?
set -e
[ "$rc" -eq 1 ] || fail "fehlende Quelle nicht gemeldet" "$rc"
grep -q '^##STEP 81 fail SERVICE_SOURCE_MISSING$' <<<"$out" || fail "falscher Code" "$out"

echo "OK: $(basename "$0")"
