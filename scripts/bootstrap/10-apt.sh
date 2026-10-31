#!/usr/bin/env bash
#
# Schritt 10: Systempakete (INSTALLATION.md §2 und §4).
#
# Bewusst knapp gehalten: hier stehen nur Pakete, ohne die kein spaeterer
# Schritt laufen kann. Alles, was ein optionaler Dienst braucht, bringt
# dessen eigener Schritt mit.
# Eingaben fuer den Fingerabdruck (scripts/build/lib/manifest.sh): solange
# sie gleich bleiben, ueberspringt ein Update diesen Schritt.
# step-inputs: bootstrap/lib/step.sh
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
# DEBIAN_FRONTEND hinter sudo, env_reset verwirft es sonst.
# --no-install-recommends: python3-pip empfiehlt build-essential und
# python3-dev, also die komplette gcc-Toolchain (rund 400 MB). Der Node
# kompiliert nichts, Schritt 50 installiert nur fertige Wheels.
"${SUDO[@]}" env DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends "${missing[@]}" \
  || step_fail APT_INSTALL_FAILED
# Die geladenen .deb-Dateien braucht niemand mehr, auf kleinen Karten sind
# das schnell ueber 100 MB. Ein Fehler hier ist kein Grund zum Abbruch.
"${SUDO[@]}" apt-get clean || step_log "apt-get clean ist fehlgeschlagen, der Paketcache bleibt liegen."

step_ok
