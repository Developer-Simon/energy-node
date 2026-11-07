#!/usr/bin/env bash
# Test for scripts/build/make_bundle.sh
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
script="$here/../build/make_bundle.sh"
verify="$here/../bootstrap/verify_bundle.sh"

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
fail() { echo "FAIL: $1"; [ -n "${2:-}" ] && printf '%s\n' "$2"; exit 1; }

openssl genpkey -algorithm ed25519 -out "$tmp/key.pem" 2>/dev/null
openssl pkey -in "$tmp/key.pem" -pubout -out "$tmp/pub.pem" 2>/dev/null

# Eingeschleuste Artefakte: kein Netz, kein Go-Compiler im Test.
printf '#!/bin/sh\necho dashboard\n' > "$tmp/fake-dashboard"

bash "$script" \
  --arch armv6 --python-minor 3.11 --abi cp311 \
  --user pruef --base /home/pruef \
  --out "$tmp/dist" --sign-key "$tmp/key.pem" \
  --dashboard-binary "$tmp/fake-dashboard" \
  --skip-wheels >"$tmp/build.log" 2>&1 \
  || fail "make_bundle.sh scheiterte" "$(cat "$tmp/build.log")"

archive="$(find "$tmp/dist" -maxdepth 1 -name 'energy-node-*-armv6.tar.gz' | head -n 1)"
[ -n "$archive" ] || fail "kein Archiv gebaut" "$(ls -R "$tmp/dist")"
[ -z "$(find "$tmp/dist" -maxdepth 1 -name 'caddy-*.tar.gz')" ] \
  || fail "Caddy-Beipack entsteht noch" "$(ls "$tmp/dist")"

# Der Tailscale- und der Caddy-Schalter gehoeren nicht mehr zum Bau.
for switch in --tailscale-tarball --caddy-binary; do
  set +e
  bash "$script" --arch armv6 --out "$tmp/dist-switch" --skip-wheels \
    "$switch" "$tmp/fake-dashboard" >/dev/null 2>&1
  rc=$?
  set -e
  [ "$rc" -eq 1 ] || fail "unbekannter Schalter $switch nicht mit Exit 1 abgelehnt" "rc=$rc"
done

out="$tmp/entpackt"
mkdir -p "$out"
tar -xzf "$archive" -C "$out"

# --- das frisch gebaute Bundle besteht die eigene Pruefung ---------------
res="$(bash "$verify" --bundle "$out" --pubkey "$tmp/pub.pem")" \
  || fail "verify_bundle lehnt das eigene Bundle ab" "$res"

# --- Tailscale-Tarball und Caddy-Beipack sind nicht mehr im Bundle ----------
[ -e "$out/tailscale" ] && fail "tailscale/ noch im Bundle" "$(find "$out" -maxdepth 2 | sort)"

# --- Layout nach Vertrag 1 ------------------------------------------------
for path in \
  bootstrap/10-apt.sh bootstrap/lib/step.sh bootstrap/verify_bundle.sh \
  dashboard/energy-node-dashboard dashboard/energy-node-dashboard.service \
  dashboard/Caddyfile \
  services/shelly/shelly_rpc_mqtt.py services/shelly/shelly-rpc.service \
  services/shelly/devices/shelly_devices.json \
  services/shelly/devices/shelly_devices.schema.json \
  services/shelly/devices/shelly_presets.json \
  config/config.json config/manifests/shelly.json
do
  [ -e "$out/$path" ] || fail "fehlt im Bundle: $path" "$(find "$out" -type f | sort)"
done

# manifest.json und config.schema.json sind KEINE Geraetedateien.
[ -e "$out/services/shelly/devices/manifest.json" ] && fail "manifest.json als Geraetedatei einsortiert"
[ -e "$out/services/shelly/devices/config.schema.json" ] && fail "Schema-Fragment als Geraetedatei einsortiert"

# --- Updater-Unit und Signaturschluessel im Bundle ----------------------------
[[ -x "${out}/dashboard/energy-node-updater" ]] \
  || fail "energy-node-updater not staged into the bundle"
[[ -f "${out}/dashboard/energy-node-updater.service" ]] \
  || fail "energy-node-updater.service not staged"
[[ -f "${out}/dashboard/energy-node-updater.path" ]] \
  || fail "energy-node-updater.path not staged"
[[ -f "${out}/dashboard/signing_key.pub.pem" ]] \
  || fail "signing public key not staged into the bundle"

# --- Units sind gerendert --------------------------------------------------
grep -q 'energynode' "$out/services/shelly/shelly-rpc.service" \
  && fail "Platzhalter in der Dienst-Unit" "$(cat "$out/services/shelly/shelly-rpc.service")"
