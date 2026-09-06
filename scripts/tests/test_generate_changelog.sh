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
grep -q "hand-written entry that only exists" "$cl" || fail "v0.5.12 hand-written section wiped"
grep -q "ancient hand-written fix"            "$cl" || fail "v0.1.42 section wiped"
grep -q "prehistoric note"                    "$cl" || fail "Unversioniert section wiped"

new_ln="$(grep -n "^## v0.5.13 " "$cl" | head -n1 | cut -d: -f1 || true)"
old_ln="$(grep -n "^## v0.5.12 " "$cl" | head -n1 | cut -d: -f1 || true)"
[ -n "$new_ln" ] && [ -n "$old_ln" ] && [ "$new_ln" -lt "$old_ln" ] \
  || fail "regenerated section not prepended above frozen history"

# frozen history byte-identical from its first heading onward
git -C "$tmp" show HEAD:dashboard/CHANGELOG.md | awk 'f||/^## v0.5.12 /{f=1;print}' > "$tmp/want_tail"
awk 'f||/^## v0.5.12 /{f=1;print}' "$cl" > "$tmp/got_tail"
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
[ "$(grep -c "^## v0.5.1[0-9] " "$cl")" -eq 2 ] \
  || fail "expected exactly two v0.5.x sections (regenerated + frozen v0.5.12)"
[ "$(grep -c "shiny new thing" "$cl")" -eq 1 ] || fail "post-fork commit duplicated across sections"

# --- --rebuild opts out --------------------------------------------------------
gen --rebuild dashboard
grep -q "another change"           "$cl" || fail "--rebuild dropped git history"
grep -q "ancient hand-written fix" "$cl" && fail "--rebuild kept frozen history"

echo "OK"
