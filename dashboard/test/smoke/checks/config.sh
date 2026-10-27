# shellcheck shell=bash
# shellcheck disable=SC2154,SC2034 # Globals kommen vom Aufrufer, FAILED wird dort gelesen
# Pruefgruppen fuer Topics, Konfigurationen, Shelly-Presets, Seed-Daten,
# Datenfluss und abgeschaltete Dienste. Jede Funktion setzt bei einem Fehler
# FAILED=1 und laeuft ohne Abbruch weiter. Die Auswahl der Gruppen trifft der Lauf.
# Globals wie BASE, COOKIES, WORK, FIXTURES, EXTRA_DEVICES kommen vom Aufrufer.

checks_topics() {
  # Diese Proben stehen nur in fixtures/battery-soc.json. Der Lauf ruft die
  # Gruppe nur fuer diese Fixture auf, sonst sagen die Proben nichts aus.
  check "Topic mit Payload wird als Probe geliefert" \
    "any(s['topic'] == 'shelly/netz_meanwell/status' and 'apower' in s.get('payload', '') for s in data)" \
    "$BASE/api/v1/topics/samples"
  check "Topic ohne Nachricht bleibt ohne Payload" \
    "any(s['topic'] == 'bms/bank_b/voltage' and not s.get('payload') for s in data)" \
    "$BASE/api/v1/topics/samples"
  check "Topic-Proben nennen das zugehoerige Geraet" \
    "any(s['topic'] == 'shelly/netz_meanwell/status' and s.get('device') == 'MeanWell Charger (grid side)' for s in data)" \
    "$BASE/api/v1/topics/samples"
  check "Geraetename steht auch ohne Nachricht auf dem Topic" \
    "any(s['topic'] == 'bms/bank_b/voltage' and s.get('device') and not s.get('payload') for s in data)" \
    "$BASE/api/v1/topics/samples"
  check "Live-Spannung der Batterieanlage ist als Probe abrufbar" \
    "any(s['topic'] == 'outstation/battery_soc/state' and 'pack_corrected_v_per_cell' in s.get('payload', '') for s in data)" \
    "$BASE/api/v1/topics/samples"
  check "Proben sind nach Topic sortiert" \
    "[s['topic'] for s in data] == sorted(s['topic'] for s in data)" \
    "$BASE/api/v1/topics/samples"
}

