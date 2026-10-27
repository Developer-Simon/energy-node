# shellcheck shell=bash
# shellcheck disable=SC2154,SC2034 # Globals kommen vom Aufrufer, FAILED wird dort gelesen
# Pruefgruppen fuer Energie: Bilanz der Fixture energie-ueberschuss und
# Live-Updates ueber den Broker. Jede Funktion setzt bei einem Fehler
# FAILED=1 und laeuft ohne Abbruch weiter. Die Auswahl der Gruppen trifft der Lauf.
# Globals wie BASE, WORK, SIMULATE kommen vom Aufrufer.

checks_energy_balance() {
  # fixtures/energie-ueberschuss.json ist genau auf die Rollen in
  # fixtures/seed/energie-alle-karten/energy.json gerechnet: die Bilanz muss
  # ohne Rest aufgehen. Geht sie das nicht, stimmt die Rollenaufloesung nicht
  # mehr - und jede Sichtpruefung der Energie-Karten waere wertlos. Nur ohne
  # --simulate pruefbar: die Sinuskurven dort bewegen PV/Netz/Batterie absichtlich
  # von diesen exakten Werten weg.
  check "PV-Ueberschuss steht vollstaendig in der Bilanz" \
    "data['balance']['pv'] == 4200 and data['balance']['grid_export'] == 1250 and data['balance']['battery_charge'] == 900" \
    "$BASE/api/v1/energy"
  check "Energiebilanz geht ohne offene Luecke auf" \
    "data['balance']['gap_applied'] == 0 and data['balance']['base'] == 1000" \
    "$BASE/api/v1/energy"
}

checks_live_push() {
  # --simulate schiebt PV/Netz/Batterie/Last per Sinuskurve ueber echte MQTT-
  # PUBLISH-Pakete auf der offenen Verbindung - genau der Weg, ueber den auch ein
  # echtes Geraet Live-Updates liefert. Ohne diese Probe wuerde ein Bruch dieses
  # Pfads (z. B. eine vergessene Sperre auf der Verbindung) erst beim manuellen
  # Blick in den Browser auffallen, als scheinbar "hangender" Chart.
  pv_payload() {
    api "$BASE/api/v1/topics/samples" | python3 -c "
import json, sys
data = json.load(sys.stdin)
print(next(s['payload'] for s in data if s['topic'] == 'pv/wechselrichter/status'))
"
  }
  first="$(pv_payload)"
  changed=0
  for _ in $(seq 1 10); do
    sleep 1
    [[ "$(pv_payload)" != "$first" ]] && { changed=1; break; }
  done
  if [[ $changed -eq 1 ]]; then
    echo "  OK   --simulate liefert Live-MQTT-Updates (PV-Leistung aendert sich)"
  else
    echo "  FEHL --simulate: PV-Leistung aendert sich nicht - Broker-Push kommt nicht an"
    FAILED=1
  fi
}
