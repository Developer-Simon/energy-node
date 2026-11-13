#!/usr/bin/env bash
# Test for scripts/bootstrap/67-network.sh: Zugangsdaten des Hotspots, das
# NetworkManager-Profil, der Waechter und sein Timer. nmcli, iw und systemctl
# sind Attrappen auf dem PATH, das WLAN-Geraet steckt im Fake-Sysfs (EN_SYSFS).
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
script="$here/../bootstrap/67-network.sh"
dash="$here/../../dashboard"

tmp="$(mktemp -d)"
trap 'chmod -R u+w "$tmp" 2>/dev/null; rm -rf "$tmp"' EXIT
fail() { echo "FAIL: $1"; [ -n "${2:-}" ] && printf '%s\n' "$2"; exit 1; }

# --- Bundle mit Waechter und Units -----------------------------------------
bundle="$tmp/bundle"
mkdir -p "$bundle/dashboard"
cp "$dash/energy-node-network-watch" "$dash/energy-node-network-watch.service" \
  "$dash/energy-node-network-watch.timer" "$bundle/dashboard/"

# --- Attrappen -------------------------------------------------------------
mkdir -p "$tmp/bin"
export CALL_LOG="$tmp/calls.log" IW_OUT="$tmp/iw.txt"
: > "$IW_OUT"
cat > "$tmp/bin/systemctl" <<'SH'
#!/bin/sh
printf 'systemctl %s\n' "$*" >> "$CALL_LOG"
exit 0
SH
cat > "$tmp/bin/nmcli" <<'SH'
#!/bin/sh
printf 'nmcli %s\n' "$*" >> "$CALL_LOG"
exit 0
SH
cat > "$tmp/bin/iw" <<'SH'
#!/bin/sh
cat "$IW_OUT"
SH
chmod +x "$tmp/bin/systemctl" "$tmp/bin/nmcli" "$tmp/bin/iw"
export PATH="$tmp/bin:$PATH"

# --- Sysfs mit wlan0 (MAC endet auf 445a) ----------------------------------
sys="$tmp/sys"
mkdir -p "$sys/class/net/wlan0/wireless"
printf '02:11:22:33:44:5a\n' > "$sys/class/net/wlan0/address"

root="$tmp/root"
mkdir -p "$root/etc"
printf 'a1b2c3d4e5f60718a9b0c1d2e3f4eeff\n' > "$root/etc/machine-id"

export EN_STATE_DIR="$tmp/state" EN_ROOT="$root" EN_BUNDLE_DIR="$bundle"
export EN_BUNDLE_VERSION=v1.0.0 EN_SUDO="" EN_SYSFS="$sys"
EN_TARGET_USER="$(id -un)"
export EN_TARGET_USER EN_SELECTION="$tmp/state/selection.json"

hotspot="$root/etc/energy-node/hotspot.json"
profile="$root/etc/NetworkManager/system-connections/energy-node-hotspot.nmconnection"

run() { bash "$script" 2>&1; }

# --- Wortliste: 256 eindeutige Woerter ohne Umlaute -------------------------
python3 - "$script" <<'PY' || fail "Wortliste ungueltig"
import re, sys
src = open(sys.argv[1], encoding="utf-8").read()
block = re.search(r"HOTSPOT_WORDS=\(\n(.*?)\n\)", src, re.S)
if not block:
    sys.exit("HOTSPOT_WORDS nicht gefunden")
words = block.group(1).split()
if len(words) != 256:
    sys.exit("%d Woerter, erwartet 256" % len(words))
if len(set(words)) != 256:
    sys.exit("doppelte Woerter")
bad = [w for w in words if not re.fullmatch(r"[a-z]+", w)]
if bad:
    sys.exit("Woerter mit anderen Zeichen: %r" % bad)
PY

# --- erster Lauf: ohne Laendercode -----------------------------------------
out="$(run)" || fail "erster Lauf scheitert" "$out"
grep -q '^##STEP 67 ok$' <<<"$out" || fail "kein ok-Marker" "$out"

python3 - "$hotspot" "$script" <<'PY' || fail "hotspot.json falsch"
import json, re, sys
doc = json.load(open(sys.argv[1], encoding="utf-8"))
if doc.get("ssid") != "EnergyNode-445A":
    sys.exit("ssid = %r" % doc.get("ssid"))
m = re.fullmatch(r"([a-z]+)-([a-z]+)-([a-z]+)-(\d{2})", doc.get("password", ""))
if not m:
    sys.exit("Passwort-Form falsch: %r" % doc.get("password"))
src = open(sys.argv[2], encoding="utf-8").read()
words = set(re.search(r"HOTSPOT_WORDS=\(\n(.*?)\n\)", src, re.S).group(1).split())
if not all(w in words for w in m.groups()[:3]):
    sys.exit("Woerter nicht aus der Liste")
PY
[ "$(stat -c %a "$hotspot")" = 640 ] || fail "hotspot.json nicht 0640: $(stat -c %a "$hotspot")"
pw1="$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["password"])' "$hotspot")"

