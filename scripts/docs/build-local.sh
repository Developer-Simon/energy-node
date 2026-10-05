#!/usr/bin/env bash
# Builds the documentation site like GitHub Pages does, into <repo>/_site.
#
# Usage: scripts/docs/build-local.sh [--pagefind]
#
# Runs Jekyll with the github-pages gem in a Ruby container (podman or
# docker), so no Ruby is needed on the host. Gems are cached in
# <repo>/.cache/docs-gems. The base URL is /energy-node like on GitHub
# Pages, so serve it from a parent directory:
#   mkdir -p .cache/serve && ln -sfn ../../_site .cache/serve/energy-node
#   python3 -m http.server -d .cache/serve 4000   # http://localhost:4000/energy-node/
# --pagefind also builds the search index with npx pagefind, as the Pages
# workflow does.
set -euo pipefail

root="$(cd "$(dirname "$0")/../.." && pwd)"
engine="$(command -v podman || command -v docker || true)"
[ -n "$engine" ] || { echo "podman or docker required" >&2; exit 1; }

mkdir -p "$root/.cache/docs-gems" "$root/_site"
# Override the URL in the Jekyll config so local redirects point at the local server
# instead of https://github.com, allowing deep-link tests to work
printf 'url: "%s"\n' "${DOCS_LOCAL_URL:-http://localhost:4000}" > "$root/.cache/docs-local.yml"
"$engine" run --rm \
  -v "$root:/repo:Z" \
  -v "$root/.cache/docs-gems:/usr/local/bundle:Z" \
  -w /repo/docs \
  -e JEKYLL_ENV=production \
  -e PAGES_REPO_NWO=Developer-Simon/energy-node \
  docker.io/library/ruby:3.3 \
  sh -c 'bundle install --quiet && bundle exec jekyll build --source /repo/docs --destination /repo/_site --baseurl /energy-node --config _config.yml,/repo/.cache/docs-local.yml'

if [ "${1:-}" = "--pagefind" ]; then
  npx --yes pagefind@1.3.0 --site "$root/_site"
fi
