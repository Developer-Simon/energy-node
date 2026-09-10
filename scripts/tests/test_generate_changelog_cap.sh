#!/usr/bin/env bash
#
# Release-cap merge in generate_changelog.sh: everything newer than a
# component's last *released* version (its VERSION as of the newest repo tag)
# belongs in ONE open section, even across a hand-picked minor/major bump.
set -euo pipefail
here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo="$(git -C "$here" rev-parse --show-toplevel)"

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

git -C "$tmp" init -q
git -C "$tmp" config user.email t@t
git -C "$tmp" config user.name t

mkdir -p "$tmp/scripts"
cp "$repo/scripts/generate_changelog.sh" "$tmp/scripts/generate_changelog.sh"
script="$tmp/scripts/generate_changelog.sh"

commit() { git -C "$tmp" add -A && git -C "$tmp" commit -qm "$1"; }
gen() { ( cd "$tmp" && bash "$script" "$@" ); }
fail() { echo "FAIL: $1"; exit 1; }
count() { grep -c "$1" "$2" || true; }

dcl="$tmp/dashboard/CHANGELOG.md"

# ---------------------------------------------------------------------------
# Scenario 1: a hand minor bump rolls every post-release section into one.
#
# The repo tag v0.5.15 marks the last dashboard release. Work then accumulated
# as v0.5.16 (left behind as a stale section with dangling hashes after its PR
# squash-merged). A hand "chore: bump minor" commit sets VERSION to v0.6.0.
# The next run must yield a SINGLE "## v0.6.0" holding the v0.5.16 work and the
# bump commit, with the tagged "## v0.5.15" kept as the cap -- not a near-empty
# "## v0.6.0" stacked on top of a "## v0.5.16" that still carries everything.
# ---------------------------------------------------------------------------
mkdir -p "$tmp/dashboard"
echo v0.5.15 > "$tmp/dashboard/VERSION"
echo a > "$tmp/dashboard/app.js"
commit "feat(dashboard): shared feature (#7)"
tag_hash="$(git -C "$tmp" rev-parse --short HEAD)"
git -C "$tmp" tag v0.5.15 "$tag_hash"

echo b > "$tmp/dashboard/app2.js"
echo v0.5.16 > "$tmp/dashboard/VERSION"
commit "feat(dashboard): accumulated post-tag feature (#9)"

echo c > "$tmp/dashboard/app3.js"
echo v0.6.0 > "$tmp/dashboard/VERSION"
commit "chore: bump minor version for the node-telemetry move"

cat > "$dcl" <<EOF
# Changelog

## v0.5.16 (2026-09-11)

### Features

