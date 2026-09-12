#!/usr/bin/env bash
# Test for scripts/bootstrap/40-tailscale.sh
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
script="$here/../bootstrap/40-tailscale.sh"

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
fail() { echo "FAIL: $1"; [ -n "${2:-}" ] && printf '%s\n' "$2"; exit 1; }

mkdir -p "$tmp/bin"
# TS_STATUS_RC=0 bedeutet angemeldet; echtes `tailscale status` endet mit
# einem Fehler, solange der Node ausgeloggt ist.
cat > "$tmp/bin/tailscale" <<'SH'
#!/usr/bin/env bash
printf 'tailscale %s\n' "$*" >> "$TS_LOG"
case "$1" in
  status) exit "${TS_STATUS_RC:-0}" ;;
  up) printf 'To authenticate, visit:\n\nhttps://login.tailscale.com/a/abc123\n' ;;
esac
SH
cat > "$tmp/bin/systemctl" <<'SH'
#!/usr/bin/env bash
printf 'systemctl %s\n' "$*" >> "$SYSTEMCTL_LOG"
SH
chmod +x "$tmp/bin/tailscale" "$tmp/bin/systemctl"
export PATH="$tmp/bin:$PATH"
export TS_LOG="$tmp/ts.log" SYSTEMCTL_LOG="$tmp/systemctl.log"

# Ein Tarball mit demselben Aufbau wie der echte: oberste Ebene ist ein
# Verzeichnis tailscale_<ver>_<arch>/.
bundle="$tmp/bundle"
mkdir -p "$tmp/pack/tailscale_1.62.0_arm/systemd" "$bundle/tailscale"
printf '#!/bin/sh\n' > "$tmp/pack/tailscale_1.62.0_arm/tailscale"
printf '#!/bin/sh\n' > "$tmp/pack/tailscale_1.62.0_arm/tailscaled"
printf '[Unit]\n'    > "$tmp/pack/tailscale_1.62.0_arm/systemd/tailscaled.service"
printf 'FLAGS=""\n'  > "$tmp/pack/tailscale_1.62.0_arm/systemd/tailscaled.defaults"
tar -czf "$bundle/tailscale/tailscale_1.62.0_arm.tgz" -C "$tmp/pack" tailscale_1.62.0_arm

export EN_STATE_DIR="$tmp/state" EN_ROOT="$tmp/root" EN_BUNDLE_DIR="$bundle"
export EN_BUNDLE_VERSION=v1.0.0 EN_SUDO="" EN_SELECTION="$tmp/selection.json"
export EN_TAILSCALE_LOGIN_WAIT=3

defaults="$tmp/root/etc/default/tailscaled"

# --- abgewaehlt -----------------------------------------------------------
printf '{"steps":{"40":false}}\n' > "$EN_SELECTION"
out="$(bash "$script")"
grep -q '^##STEP 40 skip nicht ausgewaehlt$' <<<"$out" || fail "nicht abgewaehlt" "$out"
[ -e "$tmp/root" ] && fail "abgewaehlter Schritt hat Dateien angelegt"
rm -f "$EN_SELECTION"

# --- nicht angemeldet: installiert, aber kein Stempel ---------------------
out="$(TS_STATUS_RC=1 bash "$script")"
grep -q '^##STEP 40 skip login ausstehend$' <<<"$out" || fail "kein skip" "$out"
grep -q 'https://login.tailscale.com/a/abc123' <<<"$out" || fail "Login-Adresse fehlt" "$out"
[ -f "$tmp/root/usr/sbin/tailscaled" ] || fail "tailscaled nicht installiert"
[ -f "$tmp/root/etc/systemd/system/tailscaled.service" ] || fail "Unit nicht installiert"
[ -f "$defaults" ] || fail "defaults nicht angelegt"
[ -e "$tmp/state/steps/40" ] && fail "unangemeldeter Node hat einen Stempel bekommen"

# --- angemeldet: ok, Stempel, update ---------------------------------------
: > "$TS_LOG"
out="$(TS_STATUS_RC=0 bash "$script")"
grep -q '^##STEP 40 ok$' <<<"$out" || fail "kein ok-Marker" "$out"
[ -f "$tmp/state/steps/40" ] || fail "kein Stempel"
grep -q 'tailscale update' "$TS_LOG" || fail "kein update" "$(cat "$TS_LOG")"

# --- zweiter Lauf ueberspringt --------------------------------------------
: > "$TS_LOG"
out="$(bash "$script")"
grep -q '^##STEP 40 skip bereits erledigt$' <<<"$out" || fail "nicht uebersprungen" "$out"
[ -s "$TS_LOG" ] && fail "zweiter Lauf hat tailscale aufgerufen"

# --- vorhandene defaults bleiben unangetastet ------------------------------
rm -rf "$tmp/state"
printf 'FLAGS="--verbose"\n' > "$defaults"
out="$(TS_STATUS_RC=0 bash "$script")"
grep -q '^##STEP 40 ok$' <<<"$out" || fail "Lauf mit eigener defaults nicht ok" "$out"
grep -qx 'FLAGS="--verbose"' "$defaults" || fail "defaults ueberschrieben" "$(cat "$defaults")"

# --- der verbotene Schalter bricht ab, ohne etwas zu aendern --------------
rm -rf "$tmp/state"
printf 'FLAGS="--tun=userspace-networking"\n' > "$defaults"
set +e
out="$(bash "$script")"
rc=$?
set -e
[ "$rc" -eq 1 ] || fail "verbotener Schalter nicht abgelehnt" "$rc"
grep -q '^##STEP 40 fail TAILSCALE_FLAG_INVALID$' <<<"$out" || fail "falscher Code" "$out"
grep -qx 'FLAGS="--tun=userspace-networking"' "$defaults" \
  || fail "defaults trotz Abbruch veraendert" "$(cat "$defaults")"
rm -f "$defaults"

# --- fehlender Tarball -----------------------------------------------------
rm -rf "$tmp/state" "$bundle/tailscale"
set +e
out="$(bash "$script")"
set -e
grep -q '^##STEP 40 fail TAILSCALE_TARBALL_MISSING$' <<<"$out" || fail "falscher Code" "$out"

echo "OK: $(basename "$0")"
