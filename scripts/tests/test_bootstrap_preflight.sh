#!/usr/bin/env bash
# Test for scripts/bootstrap/preflight.sh
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
script="$here/../bootstrap/preflight.sh"

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
fail() { echo "FAIL: $1"; [ -n "${2:-}" ] && printf '%s\n' "$2"; exit 1; }

# --- ein Node ohne Installation -------------------------------------------
mkdir -p "$tmp/root/etc"
cat > "$tmp/root/etc/os-release" <<'OSR'
PRETTY_NAME="Debian GNU/Linux 12 (bookworm)"
NAME="Debian GNU/Linux"
VERSION_ID="12"
ID=debian
OSR

export EN_ROOT="$tmp/root" EN_STATE_DIR="$tmp/state"
out="$("$script")" || fail "preflight.sh exited non-zero" "$out"

python3 - "$out" <<'PY' || fail "the fresh-node report is wrong" "$out"
import json, sys
data = json.loads(sys.argv[1])
assert data["os_id"] == "debian", data
assert data["os_version_id"] == "12", data
assert data["arch"], data
assert data["python_abi"].startswith("cp"), data
assert isinstance(data["disk_free_mb"], int) and data["disk_free_mb"] > 0, data
assert data["installed"] is False, data
assert data["installed_bundle_version"] == "", data
assert isinstance(data["sudo_nopasswd"], bool), data
assert isinstance(data["internet"], bool), data
PY

# --- ein Node mit vorhandener Installation ---------------------------------
mkdir -p "$tmp/state/steps"
printf 'bundle=v1.4.2\n' > "$tmp/state/steps/10"
cat > "$tmp/state/installed-manifest.json" <<'JSON'
{ "version": "v1.4.2", "components": { "dashboard": "v2.0.0" } }
JSON

out="$("$script")" || fail "preflight.sh exited non-zero on an installed node" "$out"
python3 - "$out" <<'PY' || fail "the installed-node report is wrong" "$out"
import json, sys
data = json.loads(sys.argv[1])
assert data["installed"] is True, data
assert data["installed_bundle_version"] == "v1.4.2", data
PY

# --- die Ausgabe ist genau eine Zeile JSON ---------------------------------
lines="$(printf '%s\n' "$out" | wc -l)"
[ "$lines" -eq 1 ] || fail "preflight.sh printed $lines lines, want exactly one"

# --- es hinterlaesst nichts -------------------------------------------------
[ -d "$tmp/state/steps" ] || fail "the state directory vanished"
[ -z "$(find "$tmp/state/steps" -newer "$tmp/state/installed-manifest.json" -type f)" ] \
  || fail "preflight.sh wrote a stamp - it is not a step"

# --- kein Marker in der Ausgabe --------------------------------------------
printf '%s\n' "$out" | grep -q '^##STEP' && fail "preflight.sh emitted a step marker"

echo "OK: test_bootstrap_preflight.sh"
