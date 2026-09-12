#!/usr/bin/env bash
# Test for scripts/bootstrap/20-mosquitto.sh
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
script="$here/../bootstrap/20-mosquitto.sh"

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
fail() { echo "FAIL: $1"; [ -n "${2:-}" ] && printf '%s\n' "$2"; exit 1; }

mkdir -p "$tmp/bin"
# mosquitto_passwd liest das Passwort von stdin und legt eine Datei an.
cat > "$tmp/bin/mosquitto_passwd" <<'SH'
#!/usr/bin/env bash
printf 'mosquitto_passwd %s\n' "$*" >> "$MOSQ_LOG"
file=""; user=""
while [ $# -gt 0 ]; do
  case "$1" in -c) file="$2"; shift 2 ;; *) user="$1"; shift ;;
  esac
done
read -r pw
printf '%s:%s\n' "$user" "$pw" > "$file"
SH
cat > "$tmp/bin/systemctl" <<'SH'
#!/usr/bin/env bash
printf 'systemctl %s\n' "$*" >> "$SYSTEMCTL_LOG"
SH
chmod +x "$tmp/bin/mosquitto_passwd" "$tmp/bin/systemctl"

export PATH="$tmp/bin:$PATH"
export EN_STATE_DIR="$tmp/state" EN_ROOT="$tmp/root" EN_BUNDLE_VERSION=v1.0.0 EN_SUDO=""
export MOSQ_LOG="$tmp/mosq.log" SYSTEMCTL_LOG="$tmp/systemctl.log"

pw="$tmp/mqtt.pw"
printf 'geheim123\n' > "$pw"
chmod 600 "$pw"

run() { bash "$script" --user knoten --password-file "$pw"; }

# --- erster Lauf schreibt conf und passwd ----------------------------------
out="$(run)"
grep -q '^##STEP 20 ok$' <<<"$out" || fail "kein ok-Marker" "$out"
conf="$tmp/root/etc/mosquitto/conf.d/default.conf"
[ -f "$conf" ] || fail "default.conf fehlt"
grep -qx 'listener 1883' "$conf" || fail "listener fehlt" "$(cat "$conf")"
grep -qx 'allow_anonymous false' "$conf" || fail "allow_anonymous fehlt" "$(cat "$conf")"
grep -qx 'password_file /etc/mosquitto/passwd' "$conf" || fail "password_file fehlt" "$(cat "$conf")"
grep -q 'knoten:geheim123' "$tmp/root/etc/mosquitto/passwd" || fail "passwd falsch"

# --- das Passwort steht nie in argv ---------------------------------------
grep -q 'geheim123' "$MOSQ_LOG" && fail "Passwort in der Kommandozeile" "$(cat "$MOSQ_LOG")"
grep -q 'geheim123' <<<"$out" && fail "Passwort in der Ausgabe" "$out"

# --- zweiter Lauf ueberspringt --------------------------------------------
: > "$MOSQ_LOG"
out="$(run)"
grep -q '^##STEP 20 skip' <<<"$out" || fail "zweiter Lauf nicht uebersprungen" "$out"
[ -s "$MOSQ_LOG" ] && fail "zweiter Lauf hat mosquitto_passwd aufgerufen"

# --- fremde Konfiguration wird nicht angefasst ----------------------------
rm -rf "$tmp/state"
printf 'listener 8883\n' > "$conf"
set +e
out="$(run)"
rc=$?
set -e
[ "$rc" -eq 1 ] || fail "fremde conf nicht abgelehnt" "$rc"
grep -q '^##STEP 20 fail MOSQUITTO_CONF_FOREIGN$' <<<"$out" || fail "falscher Fehlercode" "$out"
grep -qx 'listener 8883' "$conf" || fail "fremde conf wurde veraendert" "$(cat "$conf")"

# --- fehlende Argumente ----------------------------------------------------
set +e
out="$(bash "$script")"
set -e
grep -q '^##STEP 20 fail MOSQUITTO_ARGS_MISSING$' <<<"$out" || fail "fehlende Argumente nicht erkannt" "$out"

echo "OK: $(basename "$0")"
