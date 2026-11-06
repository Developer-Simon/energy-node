#!/usr/bin/env bash
#
# Prepares the minor release vX.Y.0 on the prerelease line.
#
# Runs on the checked-out `prerelease` branch, on a clean tree:
#   1. Syncs prerelease with main (scripts/version/sync_prerelease.py).
#   2. Strips the -pre suffix from every component version
#      (scripts/version/prerelease.py strip). dashboard/VERSION becomes the
#      argument instead.
#   3. Regenerates the changelogs for the release, so the open section reads
#      "## vX.Y.Z" and no "-pre" heading remains.
#   4. Commits everything as one commit "chore(release): prepare vX.Y.0".
#
# Without --push the script stops there. With --push it pushes prerelease and
# opens the PR into main through scripts/dev/create-pr.sh, which takes the
# same commit-message block (PR_MESSAGE_FILE) as every other PR.
#
# Refuses (exit 2, nothing changed): an argument that is not vX.Y.0, a version
# not above main's dashboard version, a branch other than prerelease, a dirty
# tree, a missing PR_AI_MODEL. A failure after the sync rolls the branch back to
# the commit it had after the sync.
#
# Usage:
#   scripts/dev/prepare-minor.sh vX.Y.0 [--push]
#
# Environment variables:
#   PR_AI_MODEL=…      Model for the commit trailer, e.g. "Sonnet 5.5". Required,
#                      as in create-pr.sh.
#   MAIN_REF=…         Ref of main for the sync and the version check. Default
#                      origin/main.

set -Eeuo pipefail

# shellcheck source=scripts/dev/pr-message-lib.sh
source "$(dirname "${BASH_SOURCE[0]}")/pr-message-lib.sh"

DASHBOARD_VERSION_FILE="dashboard/VERSION"

usage() {
  echo "usage: $0 vX.Y.0 [--push]" >&2
  exit 2
}

# version_key vX.Y.Z -> comparable integer
version_key() {
  local v="${1#v}" major minor patch
  IFS=. read -r major minor patch <<< "${v}"
  printf '%s\n' $(( major * 1000000 + minor * 1000 + patch ))
}

# write_version <file> <vX.Y.Z>: VERSION files get the v, JSON manifests do not
# (same JSON layout as write_json_version in scripts/version/bump-patch.sh).
write_version() {
  local file="$1" value="$2"
  if [[ "${file}" == *.json ]]; then
    python3 -c '
import json, sys
path, version = sys.argv[1], sys.argv[2]
with open(path) as fh:
    data = json.load(fh)
data["version"] = version
with open(path, "w") as fh:
    json.dump(data, fh, indent=2)
    fh.write("\n")
' "${file}" "${value#v}"
  else
    printf '%s\n' "${value}" > "${file}"
  fi
}

version="${1:-}"
[[ -n "${version}" ]] || usage
push=false
case "${2:-}" in
  "") ;;
  --push) push=true ;;
  *) usage ;;
