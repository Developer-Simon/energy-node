#!/usr/bin/env bash
#
# check_no_prerelease.sh: a final release must not carry a -pre.N version in
# any component of COMPONENTS. Runs the script in a throwaway repo with a
# trimmed COMPONENTS list (the real one would need the whole tree).
set -euo pipefail
here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo="$(cd "$here/../.." && pwd)"

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

mkdir -p "$tmp/scripts/version"
cp "$repo/scripts/version/check_no_prerelease.sh" "$tmp/scripts/version/"
cp "$repo/scripts/version/components.sh" "$tmp/scripts/version/"
# Trimmed COMPONENTS, appended after the real file so read_component_version
# stays defined: installer/ is deliberately left out.
cat >> "$tmp/scripts/version/components.sh" <<'LIST'
COMPONENTS=(
  "dashboard/:dashboard/VERSION"
  "services/shelly/:services/shelly/manifest.json"
)
LIST

script="$tmp/scripts/version/check_no_prerelease.sh"
fail() { echo "FAIL: $1"; exit 1; }

# Fresh tree with final versions everywhere in the trimmed list.
reset() {
  rm -rf "$tmp/dashboard" "$tmp/services" "$tmp/installer" "$tmp/VERSION"
  mkdir -p "$tmp/dashboard" "$tmp/services/shelly" "$tmp/installer"
  echo 0.4.5 > "$tmp/dashboard/VERSION"
  printf '{"version": "1.2.3"}\n' > "$tmp/services/shelly/manifest.json"
  echo 0.9.0 > "$tmp/installer/VERSION"
}

# --- all final: exit 0 -----------------------------------------------------
reset
out="$(bash "$script")" || fail "all final: expected exit 0"
[ -z "$out" ] || fail "all final: expected no output, got: $out"

# --- one manifest with a prerelease version: exit 1, names the file -------
reset
printf '{"version": "1.2.4-pre.1"}\n' > "$tmp/services/shelly/manifest.json"
set +e
out="$(bash "$script")"
code=$?
set -e
[ "$code" -eq 1 ] || fail "manifest with pre: expected exit 1, got $code"
grep -q '^services/shelly/manifest.json: ' <<<"$out" \
  || fail "manifest with pre: output does not name the file: $out"
[ "$(wc -l <<<"$out")" -eq 1 ] || fail "manifest with pre: expected one line, got: $out"

# --- VERSION file with a prerelease version: exit 1 ------------------------
reset
echo 0.4.6-pre.2 > "$tmp/dashboard/VERSION"
set +e
out="$(bash "$script")"
code=$?
set -e
[ "$code" -eq 1 ] || fail "VERSION with pre: expected exit 1, got $code"
grep -q '^dashboard/VERSION: ' <<<"$out" \
  || fail "VERSION with pre: output does not name the file: $out"

# --- pre version in a component outside COMPONENTS: not reported ----------
reset
echo 0.9.1-pre.1 > "$tmp/installer/VERSION"
out="$(bash "$script")" || fail "component outside COMPONENTS: expected exit 0"
[ -z "$out" ] || fail "component outside COMPONENTS: reported: $out"

echo "PASS: test_check_no_prerelease.sh"
