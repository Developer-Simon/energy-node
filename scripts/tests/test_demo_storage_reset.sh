#!/usr/bin/env bash
# Test for env_prepare_storage in demo/lib/env.sh: a demo start without --reset
# rewrites config.json (no paths.runtime_dir) and resets the fake stick. A drive
# set up in an earlier run must not survive in data/, or the dashboard shows
# "Checking" forever (drive chosen in storage.json, but no watcher without runtime_dir).
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
fail() { echo "FAIL: $1"; exit 1; }

# shellcheck source=../../demo/lib/env.sh
source "$here/../../demo/lib/env.sh"

WORK="$tmp"
mkdir -p "$WORK/data" "$WORK/storage/mount/runtime"
# State of an earlier run that set up a drive and was stopped.
echo '{"uuid":"9c4e7a12-3b58-4d06-a1f9-6e2d8b0c5a37"}' > "$WORK/data/storage.json"
echo '{"kind":"setup"}' > "$WORK/data/storage-pending.json"
ln -s "$WORK/storage/mount/runtime" "$WORK/data/runtime"
echo '{"plugged":["sdb"]}' > "$WORK/storage/plugged.json"
# Unrelated data must stay.
echo '{}' > "$WORK/data/settings.json"
mkdir -p "$WORK/data/energystats"
# A runtime symlink that does not point to the fake drive is not ours.
ln -s "$tmp" "$WORK/data/other-link"

env_prepare_storage

[ ! -e "$WORK/data/storage.json" ] || fail "storage.json survived the start"
[ ! -e "$WORK/data/storage-pending.json" ] || fail "pending marker survived the start"
[ ! -L "$WORK/data/runtime" ] || fail "runtime symlink to the fake drive survived the start"
[ ! -e "$WORK/storage/plugged.json" ] || fail "plugged.json survived the start"
[ -f "$WORK/data/settings.json" ] || fail "settings.json was removed"
[ -d "$WORK/data/energystats" ] || fail "energystats was removed"
[ -L "$WORK/data/other-link" ] || fail "a foreign symlink was removed"
[ -f "$WORK/storage/drives.json" ] || fail "drives.json missing after env_prepare_storage"

# A real runtime directory (no setup yet) stays.
rm -rf "$WORK/data/runtime"
mkdir -p "$WORK/data/runtime"
env_prepare_storage
[ -d "$WORK/data/runtime" ] || fail "a plain runtime directory was removed"

echo "OK: test_demo_storage_reset.sh"
