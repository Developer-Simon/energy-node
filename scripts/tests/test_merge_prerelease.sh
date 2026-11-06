#!/usr/bin/env bash
#
# test_merge_prerelease.sh: scripts/dev/merge-prerelease.sh with a stubbed gh
# on PATH. The stub answers `gh pr view --json <field> --jq <expr>` from the
# STUB_* variables, records every call in gh.log, and keeps a copy of the
# --body-file given to `gh pr merge` in merged-body.
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo="$(cd "$here/../.." && pwd)"
script="$repo/scripts/dev/merge-prerelease.sh"

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
mkdir -p "$tmp/bin"
stub_dir="$tmp/stub"
mkdir -p "$stub_dir"

cat > "$tmp/bin/gh" <<'STUB'
#!/usr/bin/env bash
dir="$STUB_DIR"
echo "$*" >> "$dir/gh.log"
case "$1 $2" in
  "pr view")
    expr="${*: -1}"
    case "$expr" in
      .headRefName) printf '%s\n' "$STUB_HEAD" ;;
      .baseRefName) printf '%s\n' "$STUB_BASE" ;;
      .state) printf '%s\n' "$STUB_STATE" ;;
      .title) printf '%s\n' "$STUB_TITLE" ;;
      .body) cat "$STUB_BODY" ;;
      *) echo "stub: unknown field $expr" >&2; exit 1 ;;
    esac
    ;;
  "pr merge")
    prev=""
    for arg in "$@"; do
      if [[ "$prev" == "--body-file" ]]; then cp "$arg" "$dir/merged-body"; fi
      prev="$arg"
    done
    ;;
  *) echo "stub: unexpected gh $*" >&2; exit 1 ;;
esac
STUB
chmod +x "$tmp/bin/gh"

fail() { echo "FAIL: $1"; exit 1; }

# PR description with a fenced commit message block.
body_ok="$tmp/body-ok.md"
cat > "$body_ok" <<'BODY'
## Summary

Prepares the minor release.

```
Strips the -pre suffix from every component version.

Co-authored-by: Claude Sonnet 5.5 <noreply@anthropic.com>
```
BODY

body_haiku="$tmp/body-haiku.md"
sed 's/Sonnet 5.5/Haiku 5.5/' "$body_ok" > "$body_haiku"

# run <head> <base> <state> <title> <body-file>; sets $code, $out.
run() {
  rm -f "$stub_dir/gh.log" "$stub_dir/merged-body"
  code=0
  out="$(PATH="$tmp/bin:$PATH" STUB_DIR="$stub_dir" STUB_HEAD="$1" STUB_BASE="$2" \
    STUB_STATE="$3" STUB_TITLE="$4" STUB_BODY="$5" \
    bash "$script" 42 2>&1)" || code=$?
}

merged() { [[ -f "$stub_dir/gh.log" ]] && grep -q '^pr merge ' "$stub_dir/gh.log"; }

# --- accepted: prerelease into main, merge commit ----------------------------
run prerelease main OPEN "chore(release): prepare v0.12.0" "$body_ok"
[ "$code" -eq 0 ] || fail "prerelease->main: expected exit 0, got $code: $out"
grep -q -- '^pr merge 42 --merge --subject chore(release): prepare v0.12.0 (#42) --body-file ' "$stub_dir/gh.log" \
  || fail "prerelease->main: merge call wrong: $(cat "$stub_dir/gh.log")"
grep -q -- '--squash' "$stub_dir/gh.log" && fail "prerelease->main: squash used"
[ -f "$stub_dir/merged-body" ] || fail "prerelease->main: no body file passed to gh pr merge"
grep -q '^Co-authored-by: Claude Sonnet 5.5 <noreply@anthropic.com>$' "$stub_dir/merged-body" \
  || fail "prerelease->main: merged body lacks the trailer"
grep -q 'Strips the -pre suffix' "$stub_dir/merged-body" \
  || fail "prerelease->main: merged body lacks the message"
! grep -q '^## Summary' "$stub_dir/merged-body" || fail "prerelease->main: description text leaked into the body"

# --- refused: head branch from elsewhere ------------------------------------
run feat/x main OPEN "chore(release): prepare v0.12.0" "$body_ok"
[ "$code" -eq 2 ] || fail "other head: expected exit 2, got $code"
! merged || fail "other head: gh pr merge was called"

# --- refused: base is not main ----------------------------------------------
run prerelease release OPEN "chore(release): prepare v0.12.0" "$body_ok"
[ "$code" -eq 2 ] || fail "base release: expected exit 2, got $code"
! merged || fail "base release: gh pr merge was called"

# --- refused: PR not open ---------------------------------------------------
run prerelease main MERGED "chore(release): prepare v0.12.0" "$body_ok"
[ "$code" -eq 2 ] || fail "merged PR: expected exit 2, got $code"
! merged || fail "merged PR: gh pr merge was called"

# --- refused: invalid message (Haiku trailer) --------------------------------
run prerelease main OPEN "chore(release): prepare v0.12.0" "$body_haiku"
[ "$code" -eq 2 ] || fail "haiku trailer: expected exit 2, got $code"
! merged || fail "haiku trailer: gh pr merge was called"

# --- refused: no argument ----------------------------------------------------
code=0
out="$(PATH="$tmp/bin:$PATH" STUB_DIR="$stub_dir" bash "$script" 2>&1)" || code=$?
[ "$code" -eq 2 ] || fail "no argument: expected exit 2, got $code"

echo "ok"
