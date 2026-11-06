#!/usr/bin/env bash
#
# Merges a prerelease PR into main as a MERGE commit (not a squash). This is
# the only way the prerelease line lands on main, so the history of the minor
# release stays visible. scripts/dev/merge-pr.sh keeps squashing all other PRs.
#
# Refuses (exit 2, nothing merged) unless the PR is open, its head branch is
# `prerelease` and its base is `main`. The message is checked like merge-pr.sh
# checks it: the fenced block of the PR description is the commit BODY, the PR
# title is the SUBJECT, the last line is a `Co-authored-by: Claude Sonnet|Opus`
# trailer, never Haiku.
#
# Usage:
#   scripts/dev/merge-prerelease.sh <pr-number>

set -euo pipefail

# shellcheck source=scripts/dev/pr-message-lib.sh
source "$(dirname "${BASH_SOURCE[0]}")/pr-message-lib.sh"

if [[ $# -ne 1 || -z "${1}" ]]; then
  echo "usage: $0 <pr-number>" >&2
  exit 2
fi
pr="${1}"

tmp="$(mktemp -d)"
trap 'rm -rf "${tmp}"' EXIT

head_ref="$(gh pr view "${pr}" --json headRefName --jq '.headRefName')"
base_ref="$(gh pr view "${pr}" --json baseRefName --jq '.baseRefName')"
state="$(gh pr view "${pr}" --json state --jq '.state')"
title="$(gh pr view "${pr}" --json title --jq '.title')"
gh pr view "${pr}" --json body --jq '.body' > "${tmp}/body"

if [[ "${head_ref}" != "prerelease" ]]; then
  echo "PR #${pr} comes from '${head_ref}', only prerelease PRs are merged here." >&2
  exit 2
fi
if [[ "${base_ref}" != "main" ]]; then
  echo "PR #${pr} targets '${base_ref}', the prerelease line only merges into main." >&2
  exit 2
fi
if [[ "${state}" != "OPEN" ]]; then
  echo "PR #${pr} is ${state}, only open PRs are merged." >&2
  exit 2
fi

pr_extract_block "${tmp}/body" > "${tmp}/message"
if [[ ! -s "${tmp}/message" ]]; then
  echo "PR #${pr} has no fenced commit message block in its description." >&2
  exit 2
fi

if ! pr_validate_message "${title}" "${tmp}/message"; then
  echo "PR #${pr}: fix the description (or the title) and run again." >&2
  exit 2
fi

echo "PR #${pr}: ${title} (#${pr}), ${head_ref} into ${base_ref}"
echo "----"
cat "${tmp}/message"
echo "----"

gh pr merge "${pr}" --merge --subject "${title} (#${pr})" --body-file "${tmp}/message"
