#!/usr/bin/env bash
#
# Legt /etc/energy-node/mqtt.pw auf dem Zielgeraet an, falls sie fehlt.
#
# Loest ensure_remote_mqtt_env.sh ab. Zwei Unterschiede:
#
#  1. Die Datei enthaelt nur noch das Passwort, ohne Schluesselnamen. Der
#     Benutzername ist kein Geheimnis und steht in config.json.
#  2. Eigentuemer root:${TARGET_USER}, Rechte 0640. Frueher las systemd als
#     root das EnvironmentFile und uebergab den Wert an den bereits auf den
#     Zielbenutzer herabgestuften Prozess. Ohne EnvironmentFile oeffnet der
#     Prozess die Datei selbst und braucht deshalb Gruppenleserecht.
#
# Lokaler Zwischenstand: secrets/mqtt.pw (ueber .gitignore ausgeschlossen).
#
# Usage (nach `source`):
#   ensure_remote_secrets "<ssh_target>" "${SSH_OPTS[@]}"

ENSURE_SECRETS_LIB_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
LOCAL_MQTT_PW="$(git -C "${ENSURE_SECRETS_LIB_DIR}" rev-parse --show-toplevel)/secrets/mqtt.pw"

ensure_remote_secrets() {
    local ssh_target="$1"
    shift
    local ssh_opts=("$@")

    if ssh "${ssh_opts[@]}" "${ssh_target}" "sudo test -r /etc/energy-node/mqtt.pw"; then
        return 0
    fi

    echo "==> /etc/energy-node/mqtt.pw fehlt auf ${ssh_target}."

    if [[ ! -f "${LOCAL_MQTT_PW}" ]]; then
        echo "Lege lokale Kopie unter ${LOCAL_MQTT_PW} an (per .gitignore nicht versioniert)."
        local mqtt_password
        read -rsp "MQTT-Passwort: " mqtt_password
        echo
        mkdir -p "$(dirname "${LOCAL_MQTT_PW}")"
        (
            umask 077
            printf '%s' "${mqtt_password}" > "${LOCAL_MQTT_PW}"
        )
    else
        echo "Verwende lokale Kopie ${LOCAL_MQTT_PW}."
    fi

    local remote_tmp="/tmp/mqtt.pw.$$"
    scp "${ssh_opts[@]}" "${LOCAL_MQTT_PW}" "${ssh_target}:${remote_tmp}"
    ssh "${ssh_opts[@]}" "${ssh_target}" "
        set -e
        sudo mkdir -p /etc/energy-node
        sudo chown root:${TARGET_USER} /etc/energy-node
        sudo chmod 0755 /etc/energy-node
        sudo install -o root -g ${TARGET_USER} -m 0640 '${remote_tmp}' /etc/energy-node/mqtt.pw
        rm -f '${remote_tmp}'
    "

    if ! ssh "${ssh_opts[@]}" "${ssh_target}" "sudo test -r /etc/energy-node/mqtt.pw"; then
        echo "FEHLER: mqtt.pw konnte nicht auf ${ssh_target} angelegt werden." >&2
        exit 1
    fi
    echo "==> mqtt.pw erfolgreich auf ${ssh_target} angelegt."
}
