#!/usr/bin/env bash
#
# Print the section of a CHANGELOG.md that belongs to one version, for use
# as GitHub Release notes (see .github/workflows/release.yml).
#
# Usage: extract-changelog.sh <changelog-file> <version>
#   <version> may be given with or without a leading "v".
#
# Section headings look like:  ## v0.5.13 (2026-09-06)
# The section runs from its heading (exclusive) to the next "## " heading
# (exclusive); surrounding blank lines are trimmed.
#
# Exit status: 0 printed · 1 usage/IO error · 2 no section for that version.
set -euo pipefail

if [ "$#" -ne 2 ]; then
  echo "usage: $0 <changelog-file> <version>" >&2
  exit 1
fi

file=$1
version=${2#v}

if [ ! -r "$file" ]; then
  echo "$0: cannot read $file" >&2
  exit 1
fi

section=$(
  awk -v ver="$version" '
    BEGIN { gsub(/\./, "\\.", ver) }              # dots are literal
    $0 ~ "^## v?" ver "([ )]|$)" { grab = 1; next }
    /^## / { grab = 0 }
    grab {
      if (started || NF) { started = 1; buf = buf $0 ORS }
    }
    END {
      sub(/\n+$/, "", buf)                        # trim trailing blank lines
      if (buf != "") print buf
    }
  ' "$file"
)

if [ -z "$section" ]; then
  echo "$0: no changelog section for version $version in $file" >&2
  exit 2
fi

printf '%s\n' "$section"
