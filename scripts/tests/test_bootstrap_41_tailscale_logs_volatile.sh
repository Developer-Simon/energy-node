#!/usr/bin/env bash
# Test for scripts/bootstrap/41-tailscale-logs-volatile.sh
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
script="$here/../bootstrap/41-tailscale-logs-volatile.sh"

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
fail() { echo "FAIL: $1"; [ -n "${2:-}" ] && printf '%s\n' "$2"; exit 1; }

mkdir -p "$tmp/bin"
cat > "$tmp/bin/systemctl" <<'SH'
#!/usr/bin/env bash
printf 'systemctl %s\n' "$*" >> "$SYSTEMCTL_LOG"
[ "$1" = cat ] && exit "${SYSTEMCTL_CAT_RC:-0}"
exit "${SYSTEMCTL_RC:-0}"
SH
chmod +x "$tmp/bin/systemctl"

export PATH="$tmp/bin:$PATH"
export EN_STATE_DIR="$tmp/state" EN_ROOT="$tmp/root" EN_BUNDLE_VERSION=v1.0.0 EN_SUDO=""
export EN_SELECTION="$tmp/selection.json" SYSTEMCTL_LOG="$tmp/systemctl.log"
dropin="$EN_ROOT/etc/systemd/system/tailscaled.service.d/90-energy-node-logs.conf"
stamp="$EN_STATE_DIR/steps/41"
state="$EN_ROOT/var/lib/tailscale"
marker='# von energy-node-installer verwaltet - Aenderungen gehen verloren'

# --- Tailscale fehlt: skip ohne Stempel und ohne Drop-in -------------------
: > "$SYSTEMCTL_LOG"
out="$(SYSTEMCTL_CAT_RC=1 bash "$script")"
grep -q '^##STEP 41 skip Tailscale nicht installiert$' <<<"$out" || fail "ohne Tailscale nicht uebersprungen" "$out"
[ -f "$dropin" ] && fail "ohne Tailscale Drop-in angelegt"
[ -f "$stamp" ] && fail "ohne Tailscale gestempelt"

# --- Vorgabe an (keine selection.json): Drop-in, Neustart, alte Logs weg ---
mkdir -p "$state"
echo x > "$state/tailscaled.log1.txt"
echo x > "$state/tailscaled.log2.txt"
echo '{"PrivateID":"x"}' > "$state/tailscaled.log.conf"
: > "$SYSTEMCTL_LOG"
out="$(bash "$script")"
grep -q '^##STEP 41 ok$' <<<"$out" || fail "kein ok-Marker" "$out"
[ -f "$dropin" ] || fail "kein Drop-in" "$out"
[ "$(cat "$dropin")" = "$(printf '%s\n[Service]\nRuntimeDirectory=tailscale-logs\nEnvironment=TS_LOGS_DIR=/run/tailscale-logs' "$marker")" ] \
  || fail "Drop-in-Inhalt falsch" "$(cat "$dropin")"
[ "$(cat "$SYSTEMCTL_LOG")" = "$(printf 'systemctl cat tailscaled.service\nsystemctl daemon-reload\nsystemctl restart tailscaled')" ] \
  || fail "systemctl-Log falsch" "$(cat "$SYSTEMCTL_LOG")"
[ -f "$stamp" ] || fail "kein Stempel"
[ -e "$state/tailscaled.log1.txt" ] && fail "tailscaled.log1.txt nicht entfernt"
[ -e "$state/tailscaled.log2.txt" ] && fail "tailscaled.log2.txt nicht entfernt"
[ -f "$state/tailscaled.log.conf" ] || fail "tailscaled.log.conf entfernt (Log-ID)"

# --- zweiter Lauf: uebersprungen, kein systemctl-Aufruf --------------------
: > "$SYSTEMCTL_LOG"
out="$(bash "$script")"
grep -q '^##STEP 41 skip bereits erledigt$' <<<"$out" || fail "zweiter Lauf nicht uebersprungen" "$out"
[ -s "$SYSTEMCTL_LOG" ] && fail "zweiter Lauf hat systemctl aufgerufen" "$(cat "$SYSTEMCTL_LOG")"

# --- 41 abgewaehlt: Drop-in weg, Stempel weg, daemon-reload + try-restart --
printf '{"steps":{"41":false}}\n' > "$EN_SELECTION"
: > "$SYSTEMCTL_LOG"
out="$(bash "$script")"
grep -q '^##STEP 41 skip nicht ausgewaehlt$' <<<"$out" || fail "abgewaehlt nicht uebersprungen" "$out"
[ -f "$dropin" ] && fail "Drop-in nach Abwahl noch da"
[ -f "$stamp" ] && fail "Stempel nach Abwahl noch da"
[ "$(cat "$SYSTEMCTL_LOG")" = "$(printf 'systemctl daemon-reload\nsystemctl try-restart tailscaled')" ] \
  || fail "systemctl-Log nach Abwahl falsch" "$(cat "$SYSTEMCTL_LOG")"

# --- 40 abgewaehlt, 41 nicht genannt: gilt ebenfalls als abgewaehlt --------
rm -f "$EN_SELECTION"
bash "$script" >/dev/null
[ -f "$dropin" ] || fail "Vorbereitung: kein Drop-in"
printf '{"steps":{"40":false}}\n' > "$EN_SELECTION"
out="$(bash "$script")"
grep -q '^##STEP 41 skip nicht ausgewaehlt$' <<<"$out" || fail "ohne 40 nicht uebersprungen" "$out"
[ -f "$dropin" ] && fail "Drop-in ohne 40 noch da"

# --- fremdes Drop-in: Abbruch, Datei bleibt unangetastet --------------------
rm -f "$EN_SELECTION" "$stamp"
mkdir -p "$(dirname "$dropin")"
printf '[Service]\nEnvironment=FOO=1\n' > "$dropin"
set +e
out="$(bash "$script")"
rc=$?
set -e
[ "$rc" -eq 1 ] || fail "fremdes Drop-in: Exit $rc statt 1" "$out"
grep -q '^##STEP 41 fail TAILSCALE_LOGS_CONF_FOREIGN$' <<<"$out" || fail "falscher Fehlercode bei fremdem Drop-in" "$out"
[ "$(cat "$dropin")" = "$(printf '[Service]\nEnvironment=FOO=1')" ] || fail "fremdes Drop-in veraendert" "$(cat "$dropin")"
rm -f "$dropin"

# --- systemctl restart scheitert: Fehlercode ---------------------------------
set +e
out="$(SYSTEMCTL_RC=1 bash "$script")"
rc=$?
set -e
[ "$rc" -eq 1 ] || fail "systemctl-Fehler: Exit $rc statt 1" "$out"
grep -q '^##STEP 41 fail TAILSCALE_LOGS_FAILED$' <<<"$out" || fail "falscher Fehlercode bei systemctl-Fehler" "$out"

echo "OK: $(basename "$0")"
