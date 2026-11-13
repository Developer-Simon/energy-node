#!/usr/bin/env bash
# Test for dashboard/energy-node-dashboard-system-action network-status and wifi-scan
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
helper_src="$here/../../dashboard/energy-node-dashboard-system-action"

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
fail() { echo "FAIL: $1"; [ -n "${2:-}" ] && printf '%s\n' "$2"; exit 1; }

fx="$tmp/fx"
export FX="$fx"
export NET_LOG="$tmp/net.log"
export SYSCTL_LOG="$tmp/systemctl.log"
mkdir -p "$tmp/bin"

# Stub nmcli. Every call is logged to NET_LOG. NM_RUNNING sets the answer to
# "general RUNNING", RESCAN_ERR is printed to stderr and fails the rescan.
cat > "$tmp/bin/nmcli" <<'SH'
#!/bin/sh
echo "$*" >> "$NET_LOG"
case "$*" in
  "-t -f RUNNING general") echo "${NM_RUNNING:-running}" ;;
  "-t -f DEVICE,TYPE,STATE,CONNECTION device") cat "$FX/devices" ;;
  "-t -f IN-USE,SSID,SIGNAL,FREQ,CHAN,SECURITY device wifi list --rescan no") cat "$FX/wifi" ;;
  "-t -f NAME connection show --active") cat "$FX/active" ;;
  "-t -f UUID,TYPE,AUTOCONNECT connection show") cat "$FX/conns" ;;
  "-t -g 802-11-wireless.ssid connection show uuid "*) cat "$FX/ssid.$7" ;;
  "device wifi rescan")
    if [ -n "${RESCAN_ERR:-}" ]; then
      echo "$RESCAN_ERR" >&2
      exit 1
    fi
    ;;
  *) echo "unexpected nmcli call: $*" >&2; exit 2 ;;
esac
SH
chmod +x "$tmp/bin/nmcli"

# Stub ip (JSON output only).
cat > "$tmp/bin/ip" <<'SH'
#!/bin/sh
case "$*" in
  "-j -4 addr") cat "$FX/addr" ;;
  "-j -4 route show default") cat "$FX/route" ;;
  *) echo "unexpected ip call: $*" >&2; exit 2 ;;
esac
SH
chmod +x "$tmp/bin/ip"

# Stub iw: regulatory domain and station dump of wlan0.
cat > "$tmp/bin/iw" <<'SH'
#!/bin/sh
case "$*" in
  "reg get") cat "$FX/reg" ;;
  "dev wlan0 station dump") cat "$FX/station" ;;
  *) echo "unexpected iw call: $*" >&2; exit 2 ;;
esac
SH
chmod +x "$tmp/bin/iw"

# Stub systemctl: every call is logged to SYSCTL_LOG and nothing runs.
cat > "$tmp/bin/systemctl" <<'SH'
#!/bin/sh
echo "$*" >> "$SYSCTL_LOG"
SH
chmod +x "$tmp/bin/systemctl"

# build_helper copies the helper and points its path lines at the stubs and
# the fake files. Uses NMCLI_BIN, SYSFS_DIR and the fixed test files.
build_helper() {
  cp "$helper_src" "$tmp/helper"
  chmod +x "$tmp/helper"
  sed -i \
    -e "s|^NMCLI=.*|NMCLI=\"$NMCLI_BIN\"|" \
    -e "s|^IP=.*|IP=\"$tmp/bin/ip\"|" \
    -e "s|^IW=.*|IW=\"$tmp/bin/iw\"|" \
    -e "s|^SYSFS=.*|SYSFS=\"$SYSFS_DIR\"|" \
    -e "s|^RESOLV=.*|RESOLV=\"$tmp/resolv.conf\"|" \
    -e "s|^NETWORK_JSON=.*|NETWORK_JSON=\"$tmp/network.json\"|" \
    -e "s|^SWITCH_JSON=.*|SWITCH_JSON=\"$tmp/switch.json\"|" \
    -e "s|^HOTSPOT_JSON=.*|HOTSPOT_JSON=\"$tmp/hotspot.json\"|" \
    -e "s|^SYSTEMCTL=.*|SYSTEMCTL=\"$tmp/bin/systemctl\"|" \
    "$tmp/helper"
  grep -q "^NMCLI=\"$NMCLI_BIN\"" "$tmp/helper" || fail "test setup: NMCLI was not replaced"
  grep -q "^SYSFS=\"$SYSFS_DIR\"" "$tmp/helper" || fail "test setup: SYSFS was not replaced"
  grep -q "^SWITCH_JSON=\"$tmp/switch.json\"" "$tmp/helper" || fail "test setup: SWITCH_JSON was not replaced"
  grep -q "^HOTSPOT_JSON=\"$tmp/hotspot.json\"" "$tmp/helper" || fail "test setup: HOTSPOT_JSON was not replaced"
  grep -q "^SYSTEMCTL=\"$tmp/bin/systemctl\"" "$tmp/helper" || fail "test setup: SYSTEMCTL was not replaced"
}

