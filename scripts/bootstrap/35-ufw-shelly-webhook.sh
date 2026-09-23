#!/usr/bin/env bash
#
# Schritt 35: Firewall-Freigabe fuer den Shelly-Wake-Webhook (Opt-in).
#
# Der Shelly-Dienst kann einen eingehenden HTTP-Endpunkt oeffnen, den ein
# schlafendes Gen1-Geraet (H&T) beim Aufwachen aufruft (services/shelly/
# config.schema.json, webhook_enabled/webhook_port, docs/device-services.md).
# Das ist ein zusaetzlicher, aus dem LAN erreichbarer Port - deshalb gibt
# ihn dieser Schritt nur frei, wenn der Betreiber ihn im Installer
# ausdruecklich gewaehlt hat (Manifest-Vorgabe "aus", step_opted_in: ein
# fehlender Eintrag in selection.json zaehlt als "nein").
#
# Abgewaehlt nimmt der Schritt eine frueher erteilte Freigabe wieder zurueck
# und loescht seinen Stempel, damit ein erneutes Waehlen wieder greift.
#
# Den Listener selbst schaltet weiterhin webhook_enabled in config.json ein
# (Dashboard, Standard aus); dieser Schritt oeffnet nur die Firewall.
set -euo pipefail
# shellcheck source=scripts/bootstrap/lib/step.sh
source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/lib/step.sh"

DEFAULT_PORT=8082
cfg="${EN_ROOT}/etc/energy-node/config.json"

# Bei einer Erstinstallation gibt es config.json hier noch nicht (Schritt 65
# schreibt es erst) - dann gilt der Standardport. Bei einem Update zaehlt ein
# im Dashboard geaenderter webhook_port.
webhook_port() {
  [[ -f "${cfg}" ]] || { echo "${DEFAULT_PORT}"; return; }
  python3 - "${cfg}" "${DEFAULT_PORT}" <<'PY' 2>/dev/null || echo "${DEFAULT_PORT}"
import json, sys
port = json.load(open(sys.argv[1], encoding="utf-8")).get("services", {}).get("shelly", {}).get("webhook_port")
if not isinstance(port, int) or isinstance(port, bool) or not 1 <= port <= 65535:
    port = int(sys.argv[2])
print(port)
PY
}

step_begin 35
port="$(webhook_port)"

if ! step_opted_in 35; then
  # Kann auf einem Node ohne Freigabe nichts loeschen - ufw meldet das und
  # das ist kein Fehler. Der Standardport wird mit entfernt, falls der Port
  # seit der Freigabe im Dashboard geaendert wurde.
  for p in $(printf '%s\n' "${port}" "${DEFAULT_PORT}" | sort -u); do
    "${SUDO[@]}" ufw delete allow "${p}/tcp" >/dev/null 2>&1 || true
  done
  rm -f "$(step_stamp_path 35)"
  step_skip "nicht ausgewaehlt"
  exit 0
fi
if step_done 35; then
  step_skip "bereits erledigt"
  exit 0
fi

"${SUDO[@]}" ufw allow "${port}/tcp" || step_fail UFW_FAILED

step_log "Firewall: ${port}/tcp fuer den Shelly-Wake-Webhook freigegeben"
step_ok
