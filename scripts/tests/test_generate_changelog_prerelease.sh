#!/usr/bin/env bash
#
# Prerelease line in generate_changelog.sh: commits on branch `prerelease`
# carry -pre.N versions and get one open "## vX.Y.Z-pre.N" section. Final
# releases on `main` keep their own sections, a merge of main into prerelease
# and back ends in the final version, and no -pre heading survives a release.
#
# The generated CHANGELOG.md is kept out of history (commit helper excludes
# it), so branch switches and merges never conflict on it. Every switch
# discards the generated file first.
set -euo pipefail
here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo="$(git -C "$here" rev-parse --show-toplevel)"

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

git -C "$tmp" init -q -b main
git -C "$tmp" config user.email t@t
git -C "$tmp" config user.name t

mkdir -p "$tmp/scripts"
cp "$repo/scripts/generate_changelog.sh" "$tmp/scripts/generate_changelog.sh"
script="$tmp/scripts/generate_changelog.sh"

cl="$tmp/dashboard/CHANGELOG.md"

fail() { echo "FAIL: $1"; exit 1; }
gen() { ( cd "$tmp" && bash "$script" "$@" ); }
generate() { gen dashboard || fail "$1: generator exited non-zero"; }
# Every commit is one day later than the one before, so the walk order is the
# creation order, as on the real lines where main's commits follow the older
# prerelease commits.
tick=0
commit() {
  tick=$((tick + 1))
  local d; d="$(printf '2026-01-%02dT12:00:00' "$tick")"
  git -C "$tmp" add -A -- . ':(exclude)dashboard/CHANGELOG.md' \
    && GIT_AUTHOR_DATE="$d" GIT_COMMITTER_DATE="$d" git -C "$tmp" commit -qm "$1"
}
switch_to() { git -C "$tmp" checkout -q -- dashboard/CHANGELOG.md && git -C "$tmp" checkout -q "$1"; }

# Body of the "## <version> (...)" section, empty if the heading is absent.
sec() { awk -v h="## $2 " 'index($0,h)==1{on=1;next} /^## /{on=0} on' "$1"; }
# Number of "## <version> " headings.
heads() { awk -v h="## $2 " 'index($0,h)==1{n++} END{print n+0}' "$1"; }
# Number of occurrences of <needle> inside the body of section <version>.
in_sec() { sec "$1" "$2" | grep -cF -- "$3" || true; }
# Number of occurrences of <needle> in the whole file.
count_all() { grep -cF -- "$2" "$1" || true; }
# Number of -pre headings in the file.
pre_heads() { grep -c '^## v[0-9.]*-pre\.' "$1" || true; }

# --- Setup: main at v0.4.5, tagged v0.11.0 ---------------------------------
mkdir -p "$tmp/dashboard"
echo v0.4.5 > "$tmp/dashboard/VERSION"
echo base > "$tmp/dashboard/app.js"
printf '# Changelog\n' > "$cl"
git -C "$tmp" add -A && git -C "$tmp" commit -qm "feat(dashboard): base feature (#0)"
git -C "$tmp" tag v0.11.0

# --- Step 1: prerelease commit A, version v0.4.6-pre.1 --------------------
git -C "$tmp" checkout -q -b prerelease
echo a > "$tmp/dashboard/feat_a.js"
echo v0.4.6-pre.1 > "$tmp/dashboard/VERSION"
commit "feat: A (#1)"
generate S1
[ "$(heads "$cl" v0.4.6-pre.1)" -eq 1 ]   || fail "S1: expected one section '## v0.4.6-pre.1'"
[ "$(in_sec "$cl" v0.4.6-pre.1 'A (#1)')" -eq 1 ] || fail "S1: A (#1) not in '## v0.4.6-pre.1'"

