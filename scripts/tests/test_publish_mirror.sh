#!/usr/bin/env bash
set -euo pipefail
here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo="$(git -C "$here" rev-parse --show-toplevel)"

tmp="$(mktemp -d)"
wt=""
cleanup() {
  [[ -n "$wt" ]] && git -C "$repo" worktree remove --force "$wt" >/dev/null 2>&1
  rm -rf "$tmp"
}
trap cleanup EXIT
git -C "$tmp" init -q
mkdir -p "$tmp/keep"; echo x > "$tmp/keep/x"; git -C "$tmp" add -A; git -C "$tmp" -c user.email=t@t -c user.name=t commit -qm init

bash "$repo/scripts/publish_mirror.sh" --component battery_soc --mirror-path "$tmp" --version 9.9.9 --dry-run

test -f "$tmp/custom_components/battery_soc/manifest.json"
grep -q '"version": "9.9.9"' "$tmp/custom_components/battery_soc/manifest.json"
test -f "$tmp/hacs.json"
test -f "$tmp/AI-DISCLAIMER.md"
test -f "$tmp/.github/pull_request_template.md"
test -f "$tmp/custom_components/battery_soc/brand/icon.png"
test -f "$tmp/docs/img/IntegrationDemo.png"
test -f "$tmp/docs/integration.md"
test -f "$tmp/.github/workflows/release.yml"
# the shared field descriptions are rendered from the service schema
grep -q '\[%schema:' "$repo/integrations/homeassistant/custom_components/battery_soc/strings.json"
if grep -rqE '\[%schema:[a-z0-9_]+%\]' "$tmp/custom_components"; then
  echo "mirror still has [%schema:...%] placeholders"; exit 1
fi
grep -q '"bank_a_capacity_ah": "Capacity' "$tmp/custom_components/battery_soc/translations/en.json" \
  || { echo "bank_a_capacity_ah description not rendered"; exit 1; }
# dry-run must NOT create a commit or tag
[ -z "$(git -C "$tmp" tag)" ] || { echo "dry-run created a tag"; exit 1; }
echo "OK battery_soc"

# Clean up for next test
rm -rf "$tmp/custom_components" "$tmp/.github" "$tmp/docs"
mkdir -p "$tmp/keep"; echo x > "$tmp/keep/x"; git -C "$tmp" add -A; git -C "$tmp" -c user.email=t@t -c user.name=t commit -qm clean

bash "$repo/scripts/publish_mirror.sh" --component energy_node_icons --mirror-path "$tmp" --version 9.9.9 --dry-run

test -f "$tmp/custom_components/energy_node_icons/manifest.json"
grep -q '"version": "9.9.9"' "$tmp/custom_components/energy_node_icons/manifest.json"
test -f "$tmp/hacs.json"
test -f "$tmp/AI-DISCLAIMER.md"
test -f "$tmp/.github/pull_request_template.md"
test -f "$tmp/custom_components/energy_node_icons/brand/icon.png"
test -f "$tmp/docs/integration.md"
# docs/img should NOT be present for energy_node_icons (no DOCS_IMG in release.env)
[ ! -d "$tmp/docs/img" ] || { echo "docs/img should not be present for energy_node_icons"; exit 1; }
test -f "$tmp/docs/icons/solar-panel.svg" || { echo "docs/icons missing for energy_node_icons"; exit 1; }
test -f "$tmp/.github/workflows/release.yml"
# dry-run must NOT create a commit or tag
[ -z "$(git -C "$tmp" tag)" ] || { echo "dry-run created a tag"; exit 1; }
echo "OK energy_node_icons"

