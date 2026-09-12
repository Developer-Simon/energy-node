#!/usr/bin/env bash
# Test for scripts/build/lib/wheels.sh
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
lib="$here/../build/lib/wheels.sh"

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
fail() { echo "FAIL: $1"; [ -n "${2:-}" ] && printf '%s\n' "$2"; exit 1; }

mkdir -p "$tmp/bin"
cat > "$tmp/bin/fakepip" <<'SH'
#!/usr/bin/env bash
printf 'pip %s\n' "$*" >> "$PIP_LOG"
exit "${PIP_RC:-0}"
SH
chmod +x "$tmp/bin/fakepip"
export PIP_LOG="$tmp/pip.log" EN_PIP="$tmp/bin/fakepip"

run() { bash -c 'source "$1"; shift; "$@"' _ "$lib" "$@"; }

# --- Architektur-Tabelle ---------------------------------------------------
[ "$(run arch_platform_tags armv6)" = linux_armv6l ] || fail "armv6-Tag falsch"
[ "$(run arch_uname_machines armv6 | tr '\n' ' ')" = "armv6l armv7l " ] \
  || fail "armv6-uname falsch" "$(run arch_uname_machines armv6)"
[ "$(run arch_go_env armv6 | tr '\n' ' ')" = "GOARCH=arm GOARM=6 " ] \
  || fail "armv6-Go falsch" "$(run arch_go_env armv6)"
run arch_platform_tags arm64 | grep -qx manylinux2014_aarch64 || fail "arm64-Tag fehlt"
run arch_platform_tags amd64 | grep -qx manylinux2014_x86_64 || fail "amd64-Tag fehlt"
[ "$(run arch_uname_machines amd64)" = x86_64 ] || fail "amd64-uname falsch"

set +e
run arch_platform_tags sparc64 2>/dev/null
rc=$?
set -e
[ "$rc" -ne 0 ] || fail "unbekannte Architektur nicht bemaengelt"

# --- fetch_thirdparty_wheels baut den pip-Aufruf richtig ------------------
run fetch_thirdparty_wheels "$tmp/wheels" armv6 3.11 cp311 >/dev/null
log="$(cat "$PIP_LOG")"
grep -q -- '--only-binary=:all:' <<<"$log" || fail "ohne --only-binary" "$log"
grep -q -- '--index-url https://www.piwheels.org/simple' <<<"$log" || fail "ohne piwheels" "$log"
grep -q -- '--extra-index-url https://pypi.org/simple' <<<"$log" || fail "ohne PyPI-Zweitindex" "$log"
grep -q -- '--platform linux_armv6l' <<<"$log" || fail "ohne Platform-Tag" "$log"
grep -q -- '--python-version 3.11' <<<"$log" || fail "ohne Python-Version" "$log"
grep -q -- '--implementation cp' <<<"$log" || fail "ohne Implementation" "$log"
grep -q -- '--abi cp311' <<<"$log" || fail "ohne ABI" "$log"
grep -q -- "-d $tmp/wheels" <<<"$log" || fail "ohne Zielverzeichnis" "$log"
for pkg in paho-mqtt apsystems-ez1 tinytuya requests; do
  grep -q -- "$pkg" <<<"$log" || fail "Paket $pkg fehlt" "$log"
done
[ -d "$tmp/wheels" ] || fail "Zielverzeichnis nicht angelegt"

# --- arm64 reicht beide Platform-Tags durch -------------------------------
: > "$PIP_LOG"
run fetch_thirdparty_wheels "$tmp/wheels64" arm64 3.11 cp311 >/dev/null
grep -q -- '--platform manylinux2014_aarch64' "$PIP_LOG" || fail "arm64-Tag fehlt" "$(cat "$PIP_LOG")"
grep -q -- '--platform linux_aarch64' "$PIP_LOG" || fail "zweiter arm64-Tag fehlt" "$(cat "$PIP_LOG")"

# --- ein fehlendes Rad bricht den Bau ab ----------------------------------
set +e
out="$(PIP_RC=1 run fetch_thirdparty_wheels "$tmp/w2" armv6 3.11 cp311 2>&1)"
rc=$?
set -e
[ "$rc" -ne 0 ] || fail "pip-Fehler nicht weitergereicht"
grep -qi 'kein Wheel' <<<"$out" || fail "Meldung nennt das Problem nicht" "$out"

echo "OK: $(basename "$0")"
