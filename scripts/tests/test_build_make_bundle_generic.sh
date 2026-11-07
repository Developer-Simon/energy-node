#!/usr/bin/env bash
# Test for scripts/build/make_bundle.sh without --user/--base (generic bundle)
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
script="$here/../build/make_bundle.sh"
verify="$here/../bootstrap/verify_bundle.sh"

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
fail() { echo "FAIL: $1"; [ -n "${2:-}" ] && printf '%s\n' "$2"; exit 1; }

openssl genpkey -algorithm ed25519 -out "$tmp/key.pem" 2>/dev/null
openssl pkey -in "$tmp/key.pem" -pubout -out "$tmp/pub.pem" 2>/dev/null

printf '#!/bin/sh\necho dashboard\n' > "$tmp/fake-dashboard"

build() {
  bash "$script" --arch armv6 --python-minor 3.11 --abi cp311 \
    --out "$1" --sign-key "$tmp/key.pem" \
    --dashboard-binary "$tmp/fake-dashboard" \
    --skip-wheels "${@:2}" >"$tmp/build.log" 2>&1 \
    || fail "make_bundle.sh scheiterte" "$(cat "$tmp/build.log")"
}

# --- allgemeines Bundle: keine --user/--base -------------------------------
build "$tmp/dist"
archive="$(find "$tmp/dist" -maxdepth 1 -name 'energy-node-*-armv6.tar.gz' | head -n 1)"
[ -n "$archive" ] || fail "kein Archiv gebaut"
out="$tmp/generic"; mkdir -p "$out"; tar -xzf "$archive" -C "$out"

res="$(bash "$verify" --bundle "$out" --pubkey "$tmp/pub.pem")" \
  || fail "verify_bundle lehnt das allgemeine Bundle ab" "$res"

for f in dashboard/energy-node-dashboard.service dashboard/energy-node-dashboard-system-action \
         dashboard/energy-node-dashboard-system-action.sudoers \
         services/shelly/shelly-rpc.service; do
  grep -q 'energynode' "$out/$f" || fail "Platzhalter fehlt in $f (Bundle sollte allgemein sein)"
done
grep -q '/home/energynode' "$out/dashboard/energy-node-dashboard.service" \
  || fail "Basis-Platzhalter fehlt in der Dashboard-Unit"

get() { python3 -c 'import json,sys; d=json.load(open(sys.argv[1])); print(eval(sys.argv[2], {"d": d}))' \
  "$out/manifest.json" "$1"; }
[ "$(get '"target_user" in d')" = False ] || fail "allgemeines Manifest traegt target_user"
[ "$(get '"target_base" in d')" = False ] || fail "allgemeines Manifest traegt target_base"

# --- nur --user: festgelegt, Basis folgt dem Benutzer -----------------------
build "$tmp/dist-pinned" --user pruef
archive="$(find "$tmp/dist-pinned" -maxdepth 1 -name 'energy-node-*-armv6.tar.gz' | head -n 1)"
pinned="$tmp/pinned"; mkdir -p "$pinned"; tar -xzf "$archive" -C "$pinned"
out="$pinned"
[ "$(get 'd["target_user"]')" = pruef ] || fail "target_user nicht gesetzt"
[ "$(get 'd["target_base"]')" = /home/pruef ] || fail "target_base folgt dem Benutzer nicht"
grep -q 'energynode' "$pinned/dashboard/energy-node-dashboard.service" \
  && fail "festgelegtes Bundle traegt noch den Platzhalter"

# --- --dev-version haengt Commit (und ggf. .dirty) an die Version -----------
build "$tmp/dist-dev" --dev-version
archive="$(find "$tmp/dist-dev" -maxdepth 1 -name 'energy-node-*-armv6.tar.gz' | head -n 1)"
dev="$tmp/dev"; mkdir -p "$dev"; tar -xzf "$archive" -C "$dev"
out="$dev"
want="$(tr -d '[:space:]' < "$here/../../dashboard/VERSION")-dev.$(git -C "$here" rev-parse --short HEAD)"
git -C "$here" diff --quiet HEAD -- || want="${want}.dirty"
[ "$(get 'd["version"]')" = "$want" ] || fail "--dev-version: Version $(get 'd["version"]'), erwartet $want"
case "$archive" in *"$want"*) ;; *) fail "--dev-version: Archivname ohne Dev-Version: $archive" ;; esac

# --- ungueltiger Benutzer wird vor dem Bau abgelehnt ------------------------
if bash "$script" --arch armv6 --user 'bad user' --out "$tmp/dist-bad" \
     --dashboard-binary "$tmp/fake-dashboard" --skip-wheels >/dev/null 2>&1; then
  fail "ungueltiger --user akzeptiert"
fi

# --- Python-Vorgabe je Architektur -----------------------------------------
# Ohne --python-minor/--abi: armv6 bleibt bei Python 3.11, die 64-Bit-
# Architekturen bauen fuer Python 3.13 (Debian 13).
abi_of() {
  local arch="$1" dist="$tmp/default-$1"
  bash "$script" --arch "$arch" --out "$dist" --sign-key "$tmp/key.pem" \
    --dashboard-binary "$tmp/fake-dashboard" \
    --skip-wheels >"$tmp/build-$arch.log" 2>&1 \
    || fail "make_bundle.sh --arch $arch scheiterte" "$(cat "$tmp/build-$arch.log")"
  local archive
  archive="$(find "$dist" -maxdepth 1 -name "energy-node-*-$arch.tar.gz" | head -n 1)"
  tar -xzOf "$archive" ./manifest.json | python3 -c 'import json,sys; print(json.load(sys.stdin)["python_abi"])'
}
[ "$(abi_of armv6)" = cp311 ] || fail "armv6 sollte cp311 bauen"
[ "$(abi_of arm64)" = cp313 ] || fail "arm64 sollte cp313 bauen"
[ "$(abi_of amd64)" = cp313 ] || fail "amd64 sollte cp313 bauen"

# --- Festlegungen je Architektur: kein Netz, kein Download im Bau ----------
# manifest.packages ist der Eintrag der Architektur aus packages.json. Der Bau
# laeuft ohne curl-Attrappe; ein Download waere hier ein Fehler.
packages_ok() {
  local arch="$1" archive
  local build_dir="$tmp/pkg-$arch"
  bash "$script" --arch "$arch" --out "$build_dir" --sign-key "$tmp/key.pem" \
    --dashboard-binary "$tmp/fake-dashboard" --skip-wheels >"$tmp/build-pkg-$arch.log" 2>&1 \
    || fail "make_bundle.sh --arch $arch scheiterte" "$(cat "$tmp/build-pkg-$arch.log")"
  archive="$(find "$build_dir" -maxdepth 1 -name "energy-node-*-$arch.tar.gz" | head -n 1)"
  tar -xzOf "$archive" ./manifest.json | python3 -I -c '
import json, sys
pins = json.load(open(sys.argv[1]))[sys.argv[2]]
got = json.load(sys.stdin)["packages"]
for name in ("tailscale", "caddy"):
    for key in ("version", "file", "url", "sha256", "update"):
        assert got[name][key] == pins[name][key], (name, key)
' "$packages_json" "$arch" && echo ok
}
packages_json="$here/../build/packages.json"
for arch in armv6 arm64 amd64; do
  [ "$(packages_ok "$arch")" = ok ] || fail "manifest.packages fuer $arch passt nicht zu packages.json"
done

echo "OK"
