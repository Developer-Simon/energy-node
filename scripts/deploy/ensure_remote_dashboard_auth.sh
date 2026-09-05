#!/usr/bin/env bash
#
# Legt /etc/energy-node-dashboard/auth.pw auf dem Zielgeraet an, falls sie
# fehlt. Analog zu ensure_remote_secrets.sh fuer /etc/energy-node/mqtt.pw:
#
#  1. Die Datei enthaelt nur das Admin-Passwort, ohne Schluesselnamen. Der
#     Benutzername ist kein Geheimnis und steht in config.json unter
#     dashboard.admin_username.
#  2. Eigentuemer root:${TARGET_USER}, Rechte 0640 - der Dashboard-Prozess
#     laeuft als der Zielbenutzer und muss die Datei selbst lesen koennen.
#  3. Ist die Datei auf dem Zielgeraet bereits vorhanden, bleibt sie
#     unangetastet - ein Redeploy darf ein am Dashboard geaendertes
#     Passwort nicht verwerfen.
#
# Lokaler Zwischenstand: secrets/dashboard-admin.pw (ueber .gitignore
# ausgeschlossen).
#
# Usage (nach `source`):
#   ensure_remote_dashboard_auth "<ssh_target>" "${SSH_OPTS[@]}"

ENSURE_DASHBOARD_AUTH_LIB_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
LOCAL_DASHBOARD_ADMIN_PW="$(git -C "${ENSURE_DASHBOARD_AUTH_LIB_DIR}" rev-parse --show-toplevel)/secrets/dashboard-admin.pw"

ensure_remote_dashboard_auth() {
    local ssh_target="$1"
    shift
    local ssh_opts=("$@")

    if ssh "${ssh_opts[@]}" "${ssh_target}" "sudo test -r /etc/energy-node-dashboard/auth.pw"; then
        return 0
    fi

    echo "==> /etc/energy-node-dashboard/auth.pw fehlt auf ${ssh_target}."

    if [[ ! -f "${LOCAL_DASHBOARD_ADMIN_PW}" ]]; then
        echo "Lege lokale Kopie unter ${LOCAL_DASHBOARD_ADMIN_PW} an (per .gitignore nicht versioniert)."
        local admin_password
        read -rsp "Dashboard-Admin-Passwort: " admin_password
        echo
        mkdir -p "$(dirname "${LOCAL_DASHBOARD_ADMIN_PW}")"
        (
            umask 077
            printf '%s' "${admin_password}" > "${LOCAL_DASHBOARD_ADMIN_PW}"
        )
    else
        echo "Verwende lokale Kopie ${LOCAL_DASHBOARD_ADMIN_PW}."
    fi

    local remote_tmp="/tmp/auth.pw.$$"
    scp "${ssh_opts[@]}" "${LOCAL_DASHBOARD_ADMIN_PW}" "${ssh_target}:${remote_tmp}"
    ssh "${ssh_opts[@]}" "${ssh_target}" "
        set -e
        sudo mkdir -p /etc/energy-node-dashboard
        sudo chown root:${TARGET_USER} /etc/energy-node-dashboard
        sudo chmod 0755 /etc/energy-node-dashboard
        sudo install -o root -g ${TARGET_USER} -m 0640 '${remote_tmp}' /etc/energy-node-dashboard/auth.pw
        rm -f '${remote_tmp}'
    "

    if ! ssh "${ssh_opts[@]}" "${ssh_target}" "sudo test -r /etc/energy-node-dashboard/auth.pw"; then
        echo "FEHLER: auth.pw konnte nicht auf ${ssh_target} angelegt werden." >&2
        exit 1
    fi
    echo "==> auth.pw erfolgreich auf ${ssh_target} angelegt."
}
