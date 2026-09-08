#!/usr/bin/env bash
# Startet das Dashboard lokal gegen einen Wegwerf-Broker und prueft die
# Konfigurationsseite end-to-end - ohne mosquitto, ohne Pi, ohne echte Geraete.
#
#   ./run-local-dashboard.sh                    # Pruefungen laufen, dann Abbau
#   ./run-local-dashboard.sh --keep             # laeuft weiter, URL wird ausgegeben
#   ./run-local-dashboard.sh --devices DIR      # andere Konfigurationen einspielen
#   ./run-local-dashboard.sh --fixture FILE     # andere Retained-Nachrichten
#   ./run-local-dashboard.sh --seed-data DIR    # settings/layout/energy vorbelegen
#   ./run-local-dashboard.sh --theme NAME       # Farbschema vorbelegen
#   ./run-local-dashboard.sh --simulate         # PV/Netz/Batterie/Last "leben" lassen
#   ./run-local-dashboard.sh --preset NAME      # --fixture/--seed-data/--simulate gebuendelt,
#                                                # siehe PRESETS unten; einzelne Flags danach
#                                                # ueberstimmen das Preset
#
# PRESETS (--preset NAME):
#   battery-soc      fixtures/battery-soc.json, kein Seed (= Skript-Standard)
#   energie          fixtures/energie-ueberschuss.json + seed/energie-alle-karten
#   energie-simulate wie energie, zusaetzlich --simulate
#   uebersicht-push  wie energie-simulate, zusaetzlich entity_value und
#                    entity_group - das Raster, das Stufe 3 tauschfrei macht
#   energie-kombiniert  fixtures/energie-kombiniert.json + seed/energie-kombiniert
#                       (load_mode "combined", Band/Board einzeln, Ring gesammelt)
#   alle-funktionen  fixtures/alle-funktionen.json + seed/alle-funktionen ("teuer,
#                    aber nahezu vollstaendig", siehe README)
#   geraete-kacheln  fixtures/geraete-kacheln.json + seed/geraete-kacheln (zwei
#                    schmale device-Kacheln mit Slider, span "1" auf der
#                    18rem-Untergrenze - Sichtpruefung fuer Slider-Breite und
#                    Titel-Umbruch in .device-tile-entity)
#   notification     fixtures/notification.json, kein Seed - simuliert die
#                    Automations-Topics (last_event {at, message}, state,
#                    status/online). notifications.js pollt daraus
#                    /api/v1/automation/notification und wirft beim Laden einen
#                    Warn-Toast; das Geraet "Energie-Automationen" erscheint in
#                    der Uebersicht.
#
# Warum es das gibt: das Dashboard beendet sich, wenn beim Start kein Broker
# erreichbar ist, die API verlangt eine Anmeldung, und die Anmeldung verlangt
# HTTPS. Alle drei Huerden sind hier einmal geloest.
#
# Warum --seed-data/--theme: jeder Lauf bekommt ein frisches mktemp-Verzeichnis,
# in dem weder Layout noch Farbschema stehen. Ohne Vorbelegung muss man beides
# nach jedem Neustart erneut durch die Oberflaeche klicken - fuer eine
# Sichtpruefung in vier Themes viermal. Mit Vorbelegung ist es ein Aufruf.
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
DASHBOARD_DIR="$(cd "$HERE/../.." && pwd)"
REPO_DIR="$(cd "$DASHBOARD_DIR/.." && pwd)"
SCRIPT_ROOT="$(cd "$HERE/../../.." && pwd)"