# --- Step 2: prerelease commit B, version v0.4.6-pre.2 --------------------
echo b > "$tmp/dashboard/feat_b.js"
echo v0.4.6-pre.2 > "$tmp/dashboard/VERSION"
commit "feat: B (#2)"
generate S2
[ "$(pre_heads "$cl")" -eq 1 ]                          || fail "S2: expected exactly one -pre heading"
[ "$(heads "$cl" v0.4.6-pre.2)" -eq 1 ]                 || fail "S2: expected one section '## v0.4.6-pre.2'"
[ "$(heads "$cl" v0.4.6-pre.1)" -eq 0 ]                 || fail "S2: stale '## v0.4.6-pre.1' kept"
[ "$(in_sec "$cl" v0.4.6-pre.2 'A (#1)')" -eq 1 ]       || fail "S2: A (#1) not in '## v0.4.6-pre.2'"
[ "$(in_sec "$cl" v0.4.6-pre.2 'B (#2)')" -eq 1 ]       || fail "S2: B (#2) not in '## v0.4.6-pre.2'"
cp "$cl" "$tmp/changelog.s2"
[ "$(count_all "$cl" 'A (#1)')" -eq 1 ]                 || fail "S2: A (#1) duplicated"

# --- Step 3: main commit H, version v0.4.6, tagged v0.11.1 ----------------
switch_to main
echo h > "$tmp/dashboard/fix_h.js"
echo v0.4.6 > "$tmp/dashboard/VERSION"
commit "fix: H (#3)"
git -C "$tmp" tag v0.11.1
generate S3
[ "$(heads "$cl" v0.4.6)" -eq 1 ]                       || fail "S3: expected one section '## v0.4.6'"
[ "$(in_sec "$cl" v0.4.6 'H (#3)')" -eq 1 ]             || fail "S3: H (#3) not in '## v0.4.6'"
[ "$(in_sec "$cl" v0.4.6 'A (#1)')" -eq 0 ]             || fail "S3: A (#1) leaked from prerelease into main"
[ "$(in_sec "$cl" v0.4.6 'B (#2)')" -eq 0 ]             || fail "S3: B (#2) leaked from prerelease into main"
[ "$(pre_heads "$cl")" -eq 0 ]                          || fail "S3: -pre heading on main"

# --- Step 4: merge main into prerelease, version v0.4.7-pre.2 -------------
switch_to prerelease
# VERSION conflicts (v0.4.6-pre.2 vs v0.4.6); the next line resolves it.
git -C "$tmp" merge -q --no-edit main || [ -f "$tmp/.git/MERGE_HEAD" ] || fail "S4: git merge main failed"
echo v0.4.7-pre.2 > "$tmp/dashboard/VERSION"
commit "chore: merge main into prerelease"
generate S4
[ "$(heads "$cl" v0.4.7-pre.2)" -eq 1 ]                 || fail "S4: expected one section '## v0.4.7-pre.2'"
[ "$(in_sec "$cl" v0.4.7-pre.2 'A (#1)')" -eq 1 ]       || fail "S4: A (#1) not in '## v0.4.7-pre.2'"
[ "$(in_sec "$cl" v0.4.7-pre.2 'B (#2)')" -eq 1 ]       || fail "S4: B (#2) not in '## v0.4.7-pre.2'"
[ "$(in_sec "$cl" v0.4.7-pre.2 'H (#3)')" -eq 0 ]       || fail "S4: H (#3) leaked into '## v0.4.7-pre.2'"
[ "$(heads "$cl" v0.4.6)" -eq 1 ]                       || fail "S4: expected one section '## v0.4.6'"
[ "$(in_sec "$cl" v0.4.6 'H (#3)')" -eq 1 ]             || fail "S4: H (#3) not in '## v0.4.6'"
[ "$(in_sec "$cl" v0.4.6 'A (#1)')" -eq 0 ]             || fail "S4: A (#1) in '## v0.4.6'"
[ "$(in_sec "$cl" v0.4.6 'B (#2)')" -eq 0 ]             || fail "S4: B (#2) in '## v0.4.6'"
[ "$(pre_heads "$cl")" -eq 1 ]                          || fail "S4: old '## v0.4.6-pre.2' kept next to the open section"
[ "$(count_all "$cl" 'A (#1)')" -eq 1 ]                 || fail "S4: A (#1) duplicated"

