#!/usr/bin/env bash
# Test for dashboard/energy-node-network-watch: the WLAN fallback (standby and
# parallel) and the emergency hotspot. nmcli, ip, iw and systemctl are stubs on
# PATH, the sysfs is faked and the clock is set with NOW.
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
watch_src="$here/../../dashboard/energy-node-network-watch"

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
fail() { echo "FAIL: $1"; [ -n "${2:-}" ] && printf '%s\n' "$2"; exit 1; }

[ -x "$watch_src" ] || fail "watcher is not executable"

export CALL_LOG="$tmp/calls.log"
export FX="$tmp/fx"
export SWITCH_ACTIVE="$tmp/switch-active"
export SYSFS="$tmp/sys"
export NETWORK_JSON="$tmp/network.json"
export STATE_DIR="$tmp/state"
mkdir -p "$tmp/bin"
export PATH="$tmp/bin:$PATH"

# Stub systemctl. The switch unit counts as active while SWITCH_ACTIVE exists.
cat > "$tmp/bin/systemctl" <<'SH'
#!/bin/sh
case "$*" in
  "is-active --quiet energy-node-wifi-switch.service")
    if [ -e "$SWITCH_ACTIVE" ]; then exit 0; fi
    exit 3
    ;;
  *) echo "unexpected systemctl call: $*" >&2; exit 2 ;;
esac
SH

# Stub nmcli. Reads come from FX/devices and FX/active. Changing calls are
# logged to CALL_LOG. A hotspot or WLAN connect fails when FX/hotspot-fail or
# FX/wifi-fail exists.
cat > "$tmp/bin/nmcli" <<'SH'
#!/bin/sh
case "$*" in
  "-t -f DEVICE,STATE device") cat "$FX/devices" ;;
  "-t -f NAME connection show --active") cat "$FX/active" ;;
  "connection up id energy-node-hotspot")
    echo "nmcli $*" >> "$CALL_LOG"
    if [ -e "$FX/hotspot-fail" ]; then exit 4; fi
    grep -qx energy-node-hotspot "$FX/active" 2>/dev/null || echo energy-node-hotspot >> "$FX/active"
    exit 0
    ;;
  "connection down id energy-node-hotspot")
    echo "nmcli $*" >> "$CALL_LOG"
    grep -vx energy-node-hotspot "$FX/active" > "$FX/active.new" || true
    mv "$FX/active.new" "$FX/active"
    exit 0
    ;;
  "device set wlan0 autoconnect yes"|"device set wlan0 autoconnect no")
    echo "nmcli $*" >> "$CALL_LOG"
    exit 0
    ;;
  "device disconnect wlan0")
    echo "nmcli $*" >> "$CALL_LOG"
    echo "wlan0:disconnected" > "$FX/devices"
    exit 0
    ;;
  "device connect wlan0"|"--wait 60 device connect wlan0")
    echo "nmcli $*" >> "$CALL_LOG"
    if [ -e "$FX/wifi-fail" ]; then exit 4; fi
    echo "wlan0:connected" > "$FX/devices"
    exit 0
    ;;
  *) echo "unexpected nmcli call: $*" >&2; exit 2 ;;
esac
SH

# Stub ip: eth0 has an IPv4 address when FX/eth0-addr exists.
cat > "$tmp/bin/ip" <<'SH'
#!/bin/sh
case "$*" in
  "-4 -o addr show dev eth0")
    if [ -e "$FX/eth0-addr" ]; then
      echo "2: eth0    inet 192.168.1.20/24 brd 192.168.1.255 scope global dynamic eth0"
    fi
    exit 0
    ;;
  *) echo "unexpected ip call: $*" >&2; exit 2 ;;
esac
SH

# Stub iw: the station dump is FX/clients, it fails when FX/iw-fail exists.
cat > "$tmp/bin/iw" <<'SH'
#!/bin/sh
case "$*" in
  "dev wlan0 station dump")
    if [ -e "$FX/iw-fail" ]; then exit 1; fi
    cat "$FX/clients"
    ;;
  *) echo "unexpected iw call: $*" >&2; exit 2 ;;
