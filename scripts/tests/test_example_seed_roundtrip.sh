#!/usr/bin/env bash
# Test fuer den Seed-Vertrag zwischen Bootstrap und Dashboard: example_apply.py
# schreibt <data>/example-seed.json, und das Go Paket internal/example muss
# diese Datei beim Start uebernehmen (ApplySeed, dann LoadNote).
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo="$here/../.."
helper="$repo/scripts/bootstrap/lib/example_apply.py"
example="$repo/scripts/bootstrap/lib/example.json"

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
fail() { echo "FAIL: $1"; [ -n "${2:-}" ] && printf '%s\n' "$2"; exit 1; }

devices="$tmp/devices"
data="$tmp/data"
mkdir -p "$devices" "$data"

# Bootstrap-Seite: der Helfer haengt die Beispiele an und schreibt den Seed.
python3 "$helper" apply "$example" "$devices" "$data" || fail "example_apply.py apply scheiterte"
[ -f "$data/example-seed.json" ] || fail "Seed fehlt im Datenordner" "$(ls -la "$data")"

# Dashboard-Seite: das Go Test uebernimmt genau diese Datei. Der Test muss
# laufen und darf nicht uebersprungen werden, sonst prueft er nichts.
out="$(cd "$repo/dashboard" && EXAMPLE_SEED_FILE="$data/example-seed.json" GOTOOLCHAIN=auto \
  go test -count=1 -v -run '^TestApplySeedReadsBootstrapSeed$' ./internal/example/ 2>&1)" \
  || fail "Go Test TestApplySeedReadsBootstrapSeed scheiterte" "$out"
grep -q -- '--- PASS: TestApplySeedReadsBootstrapSeed' <<<"$out" \
  || fail "Go Test lief nicht durch (uebersprungen?)" "$out"

echo "OK: $(basename "$0")"
