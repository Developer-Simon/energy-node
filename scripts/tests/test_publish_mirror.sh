#!/usr/bin/env bash
set -euo pipefail
here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo="$(git -C "$here" rev-parse --show-toplevel)"

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
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
# dry-run must NOT create a commit or tag
[ -z "$(git -C "$tmp" tag)" ] || { echo "dry-run created a tag"; exit 1; }
echo "OK energy_node_icons"
