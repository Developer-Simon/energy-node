#!/usr/bin/env bash
# Test for scripts/bootstrap/60-node-install.sh
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
script="$here/../bootstrap/60-node-install.sh"

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
fail() { echo "FAIL: $1"; [ -n "${2:-}" ] && printf '%s\n' "$2"; exit 1; }

mkdir -p "$tmp/bin"
cat > "$tmp/bin/systemctl" <<'SH'
#!/usr/bin/env bash
printf 'systemctl %s\n' "$*" >> "$SYSTEMCTL_LOG"
SH
cat > "$tmp/bin/visudo" <<'SH'
#!/usr/bin/env bash
printf 'visudo %s\n' "$*" >> "$VISUDO_LOG"
exit "${VISUDO_RC:-0}"
SH
chmod +x "$tmp/bin/systemctl" "$tmp/bin/visudo"
export PATH="$tmp/bin:$PATH"
export SYSTEMCTL_LOG="$tmp/systemctl.log" VISUDO_LOG="$tmp/visudo.log"

bundle="$tmp/bundle"
mkdir -p "$bundle/dashboard" "$bundle/config/manifests"
printf '#!/bin/sh\n'    > "$bundle/dashboard/energy-node-dashboard"
printf '[Unit]\n'       > "$bundle/dashboard/energy-node-dashboard.service"
printf '#!/bin/sh\n'    > "$bundle/dashboard/energy-node-dashboard-system-action"
printf 'energynode ALL\n' > "$bundle/dashboard/energy-node-dashboard-system-action.sudoers"
printf '{"mqtt":{}}\n'  > "$bundle/config/config.json"
printf 'v1.4.0\n'       > "$bundle/config/services-VERSION"
printf '{"service_id":"shelly"}\n'  > "$bundle/config/manifests/shelly.json"
printf '{"service_id":"tuya"}\n'    > "$bundle/config/manifests/tuya.json"

export EN_STATE_DIR="$tmp/state" EN_ROOT="$tmp/root" EN_BUNDLE_DIR="$bundle"
export EN_BUNDLE_VERSION=v1.0.0 EN_SUDO="" EN_TARGET_BASE=/home/pruef
export EN_TARGET_USER=pruef

base="$tmp/root/home/pruef"
etc="$tmp/root/etc/energy-node"

printf 'mqtt-geheim\n'  > "$tmp/mqtt.pw"
printf 'admin-geheim\n' > "$tmp/admin.pw"
chmod 600 "$tmp/mqtt.pw" "$tmp/admin.pw"

run() {
  bash "$script" --mqtt-password-file "$tmp/mqtt.pw" \
                 --admin-password-file "$tmp/admin.pw"
}

# --- erster Lauf legt alles an --------------------------------------------
out="$(run)"
grep -q '^##STEP 60 ok$' <<<"$out" || fail "kein ok-Marker" "$out"
[ -x "$base/dashboard/energy-node-dashboard" ] || fail "Binary fehlt oder nicht ausfuehrbar"
[ -f "$tmp/root/etc/systemd/system/energy-node-dashboard.service" ] || fail "Unit fehlt"
[ -x "$tmp/root/usr/local/sbin/energy-node-dashboard-system-action" ] || fail "Helper fehlt"
[ -f "$tmp/root/etc/sudoers.d/energy-node-dashboard-system-action" ] || fail "Sudoers fehlt"
[ -f "$etc/config.json" ] || fail "config.json fehlt"
[ -f "$etc/manifests/shelly.json" ] || fail "Manifest shelly fehlt"
[ -f "$etc/manifests/tuya.json" ] || fail "Manifest tuya fehlt"
[ -f "$etc/mqtt.pw" ] || fail "mqtt.pw fehlt"
[ -f "$tmp/root/etc/energy-node-dashboard/auth.pw" ] || fail "auth.pw fehlt"
[ -f "$base/devices/VERSION" ] || fail "services-VERSION fehlt"
grep -q 'systemctl enable --now energy-node-dashboard.service' "$SYSTEMCTL_LOG" \
  || fail "Dashboard nicht gestartet" "$(cat "$SYSTEMCTL_LOG")"

