#!/usr/bin/env bash
# Test for scripts/bootstrap/lib/apt_pending.py
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
helper="$here/../bootstrap/lib/apt_pending.py"

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
fail() { echo "FAIL: $1"; [ -n "${2:-}" ] && printf '%s\n' "$2"; exit 1; }

mkdir -p "$tmp/bin"
# Die Attrappe protokolliert Aufruf und Sprache. SIM_RC laesst die
# Simulation scheitern, UPGRADABLE nennt die Pakete.
cat > "$tmp/bin/apt-get" <<'SH'
#!/usr/bin/env bash
printf 'LC_ALL=%s apt-get %s\n' "${LC_ALL:-}" "$*" >> "$APT_LOG"
[ -n "${SIM_RC:-}" ] && { echo "E: dpkg was interrupted" >&2; exit "$SIM_RC"; }
echo "NOTE: This is only a simulation!"
for p in ${UPGRADABLE:-}; do
  printf 'Inst %s [1.0-1] (1.0-2 Debian:12.7/stable [armhf])\n' "$p"
  printf 'Conf %s (1.0-2 Debian:12.7/stable [armhf])\n' "$p"
done
# Neues Paket ohne alte Version (kommt bei upgrade nicht vor, darf aber
# nicht stoeren).
[ -n "${NEWPKG:-}" ] && printf 'Inst %s (2.0 Debian:12.7/stable [armhf])\n' "$NEWPKG"
exit 0
SH
chmod +x "$tmp/bin/apt-get"
export EN_APT_GET="$tmp/bin/apt-get" EN_ROOT="$tmp/root" APT_LOG="$tmp/apt.log"
run() { python3 "$helper"; }
get() { python3 -c 'import json,sys; d=json.loads(sys.argv[2]); print(eval(sys.argv[1], {"d": d}))' "$1" "$2"; }

# --- Pakete mit alter und neuer Version -------------------------------------
: > "$APT_LOG"
out="$(UPGRADABLE="libssl3 openssl" run)"
[ "$(wc -l <<<"$out")" -eq 1 ] || fail "Ausgabe ist nicht eine Zeile" "$out"
[ "$(get 'd["count"]' "$out")" = 2 ] || fail "count falsch" "$out"
[ "$(get 'd["packages"][0]' "$out")" = "{'name': 'libssl3', 'from': '1.0-1', 'to': '1.0-2'}" ] \
  || fail "Paketeintrag falsch" "$out"
grep -q -- '-s .*upgrade' "$APT_LOG" || fail "keine Simulation" "$(cat "$APT_LOG")"
grep -q -- '-o Debug::NoLocking=1' "$APT_LOG" || fail "Simulation wartet auf den Lock" "$(cat "$APT_LOG")"
grep -q '^LC_ALL=C ' "$APT_LOG" || fail "apt-get nicht mit LC_ALL=C" "$(cat "$APT_LOG")"
grep -q 'update' "$APT_LOG" && fail "Pruefung ruft apt-get update"
[ "$(get 'd["checked_at"]' "$out")" = "" ] || fail "Stand ohne Listen gemeldet" "$out"

# --- nichts zu tun ----------------------------------------------------------
out="$(run)"
[ "$(get 'd["count"]' "$out")" = 0 ] || fail "count ohne Updates nicht 0" "$out"
[ "$(get 'd["packages"]' "$out")" = "[]" ] || fail "Liste ohne Updates nicht leer" "$out"

# --- neues Paket ohne alte Version ------------------------------------------
out="$(NEWPKG=linux-image-6.6 run)"
[ "$(get 'd["packages"][0]' "$out")" = "{'name': 'linux-image-6.6', 'from': '', 'to': '2.0'}" ] \
  || fail "Paket ohne alte Version falsch" "$out"

# --- Stand: Stempel von apt-daily, sonst die neueste Release-Datei ----------
mkdir -p "$tmp/root/var/lib/apt/lists" "$tmp/root/var/lib/apt/periodic"
touch -d '2026-10-03 05:00:00 UTC' "$tmp/root/var/lib/apt/lists/deb.debian.org_dists_bookworm_InRelease"
touch -d '2026-10-01 05:00:00 UTC' "$tmp/root/var/lib/apt/lists/archive.raspberrypi.com_dists_bookworm_Release"
out="$(run)"
[ "$(get 'd["checked_at"]' "$out")" = "2026-10-03T05:00:00+00:00" ] || fail "Stand aus den Listen falsch" "$out"
touch -d '2026-10-04 06:12:00 UTC' "$tmp/root/var/lib/apt/periodic/update-success-stamp"
out="$(run)"
[ "$(get 'd["checked_at"]' "$out")" = "2026-10-04T06:12:00+00:00" ] || fail "Stand aus dem Stempel falsch" "$out"

# --- apt-get fehlt oder scheitert: null, Exit 0 ------------------------------
out="$(SIM_RC=100 run)" || fail "Simulationsfehler bricht ab"
[ "$out" = null ] || fail "Simulationsfehler nicht null" "$out"
out="$(EN_APT_GET="$tmp/bin/gibt-es-nicht" run)" || fail "fehlendes apt-get bricht ab"
[ "$out" = null ] || fail "fehlendes apt-get nicht null" "$out"

# --- als Modul: pending() liefert dasselbe ----------------------------------
out="$(UPGRADABLE="mosquitto" python3 -c 'import sys; sys.path.insert(0, sys.argv[1]); import apt_pending, json; print(json.dumps(apt_pending.pending()))' "$(dirname "$helper")")"
[ "$(get 'd["count"]' "$out")" = 1 ] || fail "Modulaufruf falsch" "$out"

echo "OK: $(basename "$0")"
