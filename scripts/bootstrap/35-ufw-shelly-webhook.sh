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
# Der Listener gehoert zum Shelly-Dienst (Schritt 83). Ist der abgewaehlt,
# gilt dieser Schritt ebenfalls als abgewaehlt - dieselbe Abhaengigkeit steht
# als "requires" im Manifest (make_bundle.sh, STEP_REQUIRES).
#
# Abgewaehlt nimmt der Schritt eine frueher erteilte Freigabe wieder zurueck
# und loescht seinen Stempel, damit ein erneutes Waehlen wieder greift.
#
# Den Listener selbst schaltet weiterhin webhook_enabled in config.json ein
# (Dashboard, Standard aus); dieser Schritt oeffnet nur die Firewall.
set -euo pipefail
# shellcheck source=scripts/bootstrap/lib/step.sh
source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/lib/step.sh"
# shellcheck source=scripts/bootstrap/lib/shelly_webhook.sh
source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/lib/shelly_webhook.sh"

step_begin 35
port="$(shelly_webhook_port)"

if ! shelly_webhook_wanted; then
  # Kann auf einem Node ohne Freigabe nichts loeschen - ufw meldet das und
  # das ist kein Fehler. Der Standardport wird mit entfernt, falls der Port
  # seit der Freigabe im Dashboard geaendert wurde.
  for p in $(printf '%s\n' "${port}" "${SHELLY_WEBHOOK_DEFAULT_PORT}" | sort -u); do
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
