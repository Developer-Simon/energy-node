#!/usr/bin/env bash
set -euo pipefail
here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo="$(git -C "$here" rev-parse --show-toplevel)"

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

git -C "$tmp" init -q
git -C "$tmp" config user.email t@t
git -C "$tmp" config user.name t

# generate_changelog.sh resolves the repo root from its own location, so run a
# copy that lives inside the throwaway repo.
mkdir -p "$tmp/scripts"
cp "$repo/scripts/generate_changelog.sh" "$tmp/scripts/generate_changelog.sh"
script="$tmp/scripts/generate_changelog.sh"
cl="$tmp/dashboard/CHANGELOG.md"

commit() { git -C "$tmp" add -A && git -C "$tmp" commit -qm "$1"; }
gen() { ( cd "$tmp" && bash "$script" "$@" ); }
fail() { echo "FAIL: $1"; exit 1; }

# Root commit: dashboard component at v0.5.13, with a hand-written CHANGELOG.md
# carrying pre-fork history that git log can no longer see.
mkdir -p "$tmp/dashboard"
echo v0.5.13 > "$tmp/dashboard/VERSION"
cat > "$cl" <<'EOF'
# Changelog

## v0.5.12 (2026-09-05)

### Features

- hand-written entry that only exists in this file (deadbee1)

## v0.1.42 (2026-08-26)

### Fixes

- ancient hand-written fix (deadbee2)

## Unversioniert (bis 2026-08-20)

### Other

- prehistoric note (deadbee3)
EOF
commit "chore: initial public release"

echo hello > "$tmp/dashboard/app.js"
commit "feat(dashboard): shiny new thing"

