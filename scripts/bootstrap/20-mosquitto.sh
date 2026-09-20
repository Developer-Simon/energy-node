#!/usr/bin/env bash
#
# Schritt 20: Mosquitto-Broker, Benutzer und conf.d/default.conf
# (INSTALLATION.md §4).
#
# Das Passwort kommt ausschliesslich als Pfad zu einer 0600-Datei. Die
# passwd-Zeile bauen wir selbst (siehe unten) statt mosquitto_passwd
# aufzurufen: das kennt keinen stdin-Weg - das Passwort kommt vom Terminal
# oder mit -b als Argument, und ein Argument stuende in /proc/<pid>/cmdline
# und waere fuer jeden lokalen Benutzer lesbar.
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

# Die Datei wird neu angelegt; danach steht genau ein Benutzer darin. Das ist
# gewollt: der Broker gehoert diesem Node, nicht einer gewachsenen Historie.
#
# Format wie mosquitto_passwd 2.x es schreibt (sha512-pbkdf2, dessen
# Vorgabe): user:$7$<Runden>$<Salt, base64>$<PBKDF2-HMAC-SHA512, base64> mit
# 12 Byte Salt, das dekodiert in den Hash eingeht. Das Passwort wird wie in
# energy_node_common.appconfig gelesen - nachlaufender Weissraum zaehlt nicht
# dazu, damit der Hash dasselbe Passwort meint wie die Clients.
hash_line="$(mktemp)"
trap 'rm -f "${hash_line}"' EXIT
python3 - "${PASSWORD_FILE}" "${MQTT_USER}" > "${hash_line}" <<'PY' || step_fail MOSQUITTO_PASSWD_FAILED
import base64, hashlib, os, sys

password_file, user = sys.argv[1], sys.argv[2]
password = open(password_file, encoding="utf-8").read().rstrip("\r\n\t ")
if not password or not user or ":" in user:
    sys.exit("Benutzername oder Passwort unbrauchbar")
salt = os.urandom(12)
digest = hashlib.pbkdf2_hmac("sha512", password.encode("utf-8"), salt, 101)
print("%s:$7$101$%s$%s" % (user, base64.b64encode(salt).decode(), base64.b64encode(digest).decode()))
PY

# Der Broker liest die Datei nach dem Wechsel auf seinen eigenen Benutzer neu
# (SIGHUP) und neuere Fassungen verweigern eine fuer alle lesbare Datei - also
# root:mosquitto 0640, wo die Gruppe existiert (das Paket legt sie an).
if { [[ "${#SUDO[@]}" -gt 0 || "$(id -u)" -eq 0 ]] \
     && getent group mosquitto >/dev/null 2>&1; }; then
  "${SUDO[@]}" install -o root -g mosquitto -m 0640 "${hash_line}" "${passwd_file}" \
    || step_fail MOSQUITTO_PASSWD_FAILED
else
  "${SUDO[@]}" install -m 0644 "${hash_line}" "${passwd_file}" \
    || step_fail MOSQUITTO_PASSWD_FAILED
fi

"${SUDO[@]}" systemctl enable --now mosquitto
"${SUDO[@]}" systemctl restart mosquitto

step_log "Broker eingerichtet, Benutzer ${MQTT_USER}."
step_ok
