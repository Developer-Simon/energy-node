#!/usr/bin/env bash
# Test for dashboard/energy-node-dashboard-system-action read-journal
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
helper_src="$here/../../dashboard/energy-node-dashboard-system-action"

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
fail() { echo "FAIL: $1"; [ -n "${2:-}" ] && printf '%s\n' "$2"; exit 1; }

# Stub journalctl: records its arguments one per line and prints a JSON line.
cat > "$tmp/journalctl" <<'SH'
#!/bin/sh
printf '%s\n' "$@" > "$JOURNAL_ARGS_FILE"
echo '{"MESSAGE":"x"}'
SH
chmod +x "$tmp/journalctl"
export JOURNAL_ARGS_FILE="$tmp/args"

# The helper runs as root with the real journalctl path; the copy under test
# points that path at the stub instead.
cp "$helper_src" "$tmp/helper"
chmod +x "$tmp/helper"
sed -i "s|^JOURNALCTL=\"/usr/bin/journalctl\"|JOURNALCTL=\"$tmp/journalctl\"|" "$tmp/helper"

helper="$tmp/helper"

# run_case LABEL WANT_EXIT STUB_CALLED(yes|no) ARGS...
run_case() {
  label="$1"; want="$2"; stub="$3"; shift 3
  rm -f "$tmp/args"
  rc=0
  "$helper" "$@" </dev/null >"$tmp/out" 2>&1 || rc=$?
  [ "$rc" -eq "$want" ] || fail "$label: exit $rc, want $want" "$(cat "$tmp/out")"
  if [ "$stub" = no ]; then
    [ ! -e "$tmp/args" ] || fail "$label: journalctl stub was called"
  fi
}

# Case 1: dashboard, 60 min, 200 lines, no cursor
run_case "dashboard 60 200" 0 yes read-journal dashboard 60 200
cat > "$tmp/want" <<'EOF'
--no-pager
--quiet
--output=json
--output-fields=MESSAGE,PRIORITY,_SYSTEMD_UNIT
--unit=energy-node-dashboard.service
--since=-60min
--lines=200
EOF
cmp -s "$tmp/args" "$tmp/want" || fail "dashboard 60 200: wrong journalctl arguments" "$(cat "$tmp/args")"

# Case 2: tailscale, 15 min, 100 lines, with cursor
run_case "tailscale 15 100 cursor" 0 yes read-journal tailscale 15 100 's=ab12;i=3f;b=cd;m=1;t=2;x=ef'
cat > "$tmp/want" <<'EOF'
--no-pager
--quiet
--output=json
--output-fields=MESSAGE,PRIORITY,_SYSTEMD_UNIT
--unit=tailscaled.service
--since=-15min
--lines=100
--after-cursor=s=ab12;i=3f;b=cd;m=1;t=2;x=ef
EOF
cmp -s "$tmp/args" "$tmp/want" || fail "tailscale 15 100 cursor: wrong journalctl arguments" "$(cat "$tmp/args")"

# Every allowlisted key maps to its unit
while read -r key unit; do
  run_case "key $key" 0 yes read-journal "$key" 60 200
  grep -qx -- "--unit=$unit" "$tmp/args" || fail "key $key: expected --unit=$unit" "$(cat "$tmp/args")"
done <<'EOF'
dashboard energy-node-dashboard.service
updater energy-node-updater.service
automation automation.service
battery-soc battery-soc.service
shelly shelly-rpc.service
trucki trucki-http.service
tuya tuya.service
apsystems apsystems-ez1.service
mosquitto mosquitto.service
caddy caddy.service
tailscale tailscaled.service
EOF

# Unit, since and lines are checked against fixed lists
run_case "unknown unit sshd" 65 no read-journal sshd 60 200
run_case "since 7 not allowed" 65 no read-journal dashboard 7 200
run_case "lines 5000 not allowed" 65 no read-journal dashboard 60 5000

# Cursor checks
run_case "cursor without s= prefix" 65 no read-journal dashboard 60 200 'i=1'
run_case "cursor with injected option" 65 no read-journal dashboard 60 200 's=1 --file=/x'
long_cursor="s=$(printf 'a%.0s' $(seq 1 255))"
[ "${#long_cursor}" -eq 257 ] || fail "test setup: cursor length is ${#long_cursor}"
run_case "cursor of 257 characters" 65 no read-journal dashboard 60 200 "$long_cursor"

# Argument count checks
run_case "missing lines argument" 64 no read-journal dashboard 60
run_case "extra argument" 64 no read-journal dashboard 60 200 s=1 extra
run_case "reboot with extra argument" 64 no reboot extra

echo "PASS: test_bootstrap_dashboard_read_journal"
