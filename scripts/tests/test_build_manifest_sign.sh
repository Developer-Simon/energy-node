#!/usr/bin/env bash
# Test for scripts/build/lib/manifest.sh
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
lib="$here/../build/lib/manifest.sh"
verify="$here/../bootstrap/verify_bundle.sh"

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
fail() { echo "FAIL: $1"; [ -n "${2:-}" ] && printf '%s\n' "$2"; exit 1; }

openssl genpkey -algorithm ed25519 -out "$tmp/key.pem" 2>/dev/null
openssl pkey -in "$tmp/key.pem" -pubout -out "$tmp/pub.pem" 2>/dev/null

bundle="$tmp/bundle"
mkdir -p "$bundle/bootstrap" "$bundle/services/shelly" "$bundle/wheels"
printf 'echo a\n'  > "$bundle/bootstrap/10-apt.sh"
printf 'print(1)\n' > "$bundle/services/shelly/mod.py"
printf 'rad\n'      > "$bundle/wheels/x-1.0-py3-none-any.whl"
cat > "$bundle/manifest.head.json" <<'JSON'
{
  "version": "v0.2.0",
  "arch": "armv6",
  "uname_machine": ["armv6l", "armv7l"],
  "python_minor": "3.11",
  "python_abi": "cp311",
  "components": { "dashboard": "v0.6.1" },
  "steps": [ { "id": "10", "optional": false } ]
}
JSON

run() { bash -c 'source "$1"; shift; "$@"' _ "$lib" "$@"; }

run write_bundle_manifest "$bundle"
[ -f "$bundle/manifest.json" ] || fail "manifest.json fehlt"
[ -e "$bundle/manifest.head.json" ] && fail "Kopfdatei blieb liegen"

get() { python3 -c 'import json,sys; d=json.load(open(sys.argv[1])); print(eval(sys.argv[2], {"d": d}))' \
  "$bundle/manifest.json" "$1"; }

[ "$(get 'd["version"]')" = v0.2.0 ] || fail "Kopf nicht uebernommen"
[ "$(get 'd["components"]["dashboard"]')" = v0.6.1 ] || fail "Komponenten fehlen"
[ "$(get 'len(d["files"])')" = 3 ] || fail "falsche Dateizahl" "$(get 'sorted(d["files"])')"
[ "$(get '"bootstrap/10-apt.sh" in d["files"]')" = True ] || fail "Skript nicht gehasht"
[ "$(get '"services/shelly/mod.py" in d["files"]')" = True ] || fail "Quelle nicht gehasht"
[ "$(get '"manifest.json" in d["files"]')" = False ] || fail "Manifest hasht sich selbst"

want="$(sha256sum "$bundle/bootstrap/10-apt.sh" | cut -d' ' -f1)"
[ "$(get 'd["files"]["bootstrap/10-apt.sh"]')" = "$want" ] || fail "Hash falsch"

# --- Signatur, und die Gegenprobe mit dem echten Pruefskript --------------
run sign_bundle_manifest "$bundle" "$tmp/key.pem"
[ -f "$bundle/manifest.json.sig" ] || fail "Signatur fehlt"
out="$(bash "$verify" --bundle "$bundle" --pubkey "$tmp/pub.pem")" \
  || fail "verify_bundle lehnt das frisch gebaute Bundle ab" "$out"

# --- nachtraeglich veraenderte Datei faellt auf --------------------------
printf 'boese\n' >> "$bundle/wheels/x-1.0-py3-none-any.whl"
set +e
out="$(bash "$verify" --bundle "$bundle" --pubkey "$tmp/pub.pem")"
set -e
[ "${out##*$'\n'}" = "FEHLER BUNDLE_HASH_MISMATCH" ] || fail "Verfaelschung nicht erkannt" "$out"

# --- fehlende Kopfdatei ---------------------------------------------------
set +e
run write_bundle_manifest "$tmp/leer" 2>/dev/null
rc=$?
set -e
[ "$rc" -ne 0 ] || fail "fehlende Kopfdatei nicht bemaengelt"

echo "OK: $(basename "$0")"
