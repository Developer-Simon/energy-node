#!/usr/bin/env bash
#
# Bumps the per-component patch version for every component whose files changed
# on the current branch relative to a base ref -- unless that component's
# version file was already bumped on the branch. Idempotent: a second run is a
# no-op.
#
# `main` is PR-protected, so nothing ever commits there directly. This script
# carries the patch bump the squash-merge used to do; it runs on the PR branch
# from .github/workflows/version-bump.yml and can also be run by hand on the
# branch (commit your changes first -- it diffs committed history, base...HEAD).
#
# Components, their version files and the "bare semver in a JSON field" special
# case for manifest.json all come from scripts/version/components.sh.
#
# Usage:
#   scripts/version/bump-patch.sh [--check] [base-ref]
#
#   --check    Write nothing; exit 1 if a bump is needed but missing.
#   base-ref   What to diff against. Defaults to origin/main.
set -euo pipefail

check_only=0
if [[ "${1:-}" == "--check" ]]; then
  check_only=1
  shift
fi
base_ref="${1:-origin/main}"

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(git rev-parse --show-toplevel)"
cd "${repo_root}"
source "${script_dir}/components.sh"

if ! git rev-parse --verify --quiet "${base_ref}" >/dev/null; then
  echo "bump-patch: base ref '${base_ref}' not found" >&2
  exit 2
fi

changed_files="$(git diff --name-only "${base_ref}...HEAD")"

# Current "version" field of a component as of an arbitrary ref, normalised to
# "vX.Y.Z" exactly like components.sh read_component_version does for the
# working tree. Returns non-zero if the file does not exist at that ref.
read_version_from_ref() {
  local ref="$1" version_file="$2" raw
  raw="$(git show "${ref}:${version_file}" 2>/dev/null)" || return 1
  if [[ "${version_file}" == *.json ]]; then
    local v
    v="$(printf '%s' "${raw}" | python3 -c 'import json, sys
try:
    print(json.load(sys.stdin).get("version", ""))
except Exception:
    pass')"
    [[ -n "${v}" ]] || return 1
    printf 'v%s\n' "${v}"
  else
    printf '%s' "${raw}" | tr -d '[:space:]'
  fi
}

write_plain_version() {
  local version_file="$1" major="$2" minor="$3" patch="$4"
  printf 'v%s.%s.%s\n' "${major}" "${minor}" "${patch}" > "${version_file}"
}

# HACS/hassfest wants a bare semver (no "v") in manifest.json's "version" field.
write_json_version() {
  local version_file="$1" version="$2"
  python3 -c '
import json, sys
path, version = sys.argv[1], sys.argv[2]
with open(path) as fh:
    data = json.load(fh)
data["version"] = version
with open(path, "w") as fh:
    json.dump(data, fh, indent=2)
    fh.write("\n")
' "${version_file}" "${version}"
}

needed=0

for component in "${COMPONENTS[@]}"; do
  dir_prefix="${component%%:*}"
  version_file="${component#*:}"

  component_touched "${dir_prefix}" "${version_file}" "${changed_files}" || continue

  if [[ ! -f "${version_file}" ]]; then
    echo "bump-patch: ${version_file} missing, skipping" >&2
    continue
  fi

  current="$(read_component_version "${version_file}")"
  base_version="$(read_version_from_ref "${base_ref}" "${version_file}" || true)"

  # The version file does not exist at the base ref: the component is new on
  # this branch, or a rename moved its version file to a path the base does not
  # carry yet. Either way it already holds the version it was created with --
  # never bump it. Without this, an unreadable base version reads as "not yet
  # bumped" and every workflow run bumps it again; because each push re-triggers
  # the workflow, that is an infinite loop of bump commits.
  if [[ -z "${base_version}" ]]; then
    continue
  fi

  # Already bumped on this branch: leave it alone (keeps the script idempotent
  # and lets a hand-picked major/minor bump on the branch survive).
  if [[ "${current}" != "${base_version}" ]]; then
    continue
  fi

  if [[ ! "${current}" =~ ^v([0-9]+)\.([0-9]+)\.([0-9]+)$ ]]; then
    echo "bump-patch: ${version_file} has unexpected format '${current}', skipping" >&2
    continue
  fi
  major="${BASH_REMATCH[1]}"
  minor="${BASH_REMATCH[2]}"
  patch="$(( ${BASH_REMATCH[3]} + 1 ))"
  needed=1

  if [[ "${check_only}" -eq 1 ]]; then
    echo "bump-patch: ${version_file} needs a bump -> v${major}.${minor}.${patch}"
    continue
  fi

  if [[ "${version_file}" == *.json ]]; then
    write_json_version "${version_file}" "${major}.${minor}.${patch}"
  else
    write_plain_version "${version_file}" "${major}" "${minor}" "${patch}"
  fi
  git add "${version_file}"
  echo "bump-patch: ${version_file} -> v${major}.${minor}.${patch}"
done

if [[ "${check_only}" -eq 1 && "${needed}" -eq 1 ]]; then
  exit 1
fi
exit 0
