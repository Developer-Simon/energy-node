#!/usr/bin/env bash
# Test for dashboard/energy-node-dashboard-system-action wifi-connect, wifi-forget
# and wifi-switch-run
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
helper_src="$here/../../dashboard/energy-node-dashboard-system-action"

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
fail() { echo "FAIL: $1"; [ -n "${2:-}" ] && printf '%s\n' "$2"; exit 1; }

export CALL_LOG="$tmp/calls.log"
export UNIT_ACTIVE="$tmp/unit-active"
export TYPES_DIR="$tmp/types"
export FX="$tmp/fx"
mkdir -p "$tmp/bin" "$TYPES_DIR"
py="$(command -v python3)"

# Stub systemctl. The switch unit counts as active while UNIT_ACTIVE exists.
cat > "$tmp/bin/systemctl" <<'SH'
#!/bin/sh
echo "systemctl $*" >> "$CALL_LOG"
case "$*" in
  "is-active --quiet energy-node-wifi-switch.service")
    if [ -e "$UNIT_ACTIVE" ]; then exit 0; fi
    exit 3
    ;;
  *) echo "unexpected systemctl call: $*" >&2; exit 2 ;;
esac
SH

# Stub systemd-run. Every call is logged, nothing is started.
cat > "$tmp/bin/systemd-run" <<'SH'
#!/bin/sh
echo "systemd-run $*" >> "$CALL_LOG"
SH

# Stub nmcli. The active list comes from FX/active (uuid:type:name). A new
# profile loaded while FX/fail-new exists fails to come up, as do the UUIDs in
# FX/fail-uuids, with the text of FX/fail-msg on stderr.
cat > "$tmp/bin/nmcli" <<'SH'
#!/bin/sh
echo "nmcli $*" >> "$CALL_LOG"
last=""
for a; do last="$a"; done
case "$*" in
  "-t -g connection.type connection show uuid "*)
    f="$TYPES_DIR/$7"
    [ -f "$f" ] || exit 10
    cat "$f"
    ;;
  "connection delete uuid "*|"connection down uuid "*|"connection modify uuid "*) exit 0 ;;
  "-t -f UUID,TYPE,NAME connection show --active") cat "$FX/active" ;;
  "connection load "*)
    keyfile_uuid="$(sed -n 's/^uuid=//p' "$3")"
    if [ -e "$FX/fail-new" ]; then printf '%s\n' "$keyfile_uuid" >> "$FX/fail-uuids"; fi
    exit 0
    ;;
  "--wait "*" connection up uuid "*)
    if grep -qx -- "$last" "$FX/fail-uuids" 2>/dev/null; then
      cat "$FX/fail-msg" >&2
      exit 4
    fi
    exit 0
    ;;
  "connection up id energy-node-hotspot") exit 0 ;;
  *) echo "unexpected nmcli call: $*" >&2; exit 2 ;;
esac
SH

# Stub ip: the wlan0 address, the default route and no neighbour entry.
cat > "$tmp/bin/ip" <<'SH'
#!/bin/sh
echo "ip $*" >> "$CALL_LOG"
case "$*" in
  "-4 -o addr show dev wlan0") cat "$FX/addr4" ;;
  "-j -4 route show default") cat "$FX/route" ;;
  "neigh show "*) exit 1 ;;
  *) echo "unexpected ip call: $*" >&2; exit 2 ;;
esac
SH

# Stub ping: succeeds with a 4.12 ms reply unless FX/ping-fail exists.
cat > "$tmp/bin/ping" <<'SH'
#!/bin/sh
echo "ping $*" >> "$CALL_LOG"
if [ -e "$FX/ping-fail" ]; then exit 1; fi
echo "64 bytes from 192.168.178.1: icmp_seq=1 ttl=64 time=4.12 ms"
SH

