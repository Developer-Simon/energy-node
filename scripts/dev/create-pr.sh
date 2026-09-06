#!/usr/bin/env bash
#
# Oeffnet einen Pull Request fuer eine Aenderung an energy-node.
#
# `main` ist seit dem Initial-Push geschuetzt: Aenderungen laufen nur noch
# ueber PRs (Branch-Protection, lin'eare History, Squash-Merge). Dieses Skript
# nimmt die aktuelle Arbeit, schiebt sie auf einen Branch und macht den PR auf.
#
# Der PR-Body kommt aus `.github/PULL_REQUEST_TEMPLATE.md`. Der Code-Block
# darin ist die spaetere Squash-Merge-Commit-Message: das Skript fuellt ihn
# aus den Branch-Commits vor, oeffnet ihn im Editor und uebernimmt die erste
# Zeile als PR-Titel (Titel == Squash-Subject == Conventional-Commits-Subject).
#
# Usage:
#   scripts/dev/create-pr.sh <branch-name> ["PR-Titel"]
#
#   <branch-name>  Ziel-Branch. Bist du schon auf einem Branch != main, wird
#                  dieser genutzt und das Argument nur zur Kontrolle geprueft.
#   "PR-Titel"     Optional. Ohne Angabe wird der Subject des letzten Commits
#                  (bzw. bei mehreren Commits deren Liste) verwendet.
#
# Umgebungsvariablen:
#   PR_SKIP_EDIT=1  Editor-Schritt ueberspringen (nicht-interaktiv / CI).
#
# Voraussetzungen: committe deine Arbeit, bevor du das Skript aufrufst - es
# verschiebt Branches, aber committet nichts fuer dich.

set -euo pipefail

branch="${1:-}"
title="${2:-}"

if [[ -z "${branch}" ]]; then
  echo "usage: $0 <branch-name> [\"PR-Titel\"]" >&2
  exit 2
fi

REPO_ROOT="$(git rev-parse --show-toplevel)"
cd "${REPO_ROOT}"

current="$(git branch --show-current)"

if [[ "${branch}" == "main" ]]; then
  echo "Ziel-Branch darf nicht 'main' sein." >&2
  exit 2
fi

if [[ "${current}" == "main" ]]; then
  # Uncommittete Aenderungen wandern mit auf den neuen Branch, bereits auf
  # main liegende Commits ebenfalls (main wird hier nicht zurueckgesetzt -
  # das macht der PR-Merge bzw. ein spaeterer `git reset` von Hand).
  git switch -c "${branch}"
elif [[ "${current}" != "${branch}" ]]; then
  echo "Du bist auf '${current}', angefordert wurde '${branch}'." >&2
  echo "Wechsle selbst auf den richtigen Branch oder rufe ohne Namen erneut auf." >&2
  exit 1
fi

git fetch origin main --quiet

if git diff --quiet "origin/main...HEAD"; then
  echo "Keine Commits gegenueber origin/main auf '${branch}'." >&2
  echo "Committe deine Arbeit und starte das Skript erneut." >&2
  exit 1
fi

git push -u origin "${branch}"

TEMPLATE="${REPO_ROOT}/.github/PULL_REQUEST_TEMPLATE.md"

# Ohne Template: altes Verhalten (Body aus Commit-Infos).
if [[ ! -f "${TEMPLATE}" ]]; then
  echo "Kein PR-Template gefunden, nutze gh --fill." >&2
  [[ -n "${title}" ]] || title="$(git log -1 --pretty=%s)"
  gh pr create --base main --head "${branch}" --title "${title}" --fill
  gh pr view --web
  exit 0
fi

# --- Squash-Merge-Message aus den Branch-Commits vorbefuellen ------------------

mapfile -t commits < <(git log --reverse --format='%H' "origin/main..HEAD")

msg_file="$(mktemp)"
body_file="$(mktemp)"
trap 'rm -f "${msg_file}" "${body_file}"' EXIT

if [[ "${#commits[@]}" -eq 1 ]]; then
  # Ein Commit: Subject + Body unveraendert uebernehmen.
  git log -1 --format='%s%n%n%b' "${commits[0]}" > "${msg_file}"
else
  # Mehrere Commits: Titel als Subject, Commit-Subjects als Body-Liste.
  subject="${title:-$(git log -1 --format='%s' "${commits[0]}")}"
  {
    printf '%s\n\n' "${subject}"
    git log --reverse --format='- %s' "origin/main..HEAD"
  } > "${msg_file}"
fi

# Den ersten Code-Block im Template durch die vorbefuellte Message ersetzen.
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

# --- Editor-Schritt ----------------------------------------------------------

if [[ -z "${PR_SKIP_EDIT:-}" && -t 0 && -t 1 ]]; then
  editor="$(git var GIT_EDITOR)"
  eval "${editor} \"${body_file}\""
fi

# --- Titel aus der ersten Zeile des Code-Blocks ziehen ----------------------

subject_line="$(awk '/^```/ { c++; next } c == 1 && NF { print; exit }' "${body_file}")"
if [[ -n "${subject_line}" ]]; then
  title="${subject_line}"
elif [[ -z "${title}" ]]; then
  title="$(git log -1 --pretty=%s)"
fi

gh pr create --base main --head "${branch}" --title "${title}" --body-file "${body_file}"
gh pr view --web
