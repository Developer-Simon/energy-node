#!/usr/bin/env bash
# Test for scripts/bootstrap/35-ufw-shelly-webhook.sh
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
script="$here/../bootstrap/35-ufw-shelly-webhook.sh"

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
fail() { echo "FAIL: $1"; [ -n "${2:-}" ] && printf '%s\n' "$2"; exit 1; }

mkdir -p "$tmp/bin"
cat > "$tmp/bin/ufw" <<'SH'
#!/usr/bin/env bash
printf 'ufw %s\n' "$*" >> "$UFW_LOG"
exit "${UFW_RC:-0}"
SH
chmod +x "$tmp/bin/ufw"

export PATH="$tmp/bin:$PATH"
export EN_STATE_DIR="$tmp/state" EN_ROOT="$tmp/root" EN_BUNDLE_VERSION=v1.0.0 EN_SUDO=""
export EN_SELECTION="$tmp/selection.json" UFW_LOG="$tmp/ufw.log"
stamp="$EN_STATE_DIR/steps/35"

# --- ohne selection.json: keine Zustimmung, keine Freigabe -----------------
out="$(bash "$script")"
grep -q '^##STEP 35 skip nicht ausgewaehlt$' <<<"$out" || fail "ohne Auswahl nicht uebersprungen" "$out"
grep -q 'ufw allow' "$UFW_LOG" 2>/dev/null && fail "ohne Auswahl freigegeben" "$(cat "$UFW_LOG")"
[ -f "$stamp" ] && fail "ohne Auswahl gestempelt"

# --- nicht genannt (Auswahl eines aelteren Bundles): ebenfalls kein Opt-in -
printf '{"steps":{"40":true,"70":true}}\n' > "$EN_SELECTION"
: > "$UFW_LOG"
out="$(bash "$script")"
grep -q '^##STEP 35 skip nicht ausgewaehlt$' <<<"$out" || fail "nicht genannter Schritt lief" "$out"
grep -q 'ufw allow' "$UFW_LOG" && fail "nicht genannter Schritt hat freigegeben" "$(cat "$UFW_LOG")"

# --- ausdruecklich gewaehlt: Standardport 8082 wird freigegeben ------------
printf '{"steps":{"35":true}}\n' > "$EN_SELECTION"
: > "$UFW_LOG"
out="$(bash "$script")"
grep -q '^##STEP 35 ok$' <<<"$out" || fail "kein ok-Marker" "$out"
grep -qx 'ufw allow 8082/tcp' "$UFW_LOG" || fail "8082 nicht freigegeben" "$(cat "$UFW_LOG")"
[ -f "$stamp" ] || fail "kein Stempel"

# --- zweiter Lauf: uebersprungen, kein ufw-Aufruf --------------------------
: > "$UFW_LOG"
out="$(bash "$script")"
grep -q '^##STEP 35 skip bereits erledigt$' <<<"$out" || fail "zweiter Lauf nicht uebersprungen" "$out"
[ -s "$UFW_LOG" ] && fail "zweiter Lauf hat ufw aufgerufen" "$(cat "$UFW_LOG")"

# --- abgewaehlt: Freigabe zurueck, Stempel weg -----------------------------
printf '{"steps":{"35":false}}\n' > "$EN_SELECTION"
out="$(bash "$script")"
grep -q '^##STEP 35 skip nicht ausgewaehlt$' <<<"$out" || fail "abgewaehlt nicht uebersprungen" "$out"
grep -qx 'ufw delete allow 8082/tcp' "$UFW_LOG" || fail "Freigabe nicht zurueckgenommen" "$(cat "$UFW_LOG")"
[ -f "$stamp" ] && fail "Stempel nach Abwahl noch da"

# --- webhook_port aus config.json gilt, wenn vorhanden ---------------------
mkdir -p "$EN_ROOT/etc/energy-node"
printf '{"services":{"shelly":{"service_id":"shelly","webhook_port":9090}}}\n' \
  > "$EN_ROOT/etc/energy-node/config.json"
printf '{"steps":{"35":true}}\n' > "$EN_SELECTION"
: > "$UFW_LOG"
out="$(bash "$script")"
grep -q '^##STEP 35 ok$' <<<"$out" || fail "mit config.json kein ok" "$out"
grep -qx 'ufw allow 9090/tcp' "$UFW_LOG" || fail "webhook_port aus config.json ignoriert" "$(cat "$UFW_LOG")"

# Abwahl mit geaendertem Port nimmt beide moeglichen Regeln zurueck.
printf '{"steps":{"35":false}}\n' > "$EN_SELECTION"
: > "$UFW_LOG"
bash "$script" >/dev/null
grep -qx 'ufw delete allow 9090/tcp' "$UFW_LOG" || fail "9090 nicht zurueckgenommen" "$(cat "$UFW_LOG")"
grep -qx 'ufw delete allow 8082/tcp' "$UFW_LOG" || fail "8082 nicht zurueckgenommen" "$(cat "$UFW_LOG")"

# Ein unbrauchbarer Port faellt auf den Standard zurueck.
printf '{"services":{"shelly":{"webhook_port":"acht"}}}\n' > "$EN_ROOT/etc/energy-node/config.json"
printf '{"steps":{"35":true}}\n' > "$EN_SELECTION"
: > "$UFW_LOG"
bash "$script" >/dev/null
grep -qx 'ufw allow 8082/tcp' "$UFW_LOG" || fail "kein Rueckfall auf 8082" "$(cat "$UFW_LOG")"

# --- ufw scheitert: Fehlercode, kein Stempel -------------------------------
rm -rf "$EN_STATE_DIR"
set +e
out="$(UFW_RC=1 bash "$script")"
set -e
grep -q '^##STEP 35 fail UFW_FAILED$' <<<"$out" || fail "falscher Fehlercode" "$out"
[ -f "$stamp" ] && fail "gescheiterter Lauf gestempelt"

echo "OK: $(basename "$0")"