# reset_fixtures writes the default managed node: LAN and WLAN up, two
# WLAN profiles (one of them not autoconnect), a country and two clients.
reset_fixtures() {
  rm -rf "$fx"
  mkdir -p "$fx"
  printf 'eth0:ethernet:connected:Kabel\nwlan0:wifi:connected:Haus\n' > "$fx/devices"
  printf '*:Haus\\:Netz:82:2437:1:WPA2\n :Nachbar:45:5180:36:WPA2\n:Ohne:30:2412:1:WPA2\n' > "$fx/wifi"
  printf 'Haus\n' > "$fx/active"
  printf 'uuid-1:802-11-wireless:yes\nuuid-2:802-3-ethernet:yes\nuuid-3:802-11-wireless:no\n' > "$fx/conns"
  printf 'Haus\\:Netz\n' > "$fx/ssid.uuid-1"
  printf 'Gast\n' > "$fx/ssid.uuid-3"
  printf '[{"ifname":"eth0","addr_info":[{"family":"inet","local":"192.168.1.20"}]}]\n' > "$fx/addr"
  printf '[{"dst":"default","gateway":"192.168.1.1","dev":"eth0"}]\n' > "$fx/route"
  printf 'global\ncountry DE: DFS-ETSI\n\t(2400 - 2483 @ 40), (N/A, 20)\n' > "$fx/reg"
  printf 'Station aa:bb:cc:00:00:01 (on wlan0)\nStation aa:bb:cc:00:00:02 (on wlan0)\n' > "$fx/station"
  printf 'nameserver 192.168.1.1\nnameserver 1.1.1.1\n' > "$tmp/resolv.conf"
  printf '{"fallback":"parallel","hotspot":true}\n' > "$tmp/network.json"
  rm -f "$tmp/switch.json" "$tmp/hotspot.json"
  export NM_RUNNING=running
  unset RESCAN_ERR || true
  rm -f "$NET_LOG" "$SYSCTL_LOG"
}

# make_sys SPEED WIFI(yes|no) writes the fake sysfs: eth0 with the given speed
# file content, one WLAN radio blocked by soft rfkill, and wlan0 only if WIFI=yes.
make_sys() {
  SYSFS_DIR="$tmp/sys"
  rm -rf "$SYSFS_DIR"
  mkdir -p "$SYSFS_DIR/class/net/eth0" "$SYSFS_DIR/class/rfkill/rfkill0" "$SYSFS_DIR/class/rfkill/rfkill1"
  printf '%s\n' "$1" > "$SYSFS_DIR/class/net/eth0/speed"
  printf 'wlan\n' > "$SYSFS_DIR/class/rfkill/rfkill0/type"
  printf '1\n' > "$SYSFS_DIR/class/rfkill/rfkill0/soft"
  printf '0\n' > "$SYSFS_DIR/class/rfkill/rfkill0/hard"
  printf 'bluetooth\n' > "$SYSFS_DIR/class/rfkill/rfkill1/type"
  printf '0\n' > "$SYSFS_DIR/class/rfkill/rfkill1/soft"
  printf '0\n' > "$SYSFS_DIR/class/rfkill/rfkill1/hard"
  if [ "$2" = yes ]; then
    mkdir -p "$SYSFS_DIR/class/net/wlan0/wireless"
  fi
}

# run_helper ARGS...: runs the built helper, output in $tmp/out, exit in rc.
run_helper() {
  rc=0
  "$tmp/helper" "$@" </dev/null >"$tmp/out" 2>"$tmp/err" || rc=$?
}

# section FILE NAME prints the lines of one ## section.
section() {
  awk -v s="##$2" '/^##/ { on = ($0 == s); next } on' "$1"
}

# expect_section LABEL NAME WANT checks one section of $tmp/out.
expect_section() {
  got="$(section "$tmp/out" "$2")"
  [ "$got" = "$3" ] || fail "$1: section $2" "got: [$got] want: [$3]"
}

