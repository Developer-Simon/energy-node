#!/usr/bin/env bash
#
# Schritt 10: Systempakete (INSTALLATION.md §2 und §4).
#
# Bewusst knapp gehalten: hier stehen nur Pakete, ohne die kein spaeterer
# Schritt laufen kann. Alles, was ein optionaler Dienst braucht, bringt
# dessen eigener Schritt mit.
set -euo pipefail
# shellcheck source=scripts/bootstrap/lib/step.sh
source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/lib/step.sh"

PACKAGES=(mosquitto mosquitto-clients ufw python3-pip ca-certificates)

step_begin 10
if step_done 10; then
  step_skip "bereits erledigt"
  exit 0
fi

missing=()
for pkg in "${PACKAGES[@]}"; do
  if ! dpkg-query -W -f='${Status}' "${pkg}" 2>/dev/null | grep -q 'ok installed'; then
    missing+=("${pkg}")
  fi
done

if [[ "${#missing[@]}" -eq 0 ]]; then
  step_log "Alle benoetigten Pakete sind bereits installiert."
  step_ok
  exit 0
fi

step_log "Fehlende Pakete: ${missing[*]}"
"${SUDO[@]}" apt-get update -qq || step_fail APT_UPDATE_FAILED
DEBIAN_FRONTEND=noninteractive "${SUDO[@]}" apt-get install -y "${missing[@]}" \
  || step_fail APT_INSTALL_FAILED

step_ok
