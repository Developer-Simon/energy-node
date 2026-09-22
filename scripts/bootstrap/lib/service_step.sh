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
# shellcheck source=scripts/bootstrap/lib/render.sh
source "${SERVICE_STEP_LIB_DIR}/render.sh"

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

# Muss diese Unit neu starten? Die Regel steht in restart_rule.py (eine
# Stelle fuer Schritt, Vorschau und Dashboard). Leere Antwort = nein.
service_restart_reason() {
  python3 "${SERVICE_STEP_LIB_DIR}/restart_rule.py" \
    "${EN_BUNDLE_DIR}" "${EN_STATE_DIR}" "$1"
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

  # Die Unit liegt im Bundle als Vorlage; hier wird sie fuer den Zielbenutzer
  # gerendert (siehe lib/render.sh).
  local rendered
  rendered="$(mktemp)"
  if ! render_unit_as "${src}/${unit}" "${rendered}" "${EN_TARGET_USER}" "${EN_TARGET_BASE}"; then
    rm -f "${rendered}"
    step_fail TARGET_INVALID
  fi
  "${SUDO[@]}" mkdir -p "${EN_ROOT}/etc/systemd/system"
  "${SUDO[@]}" install -m 0644 "${rendered}" \
    "${EN_ROOT}/etc/systemd/system/${unit}" || { rm -f "${rendered}"; step_fail SERVICE_UNIT_FAILED; }
  rm -f "${rendered}"
  "${SUDO[@]}" systemctl daemon-reload
  # enable statt enable --now, und restart nur, wenn die Regel es verlangt:
  # ein Update laesst laufende Dienste in Ruhe, die sich nicht geaendert haben
  # (restart_rule.py). Eine gestoppte Unit wird immer gestartet - restart tut
  # das ebenfalls, ein Lauf ist also fuer Erstinstallation und Update derselbe.
  # Der Koerper laeuft nur einmal je Bundle-Version (step_done).
  local reason
  reason="$(service_restart_reason "${id}")"
  "${SUDO[@]}" systemctl enable "${unit}" || step_fail SERVICE_START_FAILED
  if [[ -n "${reason}" ]] || ! "${SUDO[@]}" systemctl is-active --quiet "${unit}"; then
    "${SUDO[@]}" systemctl restart "${unit}" || step_fail SERVICE_START_FAILED
    step_log "Dienst ${dir} eingerichtet und neu gestartet (${unit}, ${reason:-war nicht aktiv})."
  else
    step_log "Dienst ${dir} eingerichtet (${unit}), unveraendert - kein Neustart."
  fi
  step_ok
}
