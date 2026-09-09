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

echo "OK"
