#!/usr/bin/env bash
#
# Opens a pull request for a change to energy-node.
#
# `main` has been protected since the initial push: changes only land through
# PRs (branch protection, linear history, squash merge). This script takes the
# current work, pushes it onto a branch and opens the PR.
#
# The PR body comes from `.github/PULL_REQUEST_TEMPLATE.md`. The fenced code
# block in it is the eventual squash-merge commit message: the script pre-fills
# it from the branch commits, opens it in the editor and takes the first line
# as the PR title (title == squash subject == Conventional Commits subject).
# Alternatively `PR_MESSAGE_FILE` supplies the message ready-made as a file (or
# via stdin) - that skips both the commit pre-fill and the editor.
#
# Usage:
#   scripts/dev/create-pr.sh [<branch-name>] ["PR title"]
#
#   <branch-name>  Target branch. If you are already on a branch != main it is
#                  used and the argument is only checked for consistency.
#                  Without an argument the current branch is used - after a
#                  confirmation prompt (non-interactive: needs PR_ASSUME_YES=1).
#   "PR title"     Optional. Without it the subject of the last commit (or, for
#                  multiple commits, their list) is used.
#
# Environment variables:
#   PR_SKIP_EDIT=1     Skip the editor step (non-interactive / CI).
#   PR_ASSUME_YES=1    Confirm the "use current branch" prompt without a TTY.
#   PR_MESSAGE_FILE=…  File with the ready-made squash-merge commit message
#                      (subject + blank line + body, without a code fence).
#                      Replaces the pre-fill from the branch commits and skips
#                      the editor; the first line becomes the PR title.
#                      "-" reads the message from stdin.
#
# Prerequisite: commit your work before calling the script - it moves branches
# around but does not commit anything for you.

set -euo pipefail

branch="${1:-}"
title="${2:-}"

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

git push -u origin "${branch}"

TEMPLATE="${REPO_ROOT}/.github/PULL_REQUEST_TEMPLATE.md"

msg_file="$(mktemp)"
body_file="$(mktemp)"
trap 'rm -f "${msg_file}" "${body_file}"' EXIT

# --- Determine the squash-merge message ------------------------------------

if [[ -n "${PR_MESSAGE_FILE:-}" ]]; then
  # Ready-made message from a file (or stdin for "-"); no editor step.
  if [[ "${PR_MESSAGE_FILE}" == "-" ]]; then
    cat > "${msg_file}"
  else
    cat "${PR_MESSAGE_FILE}" > "${msg_file}"
  fi
  if [[ ! -s "${msg_file}" ]]; then
    echo "PR_MESSAGE_FILE is empty." >&2
    exit 2
  fi
else
  # Pre-fill from the branch commits.
  mapfile -t commits < <(git log --reverse --format='%H' "origin/main..HEAD")
  if [[ "${#commits[@]}" -eq 1 ]]; then
    # One commit: take subject + body unchanged.
    git log -1 --format='%s%n%n%b' "${commits[0]}" > "${msg_file}"
  else
    # Multiple commits: title as subject, commit subjects as a body list.
    subject="${title:-$(git log -1 --format='%s' "${commits[0]}")}"
    {
      printf '%s\n\n' "${subject}"
      git log --reverse --format='- %s' "origin/main..HEAD"
    } > "${msg_file}"
  fi
fi

# Without a template: body straight from the message (or the old --fill path).
if [[ ! -f "${TEMPLATE}" ]]; then
  if [[ -n "${PR_MESSAGE_FILE:-}" ]]; then
    title="$(awk 'NF { print; exit }' "${msg_file}")"
    gh pr create --base main --head "${branch}" --title "${title}" --body-file "${msg_file}"
  else
    echo "No PR template found, using gh --fill." >&2
    [[ -n "${title}" ]] || title="$(git log -1 --pretty=%s)"
    gh pr create --base main --head "${branch}" --title "${title}" --fill
  fi
  gh pr view --web
  exit 0
fi

# Replace the first code block in the template with the pre-filled message.
awk -v msgfile="${msg_file}" '
  state == 0 && /^```/ {
    print
    while ((getline line < msgfile) > 0) print line
    state = 1
    next
  }
  state == 1 && /^```/ { print; state = 2; next }
  state == 1 { next }
  { print }
' "${TEMPLATE}" > "${body_file}"

# --- Editor step ----------------------------------------------------------

if [[ -z "${PR_SKIP_EDIT:-}" && -z "${PR_MESSAGE_FILE:-}" && -t 0 && -t 1 ]]; then
  editor="$(git var GIT_EDITOR)"
  eval "${editor} \"${body_file}\""
fi

# --- Pull the title from the first line of the code block ----------------

subject_line="$(awk '/^```/ { c++; next } c == 1 && NF { print; exit }' "${body_file}")"
if [[ -n "${subject_line}" ]]; then
  title="${subject_line}"
elif [[ -z "${title}" ]]; then
  title="$(git log -1 --pretty=%s)"
fi

gh pr create --base main --head "${branch}" --title "${title}" --body-file "${body_file}"
gh pr view --web
