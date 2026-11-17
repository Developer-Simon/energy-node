#!/usr/bin/env bash
#
# Schritt 89: Beispiel fuer einen leeren Knoten (Opt-in).
#
# Legt Beispielgeraete, ein Beispielszenario und die Freigabe fuer das
# Schalten der Beispiel-Steckdose an. Das Beispiel laeuft nur, wenn der
# Betreiber den Schritt ausdruecklich gewaehlt hat, die Schritte 81, 83 und
# 88 gewaehlt sind und der Node noch keine Geraete und keine Regeln hat.
# Die Marke beispiel-angelegt verhindert ein zweites Anlegen, auch nachdem
# der Betreiber das Beispiel wieder geloescht hat.
#
# Die Logik steckt in lib/beispiel_apply.py (check, apply, publish). Hier
# steht nur der Ablauf.
# Eingaben fuer den Fingerabdruck (scripts/build/lib/manifest.sh): solange
# sie gleich bleiben, ueberspringt ein Update diesen Schritt.
# step-inputs: bootstrap/lib/step.sh bootstrap/lib/beispiel.json bootstrap/lib/beispiel_apply.py bootstrap/lib/mqtt_probe.py
set -euo pipefail
# shellcheck source=scripts/bootstrap/lib/step.sh
source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/lib/step.sh"

LIB_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/lib"
HELPER="${LIB_DIR}/beispiel_apply.py"
EXAMPLE="${LIB_DIR}/beispiel.json"
MARKER="${EN_STATE_DIR}/beispiel-angelegt"
DEVICES="${EN_ROOT}${EN_TARGET_BASE}/devices"

step_begin 89

if ! step_opted_in 89 || ! step_selected 81 || ! step_selected 83 || ! step_selected 88; then
  step_skip "nicht ausgewaehlt"
  exit 0
fi
if step_done 89; then
  step_skip "bereits erledigt"
  exit 0
fi
# Auch nach einem Update, das den Schritt neu laufen laesst, kommt das
# Beispiel nicht zurueck, wenn es schon einmal angelegt wurde.
if [[ -f "${MARKER}" ]]; then
  step_skip "Beispiel wurde schon angelegt"
  exit 0
fi

check_rc=0
python3 "${HELPER}" check "${DEVICES}" || check_rc=$?
case "${check_rc}" in
  0) ;;
  3) step_skip "Node hat schon Geraete"; exit 0 ;;
  *) step_fail EXAMPLE_FAILED ;;
esac

# Eine Zeile pro Wert, damit Pfade mit Leerzeichen heil bleiben.
cfg_values="$(python3 - "${EN_ROOT}/etc/energy-node/config.json" <<'PY'
import json, sys
config = json.load(open(sys.argv[1], encoding="utf-8"))
mqtt = config["mqtt"]
print("\n".join([
    config["paths"]["data_dir"],
    mqtt["host"],
    str(mqtt["port"]),
    mqtt["username"],
    mqtt["password_file"],
    config["dashboard"]["node_device_id"],
]))
PY
)" || step_fail EXAMPLE_FAILED
mapfile -t cfg <<< "${cfg_values}"
data_dir="${cfg[0]}"
mqtt_host="${cfg[1]}"
mqtt_port="${cfg[2]}"
mqtt_user="${cfg[3]}"
password_file="${cfg[4]}"
node_id="${cfg[5]}"

python3 "${HELPER}" apply "${EXAMPLE}" "${DEVICES}" "${EN_ROOT}${data_dir}" || step_fail EXAMPLE_FAILED

password="$("${SUDO[@]}" cat "${EN_ROOT}${password_file}")" || step_fail EXAMPLE_FAILED
publish=(python3 "${HELPER}" publish)
if [[ -n "${EN_BEISPIEL_PUBLISH_CMD:-}" ]]; then
  publish=("${EN_BEISPIEL_PUBLISH_CMD}")
fi
printf '%s' "${password}" | "${publish[@]}" "${mqtt_host}" "${mqtt_port}" "${mqtt_user}" \
  "outstation/${node_id}/settings/simulation_active/set" "1" || step_fail EXAMPLE_FAILED

"${SUDO[@]}" systemctl restart apsystems-ez1.service shelly-rpc.service automation.service \
  energy-node-dashboard.service || step_fail EXAMPLE_FAILED

mkdir -p "${EN_STATE_DIR}"
touch "${MARKER}" || step_fail EXAMPLE_FAILED
step_log "Beispiel angelegt: Geraete, Szenario und Freigabe sind da, die Dienste laufen neu."
step_ok
