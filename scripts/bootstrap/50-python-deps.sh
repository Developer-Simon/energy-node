#!/usr/bin/env bash
#
# Schritt 50: Python-Abhaengigkeiten aus dem Bundle (INSTALLATION.md 3.2,
# E11 der Spec).
#
# --no-index ist der Kern: der Node loest nichts auf. Alles, was gebraucht
# wird, hat der Bau-Rechner aus piwheels geholt und liegt unter wheels/.
set -euo pipefail
# shellcheck source=scripts/bootstrap/lib/step.sh
source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/lib/step.sh"

# EN_PIP existiert der Testbarkeit wegen. python3 selbst zu ueberschatten
# ginge nicht - step_selected braucht denselben Interpreter.
read -r -a PIP <<< "${EN_PIP:-python3 -m pip}"

step_begin 50
if step_done 50; then
  step_skip "bereits erledigt"
  exit 0
fi

# Vorpruefung ohne Schluessel: Signatur und Hashes prueft, wer das Bundle
# entgegennimmt. Hier zaehlt nur, ob Architektur und Python-ABI passen -
# und das muss feststehen, BEVOR das erste Wheel ausgepackt ist.
verify="${EN_BUNDLE_DIR}/bootstrap/verify_bundle.sh"
[[ -f "${verify}" ]] || step_fail BUNDLE_INCOMPLETE
if ! check="$(bash "${verify}" --bundle "${EN_BUNDLE_DIR}" --target-only 2>&1)"; then
  printf '%s\n' "${check}" | sed 's/^/    /'
  step_fail "${check##*FEHLER }"
fi

shopt -s nullglob
wheels=("${EN_BUNDLE_DIR}"/wheels/*.whl)
shopt -u nullglob
[[ "${#wheels[@]}" -gt 0 ]] || step_fail WHEELS_MISSING

step_log "Installiere ${#wheels[@]} Wheels aus dem Bundle, ohne Netz."

log="$(mktemp)"
trap 'rm -f "${log}"' EXIT

pip_install() {
  "${SUDO[@]}" "${PIP[@]}" install \
    --no-index \
    --find-links "${EN_BUNDLE_DIR}/wheels" \
    --upgrade \
    "$@" \
    "${wheels[@]}"
}

# Die Ausgabe wird eingerueckt weitergereicht: eine pip-Zeile, die zufaellig
# mit ## begaenne, saehe sonst aus wie ein Marker.
show_log() { sed 's/^/    /' "${log}"; }

if ! pip_install --break-system-packages >"${log}" 2>&1; then
  if grep -q 'no such option' "${log}"; then
    step_log "Legacy-Image ohne --break-system-packages; zweiter Versuch ohne den Schalter."
    if ! pip_install >"${log}" 2>&1; then
      show_log
      if grep -q 'externally-managed-environment' "${log}"; then
        step_fail PIP_EXTERNALLY_MANAGED
      fi
      step_fail PIP_INSTALL_FAILED
    fi
  else
    show_log
    if grep -q 'externally-managed-environment' "${log}"; then
      step_fail PIP_EXTERNALLY_MANAGED
    fi
    step_fail PIP_INSTALL_FAILED
  fi
fi

step_ok
