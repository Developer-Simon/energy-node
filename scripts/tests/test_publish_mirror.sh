#!/usr/bin/env bash
set -euo pipefail
here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo="$(git -C "$here" rev-parse --show-toplevel)"

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
git -C "$tmp" init -q
mkdir -p "$tmp/keep"; echo x > "$tmp/keep/x"; git -C "$tmp" add -A; git -C "$tmp" -c user.email=t@t -c user.name=t commit -qm init

bash "$repo/scripts/publish_mirror.sh" --mirror-path "$tmp" --version 9.9.9 --dry-run

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
echo "OK"
