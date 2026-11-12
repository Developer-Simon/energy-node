#!/usr/bin/env bash
#
# test_prepare_minor.sh: scripts/dev/prepare-minor.sh without --push, in a
# throwaway repo. The repo gets a copy of scripts/ with a trimmed component
# list (appended to components.sh, like test_check_no_prerelease.sh), a `main`
# and a `prerelease` branch, and no remote: MAIN_REF=main points the script
# at the local branch.
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo="$(cd "$here/../.." && pwd)"

tmp_root="$(mktemp -d)"
trap 'rm -rf "$tmp_root"' EXIT
tmp=""

fail() { echo "FAIL: $1"; exit 1; }

# Fresh repo: main at v0.11.3, prerelease ahead of it with -pre versions.
setup() {
  tmp="$tmp_root/repo"
  rm -rf "$tmp"
  mkdir -p "$tmp"
  cp -r "$repo/scripts" "$tmp/scripts"
  rm -rf "$tmp/scripts/tests"
  find "$tmp/scripts" -name __pycache__ -prune -exec rm -rf {} +
  # The component changelogs of the real tree may mention pre-releases.
  find "$tmp/scripts" -name CHANGELOG.md -delete
  cat >> "$tmp/scripts/version/components.sh" <<'LIST'
COMPONENTS=(
  "dashboard/:dashboard/VERSION"
  "services/:services/VERSION"
  "services/shelly/:services/shelly/manifest.json"
)
CHANGELOG_TARGETS=(
  dashboard service:shelly
)
LIST

  git -C "$tmp" init -q -b main
  git -C "$tmp" config user.name test
  git -C "$tmp" config user.email test@example.invalid
  mkdir -p "$tmp/dashboard" "$tmp/services/shelly"
  echo v0.11.3 > "$tmp/dashboard/VERSION"
  echo v0.4.6 > "$tmp/services/VERSION"
  printf '{\n  "service_id": "shelly",\n  "version": "0.4.6"\n}\n' > "$tmp/services/shelly/manifest.json"
  git -C "$tmp" add -A
  git -C "$tmp" commit -q -m "feat(dashboard): base"

  git -C "$tmp" checkout -q -b prerelease
  echo v0.11.4-pre.2 > "$tmp/dashboard/VERSION"
  echo v0.4.7-pre.2 > "$tmp/services/VERSION"
  printf '{\n  "service_id": "shelly",\n  "version": "0.4.7-pre.1"\n}\n' > "$tmp/services/shelly/manifest.json"
  echo "prerelease work" > "$tmp/dashboard/feature.txt"
  git -C "$tmp" add -A
  git -C "$tmp" commit -q -m "feat(dashboard): prerelease work"
}

# Runs prepare-minor.sh in the throwaway repo; sets $code and $out.
prepare() {
  code=0
  out="$(cd "$tmp" && MAIN_REF=main PR_AI_MODEL="Sonnet 5.5" bash scripts/dev/prepare-minor.sh "$@" 2>&1)" || code=$?
}

snapshot() { echo "$(git -C "$tmp" rev-parse HEAD) $(git -C "$tmp" status --porcelain)"; }

# --- happy path: vX.Y.0 without --push --------------------------------------
setup
before="$(git -C "$tmp" rev-parse HEAD)"
prepare v0.12.0
[ "$code" -eq 0 ] || fail "v0.12.0: expected exit 0, got $code: $out"

[ "$(cat "$tmp/dashboard/VERSION")" = "v0.12.0" ] || fail "dashboard/VERSION is '$(cat "$tmp/dashboard/VERSION")', expected v0.12.0"
[ "$(cat "$tmp/services/VERSION")" = "v0.4.7" ] || fail "services/VERSION is '$(cat "$tmp/services/VERSION")', expected v0.4.7"
grep -q '"version": "0.4.7"' "$tmp/services/shelly/manifest.json" \
  || fail "manifest not stripped to 0.4.7: $(cat "$tmp/services/shelly/manifest.json")"

leftover="$(grep -rl --include=CHANGELOG.md -e '-pre' "$tmp" || true)"
[ -z "$leftover" ] || fail "a CHANGELOG.md still mentions -pre: $leftover"
grep -q -E '^## v0\.12\.0( |$)' "$tmp/dashboard/CHANGELOG.md" \
  || fail "dashboard/CHANGELOG.md has no '## v0.12.0' heading"

[ -z "$(git -C "$tmp" status --porcelain)" ] || fail "working tree not clean after prepare"
[ "$(git -C "$tmp" rev-list --count "$before"..HEAD)" -eq 1 ] || fail "expected exactly one new commit"
[ "$(git -C "$tmp" log -1 --format=%s)" = "chore(release): prepare v0.12.0" ] \
  || fail "commit subject is '$(git -C "$tmp" log -1 --format=%s)'"
git -C "$tmp" log -1 --format=%B | grep -q -i -x 'Co-authored-by: Claude Sonnet 5.5 <noreply@anthropic.com>' \
  || fail "commit lacks the Sonnet 5.5 trailer"

# --- rejections: exit non-zero, nothing changed -------------------------------
setup
before="$(snapshot)"
prepare v0.12.1
[ "$code" -ne 0 ] || fail "v0.12.1 (not vX.Y.0) was accepted"
[ "$(snapshot)" = "$before" ] || fail "v0.12.1 rejection changed the repo"

setup
git -C "$tmp" checkout -q main
before="$(snapshot)"
prepare v0.12.0
[ "$code" -ne 0 ] || fail "wrong branch (main) was accepted"
[ "$(snapshot)" = "$before" ] || fail "wrong branch rejection changed the repo"

setup
before="$(snapshot)"
prepare v0.11.0
[ "$code" -ne 0 ] || fail "v0.11.0 with main at v0.11.3 was accepted"
[ "$(snapshot)" = "$before" ] || fail "v0.11.0 rejection changed the repo"

setup
echo "uncommitted" >> "$tmp/dashboard/VERSION"
before="$(snapshot)"
prepare v0.12.0
[ "$code" -ne 0 ] || fail "dirty tree was accepted"
[ "$(snapshot)" = "$before" ] || fail "dirty tree rejection changed the repo"

echo "ok"