grep -q 'energynode' "$out/dashboard/energy-node-dashboard.service" \
  && fail "Platzhalter in der Dashboard-Unit"
grep -q '/home/pruef' "$out/dashboard/energy-node-dashboard.service" \
  || fail "Zielbasis nicht eingesetzt" "$(cat "$out/dashboard/energy-node-dashboard.service")"

# --- Manifest-Inhalt -------------------------------------------------------
get() { python3 -c 'import json,sys; d=json.load(open(sys.argv[1])); print(eval(sys.argv[2], {"d": d}))' \
  "$out/manifest.json" "$1"; }
[ "$(get 'd["arch"]')" = armv6 ] || fail "arch falsch"
[ "$(get 'd["python_abi"]')" = cp311 ] || fail "abi falsch"
[ "$(get 'sorted(d["uname_machine"])')" = "['armv6l', 'armv7l']" ] || fail "uname_machine falsch"
[ "$(get 'd["target_user"]')" = pruef ] || fail "target_user falsch"
[ "$(get 'd["target_base"]')" = /home/pruef ] || fail "target_base falsch"
[ "$(get 'sorted(s["id"] for s in d["steps"])')" \
  = "['10', '15', '17', '20', '30', '35', '40', '50', '60', '65', '70', '81', '82', '83', '84', '85', '88']" ] \
  || fail "Schrittliste falsch" "$(get 'sorted(s["id"] for s in d["steps"])')"
[ "$(get 'next(s["optional"] for s in d["steps"] if s["id"]=="40")')" = True ] \
  || fail "40 nicht optional"
[ "$(get 'next(s["optional"] for s in d["steps"] if s["id"]=="10")')" = False ] \
  || fail "10 faelschlich optional"
[ "$(get 'next(s["unit"] for s in d["steps"] if s["id"]=="83")')" = shelly-rpc.service ] \
  || fail "Unit im Schritt 83 falsch"
[ "$(get 'next(s["kind"] for s in d["steps"] if s["id"]=="83")')" = device ] \
  || fail "Schritt 83 traegt kein kind=device"
[ "$(get 'next(s["kind"] for s in d["steps"] if s["id"]=="88")')" = service ] \
  || fail "Schritt 88 traegt kein kind=service"
[ "$(get '"bootstrap" in d["components"]')" = True ] || fail "Bootstrap-Version fehlt"

# --- Festlegungen: manifest.packages ist der Eintrag der Architektur ------
packages_json="$here/../build/packages.json"
pin_ok="$(python3 -I -c '
import json, sys
pins = json.load(open(sys.argv[1]))["armv6"]
got = json.load(open(sys.argv[2]))["packages"]
assert set(got) == {"tailscale", "caddy"}, sorted(got)
for name in ("tailscale", "caddy"):
    assert got[name]["sha256"] == pins[name]["sha256"], name
    assert got[name]["version"] == pins[name]["version"], name
    # fallback_urls wandern unveraendert in manifest.packages
    assert got[name].get("fallback_urls") == pins[name].get("fallback_urls"), name
assert got["tailscale"]["fallback_urls"] == [
    "https://pkgs.tailscale.com/stable/debian/pool/tailscale_1.62.0_armhf.deb"], "armv6 tailscale fallback"
print("yes")
' "$packages_json" "$out/manifest.json" 2>&1)" || fail "manifest.packages passt nicht zu packages.json" "$pin_ok"
[ "$pin_ok" = yes ] || fail "manifest.packages passt nicht zu packages.json" "$pin_ok"

# --- ohne Schluessel entsteht keine Signatur, sonst alles gleich ----------
bash "$script" --arch amd64 --out "$tmp/dist2" \
  --dashboard-binary "$tmp/fake-dashboard" \
  --skip-wheels >/dev/null 2>&1 || fail "Bau ohne Schluessel scheiterte"
a2="$(find "$tmp/dist2" -maxdepth 1 -name '*.tar.gz' | head -n 1)"
mkdir -p "$tmp/entpackt2"
tar -xzf "$a2" -C "$tmp/entpackt2"
[ -f "$tmp/entpackt2/manifest.json" ] || fail "kein Manifest ohne Schluessel"
[ -e "$tmp/entpackt2/manifest.json.sig" ] && fail "Signatur ohne Schluessel entstanden"
[ "$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["uname_machine"])' \
     "$tmp/entpackt2/manifest.json")" = "['x86_64']" ] || fail "amd64-uname falsch"

# --- unbekannte Architektur ------------------------------------------------
set +e
bash "$script" --arch sparc64 --out "$tmp/dist3" --skip-wheels >/dev/null 2>&1
rc=$?
set -e
[ "$rc" -ne 0 ] || fail "unbekannte Architektur nicht abgelehnt"

echo "OK: $(basename "$0")"