# Stubs getent and curl: the internet check passes.
cat > "$tmp/bin/getent" <<'SH'
#!/bin/sh
echo "getent $*" >> "$CALL_LOG"
SH
cat > "$tmp/bin/curl" <<'SH'
#!/bin/sh
echo "curl $*" >> "$CALL_LOG"
SH
chmod +x "$tmp/bin"/*

stage="$tmp/data/network-connect.staged.json"
run="$tmp/run"
nm="$tmp/nm"
PROGRESS="$run/wifi-switch.json"

# build_helper OWNER copies the helper and points its path lines at the stubs,
# the temp staging file, the temp run dir and short timings. OWNER is the
# expected owner of the staging file.
build_helper() {
  owner="$1"
  cp "$helper_src" "$tmp/helper"
  chmod +x "$tmp/helper"
  sed -i \
    -e "s|^WIFI_STAGED=.*|WIFI_STAGED=\"$stage\"|" \
    -e "s|^WIFI_STAGE_OWNER=.*|WIFI_STAGE_OWNER=\"$owner\"|" \
    -e "s|^RUN_DIR=.*|RUN_DIR=\"$run\"|" \
    -e "s|^PYTHON3=.*|PYTHON3=\"$py\"|" \
    -e "s|^SYSTEMD_RUN=.*|SYSTEMD_RUN=\"$tmp/bin/systemd-run\"|" \
    -e "s|^SYSTEMCTL=.*|SYSTEMCTL=\"$tmp/bin/systemctl\"|" \
    -e "s|^NMCLI=.*|NMCLI=\"$tmp/bin/nmcli\"|" \
    -e "s|^IP=.*|IP=\"$tmp/bin/ip\"|" \
    -e "s|^PING=.*|PING=\"$tmp/bin/ping\"|" \
    -e "s|^GETENT=.*|GETENT=\"$tmp/bin/getent\"|" \
    -e "s|^CURL=.*|CURL=\"$tmp/bin/curl\"|" \
    -e "s|^NM_CONN_DIR=.*|NM_CONN_DIR=\"$nm\"|" \
    -e "s|^WIFI_DEADLINE_S=.*|WIFI_DEADLINE_S=\"5\"|" \
    -e "s|^WIFI_POLL_S=.*|WIFI_POLL_S=\"0.05\"|" \
    "$tmp/helper"
  grep -q "^WIFI_STAGED=\"$stage\"" "$tmp/helper" || fail "test setup: WIFI_STAGED was not replaced"
  grep -q "^WIFI_STAGE_OWNER=\"$owner\"" "$tmp/helper" || fail "test setup: WIFI_STAGE_OWNER was not replaced"
  grep -q "^RUN_DIR=\"$run\"" "$tmp/helper" || fail "test setup: RUN_DIR was not replaced"
  grep -q "^NMCLI=\"$tmp/bin/nmcli\"" "$tmp/helper" || fail "test setup: NMCLI was not replaced"
  grep -q "^NM_CONN_DIR=\"$nm\"" "$tmp/helper" || fail "test setup: NM_CONN_DIR was not replaced"
  grep -q "^WIFI_DEADLINE_S=\"5\"" "$tmp/helper" || fail "test setup: WIFI_DEADLINE_S was not replaced"
}

# reset clears the run state and sets the fixtures: a previous WLAN
# connection "Haus" is active, wlan0 has an address, the gateway answers.
reset() {
  rm -rf "$tmp/data" "$run" "$UNIT_ACTIVE" "$FX" "$nm"
  rm -f "$TYPES_DIR"/*
  mkdir -p "$tmp/data" "$FX" "$nm"
  : > "$CALL_LOG"
  printf 'uuid-prev:802-11-wireless:Haus\n' > "$FX/active"
  printf '3: wlan0    inet 192.168.178.52/24 brd 192.168.178.255 scope global dynamic wlan0\n' > "$FX/addr4"
  printf '[{"dst":"default","gateway":"192.168.178.1","dev":"wlan0"}]\n' > "$FX/route"
  printf 'Error: Connection activation failed: device disconnected.\n' > "$FX/fail-msg"
}

put_stage() {
  printf '%s' "$1" > "$stage"
}

# start_switch JSON stages the request and runs wifi-connect, which writes the
# request and the progress file that wifi-switch-run reads.
start_switch() {
  put_stage "$1"
  run_helper wifi-connect
  [ "$rc" -eq 0 ] || fail "start_switch: wifi-connect exit $rc" "$(cat "$tmp/err")"
}

mode_of() {
  stat -c %a "$1"
}

run_helper() {
  rc=0
  "$tmp/helper" "$@" </dev/null >"$tmp/out" 2>"$tmp/err" || rc=$?
}

ME="$(id -un)"
VALID='{"ssid":"Büro:2","password":"geheim123","hidden":false,"profile_uuid":"","security":"WPA2"}'
U_WIFI="6f1a2b3c-4d5e-4f60-8a7b-9c0d1e2f3a4b"
U_ETH="7a2b3c4d-5e6f-4a70-9b8c-0d1e2f3a4b5c"
U_SAVED="9d8e7f60-1a2b-4c3d-8e4f-5a6b7c8d9e0f"
SWITCH_CALL="systemd-run --unit=energy-node-wifi-switch --collect /usr/local/sbin/energy-node-dashboard-system-action wifi-switch-run"

# Case 1: a staging file owned by someone else than energynode -> 65.
reset
build_helper energynode
put_stage "$VALID"
run_helper wifi-connect
[ "$rc" -eq 65 ] || fail "foreign owner: exit $rc, want 65" "$(cat "$tmp/err")"
[ ! -e "$run/wifi-connect.json" ] || fail "foreign owner: request was moved"

# Case 2: invalid requests -> 65, nothing moved, no progress file.
check_invalid() {
  label="$1"
  json="$2"
  reset
  build_helper "$ME"
  put_stage "$json"
  run_helper wifi-connect
  [ "$rc" -eq 65 ] || fail "invalid $label: exit $rc, want 65" "$(cat "$tmp/err")"
  [ ! -e "$run/wifi-connect.json" ] || fail "invalid $label: request was moved"
  [ ! -e "$PROGRESS" ] || fail "invalid $label: progress file written"
}

long33="$(printf 'a%.0s' {1..33})"
long1100="$(printf 'x%.0s' {1..1100})"
long64="$(printf 'g%.0s' {1..64})"
check_invalid "empty ssid" '{"ssid":"","password":"geheim123","hidden":false,"profile_uuid":"","security":"WPA2"}'
check_invalid "ssid of 33 bytes" "{\"ssid\":\"$long33\",\"password\":\"geheim123\",\"hidden\":false,\"profile_uuid\":\"\",\"security\":\"WPA2\"}"
check_invalid "ssid with a newline" '{"ssid":"Ab\ncd","password":"geheim123","hidden":false,"profile_uuid":"","security":"WPA2"}'
check_invalid "password of 7 characters" '{"ssid":"Haus","password":"abcdefg","hidden":false,"profile_uuid":"","security":"WPA2"}'
check_invalid "password of 64 non hex characters" "{\"ssid\":\"Haus\",\"password\":\"$long64\",\"hidden\":false,\"profile_uuid\":\"\",\"security\":\"WPA2\"}"
check_invalid "password with umlauts" '{"ssid":"Haus","password":"pässwört","hidden":false,"profile_uuid":"","security":"WPA2"}'
check_invalid "profile_uuid that is no uuid" '{"ssid":"Haus","password":"geheim123","hidden":false,"profile_uuid":"not-a-uuid","security":"WPA2"}'
check_invalid "oversize request" "{\"ssid\":\"Haus\",\"password\":\"geheim123\",\"hidden\":false,\"profile_uuid\":\"\",\"security\":\"$long1100\"}"

# Case 3: valid request is moved (0600), progress file starts with all steps waiting.
reset
build_helper "$ME"
put_stage "$VALID"
run_helper wifi-connect
[ "$rc" -eq 0 ] || fail "valid: exit $rc, want 0" "$(cat "$tmp/err")"
[ ! -e "$stage" ] || fail "valid: staging file still there"
[ "$(mode_of "$run/wifi-connect.json")" = 600 ] || fail "valid: request mode is $(mode_of "$run/wifi-connect.json"), want 600"
[ "$(cat "$run/wifi-connect.json")" = "$VALID" ] || fail "valid: moved request differs" "$(cat "$run/wifi-connect.json")"
[ "$(mode_of "$PROGRESS")" = 644 ] || fail "valid: progress mode is $(mode_of "$PROGRESS"), want 644"
[ "$(grep -o '"state":"wait"' "$PROGRESS" | wc -l | tr -d ' ')" -eq 3 ] || fail "valid: progress steps are not all wait" "$(cat "$PROGRESS")"
if grep -Eq '"state":"(run|done|fail)"' "$PROGRESS"; then fail "valid: a step is not wait" "$(cat "$PROGRESS")"; fi
grep -q '"ssid":"Büro:2"' "$PROGRESS" || fail "valid: progress file has no SSID" "$(cat "$PROGRESS")"
if grep -q 'geheim123' "$PROGRESS"; then fail "valid: password in progress file"; fi
grep -qx "$SWITCH_CALL" "$CALL_LOG" || fail "valid: systemd-run call differs" "$(cat "$CALL_LOG")"
if grep -q 'geheim123' "$CALL_LOG"; then fail "valid: password appears in a stub call"; fi

# Case 4: a second wifi-connect while the switch unit is active -> 75, nothing started.
: > "$UNIT_ACTIVE"
put_stage "$VALID"
run_helper wifi-connect
[ "$rc" -eq 75 ] || fail "busy: exit $rc, want 75" "$(cat "$tmp/err")"
[ "$(grep -c '^systemd-run' "$CALL_LOG")" -eq 1 ] || fail "busy: systemd-run was called again" "$(cat "$CALL_LOG")"

# Case 5: wifi-forget only deletes UUIDs of WLAN profiles.
reset
build_helper "$ME"
printf '802-11-wireless\n' > "$TYPES_DIR/$U_WIFI"
printf '802-3-ethernet\n' > "$TYPES_DIR/$U_ETH"

run_helper wifi-forget "not-a-uuid"
[ "$rc" -eq 65 ] || fail "forget non-uuid: exit $rc, want 65" "$(cat "$tmp/err")"
run_helper wifi-forget "$U_ETH"
[ "$rc" -eq 65 ] || fail "forget non-wireless profile: exit $rc, want 65" "$(cat "$tmp/err")"
if grep -q 'connection delete' "$CALL_LOG"; then fail "forget rejected uuid: delete was called" "$(cat "$CALL_LOG")"; fi

run_helper wifi-forget "$U_WIFI"
[ "$rc" -eq 0 ] || fail "forget wireless profile: exit $rc, want 0" "$(cat "$tmp/err")"
grep -qx "nmcli connection delete uuid $U_WIFI" "$CALL_LOG" || fail "forget: delete call differs" "$(cat "$CALL_LOG")"

# Case 6: wifi-switch-run connects a new network. Keyfile 0600 with the SSID as
# byte list and the backslash of the password escaped, autoconnect yes at the
# end, and the final state is written although nobody reads it.
PW='geh\eim12'
reset
build_helper "$ME"
start_switch '{"ssid":"Büro:2","password":"geh\\eim12","hidden":false,"profile_uuid":"","security":"WPA2"}'
run_helper wifi-switch-run
[ "$rc" -eq 0 ] || fail "success: exit $rc, want 0" "$(cat "$tmp/err")"
[ "$(find "$nm" -mindepth 1 | wc -l | tr -d ' ')" -eq 1 ] || fail "success: want exactly one keyfile" "$(ls "$nm")"
kf="$(ls "$nm"/energy-node-*.nmconnection)"
[ "$(mode_of "$kf")" = 600 ] || fail "success: keyfile mode is $(mode_of "$kf"), want 600"
grep -qxF 'ssid=66;195;188;114;111;58;50;' "$kf" || fail "success: SSID byte list differs" "$(cat "$kf")"
grep -qxF 'psk=geh\\eim12' "$kf" || fail "success: psk is not backslash escaped" "$(cat "$kf")"
grep -qxF 'key-mgmt=wpa-psk' "$kf" || fail "success: key-mgmt differs" "$(cat "$kf")"
grep -qxF 'autoconnect=false' "$kf" || fail "success: keyfile is not autoconnect false while switching" "$(cat "$kf")"
grep -qxF '[ipv4]' "$kf" || fail "success: no [ipv4] section" "$(cat "$kf")"
new_uuid="$(sed -n 's/^uuid=//p' "$kf")"
grep -qxF "nmcli connection load $kf" "$CALL_LOG" || fail "success: keyfile was not loaded" "$(cat "$CALL_LOG")"
grep -q " connection up uuid $new_uuid\$" "$CALL_LOG" || fail "success: new profile was not brought up" "$(cat "$CALL_LOG")"
grep -qxF "nmcli connection modify uuid $new_uuid connection.autoconnect yes" "$CALL_LOG" || fail "success: autoconnect was not set" "$(cat "$CALL_LOG")"
grep -qF '"result":"ok"' "$PROGRESS" || fail "success: result is not ok" "$(cat "$PROGRESS")"
grep -qF '"internet":"ok"' "$PROGRESS" || fail "success: internet is not ok" "$(cat "$PROGRESS")"
grep -qF '"address":"192.168.178.52"' "$PROGRESS" || fail "success: address missing" "$(cat "$PROGRESS")"
grep -qF '"rolled_back":false' "$PROGRESS" || fail "success: rolled_back is not false" "$(cat "$PROGRESS")"
grep -q '"finished_at":"20' "$PROGRESS" || fail "success: finished_at not written" "$(cat "$PROGRESS")"
[ "$(grep -o '"state":"done"' "$PROGRESS" | wc -l | tr -d ' ')" -eq 3 ] || fail "success: not all steps done" "$(cat "$PROGRESS")"
[ ! -e "$run/wifi-connect.json" ] || fail "success: request was not consumed"
if grep -qF -- "$PW" "$CALL_LOG"; then fail "success: password appears in a stub call"; fi

# Case 7: auth error on a new network. The new profile is deleted, the previous
# WLAN connection comes back, rolled_back is true.
reset
build_helper "$ME"
touch "$FX/fail-new"
printf '%s\n' "Error: Connection activation failed: Secrets were required, but not provided." > "$FX/fail-msg"
start_switch "$VALID"
run_helper wifi-switch-run
[ "$rc" -eq 0 ] || fail "auth: exit $rc, want 0" "$(cat "$tmp/err")"
grep -qF '"result":"fail"' "$PROGRESS" || fail "auth: result is not fail" "$(cat "$PROGRESS")"
grep -qF '"error":"auth"' "$PROGRESS" || fail "auth: error is not auth" "$(cat "$PROGRESS")"
grep -qF '"rolled_back":true' "$PROGRESS" || fail "auth: rolled_back is not true" "$(cat "$PROGRESS")"
grep -qF '"state":"fail"' "$PROGRESS" || fail "auth: no failed step" "$(cat "$PROGRESS")"
[ -z "$(ls "$nm")" ] || fail "auth: keyfile of the new profile is still there" "$(ls "$nm")"
grep -q '^nmcli connection delete uuid ' "$CALL_LOG" || fail "auth: new profile was not deleted" "$(cat "$CALL_LOG")"
grep -q ' connection up uuid uuid-prev$' "$CALL_LOG" || fail "auth: previous connection was not brought up" "$(cat "$CALL_LOG")"
[ ! -e "$run/wifi-connect.json" ] || fail "auth: request was not consumed"
if grep -qF 'geheim123' "$CALL_LOG"; then fail "auth: password appears in a stub call"; fi

# Case 8: a saved profile (UUID) that fails is kept, and the previous connection
# comes back.
reset
build_helper "$ME"
printf '%s\n' "$U_SAVED" > "$FX/fail-uuids"
start_switch "{\"ssid\":\"Haus\",\"password\":\"\",\"hidden\":false,\"profile_uuid\":\"$U_SAVED\",\"security\":\"WPA2\"}"
run_helper wifi-switch-run
[ "$rc" -eq 0 ] || fail "saved: exit $rc, want 0" "$(cat "$tmp/err")"
grep -qF '"result":"fail"' "$PROGRESS" || fail "saved: result is not fail" "$(cat "$PROGRESS")"
grep -qF '"error":"failed"' "$PROGRESS" || fail "saved: error is not failed" "$(cat "$PROGRESS")"
grep -qF '"rolled_back":true' "$PROGRESS" || fail "saved: rolled_back is not true" "$(cat "$PROGRESS")"
if grep -q 'connection delete' "$CALL_LOG"; then fail "saved: a saved profile was deleted" "$(cat "$CALL_LOG")"; fi
if grep -q 'connection load' "$CALL_LOG"; then fail "saved: a keyfile was loaded" "$(cat "$CALL_LOG")"; fi
[ -z "$(ls "$nm")" ] || fail "saved: a keyfile was written" "$(ls "$nm")"
grep -qxF "nmcli connection down uuid $U_SAVED" "$CALL_LOG" || fail "saved: failed profile was not taken down" "$(cat "$CALL_LOG")"
grep -q ' connection up uuid uuid-prev$' "$CALL_LOG" || fail "saved: previous connection was not brought up" "$(cat "$CALL_LOG")"

# Case 9: the hotspot was active and the new network fails -> the hotspot is up
# again.
reset
build_helper "$ME"
printf 'uuid-hotspot:802-11-wireless:energy-node-hotspot\n' > "$FX/active"
touch "$FX/fail-new"
printf '%s\n' "Error: Connection activation failed: Secrets were required, but not provided." > "$FX/fail-msg"
start_switch "$VALID"
run_helper wifi-switch-run
[ "$rc" -eq 0 ] || fail "hotspot: exit $rc, want 0" "$(cat "$tmp/err")"
grep -qF '"rolled_back":true' "$PROGRESS" || fail "hotspot: rolled_back is not true" "$(cat "$PROGRESS")"
grep -qxF 'nmcli connection up id energy-node-hotspot' "$CALL_LOG" || fail "hotspot: hotspot was not brought up" "$(cat "$CALL_LOG")"
if grep -q ' connection up uuid uuid-prev' "$CALL_LOG"; then fail "hotspot: a previous WLAN connection was brought up" "$(cat "$CALL_LOG")"; fi

# Case 10: wifi-switch-run is not allowed through sudo, only systemd-run starts it.
if grep -q 'wifi-switch-run' "$here/../../dashboard/energy-node-dashboard-system-action.sudoers"; then
  fail "wifi-switch-run is in the sudoers file"
fi

echo "PASS: test_system_action_wifi_switch"
