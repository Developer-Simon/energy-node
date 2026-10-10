#!/usr/bin/env bash
#
# A release tag that sits on a commit which does not touch a component still
# ends that component's release. The history walk only sees commits under the
# component's path, so the tag has to be taken to the component's last commit
# at or before it. Without that, the work before and after the tag ended up in
# one open section, and entries that were already released came back in it.
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
section() { awk -v h="## $2 " 'index($0,h)==1{p=1;next} /^## /{p=0} p' "$1"; }

dcl="$tmp/dashboard/CHANGELOG.md"

# Two dashboard commits (v0.8.4, then v0.8.5), then a release tag on an
# installer-only commit, then more dashboard work (v0.8.6).
mkdir -p "$tmp/dashboard" "$tmp/installer"
echo v0.8.4 > "$tmp/dashboard/VERSION"
echo a > "$tmp/dashboard/a.js"
commit "feat(dashboard): first released feature (#1)"
echo v0.8.5 > "$tmp/dashboard/VERSION"
echo b > "$tmp/dashboard/b.js"
commit "feat(dashboard): second released feature (#2)"
echo c > "$tmp/installer/c.go"
commit "feat(installer): the commit that carries the tag (#3)"
git -C "$tmp" tag v0.8.5
echo v0.8.6 > "$tmp/dashboard/VERSION"
echo d > "$tmp/dashboard/d.js"
commit "feat(dashboard): unreleased feature (#4)"

gen dashboard
[ "$(count '^## v0.8.6 ' "$dcl")" -eq 1 ] || fail "expected one open section v0.8.6"
[ "$(count '^## v0.8.5 ' "$dcl")" -eq 1 ] || fail "expected the released section v0.8.5"
section "$dcl" v0.8.6 | grep -q "unreleased feature"       || fail "open section lost its own entry"
section "$dcl" v0.8.6 | grep -q "first released feature"   && fail "released entry #1 came back in the open section"
section "$dcl" v0.8.6 | grep -q "second released feature"  && fail "released entry #2 came back in the open section"
section "$dcl" v0.8.5 | grep -q "first released feature"   || fail "released section lost entry #1"
section "$dcl" v0.8.5 | grep -q "second released feature"  || fail "released section lost entry #2"
section "$dcl" v0.8.5 | grep -q "unreleased feature"       && fail "unreleased entry leaked into the released section"

cp "$dcl" "$tmp/after"
gen dashboard
diff -u "$tmp/after" "$dcl" || fail "second run changed the output (not idempotent)"

# ---------------------------------------------------------------------------
# A change belongs in one section only. An older section that is frozen in the
# file (its hash is unknown to git) already holds entry #2; the freshly built
# v0.8.5 section lists it too. It has to stay in the older section only.
# ---------------------------------------------------------------------------
cat >> "$dcl" <<'EOT'

## v0.7.0 (2026-09-01)

### Features

- **dashboard:** second released feature (#2) (deadbeef)
- hand written entry without a reference
EOT
gen dashboard
[ "$(count '(#2)' "$dcl")" -eq 1 ]                                  || fail "entry #2 listed in more than one section"
section "$dcl" v0.7.0 | grep -q "second released feature"           || fail "entry #2 not kept in the older section"
section "$dcl" v0.8.5 | grep -q "first released feature"            || fail "v0.8.5 lost entry #1"
section "$dcl" v0.7.0 | grep -q "hand written entry without a reference" || fail "entry without a reference was dropped"

cp "$dcl" "$tmp/after2"
gen dashboard
diff -u "$tmp/after2" "$dcl" || fail "second run changed the output (not idempotent)"

echo "PASS: test_generate_changelog_foreign_tag"
