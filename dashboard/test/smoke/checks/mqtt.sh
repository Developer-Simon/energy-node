# shellcheck shell=bash
# shellcheck disable=SC2154,SC2034 # Globals kommen vom Aufrufer, FAILED wird dort gelesen
# Pruefgruppe fuer MQTT mit falschem Passwort: der Broker lehnt die Verbindung ab,
# das Dashboard startet trotzdem, meldet den Fehler im Status und bleibt erreichbar.
# Globals wie BASE, CURL_INSECURE, WORK kommen vom Aufrufer, check und api aus core.sh.

checks_mqtt_falsch() {
  echo "==> MQTT mit falschem Passwort"
  check_status "Anmeldeseite antwortet ohne MQTT-Verbindung (HTTP 200)" 200 /dev/null "$BASE/"

  # Der erste Verbindungsversuch laeuft kurz nach dem Start. Bis zu 20 s warten,
  # bis der Status einen Fehler nennt.
  local status=""
  for _ in $(seq 1 20); do
    status="$(api "$BASE/api/v1/mqtt/status" || true)"
    [[ "$status" == *'"last_error"'* ]] && break
    sleep 1
  done
  check "MQTT-Status: nicht verbunden, letzter Fehler genannt" \
    "data['connected'] is False and 'authoriz' in data.get('last_error', '').lower()" \
    "$BASE/api/v1/mqtt/status"
  check "MQTT-Status: das Passwort erscheint nicht im Status" \
    "'smoke-falsch-1' not in json.dumps(data)" \
    "$BASE/api/v1/mqtt/status"
  check "Health-Endpunkt laeuft und zeigt MQTT als nicht verbunden" \
    "data['mqtt']['connected'] is False" \
    "$BASE/api/v1/health"
}
