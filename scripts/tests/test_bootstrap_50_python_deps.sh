#!/usr/bin/env bash
# Test for scripts/bootstrap/50-python-deps.sh
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
script="$here/../bootstrap/50-python-deps.sh"

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
fail() { echo "FAIL: $1"; [ -n "${2:-}" ] && printf '%s\n' "$2"; exit 1; }

mkdir -p "$tmp/bin"
cat > "$tmp/bin/fakepip" <<'SH'
#!/usr/bin/env bash
printf 'pip %s\n' "$*" >> "$PIP_LOG"
if [[ "$*" == *--break-system-packages* && "${PIP_NO_BSP:-0}" == 1 ]]; then
  echo "no such option: --break-system-packages" >&2
  exit 2
fi
if [[ "${PIP_EXTERNAL:-0}" == 1 ]]; then
  echo "error: externally-managed-environment" >&2
  exit 1
fi
exit "${PIP_RC:-0}"
SH
chmod +x "$tmp/bin/fakepip"
export PIP_LOG="$tmp/pip.log"

# Ein Bundle mit Wheels, Manifest und dem echten verify_bundle.sh - damit
# beweist der Test auch die Verdrahtung der Vorpruefung.
bundle="$tmp/bundle"
mkdir -p "$bundle/wheels" "$bundle/bootstrap"
printf 'x\n' > "$bundle/wheels/paho_mqtt-2.1.0-py3-none-any.whl"
printf 'x\n' > "$bundle/wheels/energy_node_common-3.1.0-py3-none-any.whl"
cp "$here/../bootstrap/verify_bundle.sh" "$bundle/bootstrap/verify_bundle.sh"

write_manifest() {
  python3 - "$bundle" "$1" "$2" > "$bundle/manifest.json" <<'PY'
import json, sys
print(json.dumps({
    "version": "v1.0.0",
    "arch": "armv6",
    "uname_machine": json.loads(sys.argv[2]),
    "python_abi": sys.argv[3],
    "files": {},
}, indent=2))
PY
}
abi="$(python3 -c 'import sys; print("cp%d%d" % sys.version_info[:2])')"
write_manifest "[\"$(uname -m)\"]" "$abi"

export EN_STATE_DIR="$tmp/state" EN_BUNDLE_DIR="$bundle"
export EN_BUNDLE_VERSION=v1.0.0 EN_SUDO="" EN_PIP="$tmp/bin/fakepip"

# --- erster Lauf installiert alle Wheels ohne Index ------------------------
out="$(bash "$script")"
grep -q '^##STEP 50 ok$' <<<"$out" || fail "kein ok-Marker" "$out"
grep -q -- '--no-index' "$PIP_LOG" || fail "ohne --no-index" "$(cat "$PIP_LOG")"
grep -q -- "--find-links $bundle/wheels" "$PIP_LOG" || fail "ohne --find-links" "$(cat "$PIP_LOG")"
grep -q -- '--break-system-packages' "$PIP_LOG" || fail "ohne --break-system-packages" "$(cat "$PIP_LOG")"
grep -q 'paho_mqtt-2.1.0' "$PIP_LOG" || fail "Wheel nicht uebergeben" "$(cat "$PIP_LOG")"
[ "$(grep -c '^pip ' "$PIP_LOG")" = 1 ] || fail "mehr als ein pip-Aufruf" "$(cat "$PIP_LOG")"

# --- zweiter Lauf ueberspringt --------------------------------------------
: > "$PIP_LOG"
out="$(bash "$script")"
grep -q '^##STEP 50 skip bereits erledigt$' <<<"$out" || fail "nicht uebersprungen" "$out"
[ -s "$PIP_LOG" ] && fail "zweiter Lauf hat pip aufgerufen"

# --- Legacy-Image: zweiter Versuch ohne den Schalter -----------------------
rm -rf "$tmp/state"; : > "$PIP_LOG"
out="$(PIP_NO_BSP=1 bash "$script")"
grep -q '^##STEP 50 ok$' <<<"$out" || fail "Legacy-Rueckfall nicht ok" "$out"
[ "$(grep -c '^pip ' "$PIP_LOG")" = 2 ] || fail "kein zweiter pip-Aufruf" "$(cat "$PIP_LOG")"
tail -n 1 "$PIP_LOG" | grep -q -- '--break-system-packages' \
  && fail "zweiter Versuch benutzte den Schalter erneut" "$(cat "$PIP_LOG")"

# --- externally managed ----------------------------------------------------
rm -rf "$tmp/state"
set +e
out="$(PIP_EXTERNAL=1 bash "$script")"
rc=$?
set -e
[ "$rc" -eq 1 ] || fail "Fehlschlag nicht weitergereicht" "$rc"
grep -q '^##STEP 50 fail PIP_EXTERNALLY_MANAGED$' <<<"$out" || fail "falscher Code" "$out"

# --- sonstiger pip-Fehler --------------------------------------------------
rm -rf "$tmp/state"
set +e
out="$(PIP_RC=1 bash "$script")"
set -e
grep -q '^##STEP 50 fail PIP_INSTALL_FAILED$' <<<"$out" || fail "falscher Code" "$out"

# --- fremde Architektur bricht ab, bevor pip laeuft ------------------------
rm -rf "$tmp/state"; : > "$PIP_LOG"
write_manifest '["sparc64"]' "$abi"
set +e
out="$(bash "$script")"
set -e
grep -q '^##STEP 50 fail ARCH_MISMATCH$' <<<"$out" || fail "falscher Code" "$out"
[ -s "$PIP_LOG" ] && fail "pip lief trotz falscher Architektur" "$(cat "$PIP_LOG")"
write_manifest "[\"$(uname -m)\"]" "$abi"

# --- keine Wheels ----------------------------------------------------------
rm -rf "$tmp/state" "$bundle/wheels"
set +e
out="$(bash "$script")"
set -e
grep -q '^##STEP 50 fail WHEELS_MISSING$' <<<"$out" || fail "falscher Code" "$out"

echo "OK: $(basename "$0")"
