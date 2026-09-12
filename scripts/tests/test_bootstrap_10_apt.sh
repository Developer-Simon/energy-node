#!/usr/bin/env bash
# Test for scripts/bootstrap/10-apt.sh
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
script="$here/../bootstrap/10-apt.sh"

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
fail() { echo "FAIL: $1"; [ -n "${2:-}" ] && printf '%s\n' "$2"; exit 1; }

mkdir -p "$tmp/bin"
cat > "$tmp/bin/apt-get" <<'SH'
#!/usr/bin/env bash
printf 'apt-get %s\n' "$*" >> "$APT_LOG"
exit "${APT_RC:-0}"
SH
# dpkg-query meldet alles als fehlend, solange INSTALLED leer ist.
cat > "$tmp/bin/dpkg-query" <<'SH'
#!/usr/bin/env bash
pkg="${*: -1}"
case " ${INSTALLED:-} " in *" $pkg "*) echo "install ok installed"; exit 0 ;; esac
exit 1
SH
chmod +x "$tmp/bin/apt-get" "$tmp/bin/dpkg-query"

export PATH="$tmp/bin:$PATH"
export EN_STATE_DIR="$tmp/state" EN_BUNDLE_VERSION=v1.0.0 EN_SUDO=""
export APT_LOG="$tmp/apt.log"

# --- erster Lauf installiert die fehlenden Pakete --------------------------
out="$(bash "$script")"
grep -q '^##STEP 10 ok$' <<<"$out" || fail "kein ok-Marker" "$out"
grep -q '^apt-get update' "$APT_LOG" || fail "kein apt-get update" "$(cat "$APT_LOG")"
grep -q 'install .*mosquitto' "$APT_LOG" || fail "mosquitto nicht installiert" "$(cat "$APT_LOG")"
grep -q 'install .*ufw' "$APT_LOG" || fail "ufw nicht installiert" "$(cat "$APT_LOG")"

# --- zweiter Lauf ueberspringt ueber den Stempel ---------------------------
: > "$APT_LOG"
out="$(bash "$script")"
grep -q '^##STEP 10 skip' <<<"$out" || fail "zweiter Lauf nicht uebersprungen" "$out"
[ -s "$APT_LOG" ] && fail "zweiter Lauf hat apt-get aufgerufen" "$(cat "$APT_LOG")"

# --- alles bereits installiert: ok ohne apt-get install --------------------
rm -rf "$tmp/state"
: > "$APT_LOG"
out="$(INSTALLED="mosquitto mosquitto-clients ufw python3-pip ca-certificates" bash "$script")"
grep -q '^##STEP 10 ok$' <<<"$out" || fail "kein ok bei vollstaendiger Installation" "$out"
grep -q 'install' "$APT_LOG" && fail "apt-get install trotz vollstaendiger Installation"

# --- Fehler von apt-get wird zum Fehlercode --------------------------------
rm -rf "$tmp/state"
set +e
out="$(APT_RC=1 bash "$script")"
rc=$?
set -e
[ "$rc" -eq 1 ] || fail "Fehlschlag nicht weitergereicht" "$rc"
grep -q '^##STEP 10 fail APT_UPDATE_FAILED$' <<<"$out" || fail "falscher Fehlercode" "$out"

echo "OK: $(basename "$0")"
