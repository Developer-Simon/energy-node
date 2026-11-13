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

# --- Passwort-Vorgabe per Datei (--hotspot-password-file) -------------------
run_args() { bash "$script" "$@" 2>&1; }
hs_field() { python3 -c 'import json, sys; print(json.load(open(sys.argv[1], encoding="utf-8"))[sys.argv[2]])' "$1" "$2"; }

# Vorgabe, keine hotspot.json: das Passwort kommt aus der Datei.
r4="$tmp/root4"
mkdir -p "$r4/etc"
cp "$root/etc/machine-id" "$r4/etc/machine-id"
hs4="$r4/etc/energy-node/hotspot.json"
prof4="$r4/etc/NetworkManager/system-connections/energy-node-hotspot.nmconnection"
printf '%s\n' 'Mein Pass#1' > "$tmp/pw1"
out="$(EN_ROOT="$r4" run_args --hotspot-password-file "$tmp/pw1")" || fail "Lauf mit Vorgabe scheitert" "$out"
grep -q '^##STEP 67 ok$' <<<"$out" || fail "Vorgabe: kein ok-Marker" "$out"
[ "$(hs_field "$hs4" ssid)" = EnergyNode-445A ] || fail "Vorgabe: ssid falsch"
[ "$(hs_field "$hs4" password)" = 'Mein Pass#1' ] || fail "Vorgabe: Passwort nicht uebernommen"
[ "$(stat -c %a "$hs4")" = 640 ] || fail "Vorgabe: hotspot.json nicht 0640"
grep -qxF 'psk=Mein Pass#1' "$prof4" || fail "Vorgabe: psk im Profil falsch" "$(cat "$prof4")"
if grep -qF 'Mein Pass#1' <<<"$out"; then fail "Passwort steht in der Ausgabe" "$out"; fi

# Vorgabe ueberschreibt eine vorhandene hotspot.json, die SSID bleibt.
r5="$tmp/root5"
mkdir -p "$r5/etc"
cp "$root/etc/machine-id" "$r5/etc/machine-id"
hs5="$r5/etc/energy-node/hotspot.json"
prof5="$r5/etc/NetworkManager/system-connections/energy-node-hotspot.nmconnection"
out="$(EN_ROOT="$r5" run_args)" || fail "Erstlauf fuer Ueberschreiben scheitert" "$out"
printf '%s\n' 'anderes-passwort' > "$tmp/pw2"
out="$(EN_ROOT="$r5" run_args --hotspot-password-file "$tmp/pw2")" || fail "Vorgabe bei vorhandener Datei scheitert" "$out"
grep -q '^##STEP 67 ok$' <<<"$out" || fail "Ueberschreiben: kein ok-Marker" "$out"
[ "$(hs_field "$hs5" ssid)" = EnergyNode-445A ] || fail "Ueberschreiben: ssid geaendert"
[ "$(hs_field "$hs5" password)" = anderes-passwort ] || fail "Vorgabe ueberschreibt das Passwort nicht"
[ "$(stat -c %a "$hs5")" = 640 ] || fail "Ueberschreiben: hotspot.json nicht 0640"
grep -qxF 'psk=anderes-passwort' "$prof5" || fail "Ueberschreiben: psk im Profil falsch" "$(cat "$prof5")"

# Ohne Vorgabe bleibt das Passwort aus dem vorherigen Lauf.
out="$(EN_ROOT="$r5" run_args)" || fail "Lauf ohne Vorgabe scheitert" "$out"
[ "$(hs_field "$hs5" password)" = anderes-passwort ] || fail "Lauf ohne Vorgabe aendert das Passwort"

# Ungueltige Vorgaben: Schritt scheitert, das Passwort steht in keiner
# Ausgabe, hotspot.json bleibt unveraendert.
before="$tmp/hs5.before"
cp "$hs5" "$before"
expect_invalid() {  # expect_invalid <printf-Format des Dateiinhalts> <Passwort fuer Ausgabeprüfung>
  local f="$tmp/bad.pw" out
  printf '%b' "$1" > "$f"
  if out="$(EN_ROOT="$r5" run_args --hotspot-password-file "$f")"; then
    fail "ungueltige Vorgabe akzeptiert" "$1"
  fi
  grep -qxF '##STEP 67 fail HOTSPOT_PASSWORD_INVALID' <<<"$out" \
    || fail "kein HOTSPOT_PASSWORD_INVALID-Marker" "$out"
  if [ -n "$2" ] && grep -qF -- "$2" <<<"$out"; then fail "Passwort steht in der Ausgabe" "$out"; fi
  cmp -s "$before" "$hs5" || fail "hotspot.json bei ungueltiger Vorgabe geaendert"
}
expect_invalid 'abc1234\n' 'abc1234'                                  # 7 Zeichen
expect_invalid 'Passwört123\n' 'Passwört123'                          # Umlaut
expect_invalid "$(printf 'g%.0s' {1..64})\n" "$(printf 'g%.0s' {1..64})"  # 64 Zeichen, keine Hex
expect_invalid '' ''                                                  # leere Datei
expect_invalid 'abcdefgh\n\n' 'abcdefgh'                              # nur ein Zeilenende darf weg

# Fehlende Datei: ebenfalls HOTSPOT_PASSWORD_INVALID.
if out="$(EN_ROOT="$r5" run_args --hotspot-password-file "$tmp/gibt-es-nicht")"; then
  fail "fehlende Datei akzeptiert" "$out"
fi
grep -qxF '##STEP 67 fail HOTSPOT_PASSWORD_INVALID' <<<"$out" \
  || fail "fehlende Datei: kein HOTSPOT_PASSWORD_INVALID-Marker" "$out"
cmp -s "$before" "$hs5" || fail "hotspot.json bei fehlender Datei geaendert"

# 64 Hex-Ziffern sind gueltig.
hex="$(printf '1f%.0s' {1..32})"
printf '%s\n' "$hex" > "$tmp/pw-hex"
out="$(EN_ROOT="$r5" run_args --hotspot-password-file "$tmp/pw-hex")" || fail "64 Hex-Ziffern abgelehnt" "$out"
grep -q '^##STEP 67 ok$' <<<"$out" || fail "64 Hex-Ziffern: kein ok-Marker" "$out"
[ "$(hs_field "$hs5" password)" = "$hex" ] || fail "64 Hex-Ziffern nicht uebernommen"

echo "PASS: 67-network.sh"
