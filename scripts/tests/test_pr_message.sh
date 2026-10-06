#!/usr/bin/env bash
# Test for scripts/dev/pr-message-lib.sh
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$here/../dev/pr-message-lib.sh"

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
fail() { echo "FAIL: $1"; [ -n "${2:-}" ] && printf '%s\n' "$2"; exit 1; }

title="feat(dashboard): show a pill"
trailer="Co-authored-by: Claude Sonnet 5.5 <noreply@anthropic.com>"

# Title line, bot and stale Claude trailers go, one model trailer stays last.
out="$(printf '%s\n\nBody line.\n\n---------\n\nCo-authored-by: Claude Opus 5.5 <noreply@anthropic.com>\nCo-authored-by: energy-node-bot <x@users.noreply.github.com>\n' "$title" \
  | pr_normalize_message "$title" "Sonnet 5.5")"
# the existing valid Opus trailer wins over the model argument
[ "$out" = "$(printf 'Body line.\n\nCo-authored-by: Claude Opus 5.5 <noreply@anthropic.com>')" ] \
  || fail "normalize with existing trailer" "$out"

out="$(printf 'Body line.\n\n\n' | pr_normalize_message "$title" "Sonnet 5.5")"
[ "$out" = "$(printf 'Body line.\n\n%s' "$trailer")" ] || fail "normalize appends trailer" "$out"

out="$(printf 'Body line.\n' | pr_normalize_message "$title" "Claude Opus 5.5")"
[ "$out" = "$(printf 'Body line.\n\nCo-authored-by: Claude Opus 5.5 <noreply@anthropic.com>')" ] \
  || fail "normalize accepts 'Claude ' prefix" "$out"

printf 'Body.\n\n%s\n' "$trailer" > "$tmp/ok"
pr_validate_message "$title" "$tmp/ok" 2>/dev/null || fail "valid message rejected"

printf '%s\n\nBody.\n\n%s\n' "$title" "$trailer" > "$tmp/title"
pr_validate_message "$title" "$tmp/title" 2>/dev/null && fail "title in body accepted"

printf 'Body.\n\nCo-authored-by: Claude Haiku 4.5 <noreply@anthropic.com>\n' > "$tmp/haiku"
pr_validate_message "$title" "$tmp/haiku" 2>/dev/null && fail "haiku accepted"

printf 'Body.\n' > "$tmp/none"
pr_validate_message "$title" "$tmp/none" 2>/dev/null && fail "missing trailer accepted"

printf 'Body.\n\n%s\n\nMore.\n' "$trailer" > "$tmp/notlast"
pr_validate_message "$title" "$tmp/notlast" 2>/dev/null && fail "trailer not last accepted"

pr_validate_message "Update stuff" "$tmp/ok" 2>/dev/null && fail "non-conventional title accepted"

printf 'x\n```text\nA\nB\n```\nrest\n' > "$tmp/pr"
[ "$(pr_extract_block "$tmp/pr")" = "$(printf 'A\nB')" ] || fail "extract block"

echo "ok"
