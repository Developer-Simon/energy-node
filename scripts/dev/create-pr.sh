#!/usr/bin/env bash
#
# Opens a pull request for a change to energy-node.
#
# `main` has been protected since the initial push: changes only land through
# PRs (branch protection, linear history, squash merge). This script takes the
# current work, pushes it onto a branch and opens the PR.
#
# The PR body comes from `.github/PULL_REQUEST_TEMPLATE.md`. The script fills
# every placeholder itself, nothing is left to be maintained by hand later.
# The fenced block is the squash-merge commit BODY: the subject is the PR title,
# so the block never repeats it, and it always ends with a
# `Co-authored-by: Claude <Sonnet|Opus> x.y <noreply@anthropic.com>` trailer
# (Haiku is rejected). `scripts/dev/merge-pr.sh` merges with exactly that block.
#
# Usage:
#   scripts/dev/create-pr.sh [<branch-name>] ["PR title"]
#
#   <branch-name>  Target branch. If you are already on a branch != main it is
#                  used and the argument is only checked for consistency.
#                  Without an argument the current branch is used - after a
#                  confirmation prompt (non-interactive: needs PR_ASSUME_YES=1).
#   "PR title"     Conventional Commits subject (or PR_TITLE). Without it the
#                  subject of the last commit is used.
#
# Environment variables (all optional except PR_AI_MODEL when no commit or
# message file carries a valid Claude trailer):
#   PR_AI_MODEL=…      Model that did the work, e.g. "Sonnet 5.5" or "Opus 5.5".
#                      Becomes the trailer and the AI disclosure line.
#   PR_MESSAGE_FILE=…  Squash-merge body without the title ("-" = stdin). Replaces
#                      the pre-fill from the branch commits and skips the editor.
#   PR_SUMMARY=…       1-3 sentences for the reviewer. Default: first paragraph
#                      of the message.
#   PR_SUITES=…        Test suites that were run, e.g. "pytest, go test, smoke".
#                      Without it that checkbox stays unchecked.
#   PR_ISSUE=…         Linked issue, e.g. "#42". Default: from `Closes #N`.
#   PR_SKIP_EDIT=1     Skip the editor step (non-interactive / CI).
#   PR_ASSUME_YES=1    Confirm the "use current branch" prompt without a TTY.
#
# Prerequisite: commit your work before calling the script - it moves branches
# around but does not commit anything for you.

set -euo pipefail

source "$(dirname "${BASH_SOURCE[0]}")/pr-message-lib.sh"

branch="${1:-}"
title="${2:-${PR_TITLE:-}}"

if [[ -n "${PR_MESSAGE_FILE:-}" && "${PR_MESSAGE_FILE}" != "-" && ! -f "${PR_MESSAGE_FILE}" ]]; then
  echo "PR_MESSAGE_FILE does not point at a file: ${PR_MESSAGE_FILE}" >&2
  exit 2
fi

REPO_ROOT="$(git rev-parse --show-toplevel)"
cd "${REPO_ROOT}"

current="$(git branch --show-current)"

# No branch name given: fall back to the current branch, but confirm first.
if [[ -z "${branch}" ]]; then
  if [[ -z "${current}" || "${current}" == "main" ]]; then
    echo "usage: $0 [<branch-name>] [\"PR title\"]" >&2
    echo "(no branch name given and HEAD is '${current:-detached}')" >&2
    exit 2
  fi
  if [[ -t 0 && -t 1 ]]; then
    read -r -p "No branch name given. Open a PR for the current branch '${current}'? [y/N] " reply
    case "${reply}" in
      [yY] | [yY][eE][sS]) ;;
      *) echo "Aborted." >&2; exit 1 ;;
    esac
  elif [[ -z "${PR_ASSUME_YES:-}" ]]; then
    echo "No branch name given and no TTY to confirm." >&2
    echo "Set PR_ASSUME_YES=1 to use the current branch '${current}'." >&2
    exit 2
  fi
  branch="${current}"
fi

if [[ "${branch}" == "main" ]]; then
  echo "Target branch must not be 'main'." >&2
  exit 2
fi

if [[ "${current}" == "main" ]]; then
  # Uncommitted changes move onto the new branch, commits already on main do
  # too (main is not reset here - the PR merge or a later manual `git reset`
  # takes care of that).
  git switch -c "${branch}"
elif [[ "${current}" != "${branch}" ]]; then
  echo "You are on '${current}', but '${branch}' was requested." >&2
  echo "Switch to the right branch yourself or call again without a name." >&2
  exit 1
fi

git fetch origin main --quiet