# --prerelease: next free beta onto a mirror branch, main and CHANGELOG untouched.
# A bare repo stands in for the mirror's origin, a stub for gh.
rm -rf "${tmp:?}"/*; mkdir -p "$tmp/origin.git" "$tmp/bin"
git -C "$tmp/origin.git" init -q --bare -b main
git clone -q "$tmp/origin.git" "$tmp/mirror" 2>/dev/null
m="$tmp/mirror"
git -C "$m" config user.email t@t; git -C "$m" config user.name t
git -C "$m" checkout -q -b main
echo x > "$m/x"; git -C "$m" add -A; git -C "$m" commit -qm init
git -C "$m" tag v9.9.9-b1
git -C "$m" push -q origin main v9.9.9-b1
cat > "$tmp/bin/gh" <<'GH'
#!/usr/bin/env bash
echo "$*" >> "$GH_LOG"
if [[ "$1 $2" == "run list" ]]; then echo 42; fi
GH
chmod +x "$tmp/bin/gh"
export GH_LOG="$tmp/gh.log"
changelog="integrations/homeassistant/CHANGELOG.md"
changelog_before="$(git -C "$repo" diff --cached -- "$changelog"; git -C "$repo" diff -- "$changelog")"

out="$(bash "$repo/scripts/publish_mirror.sh" --component battery_soc --mirror-path "$m" \
  --version 9.9.9 --prerelease prerelease/test --dry-run)"
grep -q 'would publish v9.9.9-b2 to mirror branch prerelease/test' <<<"$out" \
  || { echo "dry run did not report the next beta"; exit 1; }
[ "$(git -C "$m" rev-parse --abbrev-ref HEAD)" = main ] || { echo "dry run switched the branch"; exit 1; }
git -C "$m" reset -q --hard; git -C "$m" clean -qfd

PATH="$tmp/bin:$PATH" bash "$repo/scripts/publish_mirror.sh" --component battery_soc \
  --mirror-path "$m" --version 9.9.9 --prerelease prerelease/test >/dev/null

git -C "$tmp/origin.git" rev-parse -q --verify refs/tags/v9.9.9-b2 >/dev/null || { echo "beta tag not pushed"; exit 1; }
git -C "$tmp/origin.git" show prerelease/test:custom_components/battery_soc/manifest.json \
  | grep -q '"version": "9.9.9-b2"' || { echo "beta manifest version wrong"; exit 1; }
[ "$(git -C "$tmp/origin.git" rev-list --count main)" = 1 ] || { echo "mirror main was touched"; exit 1; }
[ "$(git -C "$m" rev-parse --abbrev-ref HEAD)" = main ] || { echo "mirror not switched back to main"; exit 1; }
grep -q 'workflow run release.yml .*--ref prerelease/test .*-f tag=v9.9.9-b2 .*-f prerelease=true' "$GH_LOG" \
  || { echo "release workflow not started as pre-release"; cat "$GH_LOG"; exit 1; }
[ "$(git -C "$repo" diff --cached -- "$changelog"; git -C "$repo" diff -- "$changelog")" = "$changelog_before" ] \
  || { echo "pre-release touched the monorepo changelog"; exit 1; }

# a second beta continues the branch; a released version and main are refused
PATH="$tmp/bin:$PATH" bash "$repo/scripts/publish_mirror.sh" --component battery_soc \
  --mirror-path "$m" --version 9.9.9 --prerelease prerelease/test >/dev/null 2>&1
git -C "$tmp/origin.git" rev-parse -q --verify refs/tags/v9.9.9-b3 >/dev/null || { echo "second beta not b3"; exit 1; }
[ "$(git -C "$tmp/origin.git" rev-list --count prerelease/test)" = 3 ] || { echo "second beta did not continue the branch"; exit 1; }
if bash "$repo/scripts/publish_mirror.sh" --component battery_soc --mirror-path "$m" \
    --version 9.9.9 --prerelease main --dry-run >/dev/null 2>&1; then
  echo "--prerelease main was accepted"; exit 1
fi
git -C "$m" tag v9.9.9
if bash "$repo/scripts/publish_mirror.sh" --component battery_soc --mirror-path "$m" \
    --version 9.9.9 --prerelease prerelease/test --dry-run >/dev/null 2>&1; then
  echo "beta of a released version was accepted"; exit 1
fi
echo "OK prerelease"

# --release with a --version above the manifest: the version goes into the
# monorepo manifest before the changelog is built, so the changelog section
# and the release notes carry it. Runs in a throwaway worktree (with this
# checkout's scripts) because --release stages files in the monorepo.
wt="$(mktemp -d)"
git -C "$repo" worktree add -q --detach "$wt" HEAD
cp "$repo/scripts/publish_mirror.sh" "$repo/scripts/generate_changelog.sh" "$wt/scripts/"
git -C "$m" checkout -q main
: > "$GH_LOG"
PATH="$tmp/bin:$PATH" bash "$wt/scripts/publish_mirror.sh" --component battery_soc \
  --mirror-path "$m" --version 9.10.0 --release >/dev/null
wt_manifest="integrations/homeassistant/custom_components/battery_soc/manifest.json"
git -C "$wt" diff --cached -- "$wt_manifest" | grep -q '^+  "version": "9.10.0"' \
  || { echo "manifest version not set and staged"; exit 1; }
grep -q '^## v9.10.0 ' "$wt/$changelog" || { echo "changelog section not named v9.10.0"; exit 1; }
git -C "$tmp/origin.git" rev-parse -q --verify refs/tags/v9.10.0 >/dev/null || { echo "release tag not pushed"; exit 1; }
grep -q 'workflow run release.yml .*-f tag=v9.10.0 .*-f notes=' "$GH_LOG" \
  || { echo "release workflow not started for v9.10.0"; cat "$GH_LOG"; exit 1; }
grep -q '^### ' "$GH_LOG" || { echo "release notes not taken from the changelog"; cat "$GH_LOG"; exit 1; }

# A release whose changelog has no section for its version aborts before
# anything is tagged or pushed.
git -C "$wt" reset -q --hard
cp "$repo/scripts/publish_mirror.sh" "$wt/scripts/"
cat > "$wt/scripts/generate_changelog.sh" <<'NOOP'
#!/usr/bin/env bash
exit 0
NOOP
if PATH="$tmp/bin:$PATH" bash "$wt/scripts/publish_mirror.sh" --component battery_soc \
    --mirror-path "$m" --version 9.11.0 --release >/dev/null 2>&1; then
  echo "release without a changelog section was accepted"; exit 1
fi
git -C "$tmp/origin.git" rev-parse -q --verify refs/tags/v9.11.0 >/dev/null \
  && { echo "tag pushed despite missing release notes"; exit 1; }
git -C "$m" rev-parse -q --verify refs/tags/v9.11.0 >/dev/null \
  && { echo "tag created despite missing release notes"; exit 1; }
echo "OK release"
