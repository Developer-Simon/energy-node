#!/usr/bin/env bash
#
# Refuses prerelease versions (-pre.N) in the component version files of the
# worktree. release.yml runs it for final tags (no "-" in the tag name), so a
# final release never ships a prerelease version.
#
# Prints one line per finding, "<file>: <version>", and exits 1 if there is
# any; exits 0 when every component carries a final version.
#
# Usage:
#   scripts/version/check_no_prerelease.sh
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo="$(cd "${script_dir}/../.." && pwd)"
# shellcheck source=scripts/version/components.sh
source "${script_dir}/components.sh"

cd "${repo}"
found=0
for entry in "${COMPONENTS[@]}"; do
  version_file="${entry#*:}"
  [[ -f "${version_file}" ]] || continue
  version="$(read_component_version "${version_file}")"
  if [[ "${version}" == *-pre.* ]]; then
    printf '%s: %s\n' "${version_file}" "${version}"
    found=1
  fi
done

exit "${found}"