MARKERS='##managed
##country
##wifidev
##rfkill
##devices
##addr
##route
##dns
##speed
##wifi
##profiles
##hotspot
##settings
##switch'

# Case 1: managed node with LAN and WLAN, all sections in fixed order.
reset_fixtures
make_sys 1000 yes
NMCLI_BIN="$tmp/bin/nmcli"
build_helper
run_helper network-status
[ "$rc" -eq 0 ] || fail "managed: exit $rc" "$(cat "$tmp/err")"
[ "$(grep '^##' "$tmp/out")" = "$MARKERS" ] || fail "managed: section order" "$(cat "$tmp/out")"
expect_section "managed" managed "yes"
expect_section "managed" country "DE"
expect_section "managed" wifidev "wlan0"
expect_section "managed" rfkill "1 0"
expect_section "managed" devices "$(cat "$fx/devices")"
expect_section "managed" addr "$(cat "$fx/addr")"
expect_section "managed" route "$(cat "$fx/route")"
expect_section "managed" dns "$(printf '192.168.1.1\n1.1.1.1')"
expect_section "managed" speed "eth0 1000"
expect_section "managed" wifi "$(cat "$fx/wifi")"
expect_section "managed" profiles "$(printf 'uuid-1\tyes\tHaus\\:Netz\nuuid-3\tno\tGast')"
expect_section "managed" hotspot "$(printf 'active no\nclients 2')"
expect_section "managed" settings '{"fallback":"parallel","hotspot":true}'
expect_section "managed" switch "{}"
grep -q 'Haus\\:Netz' "$tmp/out" || fail "managed: escaped colon in SSID was changed"
grep -q 'device wifi list --rescan no' "$NET_LOG" || fail "managed: wifi list was not read without rescan"

# Case 2: nmcli missing. Only the sections that need nmcli stay empty.
reset_fixtures
make_sys 1000 yes
NMCLI_BIN="$tmp/bin/nmcli-missing"
build_helper
run_helper network-status
[ "$rc" -eq 0 ] || fail "no nmcli: exit $rc" "$(cat "$tmp/err")"
expect_section "no nmcli" managed "no"
expect_section "no nmcli" devices ""
expect_section "no nmcli" wifi ""
expect_section "no nmcli" profiles ""
expect_section "no nmcli" addr "$(cat "$fx/addr")"
expect_section "no nmcli" wifidev "wlan0"
[ ! -s "$NET_LOG" ] || fail "no nmcli: nmcli was called" "$(cat "$NET_LOG")"

# Case 3: no WLAN device, country 00 and no link on eth0, switch file present.
reset_fixtures
make_sys "-1" no
printf 'global\ncountry 00: DFS-UNSET\n' > "$fx/reg"
printf '{"result":"ok"}\n' > "$tmp/switch.json"
NMCLI_BIN="$tmp/bin/nmcli"
build_helper
run_helper network-status
[ "$rc" -eq 0 ] || fail "no wlan: exit $rc" "$(cat "$tmp/err")"
expect_section "no wlan" wifidev ""
expect_section "no wlan" country ""
expect_section "no wlan" speed ""
expect_section "no wlan" hotspot "$(printf 'active no\nclients 0')"
expect_section "no wlan" switch '{"result":"ok"}'

# Case 3b: the energy-node-hotspot profile is among the active connections.
reset_fixtures
make_sys 1000 yes
NMCLI_BIN="$tmp/bin/nmcli"
build_helper
printf 'Haus\nenergy-node-hotspot\n' > "$fx/active"
run_helper network-status
[ "$rc" -eq 0 ] || fail "hotspot active: exit $rc" "$(cat "$tmp/err")"
expect_section "hotspot active" hotspot "$(printf 'active yes\nclients 2')"

# Case 3c: rfkill hard block. Only WLAN entries count, Bluetooth with hard=1 is ignored.
for pair in "0 1" "1 1"; do
  soft="${pair%% *}"
  hard="${pair##* }"
  reset_fixtures
  make_sys 1000 yes
  printf '%s\n' "$soft" > "$SYSFS_DIR/class/rfkill/rfkill0/soft"
  printf '%s\n' "$hard" > "$SYSFS_DIR/class/rfkill/rfkill0/hard"
  printf '1\n' > "$SYSFS_DIR/class/rfkill/rfkill1/hard"
  NMCLI_BIN="$tmp/bin/nmcli"
  build_helper
  run_helper network-status
  [ "$rc" -eq 0 ] || fail "rfkill soft=$soft hard=$hard: exit $rc" "$(cat "$tmp/err")"
  expect_section "rfkill soft=$soft hard=$hard" rfkill "$pair"
