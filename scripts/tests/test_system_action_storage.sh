#!/usr/bin/env bash
# Test for dashboard/energy-node-dashboard-system-action storage <verb> [arg]
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
helper_src="$here/../../dashboard/energy-node-dashboard-system-action"

tmp="$(mktemp -d)"
trap 'rm -rf "${tmp:?}"' EXIT
fail() { echo "FAIL: $1"; [ -n "${2:-}" ] && printf '%s\n' "$2"; exit 1; }

# Stub systemd-run: logs its arguments and exits 0.
cat > "$tmp/systemd-run" <<'SH'
#!/bin/sh
printf '%s\n' "$*" >> "$SYSTEMD_RUN_LOG"
SH
chmod +x "$tmp/systemd-run"
export SYSTEMD_RUN_LOG="$tmp/calls"

cp "$helper_src" "$tmp/helper"
chmod +x "$tmp/helper"
sed -i "s|^SYSTEMD_RUN=\"/usr/bin/systemd-run\"|SYSTEMD_RUN=\"$tmp/systemd-run\"|" "$tmp/helper"
grep -q "^SYSTEMD_RUN=\"$tmp/systemd-run\"" "$tmp/helper" || fail "Testaufbau: SYSTEMD_RUN nicht ersetzt"

UUID="3f9c2e1a-8b7d-4c6e-9f10-2a3b4c5d6e7f"
HELPER_UNIT="--wait --collect --pipe /usr/local/sbin/energy-node-storage-helper"

expect_refused() {
  want="$1"; shift
  rm -f "$SYSTEMD_RUN_LOG"
  set +e
  out="$(sh "$tmp/helper" "$@" 2>&1)"
  rc=$?
  set -e
  [ "$rc" -eq "$want" ] || fail "storage $* nicht mit $want abgelehnt (rc=$rc)" "$out"
  [ ! -e "$SYSTEMD_RUN_LOG" ] || fail "storage $* hat systemd-run aufgerufen" "$(cat "$SYSTEMD_RUN_LOG")"
}

# --- gueltige Aufrufe laufen ueber systemd-run ----------------------------
rm -f "$SYSTEMD_RUN_LOG"
sh "$tmp/helper" storage format sda || fail "storage format sda schlug fehl"
grep -qxF -e "$HELPER_UNIT format sda" "$SYSTEMD_RUN_LOG" || fail "format nicht ueber systemd-run" "$(cat "$SYSTEMD_RUN_LOG")"

rm -f "$SYSTEMD_RUN_LOG"
sh "$tmp/helper" storage mount "$UUID" || fail "storage mount schlug fehl"
grep -qxF -e "$HELPER_UNIT mount $UUID" "$SYSTEMD_RUN_LOG" || fail "mount nicht ueber systemd-run" "$(cat "$SYSTEMD_RUN_LOG")"

rm -f "$SYSTEMD_RUN_LOG"
sh "$tmp/helper" storage unmount || fail "storage unmount schlug fehl"
grep -qxF -e "$HELPER_UNIT unmount" "$SYSTEMD_RUN_LOG" || fail "unmount nicht ueber systemd-run" "$(cat "$SYSTEMD_RUN_LOG")"

# --- ungueltige Aufrufe werden abgelehnt, bevor systemd-run laeuft --------
expect_refused 64 storage
expect_refused 64 storage format sda extra
expect_refused 65 storage format
expect_refused 65 storage format ../sda
expect_refused 65 storage format 'sda;reboot'
expect_refused 65 storage mount not-a-uuid
expect_refused 65 storage unmount sda
expect_refused 65 storage reboot

echo "OK: $(basename "$0")"