if git diff --quiet "origin/main...HEAD"; then
  echo "No commits against origin/main on '${branch}'." >&2
  echo "Commit your work and start the script again." >&2
  exit 1
fi

TEMPLATE="${REPO_ROOT}/.github/PULL_REQUEST_TEMPLATE.md"

msg_file="$(mktemp)"
body_file="$(mktemp)"
trap 'rm -f "${msg_file}" "${body_file}" "${msg_file}.raw"' EXIT

# --- Title ---------------------------------------------------------------

[[ -n "${title}" ]] || title="$(git log -1 --format='%s' origin/main..HEAD)"

# --- Squash-merge body (no title, Claude trailer last) ---------------------

if [[ -n "${PR_MESSAGE_FILE:-}" ]]; then
  if [[ "${PR_MESSAGE_FILE}" == "-" ]]; then
    cat > "${msg_file}.raw"
  else
    cat "${PR_MESSAGE_FILE}" > "${msg_file}.raw"
  fi
  if [[ ! -s "${msg_file}.raw" ]]; then
    echo "PR_MESSAGE_FILE is empty." >&2
    exit 2
  fi
else
  mapfile -t commits < <(git log --reverse --format='%H' "origin/main..HEAD")
  if [[ "${#commits[@]}" -eq 1 ]]; then
    git log -1 --format='%b' "${commits[0]}" > "${msg_file}.raw"
  else
    git log --reverse --format='- %s' "origin/main..HEAD" > "${msg_file}.raw"
  fi
fi

pr_normalize_message "${title}" "${PR_AI_MODEL:-}" < "${msg_file}.raw" > "${msg_file}"

if ! grep -q -E "${PR_TRAILER_RE}" "${msg_file}"; then
  echo "No Claude trailer: set PR_AI_MODEL=\"Sonnet 5.5\" (or Opus x.y)." >&2
  exit 2
fi

# --- Derived fields ----------------------------------------------------------

ai_model="$(grep -E "${PR_TRAILER_RE}" "${msg_file}" | sed -E 's/^Co-authored-by: (Claude [^<]+) <.*/\1/')"

summary="${PR_SUMMARY:-}"
if [[ -z "${summary}" ]]; then
  summary="$(awk 'NF { p = 1; print; next } p { exit }' "${msg_file}" | grep -v -E '^Co-authored-by:' || true)"
fi
[[ -n "${summary}" ]] || summary="${title}"

issue="${PR_ISSUE:-}"
if [[ -z "${issue}" ]]; then
  issue="$(grep -o -i -E '(closes|fixes|resolves) #[0-9]+' "${msg_file}" | head -n 1 | sed -E 's/.* //' || true)"
fi
[[ -n "${issue}" ]] || issue="none"

check_title=" "
[[ "${title}" =~ ${PR_CC_RE} ]] && check_title="x"

check_secrets=" "
if "${REPO_ROOT}/scripts/dev/check_tracked_secrets.sh" > /dev/null 2>&1; then
  check_secrets="x"
fi

check_suites=" "
suites="${PR_SUITES:-}"
if [[ -n "${suites}" ]]; then check_suites="x"; else suites="not stated"; fi

# --- Fill the template -------------------------------------------------------

if [[ ! -f "${TEMPLATE}" ]]; then
  cp "${msg_file}" "${body_file}"
else
  MESSAGE="$(cat "${msg_file}")" SUMMARY="${summary}" SUITES="${suites}" ISSUE="${issue}" \
  AI_MODEL="${ai_model}" CHECK_TITLE="${check_title}" CHECK_SECRETS="${check_secrets}" \
  CHECK_AI="x" CHECK_SUITES="${check_suites}" \
  awk '
    {
      line = $0
      while (match(line, /\{\{[A-Z_]+\}\}/)) {
        key = substr(line, RSTART + 2, RLENGTH - 4)
        line = substr(line, 1, RSTART - 1) ENVIRON[key] substr(line, RSTART + RLENGTH)
      }
      print line
    }
  ' "${TEMPLATE}" > "${body_file}"
fi

# --- Editor step ----------------------------------------------------------

if [[ -z "${PR_SKIP_EDIT:-}" && -z "${PR_MESSAGE_FILE:-}" && -t 0 && -t 1 ]]; then
  editor="$(git var GIT_EDITOR)"
  eval "${editor} \"${body_file}\""
fi

# The edited block must still satisfy the rules, otherwise stop before opening.
pr_extract_block "${body_file}" > "${msg_file}"
pr_validate_message "${title}" "${msg_file}" || exit 2

git push -u origin "${branch}"
gh pr create --base main --head "${branch}" --title "${title}" --body-file "${body_file}"
gh pr view --web
