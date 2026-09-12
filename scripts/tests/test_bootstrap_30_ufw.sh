#!/usr/bin/env bash
# Test for scripts/bootstrap/30-ufw.sh
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
script="$here/../bootstrap/30-ufw.sh"

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
export EN_STATE_DIR="$tmp/state" EN_BUNDLE_VERSION=v1.0.0 EN_SUDO=""
export UFW_LOG="$tmp/ufw.log"

out="$(bash "$script")"
grep -q '^##STEP 30 ok$' <<<"$out" || fail "kein ok-Marker" "$out"
for rule in "allow ssh" "allow 1883/tcp" "allow 8080/tcp" "allow 443/tcp"; do
  grep -q "ufw $rule" "$UFW_LOG" || fail "Regel fehlt: $rule" "$(cat "$UFW_LOG")"
done
# --force, weil `ufw enable` sonst interaktiv nachfragt und in einer
# SSH-Session auf die Rueckfrage wartet.
grep -q 'ufw --force enable' "$UFW_LOG" || fail "enable fehlt" "$(cat "$UFW_LOG")"

: > "$UFW_LOG"
out="$(bash "$script")"
grep -q '^##STEP 30 skip' <<<"$out" || fail "zweiter Lauf nicht uebersprungen" "$out"
[ -s "$UFW_LOG" ] && fail "zweiter Lauf hat ufw aufgerufen"

rm -rf "$tmp/state"
set +e
out="$(UFW_RC=1 bash "$script")"
set -e
grep -q '^##STEP 30 fail UFW_FAILED$' <<<"$out" || fail "falscher Fehlercode" "$out"

echo "OK: $(basename "$0")"