# --- default (auto-freeze) --------------------------------------------------
gen dashboard
grep -q "shiny new thing"                    "$cl" || fail "new commit missing"
# v0.5.12 shares the open minor (0.5) with the regenerated section, so its
# entries are folded in rather than kept as a separate frozen block.
grep -q "hand-written entry that only exists" "$cl" || fail "v0.5.12 entry lost in same-minor merge"
[ "$(grep -c "^## v0.5.12 " "$cl")" -eq 0 ]         || fail "v0.5.12 not merged into the open-minor section"
grep -q "ancient hand-written fix"            "$cl" || fail "v0.1.42 section wiped"
grep -q "prehistoric note"                    "$cl" || fail "Unversioniert section wiped"

new_ln="$(grep -n "^## v0.5.13 " "$cl" | head -n1 | cut -d: -f1 || true)"
old_ln="$(grep -n "^## v0.1.42 " "$cl" | head -n1 | cut -d: -f1 || true)"
[ -n "$new_ln" ] && [ -n "$old_ln" ] && [ "$new_ln" -lt "$old_ln" ] \
  || fail "regenerated section not prepended above frozen history"

# frozen history (older minors) byte-identical from its first heading onward
git -C "$tmp" show HEAD:dashboard/CHANGELOG.md | awk 'f||/^## v0.1.42 /{f=1;print}' > "$tmp/want_tail"
awk 'f||/^## v0.1.42 /{f=1;print}' "$cl" > "$tmp/got_tail"
diff -u "$tmp/want_tail" "$tmp/got_tail" || fail "frozen tail was modified"

# --- idempotent -----------------------------------------------------------
cp "$cl" "$tmp/after_first"
gen dashboard
diff -u "$tmp/after_first" "$cl" || fail "second run changed the output"
[ "$(grep -c "^## v0.5.13 " "$cl")" -eq 1 ] || fail "duplicate v0.5.13 section after re-run"

# --- picks up a later commit, still one section, history intact -----------
echo more > "$tmp/dashboard/app2.js"
echo v0.5.14 > "$tmp/dashboard/VERSION"
commit "fix(dashboard): another change"
gen dashboard
grep -q "another change"                      "$cl" || fail "later commit missing"
grep -q "shiny new thing"                     "$cl" || fail "earlier post-fork commit lost"
grep -q "hand-written entry that only exists"  "$cl" || fail "hand-written history lost on 3rd run"
[ "$(grep -c "^## v0.5.1[0-9] " "$cl")" -eq 1 ] \
  || fail "open minor must stay a single section (regenerated, v0.5.12 folded in)"
[ "$(grep -c "shiny new thing" "$cl")" -eq 1 ] || fail "post-fork commit duplicated across sections"

# --- stale same-minor section (post-squash) is absorbed, not stacked --------
# After a PR squash-merges, the section the previous run wrote for this minor
# cites branch-commit hashes git can no longer see. The next run must fold that
# block into the freshly built open-minor section (union of entries, deduped by
# "(#NN)" or by text without the hash), not stack a new "## vX.Y.(Z+1)" on top.
echo seven > "$tmp/dashboard/app4.js"
echo v0.5.15 > "$tmp/dashboard/VERSION"
commit "feat(dashboard): shared feature (#7)"
sf_hash="$(git -C "$tmp" rev-parse --short HEAD)"
cat > "$cl" <<'EOF'
# Changelog

## v0.5.15 (2026-09-08)

### Features

- **dashboard:** shared feature, stale wording (#7) (deadaaa1)
- ghost entry from a squashed pr (deadaaa2)

### Fixes

- **dashboard:** another change (deadaaa3)

## v0.3.0 (2026-07-01)

### Features

- older-minor feature that stays frozen (deadaaa4)

## v0.1.42 (2026-08-26)

### Fixes

- ancient hand-written fix (deadbee2)

## Unversioniert (bis 2026-08-20)

### Other

- prehistoric note (deadbee3)
EOF
commit "docs(changelog): simulate post-squash state"
gen dashboard
[ "$(grep -c "^## v0.5.15 " "$cl")" -eq 1 ] || fail "stale same-minor section stacked instead of merging"
grep -q "ghost entry from a squashed pr" "$cl" || fail "dangling-hash entry dropped in merge"
grep -q "another change"                 "$cl" || fail "reachable entry lost in merge"
[ "$(grep -c "another change" "$cl")" -eq 1 ]  || fail "entry duplicated across merged section"
grep -q "stale wording"                  "$cl" && fail "PR-number dup not deduplicated (#7)"
[ "$(grep -c "^## v0.3.0 " "$cl")" -eq 1 ] || fail "older minor wrongly folded into open minor"
grep -q "older-minor feature that stays frozen" "$cl" || fail "frozen older-minor entry lost"
git -C "$tmp" show HEAD:dashboard/CHANGELOG.md | awk 'f||/^## v0.3.0 /{f=1;print}' > "$tmp/want_tail2"
awk 'f||/^## v0.3.0 /{f=1;print}' "$cl" > "$tmp/got_tail2"
diff -u "$tmp/want_tail2" "$tmp/got_tail2" || fail "frozen tail below the open minor was modified"

# --- a tagged release caps the merge: only post-tag stale blocks fold --------
# v0.5.15 becomes a real release tag. A later run leaves a stale "## v0.5.16"
# (post-tag, same minor as the open work) AND a stale "## v0.5.12" (written
# before the tag). Only the post-tag block folds into the open section; the
# pre-tag block stays where it is, below the tagged section.
git -C "$tmp" tag v0.5.15 "$sf_hash"
echo eight > "$tmp/dashboard/app5.js"
echo v0.5.16 > "$tmp/dashboard/VERSION"
commit "feat(dashboard): post-tag work"
cat > "$cl" <<EOF
# Changelog

## v0.5.16 (2026-09-10)

### Fixes

- stale open-section entry, dangling hash (deadbbb1)

## v0.5.15 (2026-09-08)

### Features

- **dashboard:** shared feature (#7) (${sf_hash})

## v0.5.12 (2026-09-06)

### Features

- pre-tag stale entry that must NOT fold into the open section (deadbbb3)

## v0.3.0 (2026-07-01)

### Features

- older-minor feature that stays frozen (deadaaa4)
EOF
commit "docs(changelog): pre-tag vs post-tag stale blocks"
gen dashboard
grep -q "stale open-section entry"   "$cl" || fail "post-tag stale block not folded into open section"
grep -q "post-tag work"              "$cl" || fail "open-section commit missing"
[ "$(grep -c "^## v0.5.16 " "$cl")" -eq 1 ] || fail "open section duplicated"
[ "$(grep -c "^## v0.5.15 " "$cl")" -eq 1 ] || fail "tagged release section lost or duplicated"
[ "$(grep -c "^## v0.5.12 " "$cl")" -eq 1 ] || fail "pre-tag same-minor block wrongly folded away"
grep -q "pre-tag stale entry that must NOT fold" "$cl" || fail "pre-tag stale entry lost"
o_ln="$(grep -n "^## v0.5.16 " "$cl" | cut -d: -f1)"
t_ln="$(grep -n "^## v0.5.15 " "$cl" | cut -d: -f1)"
p_ln="$(grep -n "^## v0.5.12 " "$cl" | cut -d: -f1)"
[ "$o_ln" -lt "$t_ln" ] && [ "$t_ln" -lt "$p_ln" ] || fail "section order wrong after tag-capped merge"

# --- --rebuild opts out --------------------------------------------------------
gen --rebuild dashboard
grep -q "another change"           "$cl" || fail "--rebuild dropped git history"
grep -q "ancient hand-written fix" "$cl" && fail "--rebuild kept frozen history"

# --- the version-bump workflow's housekeeping commits are not listed ---------
echo z > "$tmp/dashboard/app3.js"
commit "feat(dashboard): real change to list"
echo z2 >> "$tmp/dashboard/app3.js"
commit "chore(release): bump component versions"
printf '# Changelog\n\n' > "$tmp/dashboard/CHANGELOG.md"
commit "docs(changelog): regenerate component changelogs"
printf '# Changelog\n' > "$tmp/dashboard/CHANGELOG.md"
commit "docs(changelog): update changelogs"
gen --rebuild dashboard
grep -q "real change to list"      "$cl" || fail "real commit missing after housekeeping filter"
grep -q "bump component versions"  "$cl" && fail "chore(release) housekeeping commit was listed"
grep -q "changelogs"              "$cl" && fail "a docs(changelog) housekeeping commit was listed"

# --- the bump commit still heads the open section with its own version ------
# generate_changelog.sh runs after bump-patch.sh on the PR branch (see
# .github/workflows/version-bump.yml), so it walks the "chore(release): bump
# component versions" commit too -- it must move the open section's heading to
# the version that commit produced, without becoming an entry itself.
echo v0.5.18 > "$tmp/dashboard/VERSION"
commit "chore(release): bump component versions"
gen --rebuild dashboard
grep -q "^## v0.5.18 " "$cl"      || fail "bump commit's version did not become the open section heading"
grep -q "bump component versions" "$cl" && fail "bump commit itself got listed as an entry"

# --- semver breaking-change marker: `type!:` and `type(scope)!:` --------------
# The "!" must not defeat type detection (commit still lands in its normal
# bucket), and the entry gets a "⚠ Breaking" prefix rather than the raw
# subject leaking into "### Other".
echo b1 > "$tmp/dashboard/breaking1.js"
commit "feat!: drop the legacy node block"
echo b2 > "$tmp/dashboard/breaking2.js"
commit "fix(dashboard)!: reject configs without an explicit unit"
gen --rebuild dashboard
grep -qF '**⚠ Breaking:** drop the legacy node block' "$cl" \
  || fail "plain feat! entry missing its breaking marker / wrong bucket"
grep -qF '**⚠ Breaking — dashboard:** reject configs without an explicit unit' "$cl" \
  || fail "scoped fix! entry missing its breaking marker / wrong bucket"
grep -q "feat!: drop the legacy node block" "$cl" \
  && fail "raw feat! subject leaked (commit fell through to Other)"
awk '/^### /{sec=$0} /drop the legacy node block/{print sec}' "$cl" | grep -qx "### Features" \
  || fail "feat! not filed under Features"
awk '/^### /{sec=$0} /reject configs without an explicit unit/{print sec}' "$cl" | grep -qx "### Fixes" \
  || fail "fix(dashboard)! not filed under Fixes"

# --- cross-"(#NN)" duplicate removed by hash existence -----------------------
# A squash-merged PR's "(#NN)" entry can repeat an in-branch commit's exact
# title under a new hash. The pre-squash entry the previous run already froze
# (no "(#NN)", a hash git no longer knows) must not survive next to it: same
# text once the hash and any "(#NN)" are stripped, but only one hash is still
# reachable.
echo dup > "$tmp/dashboard/dup.js"
echo v0.5.19 > "$tmp/dashboard/VERSION"
commit "fix(dashboard): rename the frobnicator (#31)"
cat > "$cl" <<'EOF'
# Changelog

## v0.5.19 (2026-09-11)

### Fixes

- **dashboard:** rename the frobnicator (deadccc1)
EOF
commit "docs(changelog): simulate the pre-squash frobnicator entry"
gen dashboard
[ "$(grep -c "rename the frobnicator" "$cl")" -eq 1 ] \
  || fail "stale pre-squash duplicate not removed by the hash-existence dedup"
grep -q "rename the frobnicator (#31)" "$cl" \
  || fail "live (#NN) entry lost while deduplicating"

# --- a foreign commit (only CHANGELOG.md / VERSION) is not this component's --
# A squash-merge carries the PR branch's bot commits with it, so it touches
# every component's CHANGELOG.md and VERSION even when it changed nothing in
# that component. Those two files must not put a commit into the walk --
# otherwise every PR lands in every component's changelog.
echo v0.6.0 > "$tmp/dashboard/VERSION"
printf '# Changelog\n' > "$cl"
commit "feat(installer): something that never touched the dashboard"
mkdir -p "$tmp/installer"
echo real > "$tmp/installer/main.go"
commit "feat(installer): real installer work"
echo local > "$tmp/dashboard/local.js"
commit "feat(dashboard): real dashboard work"
gen --rebuild dashboard
grep -q "real dashboard work" "$cl" \
  || fail "the component's own commit is missing"
grep -q "something that never touched the dashboard" "$cl" \
  && fail "a CHANGELOG.md/VERSION-only commit was listed as dashboard work"
grep -q "real installer work" "$cl" \
  && fail "a commit outside the component was listed"

# --- the open section is headed by the current VERSION file ------------------
# The bump commit only touches VERSION, so it is no longer in the walk. The
# heading has to come from the version file itself.
echo v0.6.1 > "$tmp/dashboard/VERSION"
commit "chore(release): bump component versions"
gen --rebuild dashboard
grep -q "^## v0.6.1 " "$cl" \
  || fail "open section not headed by the current VERSION file"

# --- duplicate inside ONE section: "(#NN)" wins over the in-PR entry ---------
# Both hashes are alive here, so the hash-existence rule cannot decide; the
# entry carrying the PR reference is the one to keep.
echo dd > "$tmp/dashboard/dd.js"
commit "fix(dashboard): stop the springs from fighting"
echo dd2 >> "$tmp/dashboard/dd.js"
commit "fix(dashboard): stop the springs from fighting (#41)"
gen --rebuild dashboard
[ "$(grep -c "stop the springs from fighting" "$cl")" -eq 1 ] \
  || fail "in-PR duplicate not removed inside the section"
grep -q "stop the springs from fighting (#41)" "$cl" \
  || fail "the (#NN) entry is the one that must survive"

# --- two different PRs with the same title both stay -------------------------
echo t1 > "$tmp/dashboard/t1.js"
commit "fix(dashboard): tidy up (#50)"
echo t2 > "$tmp/dashboard/t2.js"
commit "fix(dashboard): tidy up (#51)"
gen --rebuild dashboard
[ "$(grep -c "tidy up (#5" "$cl")" -eq 2 ] \
  || fail "two distinct PRs with the same title must both be listed"

# --- an emptied "### " heading disappears with its entries -------------------
cat > "$cl" <<'EOF'
# Changelog

## v0.6.1 (2026-09-12)

### Features

- **dashboard:** only here once (#60)
- **dashboard:** only here once

### Fixes

- **dashboard:** a real fix (#61)
EOF
commit "docs(changelog): hand-written section with an in-PR duplicate"
gen dashboard
[ "$(grep -c "only here once" "$cl")" -eq 1 ] \
  || fail "duplicate inside a frozen section not removed"
grep -q "a real fix (#61)" "$cl" || fail "frozen section lost an entry"
grep -q "^### Features" "$cl"    || fail "a still-populated heading was dropped"

# --- commit trailers do not leak into an entry -------------------------------
echo tr > "$tmp/dashboard/tr.js"
commit "feat(dashboard): add the orchestrator script Co-Authored-By: Claude Haiku 4.5 <noreply@anthropic.com>"
gen --rebuild dashboard
grep -q "Co-Authored-By" "$cl" && fail "a commit trailer leaked into the changelog"
grep -qF "**dashboard:** add the orchestrator script (" "$cl" \
  || fail "entry text lost while stripping the trailer"

# --- service:<dir>: own changelog, own version out of manifest.json ---------
# A service's version lives in its manifest.json, which also carries real
# configuration -- so, unlike a plain VERSION file, it must stay in the walk.
# For commits from before the split there is no "version" field yet; the
# lookup then falls back to the services umbrella VERSION so the older
# sections keep meaningful headings.
mkdir -p "$tmp/services/shelly"
echo v0.3.0 > "$tmp/services/VERSION"
printf '{\n  "service_id": "shelly",\n  "kind": "device"\n}\n' \
  > "$tmp/services/shelly/manifest.json"
echo rpc > "$tmp/services/shelly/shelly_rpc.py"
commit "feat(shelly): talk to the RPC endpoint"
printf '{\n  "service_id": "shelly",\n  "version": "0.4.0",\n  "kind": "device"\n}\n' \
  > "$tmp/services/shelly/manifest.json"
commit "feat(shelly): give the service its own version"
scl="$tmp/services/shelly/CHANGELOG.md"
gen --rebuild service:shelly
[ -f "$scl" ] || fail "service:shelly wrote no changelog"
grep -q "talk to the RPC endpoint" "$scl" || fail "service commit missing"
grep -q "give the service its own version" "$scl" || fail "manifest.json change missing from the walk"
grep -q "^## v0.4.0 " "$scl" || fail "open section not headed by the manifest version"
awk '/^## /{sec=$0} /talk to the RPC endpoint/{print sec}' "$scl" | grep -q "v0.3.0\|v0.4.0" \
  || fail "pre-split commit did not fall back to the umbrella version"

# --- the services umbrella no longer repeats a service's work ---------------
mkdir -p "$tmp/services"
echo shared > "$tmp/services/shared.py"
commit "feat(services): shared helper"
gen --rebuild services
scl2="$tmp/services/CHANGELOG.md"
grep -q "shared helper"            "$scl2" || fail "umbrella lost its own commit"
grep -q "talk to the RPC endpoint" "$scl2" && fail "umbrella repeated a service's commit"

echo "OK"