- **dashboard:** accumulated post-tag feature, stale wording (#9) (deadc0d1)
- ghost feature from a squashed pr (deadc0d2)

## v0.5.15 (2026-09-08)

### Features

- **dashboard:** shared feature (#7) ($tag_hash)

## v0.5.12 (2026-09-06)

### Features

- pre-tag entry that must NOT fold into the open section (deadc0d3)
EOF
commit "docs(changelog): pre-minor-bump state"

gen dashboard
[ "$(count '^## v0.6.0 ' "$dcl")" -eq 1 ]  || fail "S1: expected exactly one open section"
[ "$(count '^## v0.5.16 ' "$dcl")" -eq 0 ] || fail "S1: v0.5.16 kept as its own section after the minor bump"
grep -q "accumulated post-tag feature"       "$dcl" || fail "S1: v0.5.16 work lost in the roll-up"
grep -q "ghost feature from a squashed pr"    "$dcl" || fail "S1: dangling-hash entry dropped in the roll-up"
grep -q "bump minor version for the node-telemetry move" "$dcl" || fail "S1: bump commit's own entry missing"
grep -q "stale wording" "$dcl"                        && fail "S1: (#9) duplicate not deduplicated"
[ "$(count '^## v0.5.15 ' "$dcl")" -eq 1 ]  || fail "S1: tagged cap section lost or duplicated"
[ "$(count '^## v0.5.12 ' "$dcl")" -eq 1 ]  || fail "S1: pre-tag section wrongly folded away"
grep -q "pre-tag entry that must NOT fold"    "$dcl" || fail "S1: pre-tag entry lost"
o_ln="$(grep -n '^## v0.6.0 '  "$dcl" | cut -d: -f1)"
t_ln="$(grep -n '^## v0.5.15 ' "$dcl" | cut -d: -f1)"
p_ln="$(grep -n '^## v0.5.12 ' "$dcl" | cut -d: -f1)"
[ "$o_ln" -lt "$t_ln" ] && [ "$t_ln" -lt "$p_ln" ] || fail "S1: section order wrong after the roll-up"

cp "$dcl" "$tmp/s1_after"
gen dashboard
diff -u "$tmp/s1_after" "$dcl" || fail "S1: second run changed the output (not idempotent)"

# ---------------------------------------------------------------------------
# Scenario 2: the cap is the component's VERSION at the tag, not the tag name.
#
# The repo tag here is called v9.9.9, but dashboard/VERSION at that commit is
# v0.5.30 -- that is the cap. A later stale v0.5.31 section and a hand bump to
# v0.7.0 must both fold into "## v0.7.0"; the frozen v0.5.30 section (== cap)
# and v0.5.29 (below cap) stay put. If the tag *name* were used as the cap,
# nothing would sit above it and v0.5.31 would remain its own section.
# ---------------------------------------------------------------------------
echo d > "$tmp/dashboard/app4.js"
echo v0.5.30 > "$tmp/dashboard/VERSION"
commit "feat(dashboard): release cut at v0.5.30"
git -C "$tmp" tag v9.9.9 "$(git -C "$tmp" rev-parse --short HEAD)"

echo e > "$tmp/dashboard/app5.js"
echo v0.5.31 > "$tmp/dashboard/VERSION"
commit "fix(dashboard): post-release patch (#20)"

echo f > "$tmp/dashboard/app6.js"
echo v0.7.0 > "$tmp/dashboard/VERSION"
commit "chore: bump minor again"

cat > "$dcl" <<EOF
# Changelog

## v0.5.31 (2026-09-12)

### Fixes

- **dashboard:** post-release patch, stale (#20) (deadd0d1)

## v0.5.30 (2026-09-12)

### Features

- **dashboard:** release cut at v0.5.30 (deadd0d2)

## v0.5.29 (2026-09-11)

### Fixes

- older frozen fix below the cap (deadd0d3)
EOF
commit "docs(changelog): pre-second-bump state"

gen dashboard
[ "$(count '^## v0.7.0 ' "$dcl")" -eq 1 ]  || fail "S2: expected one open section"
[ "$(count '^## v0.5.31 ' "$dcl")" -eq 0 ] || fail "S2: v0.5.31 kept separate (cap taken from the tag name?)"
grep -q "post-release patch" "$dcl"                 || fail "S2: post-cap work lost"
[ "$(count '^## v0.5.30 ' "$dcl")" -eq 1 ] || fail "S2: cap section (v0.5.30) lost or duplicated"
[ "$(count '^## v0.5.29 ' "$dcl")" -eq 1 ] || fail "S2: below-cap section wrongly folded"
grep -q "older frozen fix below the cap" "$dcl"     || fail "S2: below-cap entry lost"

# ---------------------------------------------------------------------------
# Scenario 3: the cap follows a component's historical VERSION path.
#
# Before the src/ -> services/ split, the services component carried
# src/VERSION. A repo tag taken back then must still cap the services
# changelog: read src/VERSION at the tag as the fallback when services/VERSION
# does not exist yet. (services history_prefixes stays services/ only -- the
# pre-split commit is not walked, just its VERSION at the tag is read.)
# ---------------------------------------------------------------------------
scl="$tmp/services/CHANGELOG.md"
mkdir -p "$tmp/src"
echo v0.2.6 > "$tmp/src/VERSION"
echo x > "$tmp/src/apsystems.py"
commit "feat(services): pre-split work"
git -C "$tmp" tag v0.5.40 "$(git -C "$tmp" rev-parse --short HEAD)"
git -C "$tmp" rm -q -r src >/dev/null

mkdir -p "$tmp/services"
echo v0.2.7 > "$tmp/services/VERSION"
echo x > "$tmp/services/apsystems.py"
commit "refactor(services): split src/ into services/"

echo y >> "$tmp/services/apsystems.py"
echo v0.3.0 > "$tmp/services/VERSION"
commit "chore: bump services minor"

cat > "$scl" <<EOF
# Changelog

## v0.2.7 (2026-09-13)

### Refactors

- split src/ into services/ (deade0d1)

### Features

- stale pre-split feature above the cap (deade0d2)
EOF
commit "docs(changelog): services pre-bump state"

gen services
[ "$(count '^## v0.3.0 ' "$scl")" -eq 1 ] || fail "S3: expected one open services section"
[ "$(count '^## v0.2.7 ' "$scl")" -eq 0 ] || fail "S3: v0.2.7 kept separate (src/VERSION fallback not applied)"
grep -q "split src/ into services/" "$scl"       || fail "S3: post-cap refactor entry lost"
grep -q "stale pre-split feature above the cap" "$scl" || fail "S3: stale post-cap entry lost"

# ---------------------------------------------------------------------------
# Scenario 4: the cap reads manifest.json for the ha-integration component.
#
# ha-integration's version stream is the bare semver in
# custom_components/battery_soc/manifest.json. compute the cap from that field
# at the tag, exactly as the history walk already does for the section
# headings.
# ---------------------------------------------------------------------------
hcl="$tmp/integrations/homeassistant/CHANGELOG.md"
mdir="$tmp/integrations/homeassistant/custom_components/battery_soc"
mkdir -p "$mdir"
printf '{\n  "domain": "battery_soc",\n  "version": "0.3.4"\n}\n' > "$mdir/manifest.json"
echo m > "$tmp/integrations/homeassistant/x.py"
commit "feat(ha-integration): release cut"
git -C "$tmp" tag v0.5.50 "$(git -C "$tmp" rev-parse --short HEAD)"

printf '{\n  "domain": "battery_soc",\n  "version": "0.3.5"\n}\n' > "$mdir/manifest.json"
echo m2 >> "$tmp/integrations/homeassistant/x.py"
commit "fix(ha-integration): post-release patch (#30)"

printf '{\n  "domain": "battery_soc",\n  "version": "0.4.0"\n}\n' > "$mdir/manifest.json"
echo m3 >> "$tmp/integrations/homeassistant/x.py"
commit "chore: bump ha-integration minor"

cat > "$hcl" <<EOF
# Changelog

## v0.3.5 (2026-09-14)

### Fixes

- **ha-integration:** post-release patch, stale (#30) (deadf0d1)

## v0.3.4 (2026-09-14)

### Features

- **ha-integration:** release cut (deadf0d2)
EOF
commit "docs(changelog): ha-integration pre-bump state"

gen ha-integration
[ "$(count '^## v0.4.0 ' "$hcl")" -eq 1 ] || fail "S4: expected one open ha-integration section"
[ "$(count '^## v0.3.5 ' "$hcl")" -eq 0 ] || fail "S4: v0.3.5 kept separate (manifest.json cap not read)"
grep -q "post-release patch" "$hcl"              || fail "S4: post-cap work lost"
[ "$(count '^## v0.3.4 ' "$hcl")" -eq 1 ] || fail "S4: cap section (v0.3.4) lost or duplicated"

echo "OK"
