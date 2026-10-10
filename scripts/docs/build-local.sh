#!/usr/bin/env bash
# Builds the documentation site like GitHub Pages does, into <repo>/_site.
#
# Usage: scripts/docs/build-local.sh [--pagefind] [--serve]
#
# Runs Jekyll with the github-pages gem in a Ruby container (podman or
# docker), so no Ruby is needed on the host. Gems are cached in
# <repo>/.cache/docs-gems. The base URL is /energy-node like on GitHub
# Pages, so serve it from a parent directory:
#   mkdir -p .cache/serve && ln -sfn ../../_site .cache/serve/energy-node
#   python3 -m http.server -d .cache/serve 4000   # http://localhost:4000/energy-node/
# --pagefind also builds the search index with npx pagefind, as the Pages
# workflow does. --serve does the two serve steps above after the build and
# keeps the server running until Ctrl-C.
set -euo pipefail

pagefind=0
serve=0
for arg in "$@"; do
  case "$arg" in
    --pagefind) pagefind=1 ;;
    --serve) serve=1 ;;
    *) echo "unknown option: $arg" >&2; exit 2 ;;
  esac
done

root="$(cd "$(dirname "$0")/../.." && pwd)"
engine="$(command -v podman || command -v docker || true)"
[ -n "$engine" ] || { echo "podman or docker required" >&2; exit 1; }

mkdir -p "$root/.cache/docs-gems" "$root/_site"
# Override the URL in the Jekyll config so local redirects point at the local server
# instead of https://github.com, allowing deep-link tests to work
local_url="${DOCS_LOCAL_URL:-http://localhost:4000}"
printf 'url: "%s"\n' "$local_url" > "$root/.cache/docs-local.yml"
"$engine" run --rm \
  -v "$root:/repo:Z" \
  -v "$root/.cache/docs-gems:/usr/local/bundle:Z" \
  -w /repo/docs \
  -e JEKYLL_ENV=production \
  -e PAGES_REPO_NWO=Developer-Simon/energy-node \
  docker.io/library/ruby:3.3 \
  sh -c 'bundle install --quiet && bundle exec jekyll build --source /repo/docs --destination /repo/_site --baseurl /energy-node --config _config.yml,/repo/.cache/docs-local.yml'

if [ "$pagefind" = 1 ]; then
  npx --yes pagefind@1.3.0 --site "$root/_site"
fi

site_url="${local_url%/}/energy-node/"
port="${local_url##*:}"
port="${port%%/*}"
echo
echo "Built into $root/_site"
if [ "$serve" = 1 ]; then
  mkdir -p "$root/.cache/serve"
  ln -sfn ../../_site "$root/.cache/serve/energy-node"
  echo "Serving at $site_url (Ctrl-C to stop)"
  exec python3 -m http.server -d "$root/.cache/serve" "$port"
fi
echo "Serve it with: scripts/docs/build-local.sh --serve, or"
echo "  python3 -m http.server -d $root/.cache/serve $port"
echo "Then open $site_url"
