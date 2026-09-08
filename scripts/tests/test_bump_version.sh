#!/usr/bin/env bash
# Test for scripts/version/bump-patch.sh
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
script="$here/../version/bump-patch.sh"

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

fail() { echo "FAIL: $1"; [ -n "${2:-}" ] && printf '%s\n' "$2"; exit 1; }

# A throwaway repo carrying the real component layout from git-hooks/lib.sh.
# bump-patch.sh sources lib.sh relative to its own location, so the real
# COMPONENTS list applies and the paths below must match it.
setup_repo() {
  local dir="$1"
  mkdir -p "$dir"
  git -C "$dir" init -q
  git -C "$dir" config user.email t@t
  git -C "$dir" config user.name t
  mkdir -p "$dir/libs/energy_node_common" "$dir/services" "$dir/dashboard" \
    "$dir/integrations/homeassistant/custom_components/battery_soc"
  echo v1.2.3 > "$dir/services/VERSION"
  echo v3.0.0 > "$dir/libs/energy_node_common/VERSION"
  echo v0.5.0 > "$dir/dashboard/VERSION"
  printf '{\n  "domain": "battery_soc",\n  "version": "9.9.9"\n}\n' \
    > "$dir/integrations/homeassistant/custom_components/battery_soc/manifest.json"
  git -C "$dir" add -A
  git -C "$dir" commit -qm base
  git -C "$dir" branch -M main
  git -C "$dir" checkout -qb feature
}

commit() { git -C "$1" add -A && git -C "$1" commit -qm "$2"; }
bump()   { ( cd "$1" && shift && "$script" "$@" ); }

# --- bumps a touched component, leaves the rest alone ------------------------
r="$tmp/basic"
setup_repo "$r"
mkdir -p "$r/services/battery_soc"
echo x > "$r/services/battery_soc/foo.py"
commit "$r" "feat: touch services"
bump "$r" main
[ "$(cat "$r/services/VERSION")" = v1.2.4 ] || fail "services/VERSION not bumped" "$(cat "$r/services/VERSION")"
[ "$(cat "$r/dashboard/VERSION")" = v0.5.0 ] || fail "untouched dashboard/VERSION changed"
git -C "$r" diff --cached --quiet && fail "bumped file was not staged"

# --- manifest.json gets a bare semver, stays valid JSON --------------------
r="$tmp/manifest"
setup_repo "$r"
echo x > "$r/integrations/homeassistant/thing.py"
commit "$r" "feat: touch HA integration"
bump "$r" main
got="$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["version"])' \
  "$r/integrations/homeassistant/custom_components/battery_soc/manifest.json")"
[ "$got" = 9.9.10 ] || fail "manifest version not bare-bumped" "$got"

# --- idempotent: a second run after committing the bump does nothing ------
r="$tmp/idem"
setup_repo "$r"
echo x > "$r/services/mod.py"
commit "$r" "feat: touch services"
bump "$r" main
commit "$r" "chore: bump"
out="$(bump "$r" main)"
[ -z "$out" ] || fail "second run bumped again" "$out"
[ "$(cat "$r/services/VERSION")" = v1.2.4 ] || fail "second run moved services/VERSION" "$(cat "$r/services/VERSION")"

# --- nested component isolation: libs/energy_node_common/ only ------------
r="$tmp/nested"
setup_repo "$r"
echo x > "$r/libs/energy_node_common/mod.py"
commit "$r" "feat: touch shared module"
bump "$r" main
[ "$(cat "$r/libs/energy_node_common/VERSION")" = v3.0.1 ] \
  || fail "nested VERSION not bumped" "$(cat "$r/libs/energy_node_common/VERSION")"
[ "$(cat "$r/services/VERSION")" = v1.2.3 ] \
  || fail "services/VERSION bumped by a nested-only change" "$(cat "$r/services/VERSION")"

# --- version file absent at base: created on the branch, never bumped ----
# Reproduces the rename-induced loop: a component's version file does not
# exist on the base ref yet but does on the branch (e.g. a path introduced by
# git mv). bump-patch.sh must leave it alone -- treating an unreadable base
# version as "not bumped" made the workflow re-bump it on every run.
r="$tmp/newfile"
mkdir -p "$r/services/mod"
git -C "$r" init -q
git -C "$r" config user.email t@t
git -C "$r" config user.name t
echo base > "$r/services/mod/keep.py"
git -C "$r" add -A
git -C "$r" commit -qm base
git -C "$r" branch -M main
git -C "$r" checkout -qb feature
echo v1.2.3 > "$r/services/VERSION"
echo changed > "$r/services/mod/keep.py"
commit "$r" "refactor: introduce services/VERSION"
out="$(bump "$r" main)"
[ -z "$out" ] || fail "bumped a version file that is new on the branch" "$out"
[ "$(cat "$r/services/VERSION")" = v1.2.3 ] \
  || fail "new-on-branch services/VERSION changed" "$(cat "$r/services/VERSION")"
git -C "$r" diff --cached --quiet || fail "staged a bump for a new-on-branch version file"
out="$(bump "$r" main)"
[ -z "$out" ] || fail "second run bumped the new-on-branch version file" "$out"

# --- --check: exit 1 when a bump is missing, 0 once it is there -----------
r="$tmp/check"
setup_repo "$r"
echo x > "$r/dashboard/app.js"
commit "$r" "feat: touch dashboard"
rc=0; bump "$r" --check main >/dev/null || rc=$?
[ "$rc" -eq 1 ] || fail "--check should exit 1 when bump missing, got $rc"
bump "$r" main
commit "$r" "chore: bump"
rc=0; bump "$r" --check main >/dev/null || rc=$?
[ "$rc" -eq 0 ] || fail "--check should exit 0 once bumped, got $rc"

echo "PASS: bump-patch.sh"
