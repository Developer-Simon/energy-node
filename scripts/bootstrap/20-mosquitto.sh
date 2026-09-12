#!/usr/bin/env bash
#
# Schritt 20: Mosquitto-Broker, Benutzer und conf.d/default.conf
# (INSTALLATION.md §4).
#
# Das Passwort kommt ausschliesslich als Pfad zu einer 0600-Datei und
# erreicht mosquitto_passwd ueber stdin - ein Argument stuende in
# /proc/<pid>/cmdline und waere fuer jeden lokalen Benutzer lesbar.
set -euo pipefail
# shellcheck source=scripts/bootstrap/lib/step.sh
source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/lib/step.sh"

# Kopfzeile, an der wir unsere eigene Konfiguration wiedererkennen. Eine
# default.conf ohne sie stammt von jemand anderem und wird nicht angefasst.
CONF_MARKER='# von energy-node-installer verwaltet - Aenderungen gehen verloren'

MQTT_USER=""
PASSWORD_FILE=""
while [[ $# -gt 0 ]]; do
  case "$1" in
    --user) MQTT_USER="${2:-}"; shift 2 ;;
    --password-file) PASSWORD_FILE="${2:-}"; shift 2 ;;
    *) shift ;;
  esac
done

step_begin 20
if [[ -z "${MQTT_USER}" || -z "${PASSWORD_FILE}" || ! -f "${PASSWORD_FILE}" ]]; then
  step_fail MOSQUITTO_ARGS_MISSING
fi
if step_done 20; then
  step_skip "bereits erledigt"
  exit 0
fi

conf_dir="${EN_ROOT}/etc/mosquitto/conf.d"
conf="${conf_dir}/default.conf"
passwd_file="${EN_ROOT}/etc/mosquitto/passwd"

if [[ -f "${conf}" ]] && ! grep -qxF "${CONF_MARKER}" "${conf}"; then
  step_log "In ${conf} steht eine fremde Konfiguration."
  step_fail MOSQUITTO_CONF_FOREIGN
fi

"${SUDO[@]}" mkdir -p "${conf_dir}"
"${SUDO[@]}" tee "${conf}" >/dev/null <<CONF
${CONF_MARKER}
listener 1883
allow_anonymous false
password_file /etc/mosquitto/passwd
CONF

# -c legt die Datei neu an; danach steht genau ein Benutzer darin. Das ist
# gewollt: der Broker gehoert diesem Node, nicht einer gewachsenen Historie.
"${SUDO[@]}" mosquitto_passwd -c "${passwd_file}" "${MQTT_USER}" < "${PASSWORD_FILE}" \
  || step_fail MOSQUITTO_PASSWD_FAILED

"${SUDO[@]}" systemctl enable --now mosquitto
"${SUDO[@]}" systemctl restart mosquitto

step_log "Broker eingerichtet, Benutzer ${MQTT_USER}."
step_ok