checks_battery_config() {
  check "battery_soc_devices ist als Konfiguration sichtbar" \
    "any(c['name'] == 'battery_soc_devices' for c in data)" \
    "$BASE/api/v1/configurations"

  # Dienststatus: minibroker.py --service-status meldet je Datei die SHA-256,
  # die auch das Dashboard berechnet. Erwartet wird der Status genau dieser
  # Datei - so prueft die Konfigurationsseite ihn nach dem Oeffnen und Speichern.
  CONFIG_URL="$BASE/api/v1/configurations/battery_soc_devices"
  BATTERY_STATE="ok"
  check_service_status() { # name
    local name="$1" revision
    revision=$(sha256sum "$WORK/devices/battery_soc_devices.json" | cut -d' ' -f1)
    for _ in $(seq 1 10); do
      if api "$CONFIG_URL/status" | python3 -c "
import json, sys
data = json.load(sys.stdin)
sys.exit(0 if data['received'] and data['online'] and data['runtime_status'] == '$BATTERY_STATE' and data['config_revision'] == '$revision' else 1)
" 2>/dev/null; then
        echo "  OK   $name"
        return 0
      fi
      sleep 0.5
    done
    echo "  FEHL $name"
    api "$CONFIG_URL/status" || true
    echo
    FAILED=1
  }
  check_service_status "Dienststatus battery_soc: $BATTERY_STATE mit der Pruefsumme der Datei"
  check "Dienst battery_soc gilt im Health-Endpunkt als aktiv" \
    "any(s['id'] == 'battery_soc' and s['state'] == 'active' for s in data['node']['services'])" \
    "$BASE/api/v1/health"

  # Der gemeldete Bug: Kommazahlen muessen gespeichert werden koennen.
  api "$CONFIG_URL" | python3 -c "
import json, sys
data = json.load(sys.stdin)
data[0]['charge_efficiency'] = 0.95
data[0]['full_v_per_cell'] = 3.6
json.dump(data, open('$WORK/valid.json', 'w'))
"
  if curl -sf "${CURL_INSECURE[@]}" -m 5 -b "$COOKIES" -H "$SECURE_HEADER" -H 'Content-Type: application/json' \
       -X PUT "$CONFIG_URL" --data-binary "@$WORK/valid.json" > /dev/null; then
    stored=$(api "$CONFIG_URL" | python3 -c "import json,sys; print(json.load(sys.stdin)[0]['charge_efficiency'])")
    if [[ "$stored" == "0.95" ]]; then
      echo "  OK   Kommazahl 0.95 gespeichert und zurueckgelesen"
    else
      echo "  FEHL Kommazahl kam als '$stored' zurueck"; FAILED=1
    fi
  else
    echo "  FEHL Speichern einer gueltigen Kommazahl abgelehnt"; FAILED=1
  fi

  # Die neuen DC-Felder muessen ueber Schema-Validierung und Speichern kommen -
  # ein Tippfehler im Schema faellt sonst erst auf dem Pi als Reload-Fehler auf.
  api "$CONFIG_URL" | python3 -c "
import json, sys
data = json.load(sys.stdin)
data[0]['charger_dc_power_topic'] = 'outstation/t2mg81a4e9/field/dcpower'
data[0]['dc_max_age_s'] = 45
json.dump(data, open('$WORK/dc.json', 'w'))
"
  if curl -sf "${CURL_INSECURE[@]}" -m 5 -b "$COOKIES" -H "$SECURE_HEADER" -H 'Content-Type: application/json' \
       -X PUT "$CONFIG_URL" --data-binary "@$WORK/dc.json" > /dev/null; then
    stored=$(api "$CONFIG_URL" | python3 -c "import json,sys; print(json.load(sys.stdin)[0]['dc_max_age_s'])")
    if [[ "$stored" == "45" ]]; then
      echo "  OK   DC-Felder gespeichert und zurueckgelesen"
    else
      echo "  FEHL dc_max_age_s kam als '$stored' zurueck"; FAILED=1
    fi
  else
    echo "  FEHL Speichern der DC-Felder abgelehnt"; FAILED=1
  fi

  # Gegenprobe: der Server muss den Grenzwert ablehnen (maximum: 1).
  python3 -c "
import json
data = json.load(open('$WORK/valid.json'))
data[0]['charge_efficiency'] = 1.5
json.dump(data, open('$WORK/invalid.json', 'w'))
"
  code=$(curl -s "${CURL_INSECURE[@]}" -m 5 -o /dev/null -w '%{http_code}' -b "$COOKIES" -H "$SECURE_HEADER" \
    -H 'Content-Type: application/json' -X PUT "$CONFIG_URL" --data-binary "@$WORK/invalid.json")
  if [[ "$code" == "400" || "$code" == "422" ]]; then
    echo "  OK   ungueltiger Wert 1.5 abgelehnt (HTTP $code)"
  else
    echo "  FEHL ungueltiger Wert 1.5 kam mit HTTP $code durch"; FAILED=1
  fi
  check_service_status "nach dem Speichern meldet der Dienst die neue Pruefsumme"
}