DEVICES_SOURCE="$REPO_DIR/services/battery_soc"
FIXTURE="$HERE/fixtures/battery-soc.json"
SEED_DATA=""
THEME=""
KEEP=0
SIMULATE=0
HTTP_PORT="${DASHBOARD_SMOKE_PORT:-18100}"
MQTT_PORT="${DASHBOARD_SMOKE_MQTT_PORT:-18883}"
PASSWORD="smoketest1234"
# isSecureRequest() akzeptiert TLS oder diesen Header - ohne ihn antwortet der
# Login mit "secure_login_required".
SECURE_HEADER="X-Forwarded-Proto: https"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --keep) KEEP=1; shift ;;
    --simulate) SIMULATE=1; shift ;;
    --preset)
      case "$2" in
        battery-soc)
          FIXTURE="$HERE/fixtures/battery-soc.json"; SEED_DATA=""
          ;;
        energie)
          FIXTURE="$HERE/fixtures/energie-ueberschuss.json"
          SEED_DATA="$HERE/fixtures/seed/energie-alle-karten"
          ;;
        energie-simulate)
          FIXTURE="$HERE/fixtures/energie-ueberschuss.json"
          SEED_DATA="$HERE/fixtures/seed/energie-alle-karten"
          SIMULATE=1
          ;;
        uebersicht-push)
          FIXTURE="$HERE/fixtures/energie-ueberschuss.json"
          SEED_DATA="$HERE/fixtures/seed/uebersicht-push"
          SIMULATE=1
          ;;
        energie-kombiniert)
          FIXTURE="$HERE/fixtures/energie-kombiniert.json"
          SEED_DATA="$HERE/fixtures/seed/energie-kombiniert"
          ;;
        alle-funktionen)
          FIXTURE="$HERE/fixtures/alle-funktionen.json"
          SEED_DATA="$HERE/fixtures/seed/alle-funktionen"
          ;;
        geraete-kacheln)
          FIXTURE="$HERE/fixtures/geraete-kacheln.json"
          SEED_DATA="$HERE/fixtures/seed/geraete-kacheln"
          ;;
        notification)
          FIXTURE="$HERE/fixtures/notification.json"; SEED_DATA=""
          ;;
        *)
          echo "unbekanntes Preset: $2 (battery-soc, energie, energie-simulate, uebersicht-push, energie-kombiniert, alle-funktionen, geraete-kacheln, notification)" >&2
          exit 2
          ;;
      esac
      shift 2
      ;;
    --devices) DEVICES_SOURCE="$2"; shift 2 ;;
    --fixture) FIXTURE="$2"; shift 2 ;;
    --seed-data) SEED_DATA="$2"; shift 2 ;;
    --theme) THEME="$2"; shift 2 ;;
    --port) HTTP_PORT="$2"; shift 2 ;;
    *) echo "unbekannte Option: $1" >&2; exit 2 ;;
  esac
done

if [[ ! -f "$FIXTURE" ]]; then
  echo "Fixture nicht gefunden: $FIXTURE" >&2; exit 2
fi
if [[ -n "$SEED_DATA" && ! -d "$SEED_DATA" ]]; then
  echo "Seed-Verzeichnis nicht gefunden: $SEED_DATA" >&2; exit 2
fi
# Frueh abbrechen statt erst am Schema-Fehler des Dashboards: settings.json
# wird gegen ein enum validiert, ein Tippfehler kostet sonst einen ganzen Lauf.
if [[ -n "$THEME" ]]; then
  case "$THEME" in
    mint|stromblau|signalgelb|tageslicht) ;;
    *) echo "unbekanntes Theme: $THEME (mint, stromblau, signalgelb, tageslicht)" >&2; exit 2 ;;
  esac
fi

RUN_ROOT="$HERE/.run"
mkdir -p "$RUN_ROOT"
WORK="$(mktemp -d "$RUN_ROOT/XXXXXX")"
BROKER_PID=""
DASHBOARD_PID=""

# Ein Ueberbleibsel eines abgebrochenen Laufs haelt sonst den Port, das neue
# Dashboard kann nicht binden - und curl redet unbemerkt mit dem alten Prozess,
# dessen Arbeitsverzeichnis es laengst nicht mehr gibt.
free_port() {
  local port="$1" pid
  for pid in $(ss -lntpH "sport = :$port" 2>/dev/null | grep -o 'pid=[0-9]*' | cut -d= -f2 | sort -u); do
    echo "Port $port war noch belegt (PID $pid) - wird beendet."
    kill "$pid" 2>/dev/null || true
  done
  sleep 1
}