grep -qx 'mode=ap' "$profile" || fail "Profil ohne mode=ap" "$(cat "$profile")"
grep -qx 'band=bg' "$profile" || fail "Profil ohne band=bg" "$(cat "$profile")"
grep -qx 'channel=1' "$profile" || fail "ohne Laendercode fehlt channel=1" "$(cat "$profile")"
grep -qx 'method=shared' "$profile" || fail "ipv4 nicht shared" "$(cat "$profile")"
grep -qx 'autoconnect=false' "$profile" || fail "autoconnect nicht false" "$(cat "$profile")"
grep -qx 'ssid=EnergyNode-445A' "$profile" || fail "ssid im Profil falsch" "$(cat "$profile")"
grep -qx "psk=$pw1" "$profile" || fail "psk stimmt nicht mit hotspot.json ueberein"
[ "$(stat -c %a "$profile")" = 600 ] || fail "Profil nicht 0600"

grep -qx 'nmcli connection reload' "$CALL_LOG" || fail "kein nmcli connection reload" "$(cat "$CALL_LOG")"
[ -x "$root/usr/local/sbin/energy-node-network-watch" ] || fail "Waechter nicht installiert"
[ -f "$root/etc/systemd/system/energy-node-network-watch.service" ] || fail "Service fehlt"
[ -f "$root/etc/systemd/system/energy-node-network-watch.timer" ] || fail "Timer fehlt"
grep -qx 'systemctl enable --now energy-node-network-watch.timer' "$CALL_LOG" \
  || fail "Timer nicht aktiviert" "$(cat "$CALL_LOG")"

# --- zweiter Lauf: Zugangsdaten bleiben, Profil wird neu geschrieben --------
# Der Laendercode ist jetzt gesetzt: das Profil darf keinen Kanal mehr fest
# vorgeben, das Passwort bleibt dasselbe.
echo "country DE: DFS-ETSI" > "$IW_OUT"
: > "$CALL_LOG"
out="$(run)" || fail "zweiter Lauf scheitert" "$out"
grep -q '^##STEP 67 ok$' <<<"$out" || fail "zweiter Lauf: kein ok" "$out"
pw2="$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["password"])' "$hotspot")"
[ "$pw1" = "$pw2" ] || fail "hotspot.json wurde beim zweiten Lauf ueberschrieben"
grep -q 'channel=' "$profile" && fail "mit Laendercode steht noch channel= im Profil" "$(cat "$profile")"
grep -qx "psk=$pw1" "$profile" || fail "Profil nach zweitem Lauf nicht mit dem Passwort"

# --- Weltbereich 00 zaehlt nicht als Laendercode ---------------------------
echo "country 00: DFS-UNSET" > "$IW_OUT"
out="$(run)" || fail "Lauf mit Laendercode 00 scheitert" "$out"
grep -qx 'channel=1' "$profile" || fail "Laendercode 00 gilt als gesetzt" "$(cat "$profile")"
[ "$pw1" = "$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["password"])' "$hotspot")" ] \
  || fail "Passwort nach Lauf mit 00 geaendert"

# --- ohne WLAN-Geraet: nur hotspot.json, kein Profil, Timer trotzdem --------
echo "country DE: DFS-ETSI" > "$IW_OUT"
rm -f "$profile"
mv "$sys/class/net/wlan0/wireless" "$tmp/wireless-aside"
: > "$CALL_LOG"
out="$(run)" || fail "Lauf ohne WLAN-Geraet scheitert" "$out"
grep -q '^##STEP 67 ok$' <<<"$out" || fail "ohne WLAN-Geraet: kein ok" "$out"
[ ! -e "$profile" ] || fail "ohne WLAN-Geraet entstand ein Profil"
grep -qx 'systemctl enable --now energy-node-network-watch.timer' "$CALL_LOG" \
  || fail "ohne WLAN-Geraet kein Timer-Start" "$(cat "$CALL_LOG")"
mv "$tmp/wireless-aside" "$sys/class/net/wlan0/wireless"

# --- ohne WLAN-Adresse: Kennung aus der machine-id, neue Zugangsdaten ------
root2="$tmp/root2"
mkdir -p "$root2/etc"
cp "$root/etc/machine-id" "$root2/etc/machine-id"
mkdir -p "$tmp/sys-empty"
out="$(EN_ROOT="$root2" EN_SYSFS="$tmp/sys-empty" run)" || fail "Lauf ohne MAC scheitert" "$out"
python3 -c 'import json,sys; d=json.load(open(sys.argv[1])); assert d["ssid"]=="EnergyNode-EEFF", d' \
  "$root2/etc/energy-node/hotspot.json" 2>/dev/null \
  || fail "Kennung aus der machine-id falsch" "$(cat "$root2/etc/energy-node/hotspot.json" 2>/dev/null || true)"

# --- fehlender Waechter im Bundle: Schritt scheitert ------------------------
empty="$tmp/empty-bundle"
mkdir -p "$empty/dashboard"
out="$(EN_BUNDLE_DIR="$empty" EN_ROOT="$tmp/root3" run || true)"
grep -q '^##STEP 67 fail NETWORK_WATCH_MISSING$' <<<"$out" \
  || fail "fehlender Waechter nicht gemeldet" "$out"

echo "PASS: 67-network.sh"
