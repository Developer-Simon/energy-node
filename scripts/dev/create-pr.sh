#!/usr/bin/env bash
#
# Oeffnet einen Pull Request fuer eine Aenderung an energy-node.
#
# `main` ist seit dem Initial-Push geschuetzt: Aenderungen laufen nur noch
# ueber PRs (Branch-Protection, lin'eare History, Squash-Merge). Dieses Skript
# nimmt die aktuelle Arbeit, schiebt sie auf einen Branch und macht den PR auf.
#
# Usage:
#   scripts/dev/create-pr.sh <branch-name> ["PR-Titel"]
#
#   <branch-name>  Ziel-Branch. Bist du schon auf einem Branch != main, wird
#                  dieser genutzt und das Argument nur zur Kontrolle geprueft.
#   "PR-Titel"     Optional. Ohne Angabe wird der Subject des letzten Commits
#                  verwendet.
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

if [[ -z "${title}" ]]; then
  title="$(git log -1 --pretty=%s)"
fi

gh pr create --base main --head "${branch}" --title "${title}" --fill
gh pr view --web