checks_shelly() {
  # Gruppe shelly: die Unterstuetzung schlafender H&T-Geraete end-to-end.
  # Die Presets kommen unveraendert aus services/shelly - faellt dort das
  # sleepy-Flag weg, legt das Dashboard neue H&T wieder ohne Gnadenfrist an.
  check "shelly_devices ist als Konfiguration sichtbar" \
    "any(c['name'] == 'shelly_devices' for c in data)" \
    "$BASE/api/v1/configurations"
  check "beide H&T-Presets (Gen1 und Plus) sind sleepy" \
    "all(any(p['id'] == i and p['properties'].get('sleepy') is True for p in data) for i in ('shelly_ht_gen1', 'shelly_plus_ht'))" \
    "$BASE/api/v1/shelly/presets"
  check "Gen1-H&T-Preset ist Generation 1 mit Temperatur und Feuchte" \
    "any(p['id'] == 'shelly_ht_gen1' and p['properties'].get('generation') == 1 and p['properties'].get('has_humidity') and p['properties'].get('has_temperature') for p in data)" \
    "$BASE/api/v1/shelly/presets"
  check "Luftfeuchte des Gen1-H&T ist als Probe abrufbar" \
    "any(s['topic'] == 'outstation/ht_bad/humidity' and s.get('payload') == '58.2' for s in data)" \
    "$BASE/api/v1/topics/samples"
  check "Temperatur des offline gemeldeten Plus-H&T bleibt als Probe stehen" \
    "any(s['topic'] == 'outstation/ht_keller/temperature' and s.get('payload') == '12.3' for s in data)" \
    "$BASE/api/v1/topics/samples"

  SHELLY_URL="$BASE/api/v1/configurations/shelly_devices"
  api "$SHELLY_URL" | python3 -c "
import json, sys
data = json.load(sys.stdin)
keller = next(d for d in data if d['id'] == 'ht_keller')
keller['offline_grace_s'] = 7200
json.dump(data, open('$WORK/shelly-valid.json', 'w'))
data[0]['offline_grace_s'] = 0
json.dump(data, open('$WORK/shelly-invalid.json', 'w'))
"
  if curl -sf "${CURL_INSECURE[@]}" -m 5 -b "$COOKIES" -H "$SECURE_HEADER" -H 'Content-Type: application/json' \
       -X PUT "$SHELLY_URL" --data-binary "@$WORK/shelly-valid.json" > /dev/null; then
    stored=$(api "$SHELLY_URL" | python3 -c "
import json, sys
keller = next(d for d in json.load(sys.stdin) if d['id'] == 'ht_keller')
print(keller.get('sleepy'), keller.get('offline_grace_s'))")
    if [[ "$stored" == "True 7200" ]]; then
      echo "  OK   sleepy und offline_grace_s gespeichert und zurueckgelesen"
    else
      echo "  FEHL sleepy/offline_grace_s kamen als '$stored' zurueck"; FAILED=1
    fi
  else
    echo "  FEHL Speichern eines sleepy-H&T abgelehnt"; FAILED=1
  fi
  # Gegenprobe: exclusiveMinimum 0 - eine Gnadenfrist von 0 s waere wieder
  # das alte "beim ersten verpassten Poll offline".
  code=$(curl -s "${CURL_INSECURE[@]}" -m 5 -o /dev/null -w '%{http_code}' -b "$COOKIES" -H "$SECURE_HEADER" \
    -H 'Content-Type: application/json' -X PUT "$SHELLY_URL" --data-binary "@$WORK/shelly-invalid.json")
  if [[ "$code" == "400" || "$code" == "422" ]]; then
    echo "  OK   offline_grace_s 0 abgelehnt (HTTP $code)"
  else
    echo "  FEHL offline_grace_s 0 kam mit HTTP $code durch"; FAILED=1
  fi
}

checks_seed() {
  # Vorbelegte Daten pruefen: sonst faellt erst im Browser auf, dass ein Seed
  # gar nicht angekommen ist (leeres Layout sieht aus wie ein Renderfehler).
  if [[ -f "$WORK/data/layout.json" ]]; then
    check "Layout aus --seed-data ist geladen" \
      "sum(len(g['items']) for p in data['pages'] for g in p['groups']) > 0" \
      "$BASE/api/v1/layout"
  fi
  if [[ -f "$WORK/data/energy.json" ]]; then
    check "Energie-Rollen aus --seed-data sind zugewiesen" \
      "len(data.get('assignments', {})) > 0" \
      "$BASE/api/v1/energy/roles"
  fi
  if [[ -f "$WORK/data/energy.json" ]] && grep -q '"groups"' "$WORK/data/energy.json"; then
    check "Energiegruppen und Kategorien sind im Snapshot" \
      "len(data.get('groups', [])) > 0 and len(data.get('categories', {})) > 0" \
      "$BASE/api/v1/energy"
  fi
}

checks_dataflow() {
  check "Eigenes Geraet energy_node ist ueber den Broker zurueckgekommen" \
    "any(d.get('id') == 'energy_node' for d in (data if isinstance(data, list) else data.get('devices', [])))" \
    "$BASE/api/v1/devices"
  check "Datenfluss liefert Regeln" \
    "any(n['virtual_id'].startswith('rule:') for n in data['nodes'])" \
    "$BASE/api/v1/device/map/flows"
  check "Automations-Geraet fuehrt die Regeln aus" \
    "any(e['details'].get('kind') == 'executes' and e['from'].get('device_id') == 'automation' for e in data['edges'])" \
    "$BASE/api/v1/device/map/flows"
  # Die Bank-A-Kante kommt aus battery_soc_devices.json mit bank_a_voltage_topic.
  # Die bringt nur fixtures/devices/demo mit, docs-screenshots teilt
  # die Fixture, nicht die Geraete.
  check "Datenfluss liefert eine Bank-A-Kante" \
    "any(e['details'].get('field') == 'bank_a_voltage_topic' for e in data['edges'])" \
    "$BASE/api/v1/device/map/flows"
}

checks_services_off() {
  # --installed-services-off: die Praesenz-Proben lesen echtes HTML statt
  # JSON, deshalb kein check() (das erwartet einen JSON-Body). Geprueft wird
  # derselbe End-zu-Ende-Weg wie auf dem Node: config.json -> Go-Template ->
  # gerenderte Seite - nicht nur die Unit-Tests der einzelnen Handler.
  html_lacks "Automationen-Tab fehlt bei installed_services=aus" \
    'id="tab-automations"' "$BASE/"
  html_lacks "Automationen-Fragment liefert bei installed_services=aus keinen Inhalt" \
    'x-data="automationsPanel()"' "$BASE/?fragment=panel&panel=automations"
  html_lacks "Tailscale-Unterseite fehlt bei installed_services=aus" \
    'aria-controls="settings-tailscale"' "$BASE/?fragment=panel&panel=settings"
  html_lacks "TinyTuya-Unterseite fehlt bei installed_services=aus" \
    'aria-controls="settings-tiny-tuya"' "$BASE/?fragment=panel&panel=settings"
}