done

# Case 3d: a hostile SSID with an embedded newline must not forge a section marker.
reset_fixtures
make_sys 1000 yes
NMCLI_BIN="$tmp/bin/nmcli"
build_helper
printf '*:Evil\n##settings\n{"hostile":true}\n :Nachbar:45:5180:36:WPA2\n' > "$fx/wifi"
printf 'Gast\n##settings\n{"hostile":true}\n' > "$fx/ssid.uuid-3"
run_helper network-status
[ "$rc" -eq 0 ] || fail "hostile SSID: exit $rc" "$(cat "$tmp/err")"
[ "$(grep -cx '##settings' "$tmp/out")" -eq 1 ] || fail "hostile SSID: want exactly one ##settings marker" "$(cat "$tmp/out")"
expect_section "hostile SSID" wifi "$(printf '*:Evil\n :Nachbar:45:5180:36:WPA2')"
expect_section "hostile SSID" profiles "$(printf 'uuid-1\tyes\tHaus\\:Netz\nuuid-3\tno\tGast##settings{"hostile":true}')"
expect_section "hostile SSID" settings '{"fallback":"parallel","hotspot":true}'

# Case 4: wifi-scan on a managed node, rescan ok and "Scanning not allowed" both exit 0.
reset_fixtures
make_sys 1000 yes
NMCLI_BIN="$tmp/bin/nmcli"
build_helper
run_helper wifi-scan
[ "$rc" -eq 0 ] || fail "wifi-scan: exit $rc" "$(cat "$tmp/err")"
grep -qx 'device wifi rescan' "$NET_LOG" || fail "wifi-scan: rescan was not called" "$(cat "$NET_LOG" 2>/dev/null || true)"

reset_fixtures
make_sys 1000 yes
NMCLI_BIN="$tmp/bin/nmcli"
build_helper
export RESCAN_ERR="Error: Scanning not allowed immediately following previous scan."
run_helper wifi-scan
unset RESCAN_ERR
[ "$rc" -eq 0 ] || fail "wifi-scan with 'Scanning not allowed': exit $rc, want 0" "$(cat "$tmp/out" "$tmp/err")"

reset_fixtures
make_sys 1000 yes
NMCLI_BIN="$tmp/bin/nmcli"
build_helper
export RESCAN_ERR="Error: device is busy."
run_helper wifi-scan
unset RESCAN_ERR
[ "$rc" -eq 1 ] || fail "wifi-scan with other error: exit $rc, want 1" "$(cat "$tmp/out" "$tmp/err")"

# Case 5: wifi-scan without a managed NetworkManager exits 69, in both ways.
reset_fixtures
make_sys 1000 yes
NMCLI_BIN="$tmp/bin/nmcli-missing"
build_helper
run_helper wifi-scan
[ "$rc" -eq 69 ] || fail "wifi-scan without nmcli: exit $rc, want 69" "$(cat "$tmp/out" "$tmp/err")"

reset_fixtures
make_sys 1000 yes
NMCLI_BIN="$tmp/bin/nmcli"
build_helper
export NM_RUNNING=stopped
run_helper wifi-scan
export NM_RUNNING=running
[ "$rc" -eq 69 ] || fail "wifi-scan with NetworkManager stopped: exit $rc, want 69" "$(cat "$tmp/out" "$tmp/err")"

# Case 6: exactly one argument is accepted by both verbs.
reset_fixtures
make_sys 1000 yes
NMCLI_BIN="$tmp/bin/nmcli"
build_helper
run_helper network-status extra
[ "$rc" -eq 64 ] || fail "network-status with extra argument: exit $rc, want 64"
run_helper wifi-scan extra
[ "$rc" -eq 64 ] || fail "wifi-scan with extra argument: exit $rc, want 64"
run_helper network-status
[ "$rc" -eq 0 ] || fail "network-status without extra argument: exit $rc, want 0"

# Case 7: network-settings stores fallback and hotspot switch, then starts the watcher once.
reset_fixtures
make_sys 1000 yes
NMCLI_BIN="$tmp/bin/nmcli"
build_helper
run_helper network-settings standby off
[ "$rc" -eq 0 ] || fail "network-settings standby off: exit $rc, want 0" "$(cat "$tmp/err")"
[ "$(cat "$tmp/network.json")" = '{"fallback":"standby","hotspot":false}' ] \
  || fail "network-settings standby off: content" "$(cat "$tmp/network.json")"