esac
[[ $# -le 2 ]] || usage

if [[ ! "${version}" =~ ^v[0-9]+\.[0-9]+\.0$ ]]; then
  echo "prepare-minor: '${version}' is not vX.Y.0." >&2
  exit 2
fi

REPO_ROOT="$(git rev-parse --show-toplevel)"
cd "${REPO_ROOT}"

current="$(git branch --show-current)"
if [[ "${current}" != "prerelease" ]]; then
  echo "prepare-minor: must run on branch prerelease, HEAD is '${current:-detached}'." >&2
  exit 2
fi

if [[ -n "$(git status --porcelain)" ]]; then
  echo "prepare-minor: working tree is not clean." >&2
  exit 2
fi

if [[ -z "${PR_AI_MODEL:-}" ]]; then
  echo "prepare-minor: set PR_AI_MODEL=\"Sonnet 5.5\" (or Opus x.y) for the commit trailer." >&2
  exit 2
fi

MAIN_REF="${MAIN_REF:-origin/main}"
main_version="$(git show "${MAIN_REF}:${DASHBOARD_VERSION_FILE}" 2> /dev/null | tr -d '[:space:]' || true)"
if [[ ! "${main_version}" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
  echo "prepare-minor: cannot read ${DASHBOARD_VERSION_FILE} on ${MAIN_REF}." >&2
  exit 2
fi
if (( $(version_key "${version}") <= $(version_key "${main_version}") )); then
  echo "prepare-minor: ${version} is not above ${MAIN_REF} (${main_version})." >&2
  exit 2
fi

# Commit message and PR body, built before anything is changed.
title="chore(release): prepare ${version}"
tmp_dir="$(mktemp -d)"
trap 'rm -rf "${tmp_dir}"' EXIT
body_file="${tmp_dir}/body"
commit_file="${tmp_dir}/commit"
printf '%s\n\n%s\n' "${title}" "Strips the -pre suffix from every component version, sets ${DASHBOARD_VERSION_FILE} to ${version} and regenerates the changelogs for the release." \
  | pr_normalize_message "${title}" "${PR_AI_MODEL}" > "${body_file}"
if ! grep -q -E "${PR_TRAILER_RE}" "${body_file}"; then
  echo "prepare-minor: PR_AI_MODEL '${PR_AI_MODEL}' does not name a valid Claude model." >&2
  exit 2
fi
{ printf '%s\n\n' "${title}"; cat "${body_file}"; } > "${commit_file}"

# --- Sync -------------------------------------------------------------------

echo "prepare-minor: syncing prerelease with ${MAIN_REF}"
python3 "${REPO_ROOT}/scripts/version/sync_prerelease.py" --main "${MAIN_REF}"

# --- From here on a failure rolls back to this commit -----------------------

start="$(git rev-parse HEAD)"
rollback() {
  echo "prepare-minor: failed, resetting to ${start}" >&2
  git reset -q --hard "${start}"
  git clean -fdq
}
trap rollback ERR

# --- Strip the versions -----------------------------------------------------

# shellcheck source=scripts/version/components.sh
source "${REPO_ROOT}/scripts/version/components.sh"

for entry in "${COMPONENTS[@]}"; do
  version_file="${entry#*:}"
  [[ -f "${version_file}" ]] || continue
  current_version="$(read_component_version "${version_file}")"
  [[ -n "${current_version}" ]] || continue
  next_version="$(python3 "${REPO_ROOT}/scripts/version/prerelease.py" strip "${current_version}")"
  if [[ "${version_file}" == "${DASHBOARD_VERSION_FILE}" ]]; then
    next_version="${version}"
  fi
  if [[ "${next_version}" != "${current_version}" ]]; then
    write_version "${version_file}" "${next_version}"
    echo "prepare-minor: ${version_file} ${current_version} -> ${next_version}"
  fi
done

"${REPO_ROOT}/scripts/version/check_no_prerelease.sh"

# The changelog heading comes from the committed versions, so the versions go
# in first and the generated changelogs are amended into the same commit.
git add -A
git commit -q --allow-empty -F "${commit_file}"

for target in "${CHANGELOG_TARGETS[@]}"; do
  "${REPO_ROOT}/scripts/generate_changelog.sh" "${target}" > /dev/null
done

git add -A
while IFS= read -r changelog; do
  if grep -q -E '^## v[0-9]+\.[0-9]+\.[0-9]+-pre\.' "${changelog}"; then
    echo "prepare-minor: ${changelog} still has a -pre heading." >&2
    rollback
    exit 1
  fi
done < <(git diff --cached --name-only "${start}" -- '*CHANGELOG.md')

git commit -q --amend -F "${commit_file}"
trap - ERR

echo "prepare-minor: committed ${title}"

# --- Push and PR ------------------------------------------------------------

if [[ "${push}" == true ]]; then
  PR_BASE=main PR_MESSAGE_FILE="${body_file}" PR_SKIP_EDIT=1 PR_AI_MODEL="${PR_AI_MODEL}" \
    "${REPO_ROOT}/scripts/dev/create-pr.sh" prerelease "${title}"
fi
