#!/usr/bin/env bash
# Tests for scripts/docs/pages-should-deploy.sh and scripts/docs/doc-versions.sh
set -euo pipefail

here=$(cd "$(dirname "$0")" && pwd)
repo_root="$(cd "$here/../.." && pwd)"
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
- title: Using
  items:
    - label: "Dashboard"
      url: "dashboard/index.md"
      children:
        - { label: "Settings", url: "dashboard/settings.md" }
- title: Developing
  items:
    - { label: "Data flows", url: "knowledge/data-flow.md" }
EOF

expect() {
  local want="$1" input="$2" got
  got=$(cd "$tmp/site" && printf '%s\n' "$input" | "$should_deploy" docs)
  [ "$got" = "$want" ] || fail "expected $want for: $input" "got: $got"
}

expect true  "docs/index.md"
expect true  "docs/dashboard/index.md"
expect true  "docs/dashboard/settings.md"
expect true  "docs/assets/css/site.css"
# Internal notes and redirect stubs never deploy on their own.
expect false "docs/_internal/localization.md"
expect false "docs/redirects/localization.md"
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
mkdir -p "$repo/docs/_internal" "$repo/docs/redirects"
echo x > "$repo/docs/_internal/notes.md"
echo y > "$repo/docs/redirects/old.md"
commit two

out=$("$doc_versions" "$repo")
grep -q '_internal/' <<<"$out" && fail "_internal pages must not get a version marker" "$out"
grep -q 'redirects/' <<<"$out" && fail "redirect stubs must not get a version marker" "$out"
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

# A page naming a component in its front matter carries that component's
# version as of the page's last commit. JSON manifests hold bare semver.
mkdir -p "$repo/scripts/version" "$repo/services/foo" "$repo/docs/services"
cat > "$repo/scripts/version/components.json" <<'EOF'
{"components": [
  {"id": "service:foo", "label": "Foo", "kind": "service", "version_file": "services/foo/manifest.json"},
  {"id": "tool", "label": "Tool", "kind": "app", "version_file": "tool/VERSION"}
]}
EOF
echo '{"version": "0.3.1"}' > "$repo/services/foo/manifest.json"
printf -- '---\ntitle: "Foo"\ncomponent: service:foo\n---\n\n# Foo\n' > "$repo/docs/services/foo.md"
printf -- '---\ntitle: "Tool"\ncomponent: tool\n---\n' > "$repo/docs/tool.md"
commit four
echo '{"version": "0.3.2"}' > "$repo/services/foo/manifest.json"
commit five
out=$("$doc_versions" "$repo")
grep -qxF '  "services/foo.md": { version: "v0.10.0", unreleased: true, component: "Foo service", component_version: "v0.3.1" }' <<<"$out" \
  || fail "foo.md should carry the Foo service version of its last commit" "$out"
# A component whose version file did not exist yet adds nothing.
grep -qxF '  "tool.md": { version: "v0.10.0", unreleased: true }' <<<"$out" \
  || fail "tool.md should have no component version" "$out"

# An unknown component id fails the build instead of silently dropping it.
printf -- '---\ncomponent: nope\n---\n' > "$repo/docs/bad.md"
commit six
if "$doc_versions" "$repo" >/dev/null 2>&1; then
  fail "unknown component id should fail"
fi

# --- the docs logo is the dashboard's -----------------------------------------

cmp -s "$repo_root/docs/assets/logo.svg" "$repo_root/dashboard/internal/webui/static/img/favicon.svg" \
  || fail "docs/assets/logo.svg differs from the dashboard favicon"

# --- docs colours follow the dashboard ----------------------------------------

python3 "$repo_root/scripts/docs/check_tokens.py" \
  "$repo_root/docs/assets/css/site.css" \
  "$repo_root/dashboard/internal/webui/static/css/base.css" \
  || fail "docs colours drifted from the dashboard (see above)"

echo "PASS: docs pages scripts"
