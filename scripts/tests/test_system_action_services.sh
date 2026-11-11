#!/usr/bin/env bash
# Test for dashboard/energy-node-dashboard-system-action stop-services and start-services
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
helper_src="$here/../../dashboard/energy-node-dashboard-system-action"

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
fail() { echo "FAIL: $1"; [ -n "${2:-}" ] && printf '%s\n' "$2"; exit 1; }

# Stub systemctl. "is-enabled --quiet UNIT" exits 0 only for units listed in
# ENABLED_UNITS. "VERB UNIT" is logged to SYSTEMCTL_LOG and exits 1 for units
# listed in FAIL_UNITS.
cat > "$tmp/systemctl" <<'SH'
#!/bin/sh
if [ "$1" = is-enabled ]; then
  for unit in $ENABLED_UNITS; do
    [ "$unit" = "$3" ] && exit 0
  done
  exit 1
fi
printf '%s %s\n' "$1" "$2" >> "$SYSTEMCTL_LOG"
for unit in $FAIL_UNITS; do
  [ "$unit" = "$2" ] && exit 1
done
exit 0
SH
chmod +x "$tmp/systemctl"
export SYSTEMCTL_LOG="$tmp/calls"

# The helper runs as root with the real systemctl path; the copy under test
# points that path at the stub instead.
cp "$helper_src" "$tmp/helper"
chmod +x "$tmp/helper"
sed -i "s|^SYSTEMCTL=\"/bin/systemctl\"|SYSTEMCTL=\"$tmp/systemctl\"|" "$tmp/helper"
grep -q "^SYSTEMCTL=\"$tmp/systemctl\"" "$tmp/helper" || fail "test setup: SYSTEMCTL was not replaced"

ALL_UNITS="automation.service battery-soc.service shelly-rpc.service trucki-http.service tuya.service apsystems-ez1.service"

# run_case LABEL WANT_EXIT ENABLED_UNITS FAIL_UNITS ARGS...
run_case() {
  label="$1"; want="$2"; export ENABLED_UNITS="$3" FAIL_UNITS="$4"; shift 4
  rm -f "$SYSTEMCTL_LOG"
  rc=0
  "$tmp/helper" "$@" </dev/null >"$tmp/out" 2>&1 || rc=$?
  [ "$rc" -eq "$want" ] || fail "$label: exit $rc, want $want" "$(cat "$tmp/out")"
}

# want_calls LABEL CALLS...: the logged systemctl calls, one per argument
want_calls() {
  label="$1"; shift
  printf '%s\n' "$@" > "$tmp/want"
  if [ "$#" -eq 0 ]; then
    rm -f "$tmp/want"; : > "$tmp/want"
  fi
  if [ -e "$SYSTEMCTL_LOG" ]; then
    cmp -s "$SYSTEMCTL_LOG" "$tmp/want" || fail "$label: wrong systemctl calls" "$(cat "$SYSTEMCTL_LOG")"
  else
    [ ! -s "$tmp/want" ] || fail "$label: systemctl was not called"
  fi
}

# stop-services stops only the enabled units
run_case "stop enabled only" 0 "automation.service tuya.service" "" stop-services
want_calls "stop enabled only" "stop automation.service" "stop tuya.service"

# A failing unit does not stop the loop, and the verb exits non-zero
run_case "stop with one failing unit" 1 "$ALL_UNITS" "battery-soc.service" stop-services
want_calls "stop with one failing unit" \
  "stop automation.service" "stop battery-soc.service" "stop shelly-rpc.service" \
  "stop trucki-http.service" "stop tuya.service" "stop apsystems-ez1.service"

# No enabled units: nothing is called, exit 0
run_case "stop with nothing enabled" 0 "" "" stop-services
want_calls "stop with nothing enabled"

# start-services mirrors stop-services
run_case "start enabled only" 0 "shelly-rpc.service trucki-http.service" "" start-services
want_calls "start enabled only" "start shelly-rpc.service" "start trucki-http.service"

run_case "start with one failing unit" 1 "$ALL_UNITS" "tuya.service" start-services
want_calls "start with one failing unit" \
  "start automation.service" "start battery-soc.service" "start shelly-rpc.service" \
  "start trucki-http.service" "start tuya.service" "start apsystems-ez1.service"

# Extra arguments and unknown verbs are rejected before systemctl is called
run_case "stop with extra argument" 64 "$ALL_UNITS" "" stop-services extra
want_calls "stop with extra argument"
run_case "start with extra argument" 64 "$ALL_UNITS" "" start-services extra
want_calls "start with extra argument"
run_case "unknown verb" 64 "$ALL_UNITS" "" stop-everything
want_calls "unknown verb"

echo "PASS: test_system_action_services"