# --- Step 4b: the same merge with the old -pre section still in the file ---
# On the real line CHANGELOG.md is tracked, so the sync run regenerates it with
# the section of the previous prerelease number in place. Main's commits come
# later in the walk than the prerelease ones, so the tagged release is the last
# section. The old -pre section must still end in the new open -pre section.
# An entry with a hash Git does not know (a squash merged PR keeps only its
# number) makes the old section count as frozen text, as on the real line.
cp "$tmp/changelog.s2" "$cl"
sed -i '0,/^- /s//- **dashboard:** entry only the old file knows (#99) (deadbee)\n- /' "$cl"
generate S4b
[ "$(heads "$cl" v0.4.7-pre.2)" -eq 1 ]                 || fail "S4b: expected one section '## v0.4.7-pre.2'"
[ "$(in_sec "$cl" v0.4.7-pre.2 'A (#1)')" -eq 1 ]       || fail "S4b: A (#1) not in '## v0.4.7-pre.2'"
[ "$(in_sec "$cl" v0.4.7-pre.2 'B (#2)')" -eq 1 ]       || fail "S4b: B (#2) not in '## v0.4.7-pre.2'"
[ "$(in_sec "$cl" v0.4.7-pre.2 'only the old file knows')" -eq 1 ] || fail "S4b: the old entry is not in '## v0.4.7-pre.2'"
[ "$(in_sec "$cl" v0.4.6 'only the old file knows')" -eq 0 ] || fail "S4b: the old entry was folded into the released '## v0.4.6'"
[ "$(in_sec "$cl" v0.4.6 'A (#1)')" -eq 0 ]             || fail "S4b: A (#1) folded into the released '## v0.4.6'"
[ "$(in_sec "$cl" v0.4.6 'B (#2)')" -eq 0 ]             || fail "S4b: B (#2) folded into the released '## v0.4.6'"
[ "$(in_sec "$cl" v0.4.6 'H (#3)')" -eq 1 ]             || fail "S4b: H (#3) not in '## v0.4.6'"
[ "$(pre_heads "$cl")" -eq 1 ]                          || fail "S4b: old -pre heading kept"
[ "$(count_all "$cl" 'A (#1)')" -eq 1 ]                 || fail "S4b: A (#1) duplicated"

# --- Step 5: final version v0.4.7 on prerelease, --no-ff merge to main ----
echo v0.4.7 > "$tmp/dashboard/VERSION"
commit "chore: set version v0.4.7"
switch_to main
git -C "$tmp" merge -q --no-ff --no-edit prerelease
git -C "$tmp" tag v0.12.0
generate S5
[ "$(heads "$cl" v0.4.7)" -eq 1 ]                       || fail "S5: expected one section '## v0.4.7'"
[ "$(in_sec "$cl" v0.4.7 'A (#1)')" -eq 1 ]             || fail "S5: A (#1) not in '## v0.4.7'"
[ "$(in_sec "$cl" v0.4.7 'B (#2)')" -eq 1 ]             || fail "S5: B (#2) not in '## v0.4.7'"
[ "$(in_sec "$cl" v0.4.7 'H (#3)')" -eq 0 ]             || fail "S5: H (#3) in '## v0.4.7'"
[ "$(heads "$cl" v0.4.6)" -eq 1 ]                       || fail "S5: expected one section '## v0.4.6'"
[ "$(in_sec "$cl" v0.4.6 'H (#3)')" -eq 1 ]             || fail "S5: H (#3) not in '## v0.4.6'"
[ "$(in_sec "$cl" v0.4.6 'A (#1)')" -eq 0 ]             || fail "S5: A (#1) in '## v0.4.6'"
[ "$(in_sec "$cl" v0.4.6 'B (#2)')" -eq 0 ]             || fail "S5: B (#2) in '## v0.4.6'"
[ "$(count_all "$cl" '-pre')" -eq 0 ]                   || fail "S5: -pre left in the changelog after the release"
[ "$(count_all "$cl" 'A (#1)')" -eq 1 ]                 || fail "S5: A (#1) duplicated"
o_ln="$(grep -n '^## v0.4.7 ' "$cl" | cut -d: -f1)"
p_ln="$(grep -n '^## v0.4.6 ' "$cl" | cut -d: -f1)"
[ "$o_ln" -lt "$p_ln" ]                                 || fail "S5: section order wrong (v0.4.7 must precede v0.4.6)"

echo "OK"
