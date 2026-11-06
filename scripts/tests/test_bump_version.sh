#!/usr/bin/env bash
# Test for scripts/version/bump-patch.sh
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
script="$here/../version/bump-patch.sh"

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

fail() { echo "FAIL: $1"; [ -n "${2:-}" ] && printf '%s\n' "$2"; exit 1; }

# A throwaway repo carrying the real component layout from
# scripts/version/components.sh. bump-patch.sh sources components.sh relative to
# its own location, so the real COMPONENTS list applies and the paths below must
# match it.
setup_repo() {
  local dir="$1"
  mkdir -p "$dir"
  git -C "$dir" init -q
  git -C "$dir" config user.email t@t
  git -C "$dir" config user.name t
  mkdir -p "$dir/libs/energy_node_common" "$dir/services" "$dir/dashboard" \
    "$dir/scripts/bootstrap" \
    "$dir/integrations/homeassistant/custom_components/battery_soc"
  # Jeder Dienst unter services/ ist eine eigene Komponente und traegt seine
  # Version im "version"-Feld seiner manifest.json.
  local svc
  for svc in apsystems_ez1 automation battery_soc shelly trucki tuya_mqtt; do
    mkdir -p "$dir/services/$svc"
    printf '{\n  "service_id": "%s",\n  "version": "0.4.0",\n  "kind": "device"\n}\n' \
      "$svc" > "$dir/services/$svc/manifest.json"
  done
  echo v3.0.0 > "$dir/libs/energy_node_common/VERSION"
  echo v0.5.0 > "$dir/dashboard/VERSION"
  mkdir -p "$dir/dashboard/internal/appconfig"
  echo '{}' > "$dir/dashboard/internal/appconfig/energy-node.config.json"
  echo v0.1.0 > "$dir/scripts/bootstrap/VERSION"
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
echo x > "$r/dashboard/app.js"
commit "$r" "feat: touch dashboard"
bump "$r" main
[ "$(cat "$r/dashboard/VERSION")" = v0.5.1 ] || fail "dashboard/VERSION not bumped" "$(cat "$r/dashboard/VERSION")"
[ "$(cat "$r/libs/energy_node_common/VERSION")" = v3.0.0 ] || fail "untouched libs VERSION changed"
git -C "$r" diff --cached --quiet && fail "bumped file was not staged"

# --- a single service bumps only its own manifest ---------------------------
svc_version() {
  python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["version"])' \
    "$1/services/$2/manifest.json"
}
r="$tmp/one-service"
setup_repo "$r"
echo x > "$r/services/shelly/shelly_rpc.py"
commit "$r" "feat(shelly): touch one service"
bump "$r" main
[ "$(svc_version "$r" shelly)" = 0.4.1 ] \
  || fail "the touched service's manifest version was not bumped" "$(svc_version "$r" shelly)"
[ "$(svc_version "$r" trucki)" = 0.4.0 ] \
  || fail "an untouched service was bumped" "$(svc_version "$r" trucki)"
[ ! -e "$r/services/VERSION" ] || fail "a services/VERSION umbrella exists again"

# --- config template bumps the dashboard, not the services ------------------
r="$tmp/umbrella"
setup_repo "$r"
echo '{"x":1}' > "$r/dashboard/internal/appconfig/energy-node.config.json"
commit "$r" "feat: change the config template"
bump "$r" main
[ "$(cat "$r/dashboard/VERSION")" = v0.5.1 ] \
  || fail "config template change did not bump the dashboard" "$(cat "$r/dashboard/VERSION")"
[ "$(svc_version "$r" shelly)" = 0.4.0 ] \
  || fail "config template change bumped an individual service" "$(svc_version "$r" shelly)"

# --- a service's CHANGELOG.md alone bumps nothing ---------------------------
r="$tmp/service-changelog"
setup_repo "$r"
printf '# Changelog\n' > "$r/services/trucki/CHANGELOG.md"
commit "$r" "docs(changelog): update changelogs"
bump "$r" main
[ "$(svc_version "$r" trucki)" = 0.4.0 ] \
  || fail "a changelog-only change bumped the service" "$(svc_version "$r" trucki)"

# --- manifest.json gets a bare semver, stays valid JSON --------------------
r="$tmp/manifest"
setup_repo "$r"
echo x > "$r/integrations/homeassistant/custom_components/battery_soc/thing.py"
commit "$r" "feat: touch HA integration"
bump "$r" main
got="$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["version"])' \
  "$r/integrations/homeassistant/custom_components/battery_soc/manifest.json")"
[ "$got" = 9.9.10 ] || fail "manifest version not bare-bumped" "$got"

# --- HA tests, README and mirror templates do not bump the integration -----
r="$tmp/ha-outside-component"
setup_repo "$r"
mkdir -p "$r/integrations/homeassistant/tests" "$r/integrations/homeassistant/mirror/battery_soc"
echo x > "$r/integrations/homeassistant/tests/test_thing.py"
echo x > "$r/integrations/homeassistant/README.md"
echo x > "$r/integrations/homeassistant/mirror/battery_soc/README.md"
commit "$r" "docs: touch HA tests, README and mirror"
bump "$r" main
got="$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["version"])' \
  "$r/integrations/homeassistant/custom_components/battery_soc/manifest.json")"
