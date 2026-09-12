#!/usr/bin/env bash
#
# Gemeinsamer Rumpf aller Python-Dienst-Schritte (81-*.sh bis 88-*.sh).
#
# Jeder dieser Schritte besteht aus genau einem Aufruf:
#   service_step <id> <verzeichnis> <unit>
# Was die Dienste unterscheidet, steht im Bundle - welche .py-Dateien, welche
# Geraetedateien, welche Unit -, nicht im Code. Deshalb eine Funktion und
# sechs dreizeilige Aufrufer statt sechs fast gleicher Skripte.

SERVICE_STEP_LIB_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=scripts/bootstrap/lib/step.sh
source "${SERVICE_STEP_LIB_DIR}/step.sh"

# Geraetedateien zerfallen in zwei Klassen. Ausgeliefert und damit bei jedem
# Lauf erneuert: *.schema.json und *_presets.json. Alles andere
# (*_devices.json, automation_rules.json) sind Betreiberdaten, die das
# Dashboard live bearbeitet - die werden nur angelegt, wenn sie fehlen.
service_step_is_artefact() {
  case "$1" in
    *.schema.json|*_presets.json) return 0 ;;
    *) return 1 ;;
  esac
}

# Achtung: step_fail beendet den Prozess. Das ist gewollt - jedes NN-*.sh
# ruft service_step genau einmal und hat danach nichts mehr zu tun.
service_step() {
  local id="$1" dir="$2" unit="$3"

  step_begin "${id}"
  if ! step_selected "${id}"; then
    step_skip "nicht ausgewaehlt"
    return 0
  fi
  if step_done "${id}"; then
    step_skip "bereits erledigt"
    return 0
  fi

  local src="${EN_BUNDLE_DIR}/services/${dir}"
  [[ -d "${src}" && -f "${src}/${unit}" ]] || step_fail SERVICE_SOURCE_MISSING

  # Quellen und Geraetedateien gehoeren dem Zielbenutzer und liegen unter
  # dessen Heimatverzeichnis - hier braucht es kein sudo. Nur die Unit und
  # systemctl weiter unten sind privilegiert.
  local target="${EN_ROOT}${EN_TARGET_BASE}/${dir}"
  local devices="${EN_ROOT}${EN_TARGET_BASE}/devices"
  mkdir -p "${target}" "${devices}"

  local file name
  for file in "${src}"/*.py; do
    [[ -f "${file}" ]] || continue
    install -m 0644 "${file}" "${target}/"
  done

  for file in "${src}"/devices/*.json; do
    [[ -f "${file}" ]] || continue
    name="$(basename "${file}")"
    if service_step_is_artefact "${name}"; then
      install -m 0644 "${file}" "${devices}/${name}"
    elif [[ ! -e "${devices}/${name}" ]]; then
      install -m 0644 "${file}" "${devices}/${name}"
      step_log "Vorlage angelegt: ${name}"
    else
      step_log "Betreiberdatei bleibt unangetastet: ${name}"
    fi
  done

  "${SUDO[@]}" mkdir -p "${EN_ROOT}/etc/systemd/system"
  "${SUDO[@]}" install -m 0644 "${src}/${unit}" \
    "${EN_ROOT}/etc/systemd/system/${unit}" || step_fail SERVICE_UNIT_FAILED
  "${SUDO[@]}" systemctl daemon-reload
  "${SUDO[@]}" systemctl enable --now "${unit}" || step_fail SERVICE_START_FAILED

  step_log "Dienst ${dir} eingerichtet (${unit})."
  step_ok
}