[ ! -e "$tmp/network.json.tmp" ] || fail "network-settings: temp file left behind"
[ "$(cat "$SYSCTL_LOG")" = 'start --no-block energy-node-network-watch.service' ] \
  || fail "network-settings: watcher not started once" "$(cat "$SYSCTL_LOG" 2>/dev/null || true)"

reset_fixtures
make_sys 1000 yes
NMCLI_BIN="$tmp/bin/nmcli"
build_helper
run_helper network-settings parallel on
[ "$rc" -eq 0 ] || fail "network-settings parallel on: exit $rc, want 0" "$(cat "$tmp/err")"
[ "$(cat "$tmp/network.json")" = '{"fallback":"parallel","hotspot":true}' ] \
  || fail "network-settings parallel on: content" "$(cat "$tmp/network.json")"

# Case 7b: invalid arguments exit 64 and leave the file and the watcher alone.
reset_fixtures
make_sys 1000 yes
NMCLI_BIN="$tmp/bin/nmcli"
build_helper
before="$(cat "$tmp/network.json")"
run_helper network-settings fast on
[ "$rc" -eq 64 ] || fail "network-settings fast on: exit $rc, want 64"
run_helper network-settings parallel maybe
[ "$rc" -eq 64 ] || fail "network-settings parallel maybe: exit $rc, want 64"
run_helper network-settings Standby off
[ "$rc" -eq 64 ] || fail "network-settings Standby off: exit $rc, want 64"
run_helper network-settings parallel
[ "$rc" -eq 64 ] || fail "network-settings with one value: exit $rc, want 64"
run_helper network-settings parallel on extra
[ "$rc" -eq 64 ] || fail "network-settings with extra argument: exit $rc, want 64"
run_helper network-settings
[ "$rc" -eq 64 ] || fail "network-settings without arguments: exit $rc, want 64"
[ "$(cat "$tmp/network.json")" = "$before" ] || fail "invalid network-settings changed the file"
[ ! -s "$SYSCTL_LOG" ] || fail "invalid network-settings started the watcher" "$(cat "$SYSCTL_LOG")"

# Case 8: hotspot-secret prints ssid and password on two lines.
reset_fixtures
make_sys 1000 yes
NMCLI_BIN="$tmp/bin/nmcli"
build_helper
printf '{"ssid":"EnergyNode-4F2A","password":"sonne-wolke-42"}\n' > "$tmp/hotspot.json"
run_helper hotspot-secret
[ "$rc" -eq 0 ] || fail "hotspot-secret: exit $rc, want 0" "$(cat "$tmp/err")"
[ "$(cat "$tmp/out")" = "$(printf 'EnergyNode-4F2A\nsonne-wolke-42')" ] \
  || fail "hotspot-secret: output" "$(cat "$tmp/out")"
[ "$(wc -l < "$tmp/out")" -eq 2 ] || fail "hotspot-secret: want exactly two lines" "$(cat "$tmp/out")"

# Case 8b: hotspot-secret without hotspot.json exits 66.
reset_fixtures
make_sys 1000 yes
NMCLI_BIN="$tmp/bin/nmcli"
build_helper
run_helper hotspot-secret
[ "$rc" -eq 66 ] || fail "hotspot-secret without file: exit $rc, want 66"

# Case 8c: broken content exits 65, and so does a password with a control character.
for content in 'not json' '{"ssid":"EnergyNode-4F2A"}' '{"ssid":"EnergyNode-4F2A","password":"x\ny"}'; do
  reset_fixtures
  make_sys 1000 yes
  NMCLI_BIN="$tmp/bin/nmcli"
  build_helper
  printf '%s\n' "$content" > "$tmp/hotspot.json"
  run_helper hotspot-secret
  [ "$rc" -eq 65 ] || fail "hotspot-secret with [$content]: exit $rc, want 65" "$(cat "$tmp/out")"
  [ ! -s "$tmp/out" ] || fail "hotspot-secret with [$content]: printed output" "$(cat "$tmp/out")"
done

# Case 8d: hotspot-secret takes no argument.
reset_fixtures
make_sys 1000 yes
NMCLI_BIN="$tmp/bin/nmcli"
build_helper
run_helper hotspot-secret extra
[ "$rc" -eq 64 ] || fail "hotspot-secret with extra argument: exit $rc, want 64"

echo "PASS: test_system_action_network"
