#!/usr/bin/env bash
#
# Squash-merges a PR with the commit message from its description.
#
# The fenced block of the PR description is the commit BODY, the PR title is
# the SUBJECT (GitHub's merge button would use the commit list instead). Before
# merging, the message is checked: no title line in the body, last line is a
# `Co-authored-by: Claude Sonnet|Opus|Fable x.y <noreply@anthropic.com>`
# trailer, never Haiku.
#
# Usage:
#   scripts/dev/merge-pr.sh [<pr-number>]    default: the PR of the current branch
#   scripts/dev/merge-pr.sh --check [<pr>]   only validate, do not merge
#
# Environment: PR_MERGE_AUTO=1 uses `gh pr merge --auto` (waits for checks).

set -euo pipefail

source "$(dirname "${BASH_SOURCE[0]}")/pr-message-lib.sh"

check_only=0
if [[ "${1:-}" == "--check" ]]; then check_only=1; shift; fi
pr="${1:-}"

tmp="$(mktemp -d)"
trap 'rm -rf "${tmp}"' EXIT

view=(gh pr view)
[[ -n "${pr}" ]] && view+=("${pr}")
"${view[@]}" --json number,title,body --jq '.number' > "${tmp}/number"
"${view[@]}" --json number,title,body --jq '.title' > "${tmp}/title"
"${view[@]}" --json number,title,body --jq '.body' > "${tmp}/body"
pr="$(cat "${tmp}/number")"
title="$(cat "${tmp}/title")"

pr_extract_block "${tmp}/body" > "${tmp}/message"
if [[ ! -s "${tmp}/message" ]]; then
  echo "PR #${pr} has no fenced commit message block in its description." >&2
  exit 2
fi

if ! pr_validate_message "${title}" "${tmp}/message"; then
  echo "PR #${pr}: fix the description (or the title) and run again." >&2
  exit 2
fi

echo "PR #${pr}: ${title} (#${pr})"
echo "----"
cat "${tmp}/message"
echo "----"
[[ "${check_only}" -eq 1 ]] && { echo "Message is valid."; exit 0; }

args=(--squash --subject "${title} (#${pr})" --body-file "${tmp}/message")
[[ -n "${PR_MERGE_AUTO:-}" ]] && args+=(--auto)
gh pr merge "${pr}" "${args[@]}"
