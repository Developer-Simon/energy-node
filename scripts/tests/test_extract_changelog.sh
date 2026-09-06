#!/usr/bin/env bash
# Test for scripts/release/extract-changelog.sh
set -euo pipefail

here=$(cd "$(dirname "$0")" && pwd)
script="$here/../release/extract-changelog.sh"
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

cat > "$tmp/CHANGELOG.md" <<'EOF'
# Changelog

## v0.6.0 (2026-09-10)

### Features

- **release:** add tag-driven GitHub release workflow (abc1234)

## v0.5.13 (2026-09-06)

### Documentation

- update changelogs (6284c1a)
EOF

fail() { echo "FAIL: $1"; [ -n "${2:-}" ] && printf '%s\n' "$2"; exit 1; }

# Extracts the requested section...
out=$("$script" "$tmp/CHANGELOG.md" v0.6.0)
case "$out" in
  *"add tag-driven GitHub release workflow"*) ;;
  *) fail "v0.6.0 section not extracted" "$out" ;;
esac
# ...and does not leak into the next section.
case "$out" in
  *"update changelogs"*) fail "leaked into the next section" "$out" ;;
esac

# Leading "v" is optional and does not change the result.
out2=$("$script" "$tmp/CHANGELOG.md" 0.6.0)
[ "$out" = "$out2" ] || fail "leading v changed the result"

# No leading or trailing blank lines.
[ -n "$(printf '%s\n' "$out" | head -1)" ] || fail "leading blank line"
[ -n "$(printf '%s\n' "$out" | tail -1)" ] || fail "trailing blank line"

# Unknown version exits 2.
rc=0
"$script" "$tmp/CHANGELOG.md" 9.9.9 >/dev/null 2>&1 || rc=$?
[ "$rc" -eq 2 ] || fail "expected exit 2 for unknown version, got $rc"

# Missing file exits 1.
rc=0
"$script" "$tmp/nope.md" 0.6.0 >/dev/null 2>&1 || rc=$?
[ "$rc" -eq 1 ] || fail "expected exit 1 for missing file, got $rc"

echo "PASS: extract-changelog.sh"