cleanup() {
  [[ -n "$DASHBOARD_PID" ]] && kill "$DASHBOARD_PID" 2>/dev/null || true
  [[ -n "$BROKER_PID" ]] && kill "$BROKER_PID" 2>/dev/null || true
  # `go run` startet das Binary als Kindprozess - der ueberlebt das kill sonst.
  pkill -f "go-build.*exe/dashboard" 2>/dev/null || true
  [[ $KEEP -eq 0 ]] && rm -rf "$WORK"
  return 0
}
trap cleanup EXIT

check_history_exchange() {
  echo "==> Verlauf-Austausch: Ankuendigung"
  local announced
  announced="$(curl -sf -b "$COOKIES" -H "$SECURE_HEADER" "${BASE}/api/v1/history/exchange")" || {
    echo "FEHLER: Ankuendigung nicht erreichbar" >&2
    return 1
  }
  echo "${announced}" | grep -q '"protocol":1' || {
    echo "FEHLER: Ankuendigung nennt nicht Protokoll 1: ${announced}" >&2
    return 1
  }
  echo "${announced}" | grep -q '"tiers":\["1m","5m"\]' || {
    echo "FEHLER: Ankuendigung nennt nicht genau die Stufen 1m und 5m" >&2
    return 1
  }

  echo "==> Verlauf-Austausch: zwei gleichzeitige Peers"
  local first_out second_out
  first_out="$(mktemp)"
  second_out="$(mktemp)"
  curl -sN --max-time 8 -b "$COOKIES" -H "$SECURE_HEADER" "${BASE}/api/v1/history/exchange/stream" >"${first_out}" &
  local first_pid=$!
  curl -sN --max-time 8 -b "$COOKIES" -H "$SECURE_HEADER" "${BASE}/api/v1/history/exchange/stream" >"${second_out}" &
  local second_pid=$!
  sleep 2

  local first_peer
  first_peer="$(grep -m1 '"peer_id"' "${first_out}" | sed 's/.*"peer_id":"\([^"]*\)".*/\1/')"
  if [ -z "${first_peer}" ]; then
    echo "FEHLER: erster Peer hat kein hello erhalten" >&2
    kill "${first_pid}" "${second_pid}" 2>/dev/null
    return 1
  fi

  curl -sf -X POST -b "$COOKIES" -H "$SECURE_HEADER" "${BASE}/api/v1/history/exchange/offer" \
    -H 'Content-Type: application/json' \
    -d "{\"peer\":\"${first_peer}\",\"coverage\":{\"1m\":{\"role:pv\":{\"from\":0,\"step\":3600000,\"n\":[60]}}}}" \
    -o /dev/null || {
    echo "FEHLER: Angebot wurde abgelehnt" >&2
    kill "${first_pid}" "${second_pid}" 2>/dev/null
    return 1
  }
  sleep 2
  kill "${first_pid}" "${second_pid}" 2>/dev/null
  wait "${first_pid}" "${second_pid}" 2>/dev/null

  grep -q "\"peer\":\"${first_peer}\"" "${second_out}" || {
    echo "FEHLER: das Angebot kam beim zweiten Peer nicht an" >&2
    cat "${second_out}" >&2
    return 1
  }
  grep -q "\"peer\":\"${first_peer}\"" "${first_out}" && {
    echo "FEHLER: der Absender hat sein eigenes Angebot zurueckerhalten" >&2
    return 1
  }
  rm -f "${first_out}" "${second_out}"
  echo "    ok: Angebot erreichte genau den anderen Peer"
}

free_port "$HTTP_PORT"
free_port "$MQTT_PORT"

mkdir -p "$WORK/devices" "$WORK/data"
# Nur Paare aus *.json + *.schema.json sind fuer den Manager sichtbar.
cp "$DEVICES_SOURCE"/*.json "$WORK/devices/" 2>/dev/null || true
echo "Konfigurationen: $(ls "$WORK/devices" | tr '\n' ' ')"

# Dasselbe Muster wie --devices, nur fuer den Datenordner: settings.json,
# layout.json, energy.json, device-map.json werden gelesen, bevor das
# Dashboard sie das erste Mal selbst schreibt.
# users.json bewusst mitkopierbar, aber mit Vorsicht: liegt eine Datei da,
# legt DASHBOARD_ADMIN_PASSWORD keinen Admin mehr an und die Anmeldung unten
# scheitert, wenn der Hash nicht zu "$PASSWORD" passt.
if [[ -n "$SEED_DATA" ]]; then
  cp "$SEED_DATA"/*.json "$WORK/data/" 2>/dev/null || true
  echo "Seed-Daten: $(ls "$WORK/data" | tr '\n' ' ')"
fi

# Nach dem Kopieren, damit --theme ein geseedetes settings.json ueberstimmt.
# Teilweise gefuellte settings.json sind gueltig: Settings.UnmarshalJSON
# startet von Default() und ergaenzt jedes fehlende Feld.
if [[ -n "$THEME" ]]; then
  python3 - "$WORK/data/settings.json" "$THEME" <<'PY'
import json, pathlib, sys
path, theme = pathlib.Path(sys.argv[1]), sys.argv[2]
data = json.loads(path.read_text(encoding="utf-8")) if path.exists() else {}
data["theme"] = theme
path.write_text(json.dumps(data, indent=2) + "\n", encoding="utf-8")
PY
  echo "Farbschema: $THEME"
fi

echo "Fixture: $FIXTURE"
BROKER_ARGS=("$MQTT_PORT" "$FIXTURE")
[[ $SIMULATE -eq 1 ]] && BROKER_ARGS+=("--simulate")
python3 "$HERE/minibroker.py" "${BROKER_ARGS[@]}" > "$WORK/broker.log" 2>&1 &
BROKER_PID=$!
sleep 1

# Der Smoke-Test schreibt eine vollstaendige config.json ins
# Arbeitsverzeichnis und startet mit --config. So laeuft er ueber genau den
# Ladepfad, den auch das Zielgeraet nutzt, statt ihn zu umgehen.
printf '%s' "$PASSWORD" > "$WORK/auth.pw"
chmod 600 "$WORK/auth.pw"
python3 - "$SCRIPT_ROOT/services/energy-node.config.json" "$WORK/config.json" \
         "$HTTP_PORT" "$WORK/devices" "$WORK/data" "$MQTT_PORT" "$WORK/auth.pw" <<'PY'
import json
import sys

vorlage, ziel, http_port, devices_dir, data_dir, mqtt_port, admin_pw = sys.argv[1:8]
config = json.loads(open(vorlage, encoding="utf-8").read())
config["mqtt"]["host"] = "127.0.0.1"
config["mqtt"]["port"] = int(mqtt_port)
config["mqtt"]["username"] = ""
config["mqtt"]["password_file"] = ""
config["paths"]["devices_dir"] = devices_dir
config["paths"]["data_dir"] = data_dir
config["dashboard"]["port"] = int(http_port)
config["dashboard"]["bind_address"] = "127.0.0.1"
config["dashboard"]["admin_password_file"] = admin_pw
open(ziel, "w", encoding="utf-8").write(json.dumps(config, indent=2) + "\n")
PY

# Ohne -ldflags zeigt die Versions-Vorschau in den Einstellungen nur den
# main.go-Default "dev" - das verschleiert, welcher Stand tatsaechlich
# laeuft. Stattdessen dashboard/VERSION mit "-dev" kombinieren, analog zum
# Branch-Suffix in scripts/deploy/deploy_dashboard_to_remote.sh.
DASHBOARD_VERSION="$(tr -d '[:space:]' < "$DASHBOARD_DIR/VERSION")-dev"
(
  cd "$DASHBOARD_DIR"
  go run -ldflags "-X main.buildVersion=${DASHBOARD_VERSION}" ./cmd/dashboard --config "$WORK/config.json"
) > "$WORK/dashboard.log" 2>&1 &
DASHBOARD_PID=$!

BASE="http://localhost:$HTTP_PORT"
# Jede API-Antwort zaehlt als "erreichbar" - auch 401, denn die Endpunkte
# verlangen eine Anmeldung, die es an dieser Stelle noch nicht gibt.
listening() { [[ "$(curl -s -m 2 -o /dev/null -w '%{http_code}' "$BASE/api/v1/health")" != "000" ]]; }
for _ in $(seq 1 60); do
  listening && break
  sleep 1
done
if ! listening; then
  echo "Dashboard nicht erreichbar:" >&2
  cat "$WORK/dashboard.log" >&2
  exit 1
fi

COOKIES="$WORK/cookies.txt"
curl -sf -m 5 -c "$COOKIES" -H "$SECURE_HEADER" -H 'Content-Type: application/json' \
  -X POST "$BASE/api/v1/auth/login" \
  -d "{\"username\":\"admin\",\"password\":\"$PASSWORD\"}" > /dev/null
api() { curl -sf -m 5 -b "$COOKIES" -H "$SECURE_HEADER" "$@"; }

# Die Registry braucht einen Moment, bis Discovery und Zustaende verarbeitet sind.
sleep 2

echo
echo "=== /api/v1/topics ==="
api "$BASE/api/v1/topics" | python3 -m json.tool
echo "=== /api/v1/topics/samples ==="
api "$BASE/api/v1/topics/samples" | python3 -m json.tool

echo "=== Pruefungen ==="
FAILED=0
check() { # name, bedingung-als-python-ausdruck ueber `data`
  local name="$1" expression="$2" source="$3"
  if api "$source" | python3 -c "
import json, sys
data = json.load(sys.stdin)
sys.exit(0 if ($expression) else 1)
"; then
    echo "  OK   $name"
  else
    echo "  FEHL $name"
    FAILED=1
  fi
}

# Die beiden folgenden Proben stehen so nur in fixtures/battery-soc.json -
# mit --fixture liegen andere Topics an, dann sagen sie nichts aus.
if [[ "$(basename "$FIXTURE")" == "battery-soc.json" ]]; then
  check "Topic mit Payload wird als Probe geliefert" \
    "any(s['topic'] == 'shelly/netz_meanwell/status' and 'apower' in s.get('payload', '') for s in data)" \
    "$BASE/api/v1/topics/samples"
  check "Topic ohne Nachricht bleibt ohne Payload" \
    "any(s['topic'] == 'bms/bank_b/voltage' and not s.get('payload') for s in data)" \
    "$BASE/api/v1/topics/samples"
  check "Topic-Proben nennen das zugehoerige Geraet" \
    "any(s['topic'] == 'shelly/netz_meanwell/status' and s.get('device') == 'MeanWell-Ladegeraet (Netzseite)' for s in data)" \
    "$BASE/api/v1/topics/samples"
  check "Geraetename steht auch ohne Nachricht auf dem Topic" \
    "any(s['topic'] == 'bms/bank_b/voltage' and s.get('device') and not s.get('payload') for s in data)" \
    "$BASE/api/v1/topics/samples"
  check "Live-Spannung der Batterieanlage ist als Probe abrufbar" \
    "any(s['topic'] == 'outstation/battery_soc/state' and 'pack_corrected_v_per_cell' in s.get('payload', '') for s in data)" \
    "$BASE/api/v1/topics/samples"
fi
check "Proben sind nach Topic sortiert" \
  "[s['topic'] for s in data] == sorted(s['topic'] for s in data)" \
  "$BASE/api/v1/topics/samples"
check "battery_soc_devices ist als Konfiguration sichtbar" \
  "any(c['name'] == 'battery_soc_devices' for c in data)" \
  "$BASE/api/v1/configurations"

# Der gemeldete Bug: Kommazahlen muessen gespeichert werden koennen.
CONFIG_URL="$BASE/api/v1/configurations/battery_soc_devices"
api "$CONFIG_URL" | python3 -c "
import json, sys
data = json.load(sys.stdin)
data[0]['charge_efficiency'] = 0.95
data[0]['full_v_per_cell'] = 3.6
json.dump(data, open('$WORK/valid.json', 'w'))
"
if curl -sf -m 5 -b "$COOKIES" -H "$SECURE_HEADER" -H 'Content-Type: application/json' \
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
if curl -sf -m 5 -b "$COOKIES" -H "$SECURE_HEADER" -H 'Content-Type: application/json' \
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
code=$(curl -s -m 5 -o /dev/null -w '%{http_code}' -b "$COOKIES" -H "$SECURE_HEADER" \
  -H 'Content-Type: application/json' -X PUT "$CONFIG_URL" --data-binary "@$WORK/invalid.json")
if [[ "$code" == "400" || "$code" == "422" ]]; then
  echo "  OK   ungueltiger Wert 1.5 abgelehnt (HTTP $code)"
else
  echo "  FEHL ungueltiger Wert 1.5 kam mit HTTP $code durch"; FAILED=1
fi

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
if [[ -n "$THEME" ]]; then
  check "Farbschema aus --theme ist aktiv" \
    "data['theme'] == '$THEME'" \
    "$BASE/api/v1/settings"
fi

# fixtures/energie-ueberschuss.json ist genau auf die Rollen in
# fixtures/seed/energie-alle-karten/energy.json gerechnet: die Bilanz muss
# ohne Rest aufgehen. Geht sie das nicht, stimmt die Rollenaufloesung nicht
# mehr - und jede Sichtpruefung der Energie-Karten waere wertlos. Nur ohne
# --simulate pruefbar: die Sinuskurven dort bewegen PV/Netz/Batterie absichtlich
# von diesen exakten Werten weg.
if [[ "$(basename "$FIXTURE")" == "energie-ueberschuss.json" && -f "$WORK/data/energy.json" && $SIMULATE -eq 0 ]]; then
  check "PV-Ueberschuss steht vollstaendig in der Bilanz" \
    "data['balance']['pv'] == 4200 and data['balance']['grid_export'] == 1250 and data['balance']['battery_charge'] == 900" \
    "$BASE/api/v1/energy"
  check "Energiebilanz geht ohne offene Luecke auf" \
    "data['balance']['gap_applied'] == 0 and data['balance']['base'] == 1000" \
    "$BASE/api/v1/energy"
fi

# --simulate schiebt PV/Netz/Batterie/Last per Sinuskurve ueber echte MQTT-
# PUBLISH-Pakete auf der offenen Verbindung - genau der Weg, ueber den auch ein
# echtes Geraet Live-Updates liefert. Ohne diese Probe wuerde ein Bruch dieses
# Pfads (z. B. eine vergessene Sperre auf der Verbindung) erst beim manuellen
# --keep-Blick in den Browser auffallen, als scheinbar "hangender" Chart.
if [[ $SIMULATE -eq 1 && "$(basename "$FIXTURE")" == "energie-ueberschuss.json" ]]; then
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
fi

check_history_exchange || FAILED=1

echo
if [[ $KEEP -eq 1 ]]; then
  echo "Dashboard laeuft weiter: $BASE  (Login admin / $PASSWORD)"
  echo "Arbeitsverzeichnis: $WORK"
  # go:embed bindet Templates/CSS/JS beim Kompilieren ein - dieser Prozess
  # zeigt bis zu seinem Ende den Stand von eben, auch bei neuen Dateien.
  echo "Achtung: Quelldateien (Templates, CSS, JS) sind einkompiliert -"
  echo "nach jeder Aenderung neu starten, sonst pruefst du den alten Stand."
  echo "Abbruch mit Strg-C."
  wait "$DASHBOARD_PID"
fi

[[ $FAILED -eq 0 ]] && echo "Smoke-Test bestanden." || echo "Smoke-Test FEHLGESCHLAGEN."
exit $FAILED
