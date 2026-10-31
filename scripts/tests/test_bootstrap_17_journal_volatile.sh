#!/usr/bin/env bash
# Test for scripts/bootstrap/17-journal-volatile.sh
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
script="$here/../bootstrap/17-journal-volatile.sh"

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
fail() { echo "FAIL: $1"; [ -n "${2:-}" ] && printf '%s\n' "$2"; exit 1; }

mkdir -p "$tmp/bin"
cat > "$tmp/bin/systemctl" <<'SH'
#!/usr/bin/env bash
printf 'systemctl %s\n' "$*" >> "$SYSTEMCTL_LOG"
exit "${SYSTEMCTL_RC:-0}"
SH
chmod +x "$tmp/bin/systemctl"

export PATH="$tmp/bin:$PATH"
export EN_STATE_DIR="$tmp/state" EN_ROOT="$tmp/root" EN_BUNDLE_VERSION=v1.0.0 EN_SUDO=""
export EN_SELECTION="$tmp/selection.json" SYSTEMCTL_LOG="$tmp/systemctl.log"
dropin="$EN_ROOT/etc/systemd/journald.conf.d/90-energy-node-volatile.conf"
stamp="$EN_STATE_DIR/steps/17"
persistent="$EN_ROOT/var/log/journal"
marker='# von energy-node-installer verwaltet - Aenderungen gehen verloren'

# --- ohne selection.json: keine Zustimmung, kein Eingriff -------------------
out="$(bash "$script")"
grep -q '^##STEP 17 skip nicht ausgewaehlt$' <<<"$out" || fail "ohne Auswahl nicht uebersprungen" "$out"
[ -f "$dropin" ] && fail "ohne Auswahl Drop-in angelegt"
[ -f "$stamp" ] && fail "ohne Auswahl gestempelt"
[ -s "$SYSTEMCTL_LOG" ] 2>/dev/null && fail "ohne Auswahl systemctl aufgerufen" "$(cat "$SYSTEMCTL_LOG")"

# --- ausdruecklich gewaehlt: Drop-in, Journal weg, Stempel -----------------
mkdir -p "$persistent/abc" && echo x > "$persistent/abc/system.journal"
printf '{"steps":{"17":true}}\n' > "$EN_SELECTION"
: > "$SYSTEMCTL_LOG"
out="$(bash "$script")"
grep -q '^##STEP 17 ok$' <<<"$out" || fail "kein ok-Marker" "$out"
[ -f "$dropin" ] || fail "kein Drop-in" "$out"
grep -qxF 'Storage=volatile' "$dropin" || fail "Storage=volatile fehlt" "$(cat "$dropin")"
grep -qxF 'RuntimeMaxUse=50M' "$dropin" || fail "RuntimeMaxUse fehlt" "$(cat "$dropin")"
grep -qxF "$marker" "$dropin" || fail "Kopfzeile fehlt" "$(cat "$dropin")"
[ "$(cat "$SYSTEMCTL_LOG")" = "systemctl restart systemd-journald" ] || fail "systemctl-Log falsch" "$(cat "$SYSTEMCTL_LOG")"
[ -f "$stamp" ] || fail "kein Stempel"
[ -e "$persistent/abc" ] && fail "bisheriges Journal nicht entfernt"

# --- zweiter Lauf: uebersprungen, kein systemctl-Aufruf --------------------
: > "$SYSTEMCTL_LOG"
out="$(bash "$script")"
grep -q '^##STEP 17 skip bereits erledigt$' <<<"$out" || fail "zweiter Lauf nicht uebersprungen" "$out"
[ -s "$SYSTEMCTL_LOG" ] && fail "zweiter Lauf hat systemctl aufgerufen" "$(cat "$SYSTEMCTL_LOG")"

# --- abgewaehlt: Drop-in weg, Stempel weg, Neustart --------------------------
printf '{"steps":{"17":false}}\n' > "$EN_SELECTION"
: > "$SYSTEMCTL_LOG"
out="$(bash "$script")"
grep -q '^##STEP 17 skip nicht ausgewaehlt$' <<<"$out" || fail "abgewaehlt nicht uebersprungen" "$out"
[ -f "$dropin" ] && fail "Drop-in nach Abwahl noch da"
[ -f "$stamp" ] && fail "Stempel nach Abwahl noch da"
grep -q 'restart systemd-journald' "$SYSTEMCTL_LOG" || fail "kein Neustart nach Abwahl" "$(cat "$SYSTEMCTL_LOG")"

# --- fremdes Drop-in: Abbruch, Datei bleibt unangetastet --------------------
mkdir -p "$(dirname "$dropin")"
printf '[Journal]\nStorage=persistent\n' > "$dropin"
rm -f "$stamp"
printf '{"steps":{"17":true}}\n' > "$EN_SELECTION"
set +e
out="$(bash "$script")"
rc=$?
set -e
[ "$rc" -eq 1 ] || fail "fremdes Drop-in: Exit $rc statt 1" "$out"
grep -q '^##STEP 17 fail JOURNAL_CONF_FOREIGN$' <<<"$out" || fail "falscher Fehlercode bei fremdem Drop-in" "$out"
[ "$(cat "$dropin")" = "$(printf '[Journal]\nStorage=persistent')" ] || fail "fremdes Drop-in veraendert" "$(cat "$dropin")"
rm -f "$dropin"

# --- systemctl scheitert: Fehlercode -----------------------------------------
rm -f "$stamp"
printf '{"steps":{"17":true}}\n' > "$EN_SELECTION"
set +e
out="$(SYSTEMCTL_RC=1 bash "$script")"
rc=$?
set -e
[ "$rc" -eq 1 ] || fail "systemctl-Fehler: Exit $rc statt 1" "$out"
grep -q '^##STEP 17 fail JOURNALD_RESTART_FAILED$' <<<"$out" || fail "falscher Fehlercode bei systemctl-Fehler" "$out"

echo "OK: $(basename "$0")"
