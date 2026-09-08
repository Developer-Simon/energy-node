#!/usr/bin/env bash
#
# Installiert /etc/energy-node/config.json aus der Repository-Vorlage,
# falls die Datei auf dem Zielgeraet fehlt.
#
# Existiert sie, bleibt sie unangetastet: das Dashboard darf sie ueber
# PUT /api/v1/system/config bearbeiten, und ein Deploy darf diese
# Bearbeitung nicht verwerfen. --force-config ueberschreibt nach Rueckfrage.
# Dieselbe Regel gilt fuer automation_rules.json und bridge.conf.
#
# Usage (nach `source`):
#   ensure_remote_config "<ssh_target>" "<force:true|false>" "${SSH_OPTS[@]}"

ENSURE_CONFIG_LIB_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CONFIG_TEMPLATE="$(git -C "${ENSURE_CONFIG_LIB_DIR}" rev-parse --show-toplevel)/services/energy-node.config.json"

ensure_remote_config() {
    local ssh_target="$1"
    local force="$2"
    shift 2
    local ssh_opts=("$@")

    if [[ ! -f "${CONFIG_TEMPLATE}" ]]; then
        echo "FEHLER: Vorlage ${CONFIG_TEMPLATE} fehlt." >&2
        exit 1
    fi

    if ssh "${ssh_opts[@]}" "${ssh_target}" "sudo test -f /etc/energy-node/config.json"; then
        if [[ "${force}" != "true" ]]; then
            echo "==> /etc/energy-node/config.json vorhanden, bleibt unangetastet."
            return 0
        fi
        echo "==> --force-config: die vorhandene config.json auf ${ssh_target} wird ueberschrieben."
        echo "    Damit gehen alle ueber das Dashboard vorgenommenen Aenderungen verloren."
        local antwort
        read -rp "Wirklich ueberschreiben? [ja/NEIN] " antwort
        if [[ "${antwort}" != "ja" ]]; then
            echo "Abgebrochen."
            exit 1
        fi
    fi

    echo "==> Installiere ${CONFIG_TEMPLATE} nach ${ssh_target}:/etc/energy-node/config.json"
    local remote_tmp="/tmp/energy-node.config.json.$$"
    scp "${ssh_opts[@]}" "${CONFIG_TEMPLATE}" "${ssh_target}:${remote_tmp}"
    ssh "${ssh_opts[@]}" "${ssh_target}" "
        set -e
        sudo mkdir -p /etc/energy-node
        sudo chown root:${TARGET_USER} /etc/energy-node
        sudo chmod 0755 /etc/energy-node
        sudo install -o root -g ${TARGET_USER} -m 0664 '${remote_tmp}' /etc/energy-node/config.json
        rm -f '${remote_tmp}'
    "
    echo "==> config.json auf ${ssh_target} installiert."
}
