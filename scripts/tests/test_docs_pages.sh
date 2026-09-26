#!/usr/bin/env bash
# Tests for scripts/docs/pages-should-deploy.sh and scripts/docs/doc-versions.sh
set -euo pipefail

here=$(cd "$(dirname "$0")" && pwd)
should_deploy="$here/../docs/pages-should-deploy.sh"
doc_versions="$here/../docs/doc-versions.sh"
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

fail() { echo "FAIL: $1"; [ -n "${2:-}" ] && printf '%s\n' "$2"; exit 1; }

# --- pages-should-deploy.sh -------------------------------------------------

mkdir -p "$tmp/site/docs/_data"
cat > "$tmp/site/docs/_data/nav.yml" <<'EOF'
- title: Overview
  items:
    - { label: "Home", url: "index.md" }
- title: Knowledge
  sections:
    - label: General
      items:
        - { label: "Data flows", url: "knowledge/data-flow.md" }
EOF

expect() {
  local want="$1" input="$2" got
  got=$(cd "$tmp/site" && printf '%s\n' "$input" | "$should_deploy" docs)
  [ "$got" = "$want" ] || fail "expected $want for: $input" "got: $got"
}

expect true  "docs/index.md"
expect true  "docs/knowledge/data-flow.md"
expect true  "docs/_layouts/default.html"
expect true  "docs/_data/nav.yml"
expect true  "docs/images/x.png"
expect true  ".github/workflows/pages.yml"
expect true  "scripts/docs/doc-versions.sh"
expect true  $'dashboard/main.go\ndocs/knowledge/data-flow.md'
# Documents outside the navigation, and non-docs paths, do not deploy.
expect false "docs/knowledge/dashboard/lazy-assets-cache-busting.md"
expect false $'dashboard/main.go\ndocs/knowledge/dashboard/lazy-assets-cache-busting.md'
expect false "docs/knowledge/data-flow.md.orig"
expect false ""

# --- doc-versions.sh --------------------------------------------------------

repo="$tmp/repo"
mkdir -p "$repo/docs/knowledge" "$repo/dashboard"
git -C "$repo" init -q
git -C "$repo" config user.email test@example.com
git -C "$repo" config user.name test
commit() { git -C "$repo" add -A && git -C "$repo" commit -qm "$1"; }

echo v0.1.0 > "$repo/dashboard/VERSION"
echo a > "$repo/docs/index.md"
echo a > "$repo/docs/knowledge/old.md"
commit one
git -C "$repo" tag v0.1.0

echo v0.2.0 > "$repo/dashboard/VERSION"
echo b > "$repo/docs/knowledge/new.md"
commit two

out=$("$doc_versions" "$repo")
grep -qx 'latest_release: "v0.1.0"' <<<"$out" || fail "latest release not v0.1.0" "$out"
grep -qxF '  "index.md": { version: "v0.1.0", unreleased: false }' <<<"$out" \
  || fail "index.md should be released v0.1.0" "$out"
grep -qxF '  "knowledge/old.md": { version: "v0.1.0", unreleased: false }' <<<"$out" \
  || fail "old.md should be released v0.1.0" "$out"
grep -qxF '  "knowledge/new.md": { version: "v0.2.0", unreleased: true }' <<<"$out" \
  || fail "new.md should be unreleased v0.2.0" "$out"

# A later release tag clears the unreleased flag. Non-release tags are ignored.
git -C "$repo" tag v0.2.0
git -C "$repo" tag v0.3.0-b1
out=$("$doc_versions" "$repo")
grep -qx 'latest_release: "v0.2.0"' <<<"$out" || fail "latest release not v0.2.0" "$out"
grep -qxF '  "knowledge/new.md": { version: "v0.2.0", unreleased: false }' <<<"$out" \
  || fail "new.md should be released after tagging v0.2.0" "$out"

# Version comparison is numeric, not lexical.
echo v0.10.0 > "$repo/dashboard/VERSION"
echo c > "$repo/docs/index.md"
commit three
out=$("$doc_versions" "$repo")
grep -qxF '  "index.md": { version: "v0.10.0", unreleased: true }' <<<"$out" \
  || fail "v0.10.0 should be newer than v0.2.0" "$out"

echo "PASS: docs pages scripts"