[ "$got" = 9.9.9 ] || fail "a change outside custom_components/battery_soc bumped it" "$got"

# --- idempotent: a second run after committing the bump does nothing ------
r="$tmp/idem"
setup_repo "$r"
echo x > "$r/dashboard/app.js"
commit "$r" "feat: touch dashboard"
bump "$r" main
commit "$r" "chore: bump"
out="$(bump "$r" main)"
[ -z "$out" ] || fail "second run bumped again" "$out"
[ "$(cat "$r/dashboard/VERSION")" = v0.5.1 ] || fail "second run moved dashboard/VERSION" "$(cat "$r/dashboard/VERSION")"

# --- nested component isolation: libs/energy_node_common/ only ------------
r="$tmp/nested"
setup_repo "$r"
echo x > "$r/libs/energy_node_common/mod.py"
commit "$r" "feat: touch shared module"
bump "$r" main
[ "$(cat "$r/libs/energy_node_common/VERSION")" = v3.0.1 ] \
  || fail "nested VERSION not bumped" "$(cat "$r/libs/energy_node_common/VERSION")"
[ "$(cat "$r/dashboard/VERSION")" = v0.5.0 ] \
  || fail "dashboard/VERSION bumped by a nested-only change" "$(cat "$r/dashboard/VERSION")"

# --- version file absent at base: created on the branch, never bumped ----
# Reproduces the rename-induced loop: a component's version file does not
# exist on the base ref yet but does on the branch (e.g. a path introduced by
# git mv). bump-patch.sh must leave it alone -- treating an unreadable base
# version as "not bumped" made the workflow re-bump it on every run.
r="$tmp/newfile"
mkdir -p "$r/dashboard"
git -C "$r" init -q
git -C "$r" config user.email t@t
git -C "$r" config user.name t
echo base > "$r/dashboard/keep.py"
git -C "$r" add -A
git -C "$r" commit -qm base
git -C "$r" branch -M main
git -C "$r" checkout -qb feature
echo v0.5.0 > "$r/dashboard/VERSION"
echo changed > "$r/dashboard/keep.py"
commit "$r" "refactor: introduce dashboard/VERSION"
out="$(bump "$r" main)"
[ -z "$out" ] || fail "bumped a version file that is new on the branch" "$out"
[ "$(cat "$r/dashboard/VERSION")" = v0.5.0 ] \
  || fail "new-on-branch dashboard/VERSION changed" "$(cat "$r/dashboard/VERSION")"
git -C "$r" diff --cached --quiet || fail "staged a bump for a new-on-branch version file"
out="$(bump "$r" main)"
[ -z "$out" ] || fail "second run bumped the new-on-branch version file" "$out"

# --- a CHANGELOG.md-only change does not bump the component --------------
r="$tmp/changelog"
setup_repo "$r"
printf '# Changelog\n\n## v0.5.0\n' > "$r/dashboard/CHANGELOG.md"
commit "$r" "docs(changelog): update changelogs"
out="$(bump "$r" main)"
[ -z "$out" ] || fail "a changelog-only change bumped" "$out"
[ "$(cat "$r/dashboard/VERSION")" = v0.5.0 ] \
  || fail "dashboard/VERSION bumped by a changelog-only change" "$(cat "$r/dashboard/VERSION")"
# ...but a real change alongside the changelog still bumps
echo y > "$r/dashboard/app.js"
commit "$r" "feat: real dashboard change"
bump "$r" main
[ "$(cat "$r/dashboard/VERSION")" = v0.5.1 ] \
  || fail "dashboard/VERSION not bumped by a real change next to the changelog" "$(cat "$r/dashboard/VERSION")"

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

# --- bootstrap ist eine eigene Komponente ---------------------------------
r="$tmp/bootstrap"
setup_repo "$r"
echo x > "$r/scripts/bootstrap/10-apt.sh"
commit "$r" "feat: touch bootstrap"
bump "$r" main
[ "$(cat "$r/scripts/bootstrap/VERSION")" = v0.1.1 ] \
  || fail "scripts/bootstrap/VERSION not bumped" "$(cat "$r/scripts/bootstrap/VERSION")"
[ "$(cat "$r/dashboard/VERSION")" = v0.5.0 ] \
  || fail "untouched dashboard/VERSION changed" "$(cat "$r/dashboard/VERSION")"

# --- scripts/ ausserhalb von bootstrap/ bumpt nichts ----------------------
# scripts/ selbst ist keine Komponente; nur das Unterverzeichnis bootstrap/
# ist eines. Ein Commit an scripts/dev/ darf deshalb nichts anfassen.
r="$tmp/scripts-only"
setup_repo "$r"
mkdir -p "$r/scripts/dev"
echo x > "$r/scripts/dev/foo.sh"
commit "$r" "chore: touch a non-component script"
out="$(bump "$r" main)"
[ -z "$out" ] || fail "a scripts/dev change bumped something" "$out"
[ "$(cat "$r/scripts/bootstrap/VERSION")" = v0.1.0 ] \
  || fail "scripts/bootstrap/VERSION bumped by a scripts/dev change" \
     "$(cat "$r/scripts/bootstrap/VERSION")"

echo "PASS: bump-patch.sh"
