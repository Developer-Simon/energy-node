#!/usr/bin/env bash
#
# Gemeinsames fuer den Shelly-Wake-Webhook: Schritt 35 oeffnet seinen Port,
# diagnose.sh prueft ihn. Setzt lib/step.sh voraus (step_opted_in,
# step_selected, EN_ROOT).

SHELLY_WEBHOOK_STEP=35
SHELLY_WEBHOOK_DEFAULT_PORT=8082
# Der Listener gehoert zum Shelly-Dienst. Dieselbe Abhaengigkeit steht als
# "requires" im Manifest (make_bundle.sh, STEP_REQUIRES) und wird dort von
# plan.sh und der Oberflaeche gelesen.
SHELLY_WEBHOOK_REQUIRED_STEP=83

# shelly_webhook_wanted: Erfolg, wenn der Betreiber die Freigabe gewaehlt hat
# UND der Shelly-Dienst gewaehlt ist.
shelly_webhook_wanted() {
  step_opted_in "${SHELLY_WEBHOOK_STEP}" && step_selected "${SHELLY_WEBHOOK_REQUIRED_STEP}"
}

# shelly_webhook_port: webhook_port aus config.json, sonst der Standard. Bei
# einer Erstinstallation gibt es config.json vor Schritt 65 noch nicht.
shelly_webhook_port() {
  local cfg="${EN_ROOT}/etc/energy-node/config.json"
  [[ -f "${cfg}" ]] || { echo "${SHELLY_WEBHOOK_DEFAULT_PORT}"; return; }
  python3 - "${cfg}" "${SHELLY_WEBHOOK_DEFAULT_PORT}" <<'PY' 2>/dev/null || echo "${SHELLY_WEBHOOK_DEFAULT_PORT}"
import json, sys
port = json.load(open(sys.argv[1], encoding="utf-8")).get("services", {}).get("shelly", {}).get("webhook_port")
if not isinstance(port, int) or isinstance(port, bool) or not 1 <= port <= 65535:
    port = int(sys.argv[2])
print(port)
PY
}
