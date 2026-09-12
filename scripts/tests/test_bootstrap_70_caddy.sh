#!/usr/bin/env bash
# Test for scripts/bootstrap/70-caddy.sh
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
script="$here/../bootstrap/70-caddy.sh"

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
fail() { echo "FAIL: $1"; [ -n "${2:-}" ] && printf '%s\n' "$2"; exit 1; }

mkdir -p "$tmp/bin"
cat > "$tmp/bin/caddy" <<'SH'
#!/usr/bin/env bash
printf 'caddy %s\n' "$*" >> "$CADDY_LOG"
exit "${CADDY_RC:-0}"
SH
cat > "$tmp/bin/systemctl" <<'SH'
#!/usr/bin/env bash
printf 'systemctl %s\n' "$*" >> "$SYSTEMCTL_LOG"
SH
chmod +x "$tmp/bin/caddy" "$tmp/bin/systemctl"
export PATH="$tmp/bin:$PATH"
export CADDY_LOG="$tmp/caddy.log" SYSTEMCTL_LOG="$tmp/systemctl.log"

bundle="$tmp/bundle"
mkdir -p "$bundle/caddy" "$bundle/dashboard"
printf '#!/bin/sh\n'              > "$bundle/caddy/caddy"
printf ':443 {\n  tls internal\n}\n' > "$bundle/dashboard/Caddyfile"

export EN_STATE_DIR="$tmp/state" EN_ROOT="$tmp/root" EN_BUNDLE_DIR="$bundle"
export EN_BUNDLE_VERSION=v1.0.0 EN_SUDO="" EN_SELECTION="$tmp/selection.json"

caddyfile="$tmp/root/etc/caddy/Caddyfile"

# --- abgewaehlt -----------------------------------------------------------
printf '{"steps":{"70":false}}\n' > "$EN_SELECTION"
out="$(bash "$script")"
grep -q '^##STEP 70 skip nicht ausgewaehlt$' <<<"$out" || fail "nicht abgewaehlt" "$out"
[ -e "$tmp/root" ] && fail "abgewaehlter Schritt hat Dateien angelegt"
rm -f "$EN_SELECTION"

# --- erster Lauf ----------------------------------------------------------
out="$(bash "$script")"
grep -q '^##STEP 70 ok$' <<<"$out" || fail "kein ok-Marker" "$out"
[ -x "$tmp/root/usr/bin/caddy" ] || fail "Binary nicht installiert"
[ -f "$caddyfile" ] || fail "Caddyfile nicht installiert"
grep -q 'caddy validate' "$CADDY_LOG" || fail "keine Validierung" "$(cat "$CADDY_LOG")"
grep -q 'systemctl enable --now caddy' "$SYSTEMCTL_LOG" \
  || fail "Caddy nicht gestartet" "$(cat "$SYSTEMCTL_LOG")"

# --- zweiter Lauf ueberspringt --------------------------------------------
: > "$CADDY_LOG"
out="$(bash "$script")"
grep -q '^##STEP 70 skip bereits erledigt$' <<<"$out" || fail "nicht uebersprungen" "$out"
[ -s "$CADDY_LOG" ] && fail "zweiter Lauf hat caddy aufgerufen"

# --- eigene Caddyfile bleibt stehen und wird gemeldet ---------------------
rm -rf "$tmp/state"
printf ':8443 {\n}\n' > "$caddyfile"
out="$(bash "$script")"
grep -q '^##STEP 70 ok$' <<<"$out" || fail "Lauf mit eigener Caddyfile nicht ok" "$out"
grep -qx ':8443 {' "$caddyfile" || fail "eigene Caddyfile ueberschrieben" "$(cat "$caddyfile")"
grep -qi 'weicht ab' <<<"$out" || fail "Abweichung nicht gemeldet" "$out"

# --- ungueltige Konfiguration --------------------------------------------
rm -rf "$tmp/state"
set +e
out="$(CADDY_RC=1 bash "$script")"
rc=$?
set -e
[ "$rc" -eq 1 ] || fail "ungueltige Konfiguration nicht abgelehnt" "$rc"
grep -q '^##STEP 70 fail CADDY_CONFIG_INVALID$' <<<"$out" || fail "falscher Code" "$out"

# --- fehlendes Beipack ----------------------------------------------------
rm -rf "$tmp/state" "$bundle/caddy"
set +e
out="$(bash "$script")"
set -e
grep -q '^##STEP 70 fail CADDY_BINARY_MISSING$' <<<"$out" || fail "falscher Code" "$out"

echo "OK: $(basename "$0")"
