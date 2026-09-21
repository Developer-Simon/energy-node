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
mkdir -p "$tmp/pack/tailscale_1.62.0_arm/systemd"
printf '#!/bin/sh\n' > "$tmp/pack/tailscale_1.62.0_arm/tailscale"
printf '#!/bin/sh\n' > "$tmp/pack/tailscale_1.62.0_arm/tailscaled"
printf '[Unit]\n'    > "$tmp/pack/tailscale_1.62.0_arm/systemd/tailscaled.service"
printf 'FLAGS=""\n'  > "$tmp/pack/tailscale_1.62.0_arm/systemd/tailscaled.defaults"
tar -czf "$tmp/ts.tgz" -C "$tmp/pack" tailscale_1.62.0_arm

build() {
  bash "$script" --arch armv6 --python-minor 3.11 --abi cp311 \
    --out "$1" --sign-key "$tmp/key.pem" \
    --dashboard-binary "$tmp/fake-dashboard" --tailscale-tarball "$tmp/ts.tgz" \
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

# --- ungueltiger Benutzer wird vor dem Bau abgelehnt ------------------------
if bash "$script" --arch armv6 --user 'bad user' --out "$tmp/dist-bad" \
     --dashboard-binary "$tmp/fake-dashboard" --skip-wheels >/dev/null 2>&1; then
  fail "ungueltiger --user akzeptiert"
fi

echo "OK"
