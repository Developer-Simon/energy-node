#!/usr/bin/env bash
#
# Schritt 60: Dashboard-Binary, Unit, System-Action-Helfer, Sudoers-Regel,
# zentrale Konfiguration, Dienst-Manifeste und Geheimnisse
# (INSTALLATION.md 6).
#
# Das ist die Zusammenfassung dessen, was deploy_dashboard_to_remote.sh und
# die drei ensure_remote_*.sh heute ueber SSH tun - nur eben auf dem Node
# und aus dem Bundle statt aus dem Arbeitsverzeichnis.
#
# Passwoerter kommen ausschliesslich als Pfad zu einer 0600-Datei. Ein
# Argument mit dem Passwort selbst stuende in /proc/<pid>/cmdline.
set -euo pipefail
# shellcheck source=scripts/bootstrap/lib/step.sh
source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/lib/step.sh"

BINARY_NAME="energy-node-dashboard"
MQTT_PW_FILE=""
ADMIN_PW_FILE=""
while [[ $# -gt 0 ]]; do
  case "$1" in
    --mqtt-password-file) MQTT_PW_FILE="${2:-}"; shift 2 ;;
    --admin-password-file) ADMIN_PW_FILE="${2:-}"; shift 2 ;;
    *) shift ;;
  esac
done

step_begin 60
if step_done 60; then
  step_skip "bereits erledigt"
  exit 0
fi

dash="${EN_BUNDLE_DIR}/dashboard"
[[ -f "${dash}/${BINARY_NAME}" ]] || step_fail DASHBOARD_BINARY_MISSING
cfg_template="${EN_BUNDLE_DIR}/config/config.json"
[[ -f "${cfg_template}" ]] || step_fail CONFIG_TEMPLATE_MISSING

shopt -s nullglob
manifests=("${EN_BUNDLE_DIR}"/config/manifests/*.json)
shopt -u nullglob
[[ "${#manifests[@]}" -gt 0 ]] || step_fail MANIFESTS_MISSING

# Eigentuemer und Gruppe nur setzen, wenn wir sie setzen koennen. Der Test
# laeuft als normaler Benutzer mit EN_SUDO=""; ein hartes -o root machte
# dort jeden Lauf zum Fehler, ohne irgendetwas zu beweisen.
install_owned() {
  local mode="$1" src="$2" dst="$3"
  if [[ "${#SUDO[@]}" -gt 0 || "$(id -u)" -eq 0 ]]; then
    "${SUDO[@]}" install -o root -g "${EN_TARGET_USER}" -m "${mode}" "${src}" "${dst}"
  else
    install -m "${mode}" "${src}" "${dst}"
  fi
}

# --- Sudoers zuerst pruefen, dann erst irgendetwas installieren -----------
# visudo laeuft gegen die Datei im Bundle. Andersherum laege im Fehlerfall
# bereits eine kaputte Regel unter /etc/sudoers.d - und die kann sudo
# insgesamt aussperren.
sudoers_src="${dash}/${BINARY_NAME}-system-action.sudoers"
if [[ -f "${sudoers_src}" ]]; then
  "${SUDO[@]}" visudo -cf "${sudoers_src}" >/dev/null || step_fail SUDOERS_INVALID
fi

# --- Laufzeitdateien im Heimatverzeichnis des Zielbenutzers ---------------
target="${EN_ROOT}${EN_TARGET_BASE}/dashboard"
devices="${EN_ROOT}${EN_TARGET_BASE}/devices"
mkdir -p "${target}" "${devices}"
install -m 0755 "${dash}/${BINARY_NAME}" "${target}/${BINARY_NAME}"
if [[ -f "${EN_BUNDLE_DIR}/config/services-VERSION" ]]; then
  install -m 0644 "${EN_BUNDLE_DIR}/config/services-VERSION" "${devices}/VERSION"
fi

# --- systemd, Helfer, Sudoers --------------------------------------------
"${SUDO[@]}" mkdir -p "${EN_ROOT}/etc/systemd/system" "${EN_ROOT}/usr/local/sbin" \
  "${EN_ROOT}/etc/sudoers.d"
"${SUDO[@]}" install -m 0644 "${dash}/${BINARY_NAME}.service" \
  "${EN_ROOT}/etc/systemd/system/${BINARY_NAME}.service"
"${SUDO[@]}" install -m 0755 "${dash}/${BINARY_NAME}-system-action" \
  "${EN_ROOT}/usr/local/sbin/${BINARY_NAME}-system-action"
if [[ -f "${sudoers_src}" ]]; then
  "${SUDO[@]}" install -m 0440 "${sudoers_src}" \
    "${EN_ROOT}/etc/sudoers.d/${BINARY_NAME}-system-action"
fi

# --- zentrale Konfiguration ----------------------------------------------
etc="${EN_ROOT}/etc/energy-node"
"${SUDO[@]}" mkdir -p "${etc}/manifests"
if [[ -e "${etc}/config.json" ]]; then
  step_log "config.json vorhanden, bleibt unangetastet."
else
  install_owned 0664 "${cfg_template}" "${etc}/config.json"
  step_log "config.json aus der Vorlage angelegt."
fi

# Der Manifestsatz wird vollstaendig ersetzt: energy_node_common prueft ihn
# in beide Richtungen gegen den services-Block der config.json, also laesst
# ein uebrig gebliebenes Manifest eines entfernten Dienstes keine Bruecke
# mehr starten.
shipped=" "
for manifest in "${manifests[@]}"; do
  name="$(basename "${manifest}")"
  shipped+="${name} "
  install_owned 0644 "${manifest}" "${etc}/manifests/${name}"
done
for existing in "${etc}"/manifests/*.json; do
  [[ -f "${existing}" ]] || continue
  name="$(basename "${existing}")"
  case "${shipped}" in
    *" ${name} "*) ;;
    *) step_log "Entferne veraltetes Manifest: ${name}"
       "${SUDO[@]}" rm -f "${existing}" ;;
  esac
done

# --- Geheimnisse: nur anlegen, nie ueberschreiben ------------------------
place_secret() {
  local src="$1" dst="$2" name="$3"
  [[ -n "${src}" ]] || { step_log "${name}: kein Pfad uebergeben, bleibt offen."; return 0; }
  [[ -f "${src}" ]] || step_fail SECRET_FILE_MISSING
  if [[ -e "${dst}" ]]; then
    step_log "${name} vorhanden, bleibt unangetastet."
    return 0
  fi
  install_owned 0640 "${src}" "${dst}"
  step_log "${name} angelegt."
}
place_secret "${MQTT_PW_FILE}" "${etc}/mqtt.pw" "mqtt.pw"
"${SUDO[@]}" mkdir -p "${EN_ROOT}/etc/${BINARY_NAME}"
place_secret "${ADMIN_PW_FILE}" "${EN_ROOT}/etc/${BINARY_NAME}/auth.pw" "auth.pw"

# --- starten --------------------------------------------------------------
"${SUDO[@]}" systemctl daemon-reload
"${SUDO[@]}" systemctl enable --now "${BINARY_NAME}.service" \
  || step_fail DASHBOARD_START_FAILED

step_log "Dashboard eingerichtet; erreichbar auf Port 8080."
step_ok
