#!/usr/bin/env bash
#
# Schritt 50: Python-Abhaengigkeiten aus dem Bundle (INSTALLATION.md 3.2,
# E11 der Spec).
#
# --no-index ist der Kern: der Node loest nichts auf. Alles, was gebraucht
# wird, hat der Bau-Rechner aus piwheels geholt und liegt unter wheels/.
# Eingaben fuer den Fingerabdruck (scripts/build/lib/manifest.sh): solange
# sie gleich bleiben, ueberspringt ein Update diesen Schritt.
# Hat das Manifest Festlegungen (manifest.wheels), holt lib/wheels.sh die
# Wheels, die fehlen, vom Node selbst. Ein altes Bundle ohne das Feld
# installiert wie bisher nur die Wheels aus dem Bundle.
# step-inputs: bootstrap/lib/step.sh bootstrap/lib/wheels.sh bootstrap/verify_bundle.sh wheels/ manifest:wheels
set -euo pipefail
# shellcheck source=scripts/bootstrap/lib/step.sh
source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/lib/step.sh"
# shellcheck source=scripts/bootstrap/lib/wheels.sh
source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/lib/wheels.sh"

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
find_links=(--find-links "${EN_BUNDLE_DIR}/wheels")
WHEELS_DOWNLOADED=0

if wheels_declared; then
  # Festlegungen: fehlende Wheels holen, dann pip nur die gelisteten Dateien
  # aus dem Node-Verzeichnis geben, dazu die eigenen Wheels aus dem Bundle.
  # Eine Bundle-Kopie derselben Festlegung entfaellt, sonst bekaeme pip zwei.
  wheels_sync
  find_links+=(--find-links "${EN_WHEELS_DIR}")
  pinned_files=()
  mapfile -t pinned_files < <(wheel_pins | cut -f1)
  declare -A listed=()
  for file in "${pinned_files[@]}"; do
    listed["${file}"]=1
  done
  own=()
  for wheel in "${wheels[@]}"; do
    name="$(basename -- "${wheel}")"
    [[ -n "${listed[${name}]+x}" ]] || own+=("${wheel}")
  done
  wheels=("${own[@]}")
  for file in "${pinned_files[@]}"; do
    wheels+=("${EN_WHEELS_DIR}/${file}")
  done
fi
[[ "${#wheels[@]}" -gt 0 ]] || step_fail WHEELS_MISSING

if [[ "${WHEELS_DOWNLOADED}" -gt 0 ]]; then
  step_log "Installiere ${#wheels[@]} Wheels, ${WHEELS_DOWNLOADED} davon eben vom Node geladen."
else
  step_log "Installiere ${#wheels[@]} Wheels aus dem Bundle, ohne Netz."
fi

log="$(mktemp)"
trap 'rm -f "${log}"' EXIT

pip_install() {
  "${SUDO[@]}" "${PIP[@]}" install \
    --no-index \
    "${find_links[@]}" \
    --ignore-installed \
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