# --- visudo lief gegen die Datei im Bundle, nicht gegen die installierte ---
grep -q "visudo -cf $bundle/dashboard/" "$VISUDO_LOG" \
  || fail "visudo prueft die falsche Datei" "$(cat "$VISUDO_LOG")"

# --- kein Passwort in der Ausgabe -----------------------------------------
grep -q 'mqtt-geheim' <<<"$out"  && fail "MQTT-Passwort in der Ausgabe" "$out"
grep -q 'admin-geheim' <<<"$out" && fail "Admin-Passwort in der Ausgabe" "$out"

# --- zweiter Lauf ueberspringt --------------------------------------------
: > "$SYSTEMCTL_LOG"
out="$(run)"
grep -q '^##STEP 60 skip bereits erledigt$' <<<"$out" || fail "nicht uebersprungen" "$out"
[ -s "$SYSTEMCTL_LOG" ] && fail "zweiter Lauf hat systemctl aufgerufen"

# --- Betreiberdaten bleiben, Manifestsatz wird ersetzt --------------------
printf '{"mqtt":{"host":"meins"}}\n' > "$etc/config.json"
printf 'mein-passwort\n'             > "$etc/mqtt.pw"
printf '{"service_id":"weg"}\n'      > "$etc/manifests/weg.json"
rm -rf "$tmp/state"
out="$(run)"
grep -q '^##STEP 60 ok$' <<<"$out" || fail "dritter Lauf nicht ok" "$out"
grep -q 'meins' "$etc/config.json" || fail "config.json ueberschrieben" "$(cat "$etc/config.json")"
grep -q 'mein-passwort' "$etc/mqtt.pw" || fail "mqtt.pw ueberschrieben"
[ -e "$etc/manifests/weg.json" ] && fail "veraltetes Manifest nicht entfernt"
[ -f "$etc/manifests/shelly.json" ] || fail "Manifest beim Ersetzen verloren"

# --- kaputte Sudoers-Regel wird nicht installiert -------------------------
rm -rf "$tmp/state" "$tmp/root"
set +e
out="$(VISUDO_RC=1 run)"
rc=$?
set -e
[ "$rc" -eq 1 ] || fail "kaputte Sudoers nicht abgelehnt" "$rc"
grep -q '^##STEP 60 fail SUDOERS_INVALID$' <<<"$out" || fail "falscher Code" "$out"
[ -e "$tmp/root/etc/sudoers.d/energy-node-dashboard-system-action" ] \
  && fail "kaputte Sudoers-Regel wurde trotzdem installiert"

# --- ohne Passwortdateien laeuft der Schritt trotzdem durch --------------
rm -rf "$tmp/state" "$tmp/root"
out="$(bash "$script")"
grep -q '^##STEP 60 ok$' <<<"$out" || fail "Lauf ohne Passwoerter nicht ok" "$out"
[ -e "$etc/mqtt.pw" ] && fail "mqtt.pw ohne Quelle angelegt"

# --- benannte, aber fehlende Passwortdatei ist ein Fehler ----------------
rm -rf "$tmp/state" "$tmp/root"
set +e
out="$(bash "$script" --mqtt-password-file "$tmp/gibtsnicht.pw")"
set -e
grep -q '^##STEP 60 fail SECRET_FILE_MISSING$' <<<"$out" || fail "falscher Code" "$out"

# --- fehlendes Binary ------------------------------------------------------
rm -rf "$tmp/state" "$tmp/root"
rm -f "$bundle/dashboard/energy-node-dashboard"
set +e
out="$(run)"
set -e
grep -q '^##STEP 60 fail DASHBOARD_BINARY_MISSING$' <<<"$out" || fail "falscher Code" "$out"

echo "OK: $(basename "$0")"
