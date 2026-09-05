#!/usr/bin/env bash
#
# Installs this repo's tracked git hooks (git-hooks/) into .git/hooks/, since
# git does not version hooks itself. Re-run after pulling changes under
# git-hooks/.
set -euo pipefail

repo_root="$(git -C "$(dirname "${BASH_SOURCE[0]}")" rev-parse --show-toplevel)"
hooks_source="${repo_root}/git-hooks"
hooks_target="$(git -C "${repo_root}" rev-parse --git-path hooks)"

for hook in "${hooks_source}"/*; do
  name="$(basename "${hook}")"
  ln -sf "${hook}" "${hooks_target}/${name}"
  echo "installed ${name} -> ${hooks_target}/${name}"
done