esac
SH
chmod +x "$tmp/bin"/*

# reset clears everything: LAN up, WLAN disconnected, no hotspot, standby.
reset() {
  rm -rf "$FX" "$STATE_DIR" "$SYSFS" "$SWITCH_ACTIVE" "$NETWORK_JSON"
  mkdir -p "$FX" "$STATE_DIR" "$SYSFS/class/net/eth0" "$SYSFS/class/net/wlan0/wireless"
  : > "$CALL_LOG"
  echo 1 > "$SYSFS/class/net/eth0/carrier"
  touch "$FX/eth0-addr"
  echo "wlan0:disconnected" > "$FX/devices"
  : > "$FX/active"
  : > "$FX/clients"
  printf '{"fallback":"standby","hotspot":true}\n' > "$NETWORK_JSON"
}

set_settings() {
  printf '%s\n' "$1" > "$NETWORK_JSON"
}

lan_up() {
  echo 1 > "$SYSFS/class/net/eth0/carrier"
  touch "$FX/eth0-addr"
}

lan_down() {
  echo 0 > "$SYSFS/class/net/eth0/carrier"
  rm -f "$FX/eth0-addr"
}

# tick NOW runs the watcher once at that time, its calls go to CALL_LOG.
tick() {
  : > "$CALL_LOG"
  rc=0
  NOW="$1" "$watch_src" </dev/null >"$tmp/out" 2>"$tmp/err" || rc=$?
  [ "$rc" -eq 0 ] || fail "tick $1: exit $rc" "$(cat "$tmp/err")"
}

has_call() {
  grep -qxF "nmcli $1" "$CALL_LOG"
}

no_calls() {
  [ ! -s "$CALL_LOG" ]
}

hotspot_active() {
  grep -qx energy-node-hotspot "$FX/active"
}

# line_of CALL prints the line number of the first call, empty if none.
line_of() {
  grep -nxF "nmcli $1" "$CALL_LOG" | head -1 | cut -d: -f1
}

# Case 1: standby, LAN there -> WLAN is switched off and disconnected.
reset
echo "wlan0:connected" > "$FX/devices"
tick 1000
has_call "device set wlan0 autoconnect no" || fail "standby LAN: autoconnect no missing" "$(cat "$CALL_LOG")"
has_call "device disconnect wlan0" || fail "standby LAN: disconnect missing" "$(cat "$CALL_LOG")"
has_call "device set wlan0 autoconnect yes" && fail "standby LAN: autoconnect yes called" "$(cat "$CALL_LOG")"
hotspot_active && fail "standby LAN: hotspot up"

# Case 2: standby, LAN gone for 20 s -> nothing yet.
reset
lan_down
tick 1000
tick 1020
no_calls || fail "standby LAN gone 20 s: calls made" "$(cat "$CALL_LOG")"

# Case 3: standby, LAN gone for 31 s -> WLAN on. LAN back -> WLAN off again.
tick 1031
has_call "device set wlan0 autoconnect yes" || fail "standby LAN gone 31 s: autoconnect yes missing" "$(cat "$CALL_LOG")"
has_call "device connect wlan0" || fail "standby LAN gone 31 s: connect missing" "$(cat "$CALL_LOG")"
lan_up
tick 1046
has_call "device set wlan0 autoconnect no" || fail "LAN back: autoconnect no missing" "$(cat "$CALL_LOG")"
has_call "device disconnect wlan0" || fail "LAN back: disconnect missing" "$(cat "$CALL_LOG")"

# Case 4: parallel -> only autoconnect yes, whatever the LAN does.
reset
set_settings '{"fallback":"parallel","hotspot":true}'
lan_up
tick 1000
has_call "device set wlan0 autoconnect yes" || fail "parallel: autoconnect yes missing" "$(cat "$CALL_LOG")"
has_call "device disconnect wlan0" && fail "parallel: disconnect called" "$(cat "$CALL_LOG")"
has_call "device set wlan0 autoconnect no" && fail "parallel: autoconnect no called" "$(cat "$CALL_LOG")"

# Case 5: no network.json -> parallel.
reset
rm -f "$NETWORK_JSON"
lan_up
tick 1000
has_call "device set wlan0 autoconnect yes" || fail "missing settings: parallel default missing" "$(cat "$CALL_LOG")"

# Case 6: 5 min without LAN and WLAN -> hotspot up. Before that, nothing.
reset
set_settings '{"fallback":"parallel","hotspot":true}'
lan_down
tick 1000
has_call "connection up id energy-node-hotspot" && fail "offline 0 s: hotspot up too early"
tick 1299
has_call "connection up id energy-node-hotspot" && fail "offline 299 s: hotspot up too early"
tick 1300
has_call "connection up id energy-node-hotspot" || fail "offline 300 s: hotspot not up" "$(cat "$CALL_LOG")"
hotspot_active || fail "offline 300 s: hotspot not in active list"

# Case 7: the cable is back while the hotspot is up -> hotspot down.
lan_up
tick 1315
has_call "connection down id energy-node-hotspot" || fail "cable back: hotspot not down" "$(cat "$CALL_LOG")"
hotspot_active && fail "cable back: hotspot still active"

# Case 8: the hotspot is up, no client for 5 min, WLAN connect fails -> down,
# connect attempt, hotspot up again. Before 5 min since opening, nothing.
reset
set_settings '{"fallback":"parallel","hotspot":true}'
lan_down
echo energy-node-hotspot > "$FX/active"
touch "$FX/wifi-fail"
tick 1000
has_call "connection down id energy-node-hotspot" && fail "open hotspot, 0 s: hotspot went down" "$(cat "$CALL_LOG")"
tick 1299
has_call "--wait 60 device connect wlan0" && fail "open hotspot, 299 s: WLAN tried too early" "$(cat "$CALL_LOG")"
tick 1300
d="$(line_of "connection down id energy-node-hotspot")"
c="$(line_of "--wait 60 device connect wlan0")"
u="$(line_of "connection up id energy-node-hotspot")"
if [ -z "$d" ] || [ -z "$c" ] || [ -z "$u" ]; then
  fail "open hotspot, 300 s: down, connect or up missing" "$(cat "$CALL_LOG")"
fi
if [ "$d" -ge "$c" ] || [ "$c" -ge "$u" ]; then
  fail "open hotspot, 300 s: wrong order" "$(cat "$CALL_LOG")"
fi
hotspot_active || fail "open hotspot, 300 s: hotspot not up again after failed connect"

# Case 9: after the failed try the hotspot waits another 5 min before it tries again.
tick 1400
has_call "--wait 60 device connect wlan0" && fail "after failed try, 100 s: WLAN tried again too early" "$(cat "$CALL_LOG")"

# Case 10: the hotspot is up, the WLAN try works -> the hotspot stays down.
reset
set_settings '{"fallback":"parallel","hotspot":true}'
lan_down
echo energy-node-hotspot > "$FX/active"
echo 1000 > "$STATE_DIR/hotspot_since"
tick 1300
has_call "connection down id energy-node-hotspot" || fail "WLAN works: hotspot not down" "$(cat "$CALL_LOG")"
has_call "connection up id energy-node-hotspot" && fail "WLAN works: hotspot up again" "$(cat "$CALL_LOG")"
hotspot_active && fail "WLAN works: hotspot still active"

# Case 11: a client is connected to the open hotspot -> nothing, even after 5 min.
reset
set_settings '{"fallback":"parallel","hotspot":true}'
lan_down
echo energy-node-hotspot > "$FX/active"
echo 1000 > "$STATE_DIR/hotspot_since"
echo "Station aa:bb:cc:dd:ee:ff (on wlan0)" > "$FX/clients"
tick 1400
has_call "connection down id energy-node-hotspot" && fail "client connected: hotspot went down" "$(cat "$CALL_LOG")"
has_call "--wait 60 device connect wlan0" && fail "client connected: WLAN tried" "$(cat "$CALL_LOG")"

# Case 12: hotspot switched off in the settings -> an active hotspot goes down.
reset
set_settings '{"fallback":"parallel","hotspot":false}'
lan_down
echo energy-node-hotspot > "$FX/active"
tick 1000
has_call "connection down id energy-node-hotspot" || fail "hotspot off: hotspot not down" "$(cat "$CALL_LOG")"
hotspot_active && fail "hotspot off: hotspot still active"

# Case 13: hotspot switched off in the settings and not up -> no hotspot.
reset
set_settings '{"fallback":"parallel","hotspot":false}'
lan_down
tick 1000
tick 1400
has_call "connection up id energy-node-hotspot" && fail "hotspot off: hotspot came up" "$(cat "$CALL_LOG")"

# Case 14: no WLAN device -> no hotspot, even offline for 10 min.
reset
set_settings '{"fallback":"parallel","hotspot":true}'
rm -rf "$SYSFS/class/net/wlan0"
lan_down
tick 1000
tick 1600
has_call "connection up id energy-node-hotspot" && fail "no WLAN device: hotspot came up" "$(cat "$CALL_LOG")"

# Case 15: the WLAN switch runs -> the watcher does nothing at all.
reset
lan_down
touch "$SWITCH_ACTIVE"
tick 1000
tick 1400
no_calls || fail "switch running: calls made" "$(cat "$CALL_LOG")"
hotspot_active && fail "switch running: hotspot came up"

echo "PASS: test_network_watch"
