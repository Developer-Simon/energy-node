#!/usr/bin/env bash
# Decides whether a push needs a GitHub Pages redeploy of docs/.
#
# Usage: pages-should-deploy.sh <docs-dir> < changed-files
#
# Reads repo-relative changed paths (one per line) on stdin and prints
# "true" or "false". Only documents listed in <docs-dir>/_data/nav.yml count,
# plus the site's own build inputs (config, data, layouts, includes, images,
# assets) and the deploy tooling. Changes to documents outside the navigation
# (e.g. the dashboard's lazy-asset notes, touched on almost every frontend PR)
# do not trigger a deploy on their own. They are published with the next one.
set -euo pipefail

docs_dir="${1:?usage: pages-should-deploy.sh <docs-dir> < changed-files}"
nav="$docs_dir/_data/nav.yml"
[ -f "$nav" ] || { echo "nav file not found: $nav" >&2; exit 1; }

prefix="${docs_dir%/}/"
prefix="${prefix#./}"

# Every `url: "..."` in the navigation, as a repo-relative path.
nav_paths="$(grep -oE 'url: *"[^"]+"' "$nav" | sed -E 's/^url: *"(.*)"$/\1/' | sed "s|^|$prefix|")"

infra_re="^(${prefix}(_config\.yml|_data/|_layouts/|_includes/|images/|assets/|Gemfile)|\.github/workflows/pages\.yml$|scripts/docs/)"

while IFS= read -r path; do
  [ -n "$path" ] || continue
  if grep -qE "$infra_re" <<<"$path" || grep -qxF "$path" <<<"$nav_paths"; then
    echo true
    exit 0
  fi
done

echo false
