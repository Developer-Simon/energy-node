#!/usr/bin/env bash
# Prints a Markdown table (key | de | en) of the catalog keys that are new
# on this branch compared with a base ref (default origin/main). Used for the
# PR description of the localization migration.
set -euo pipefail
base="${1:-origin/main}"
root="$(git rev-parse --show-toplevel)"
dir="dashboard/internal/webui/catalogs"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
git show "$base:$dir/de.json" > "$tmp/base.json"
"$root/.venv/bin/python" - "$tmp/base.json" "$root/$dir/de.json" "$root/$dir/en.json" <<'PY'
import json, sys
base, de, en = (json.load(open(p, encoding="utf-8")) for p in sys.argv[1:4])
esc = lambda s: s.replace("|", "\\|").replace("\n", " ")
print("| key | de | en |\n|---|---|---|")
for key in sorted(set(de) - set(base)):
    print(f"| `{key}` | {esc(de[key])} | {esc(en.get(key, ''))} |")
PY
