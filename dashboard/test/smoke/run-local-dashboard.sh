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
#   ./run-local-dashboard.sh --fake-companion   # fake_companion.py speist die Energiestatistik
#                                                # mit erfundenen Stundenwerten (400 Tage)
#   ./run-local-dashboard.sh --installed-services-off  # installed_services: alle Dienste aus
#   ./run-local-dashboard.sh --https             # echtes TLS statt X-Forwarded-Proto-Trick
#   ./run-local-dashboard.sh --reject-config NAME  # der simulierte Dienst lehnt NAME
#                                                # (z. B. battery_soc_devices) ab -
#                                                # Sichtpruefung fuer "Abgelehnt" auf
#                                                # der Konfigurationsseite. Mehrfach angebbar
#   ./run-local-dashboard.sh --simulate-installed  # legt einen Installer-Zustand
#                                                # (fixtures/installed-state: Manifest,
#                                                # Changelog, Auswahl) an, Sichtpruefung
#                                                # fuer Einstellungen > Versionen. Nicht
#                                                # mit --simulate-package kombinierbar
#   ./run-local-dashboard.sh --real-changelog   # wie --simulate-installed, aber mit dem
#                                                # echten Changelog des Repos (real_changelog.py),
#                                                # der Vorgaenger ist der letzte Tag. Die ersten
#                                                # zwei Highlights tragen Beispiel-Details. Die
#                                                # Fixture-Proben der Versionsseite entfallen.
#   ./run-local-dashboard.sh --simulate-update  # Update-Pruefung meldet immer v9.9.9
#                                                # verfuegbar (fake_github_releases.py statt
#                                                # der echten GitHub-API) - Sichtpruefung fuer
#                                                # die Masthead-Pille und den Button in
#                                                # Systemzugriff
#   ./run-local-dashboard.sh --simulate-package # wie --simulate-update, und der Fake-GitHub bietet
#                                                # zusaetzlich ein Bundle zum Herunterladen an:
#                                                # "Aktualisieren" (/redeploy/) laedt es, dann
#                                                # folgt die Vorschau (Paketbezug, OTA)
#   ./run-local-dashboard.sh --simulate-journal # simulierter system-action-Helper (read-journal)
#                                                # plus sudo-Shim, damit die Service-Logs der
#                                                # Diagnoseseite Zeilen zeigen. Braucht eine
#                                                # Admin-Anmeldung, also meist --https
#   ./run-local-dashboard.sh --preset NAME      # --fixture/--seed-data/--simulate gebuendelt,
#                                                # siehe PRESETS unten; einzelne Flags danach
#                                                # ueberstimmen das Preset
#
# PRESETS (--preset NAME):
#   battery-soc      fixtures/battery-soc.json, kein Seed (frueherer Skript-Standard)
#   energie          fixtures/energie-ueberschuss.json + seed/energie-alle-karten
#   energie-simulate wie energie, zusaetzlich --simulate
#   uebersicht-push  wie energie-simulate, zusaetzlich entity_value und
#                    entity_group - das Raster, das Stufe 3 tauschfrei macht
#   energie-kombiniert  fixtures/energie-kombiniert.json + seed/energie-kombiniert
#                       (load_mode "combined", Band/Board einzeln, Ring gesammelt)
#   alle-funktionen  Skript-Standard ohne weitere Flags. fixtures/alle-funktionen.json + seed/alle-funktionen ("teuer,
#                    aber nahezu vollstaendig", siehe README)
#   geraete-kacheln  fixtures/geraete-kacheln.json + seed/geraete-kacheln (zwei
#                    schmale device-Kacheln mit Slider, span "1" auf der
#                    18rem-Untergrenze - Sichtpruefung fuer Slider-Breite und
#                    Titel-Umbruch in .device-tile-entity)
#   docs-screenshots fixtures/alle-funktionen.json + seed/docs-screenshots, dazu
#                    die Automationsregeln (Schema aus services/automation,
#                    eine Beispielregel aus fixtures/devices/docs-screenshots);
#                    Grundlage fuer docs-screenshots.mjs und docs/images/
#   notification     fixtures/notification.json, kein Seed - simuliert die
#                    Automations-Topics (last_event {at, message}, state,
#                    status/online). notifications.js pollt daraus
#                    /api/v1/automation/notification und wirft beim Laden einen
#                    Warn-Toast; das Geraet "Energie-Automationen" erscheint in
#                    der Uebersicht.
#   keine-optionalen-dienste  fixtures/battery-soc.json, kein Seed, zusaetzlich
#                    --installed-services-off - installed_services in
#                    config.json wird auf alle sieben Dienste = false gesetzt
#                    (Installer-Spec E7). Sichtpruefung + automatisierte
#                    Probe, dass der Automationen-Tab sowie die
#                    Tailscale-/TinyTuya-Unterseiten dann fehlen.
#   shelly-ht        fixtures/shelly-ht.json + seed/shelly-ht, zusaetzlich die
#                    Shelly-Konfiguration (services/shelly: Schema + Presets,
#                    Geraete aus fixtures/devices/shelly-ht) - zwei schlafende
#                    H&T (Gen1 online, Plus nach offline_grace_s offline).
#                    Automatisierte Proben: H&T-Presets sind sleepy, sleepy/
#                    offline_grace_s ueberstehen Schema-Validierung und
#                    Speichern, Temperatur/Feuchte kommen als Proben an.
#   verlauf-kachel   wie energie-simulate, mit einer gebundenen und einer
#                    eigenen history_view-Kachel; Pruefung mit history-view-tile.mjs
#   statistik        wie verlauf-kachel, mit der Kachel energy_stats und dem
#                    Fake-Companion (Energiestatistik mit Daten)
#   service-logs     wie battery-soc, mit --https und --simulate-journal: ein
#                    simulierter system-action-Helper liefert Journal-Zeilen
#                    fuer die Service-Logs der Diagnoseseite (Login admin,
#                    Selbstsigniert-Warnung im Browser bestaetigen)
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
# Weitere Konfigurationsverzeichnisse nach DEVICES_SOURCE, in dieser
# Reihenfolge kopiert - eine spaetere Datei gleichen Namens gewinnt.
EXTRA_DEVICES=()
FIXTURE="$HERE/fixtures/battery-soc.json"
SEED_DATA=""
THEME=""
KEEP=0
SIMULATE=0
FAKE_COMPANION=0
INSTALLED_SERVICES_OFF=0
HTTPS=0
SIMULATE_UPDATE=0
SIMULATE_JOURNAL=0
SIMULATE_PACKAGE=0
SIMULATE_INSTALLED=0
REAL_CHANGELOG=0
REJECT_CONFIGS=()
HTTP_PORT="${DASHBOARD_SMOKE_PORT:-18100}"
MQTT_PORT="${DASHBOARD_SMOKE_MQTT_PORT:-18883}"
UPDATES_API_PORT="${DASHBOARD_SMOKE_UPDATES_API_PORT:-18884}"
PASSWORD="smoketest1234"
# isSecureRequest() akzeptiert TLS oder diesen Header - ohne ihn antwortet der
# Login mit "secure_login_required". Bei --https laeuft der Server mit einem
# echten (selbstsignierten) Zertifikat, dann ist der Header ueberfluessig,
# schadet aber nicht.
SECURE_HEADER="X-Forwarded-Proto: https"

# Ohne --preset/--fixture/--seed-data/--devices gilt das Preset alle-funktionen,
# das auch die Automationen mitbringt. Das alte Verhalten bleibt mit
# --preset battery-soc erreichbar.
if ! printf '%s\n' "$@" | grep -qxE -e '--preset|--fixture|--seed-data|--devices'; then
  set -- --preset alle-funktionen "$@"
fi

while [[ $# -gt 0 ]]; do
  case "$1" in
    --keep) KEEP=1; shift ;;
    --simulate) SIMULATE=1; shift ;;
    --fake-companion) FAKE_COMPANION=1; shift ;;
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
          EXTRA_DEVICES=("$REPO_DIR/services/automation" "$HERE/fixtures/devices/alle-funktionen")
          ;;
        geraete-kacheln)
          FIXTURE="$HERE/fixtures/geraete-kacheln.json"
          SEED_DATA="$HERE/fixtures/seed/geraete-kacheln"
          ;;
        docs-screenshots)
          FIXTURE="$HERE/fixtures/alle-funktionen.json"
          SEED_DATA="$HERE/fixtures/seed/docs-screenshots"
          # Ohne das Schema meldet das Dashboard "configuration
          # automation_rules has no matching schema"; die Regel aus der
          # Fixture ueberschreibt die leere Vorgabe des Dienstes.
          EXTRA_DEVICES=("$REPO_DIR/services/automation" "$HERE/fixtures/devices/docs-screenshots")
          ;;
        notification)
          FIXTURE="$HERE/fixtures/notification.json"; SEED_DATA=""
          ;;
        keine-optionalen-dienste)
          FIXTURE="$HERE/fixtures/battery-soc.json"; SEED_DATA=""
          INSTALLED_SERVICES_OFF=1
          ;;
        shelly-ht)
          FIXTURE="$HERE/fixtures/shelly-ht.json"
          SEED_DATA="$HERE/fixtures/seed/shelly-ht"
          # Schema und Presets aus dem Dienst selbst, damit die Probe den
          # Stand des Branches prueft; die Geraeteliste aus der Fixture.
          EXTRA_DEVICES=("$REPO_DIR/services/shelly" "$HERE/fixtures/devices/shelly-ht")
          ;;
        verlauf-kachel)
          FIXTURE="$HERE/fixtures/energie-ueberschuss.json"
          SEED_DATA="$HERE/fixtures/seed/verlauf-kachel"
          SIMULATE=1
          ;;
        statistik)
          FIXTURE="$HERE/fixtures/energie-ueberschuss.json"
          SEED_DATA="$HERE/fixtures/seed/statistik"
          SIMULATE=1
          FAKE_COMPANION=1
          ;;
        service-logs)
          FIXTURE="$HERE/fixtures/battery-soc.json"; SEED_DATA=""
          HTTPS=1
          SIMULATE_JOURNAL=1
          ;;
        *)
          echo "unbekanntes Preset: $2 (battery-soc, energie, energie-simulate, uebersicht-push, energie-kombiniert, alle-funktionen, docs-screenshots, geraete-kacheln, notification, keine-optionalen-dienste, shelly-ht, verlauf-kachel, statistik, service-logs)" >&2
          exit 2
          ;;
      esac
      shift 2
      ;;
    --devices) DEVICES_SOURCE="$2"; shift 2 ;;
    --fixture) FIXTURE="$2"; shift 2 ;;
    --seed-data) SEED_DATA="$2"; shift 2 ;;
    --theme) THEME="$2"; shift 2 ;;
    --installed-services-off) INSTALLED_SERVICES_OFF=1; shift ;;
    --https) HTTPS=1; shift ;;
    --simulate-journal) SIMULATE_JOURNAL=1; shift ;;
    --simulate-update) SIMULATE_UPDATE=1; shift ;;
    --simulate-package) SIMULATE_UPDATE=1; SIMULATE_PACKAGE=1; shift ;;
    --simulate-installed) SIMULATE_INSTALLED=1; shift ;;
    --real-changelog) SIMULATE_INSTALLED=1; REAL_CHANGELOG=1; shift ;;
    --reject-config) REJECT_CONFIGS+=("$2"); shift 2 ;;
    --port) HTTP_PORT="$2"; shift 2 ;;
    *) echo "unbekannte Option: $1" >&2; exit 2 ;;
  esac
done

if [[ $SIMULATE_INSTALLED -eq 1 && $SIMULATE_PACKAGE -eq 1 ]]; then
  echo "--simulate-installed und --simulate-package schreiben beide installed-manifest.json, bitte nur eins" >&2; exit 2
fi

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
FAKE_COMPANION_PID=""
DASHBOARD_PID=""
UPDATES_API_PID=""

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
  [[ -n "$FAKE_COMPANION_PID" ]] && kill "$FAKE_COMPANION_PID" 2>/dev/null || true
  [[ -n "$UPDATES_API_PID" ]] && kill "$UPDATES_API_PID" 2>/dev/null || true
  # `go run` startet das Binary als Kindprozess - der ueberlebt das kill sonst.
  pkill -f "go-build.*exe/dashboard" 2>/dev/null || true
  [[ $KEEP -eq 0 ]] && rm -rf "$WORK"
  return 0
}
trap cleanup EXIT

check_history_exchange() {
  echo "==> Verlauf-Austausch: Ankuendigung"
  local announced
  announced="$(curl -sf "${CURL_INSECURE[@]}" -b "$COOKIES" -H "$SECURE_HEADER" "${BASE}/api/v1/history/exchange")" || {
    echo "FEHLER: Ankuendigung nicht erreichbar" >&2
    return 1
  }
  echo "${announced}" | grep -q '"protocol":1' || {
    echo "FEHLER: Ankuendigung nennt nicht Protokoll 1: ${announced}" >&2
    return 1
  }
  echo "${announced}" | grep -q '"tiers":\["1m","5m","1h"\]' || {
    echo "FEHLER: Ankuendigung nennt nicht genau die Stufen 1m, 5m und 1h" >&2
    return 1
  }

  echo "==> Verlauf-Austausch: zwei gleichzeitige Peers"
  local first_out second_out
  first_out="$(mktemp)"
  second_out="$(mktemp)"
  curl -sN "${CURL_INSECURE[@]}" --max-time 8 -b "$COOKIES" -H "$SECURE_HEADER" "${BASE}/api/v1/history/exchange/stream" >"${first_out}" &
  local first_pid=$!
  curl -sN "${CURL_INSECURE[@]}" --max-time 8 -b "$COOKIES" -H "$SECURE_HEADER" "${BASE}/api/v1/history/exchange/stream" >"${second_out}" &
  local second_pid=$!
  sleep 2

  local first_peer
  first_peer="$(grep -m1 '"peer_id"' "${first_out}" | sed 's/.*"peer_id":"\([^"]*\)".*/\1/')"
  if [ -z "${first_peer}" ]; then
    echo "FEHLER: erster Peer hat kein hello erhalten" >&2
    kill "${first_pid}" "${second_pid}" 2>/dev/null
    return 1
  fi

  curl -sf "${CURL_INSECURE[@]}" -X POST -b "$COOKIES" -H "$SECURE_HEADER" "${BASE}/api/v1/history/exchange/offer" \
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
[[ $SIMULATE_UPDATE -eq 1 ]] && free_port "$UPDATES_API_PORT"

mkdir -p "$WORK/devices" "$WORK/data"
# --simulate-installed: dasselbe Zustandsverzeichnis, das Installer und Updater
# auf einem echten Knoten unter /var/lib/energy-node-installer hinterlassen.
if [[ $SIMULATE_INSTALLED -eq 1 ]]; then
  cp -r "$HERE/fixtures/installed-state" "$WORK/installer-state"
fi
# Nur Paare aus *.json + *.schema.json sind fuer den Manager sichtbar.
cp "$DEVICES_SOURCE"/*.json "$WORK/devices/" 2>/dev/null || true
for extra in "${EXTRA_DEVICES[@]}"; do
  cp "$extra"/*.json "$WORK/devices/" 2>/dev/null || true
done
echo "Konfigurationen: $(ls "$WORK/devices" | tr '\n' ' ')"

# Je Konfiguration ein Dienst-Manifest neben config.json, wie es der Installer
# unter manifests/ ablegt. Nur fuer die dort genannten Dienste beobachtet das
# Dashboard outstation/<id>/settings/status - und genau diese Topics meldet
# minibroker.py --service-status unten fuer jede Datei im devices-Ordner.
mkdir -p "$WORK/manifests"
python3 - "$WORK/devices" "$WORK/manifests" <<'PY'
import json, pathlib, sys
devices, manifests = pathlib.Path(sys.argv[1]), pathlib.Path(sys.argv[2])
for path in devices.glob("*.json"):
    name = path.stem
    if name.endswith(".schema"):
        continue
    # Spiegel von config.ServiceIDForConfig (internal/config/config.go).
    service_id = "automation" if name == "automation_rules" else (name[:-len("_devices")] if name.endswith("_devices") else "")
    if service_id:
        (manifests / f"{service_id}.json").write_text(json.dumps({"service_id": service_id}) + "\n", encoding="utf-8")
PY
echo "Dienste: $(ls "$WORK/manifests" | sed 's/\.json$//' | tr '\n' ' ')"

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
BROKER_ARGS=("$MQTT_PORT" "$FIXTURE" "--service-status" "$WORK/devices")
[[ $SIMULATE -eq 1 ]] && BROKER_ARGS+=("--simulate")
for name in "${REJECT_CONFIGS[@]}"; do
  BROKER_ARGS+=("--reject-config" "$name")
done
python3 "$HERE/minibroker.py" "${BROKER_ARGS[@]}" > "$WORK/broker.log" 2>&1 &
BROKER_PID=$!
sleep 1

FAKE_GITHUB_ARGS=("$UPDATES_API_PORT" "v9.9.9")
if [[ $SIMULATE_PACKAGE -eq 1 ]]; then
  # Ein Mini-Bundle fuer die Architektur, auf der das Dashboard hier laeuft
  # (internal/bundlefetch fragt nach runtime.GOARCH), plus ein Zustands-
  # verzeichnis, wie es ein installierter Node hat: Auswahl und das Manifest
  # der installierten Version, damit die Vorschau etwas zum Vergleichen hat.
  # Signiert ist es nicht wirklich - das Dashboard prueft nur, dass
  # manifest.json.sig existiert; die Signatur prueft erst der Updater als root.
  case "$(cd "$DASHBOARD_DIR" && go env GOARCH)" in
    arm) PACKAGE_ARCH="armv6" ;;
    *) PACKAGE_ARCH="$(cd "$DASHBOARD_DIR" && go env GOARCH)" ;;
  esac
  PACKAGE_ASSET="energy-node-v9.9.9-$PACKAGE_ARCH.tar.gz"
  mkdir -p "$WORK/package" "$WORK/installer-state"
  python3 - "$WORK/package/$PACKAGE_ASSET" "$PACKAGE_ARCH" "$WORK/installer-state" <<'PY'
import io, json, os, sys, tarfile, urllib.parse

archive, arch, state = sys.argv[1:4]
manifest = {
    "version": "v9.9.9",
    "arch": arch,
    "components": {"bootstrap": "1.2.0", "dashboard": "9.9.9", "services": "3.4.0"},
    "steps": [
        {"id": "10", "optional": False}, {"id": "20", "optional": False},
        {"id": "30", "optional": False}, {"id": "40", "optional": True},
        {"id": "50", "optional": False}, {"id": "60", "optional": False},
        {"id": "65", "optional": False}, {"id": "70", "optional": True},
        # Zwei Dienstschritte, damit die Vorschau etwas zum Neustart-Vergleich
        # hat: battery_soc geaendert (Neustart), apsystems unveraendert.
        {"id": "81", "optional": True, "service_id": "apsystems", "dir": "apsystems_ez1", "unit": "apsystems-ez1.service", "version": "v0.4.0"},
        {"id": "82", "optional": True, "service_id": "battery_soc", "dir": "battery_soc", "unit": "battery-soc.service", "version": "v0.5.0"},
    ],
}
def entry(text, scope, pr, breaking=False, details=None):
    e = {"text": text, "breaking": breaking, "scope": scope, "pr": pr, "highlight": True}
    if details:
        e["details"] = details
    return e
# Beispiel-Details fuer die Neuerungen-Zeilen: Bild mit Ausschnitt und Doku-Link,
# dazu ein unerreichbares Bild (Bild und Umschalter muessen verschwinden).
SHOT = ("<svg xmlns='http://www.w3.org/2000/svg' width='1000' height='600' viewBox='0 0 1000 600'>"
        "<rect width='1000' height='600' fill='#dfe7ee'/><rect x='500' y='120' width='500' height='240' fill='#f2a33a'/>"
        "<text x='750' y='250' font-size='40' text-anchor='middle' font-family='sans-serif'>crop area</text>"
        "<text x='250' y='300' font-size='40' text-anchor='middle' font-family='sans-serif'>whole image</text></svg>")
DETAILS_SHOT = {
    "text": {"en": "Open a row to see what changed. The image starts on the crop and can show the whole picture.",
             "de": "Zeile öffnen, um zu sehen, was sich geändert hat. Das Bild zeigt zuerst den Ausschnitt, auf Wunsch das ganze Bild."},
    "image": {"url": "data:image/svg+xml," + urllib.parse.quote(SHOT), "width": 1000, "height": 600, "crop": [0.5, 0.2, 0.5, 0.4]},
    "doc": "https://developer-simon.github.io/energy-node/dashboard/settings-versions.html",
}
DETAILS_BROKEN = {
    "text": {"en": "This row has an unreachable image. Image and toggle must disappear."},
    "image": {"url": "http://127.0.0.1:9/missing.png", "width": 1000, "height": 600, "crop": [0.1, 0.1, 0.5, 0.5]},
}
# Ein Commit innerhalb eines PRs: Detail, kein Highlight (make_changelog_json.py).
def detail(text, scope, sha):
    return {"text": text, "breaking": False, "scope": scope, "hash": sha, "highlight": False}

# Was-ist-neu: installiert sind Dashboard 9.9.7, Bootstrap 1.1.0 und
# battery_soc v0.4.0 (siehe installed-manifest.json unten). Die Seite zeigt nur,
# was darueber liegt, mit dem Breaking Change von 9.9.8 oben.
changelog = {
    "schema_version": 1,
    "generated_at": "2026-10-01T12:00:00+02:00",
    "bundle_version": "v9.9.9",
    "components": [
        {"id": "dashboard", "label": "Dashboard", "kind": "app", "version": "9.9.9", "releases": [
            {"version": "9.9.9", "date": "2026-10-01", "groups": [
                {"type": "feat", "label": "Features", "entries": [
                    entry("show what is new before an update", "redeploy", 82, details=DETAILS_SHOT),
                    detail("count the changes per type in the preview card", "redeploy", "a1b2c3d")]},
                {"type": "fix", "label": "Fixes", "entries": [
                    entry("rest the battery flow icon on its fill level at 0 W", "energy", 81, details=DETAILS_BROKEN),
                    detail("keep the changelog filter when the language switches", "redeploy", "b2c3d4e")]},
                {"type": "refactor", "label": "Refactors", "entries": [
                    detail("share the changelog model between installer and dashboard", "redeploy", "c3d4e5f")]}]},
            {"version": "9.9.8", "date": "2026-09-28", "groups": [
                {"type": "feat", "label": "Features", "entries": [
                    entry("add a versions page", "settings", 71),
                    entry("fold the config.json node block into dashboard.node_*", "config", 70, True)]}]},
            {"version": "9.9.7", "date": "2026-09-20", "groups": [
                {"type": "feat", "label": "Features", "entries": [
                    entry("already installed, must not appear", "settings", 60)]}]}]},
        {"id": "service:battery_soc", "label": "Batterie-SoC", "kind": "service", "version": "v0.5.0", "releases": [
            {"version": "v0.5.0", "date": "2026-09-30", "groups": [
                {"type": "feat", "label": "Features", "entries": [
                    entry("save the state on an interval and recover after a crash", "battery_soc", 78)]},
                {"type": "test", "label": "Tests", "entries": [
                    detail("cover a crash between two saves", "battery_soc", "d4e5f6a")]}]}]},
        {"id": "bootstrap", "label": "Bootstrap", "kind": "tool", "version": "1.2.0", "releases": [
            {"version": "1.2.0", "date": "2026-09-29", "groups": [
                {"type": "fix", "label": "Fixes", "entries": [
                    entry("keep the changed service selection in the update preview", "installer", 80)]}]}]},
    ],
}
files = {
    "./manifest.json": json.dumps(manifest).encode(),
    "./changelog.json": json.dumps(changelog).encode(),
    f"./changelog/{manifest['version']}.json": json.dumps({**changelog, "bundle_version": manifest["version"]}).encode(),
    "./manifest.json.sig": b"smoke-test-not-a-real-signature",
    "./bootstrap/10-apt.sh": b"#!/bin/sh\n",
    # Nicht komprimierbar, damit der Download ein paar Sekunden dauert und der
    # Fortschritt (20 %-Schritte) auf dem Bildschirm zu sehen ist.
    "./payload/wheels.bin": os.urandom(6 * 1024 * 1024),
}
with tarfile.open(archive, "w:gz") as tar:
    for name, body in files.items():
        info = tarfile.TarInfo(name)
        info.size = len(body)
        info.mode = 0o755 if name.endswith(".sh") else 0o644
        tar.addfile(info, io.BytesIO(body))
json.dump({"steps": {"40": True, "70": False, "81": True, "82": True}}, open(os.path.join(state, "selection.json"), "w"))
json.dump({
    "version": "v0.7.0",
    "components": {"bootstrap": "1.1.0", "dashboard": "9.9.7", "services": "3.4.0"},
    "steps": [
        {"id": "82", "dir": "battery_soc", "version": "v0.4.0"},
        {"id": "81", "dir": "apsystems_ez1", "version": "v0.4.0"},
    ],
}, open(os.path.join(state, "installed-manifest.json"), "w"))
PY
  FAKE_GITHUB_ARGS+=("$WORK/package/$PACKAGE_ASSET")
fi
if [[ $SIMULATE_UPDATE -eq 1 ]]; then
  python3 "$HERE/fake_github_releases.py" "${FAKE_GITHUB_ARGS[@]}" > "$WORK/updates-api.log" 2>&1 &
  UPDATES_API_PID=$!
  sleep 1
  echo "Update-Simulation: v9.9.9 ueber http://127.0.0.1:$UPDATES_API_PORT"
  [[ $SIMULATE_PACKAGE -eq 1 ]] && echo "Paketbezug-Simulation: $PACKAGE_ASSET wird als Release-Asset angeboten"
fi

# Der Smoke-Test schreibt eine vollstaendige config.json ins
# Arbeitsverzeichnis und startet mit --config. So laeuft er ueber genau den
# Ladepfad, den auch das Zielgeraet nutzt, statt ihn zu umgehen.
printf '%s' "$PASSWORD" > "$WORK/auth.pw"
chmod 600 "$WORK/auth.pw"

CURL_INSECURE=()
if [[ $HTTPS -eq 1 ]]; then
  # Selbstsigniertes Zertifikat fuer localhost - reicht fuer ListenAndServeTLS,
  # der Browser/curl muss ihm nur ausdruecklich vertrauen (-k bzw. Klick-durch).
  openssl req -x509 -newkey rsa:2048 -nodes -days 1 \
    -keyout "$WORK/tls.key" -out "$WORK/tls.crt" \
    -subj "/CN=localhost" -addext "subjectAltName=DNS:localhost,IP:127.0.0.1" \
    2>"$WORK/openssl.log" || {
    echo "FEHLER: Zertifikat konnte nicht erzeugt werden:" >&2
    cat "$WORK/openssl.log" >&2
    exit 1
  }
  CURL_INSECURE=(-k)
fi

python3 - "$SCRIPT_ROOT/services/energy-node.config.json" "$WORK/config.json" \
         "$HTTP_PORT" "$WORK/devices" "$WORK/data" "$MQTT_PORT" "$WORK/auth.pw" \
         "$INSTALLED_SERVICES_OFF" "$WORK/tls.crt" "$WORK/tls.key" "$HTTPS" \
         "$SIMULATE_JOURNAL" "$HERE/fake_journal_helper.py" <<'PY'
import json
import sys

(vorlage, ziel, http_port, devices_dir, data_dir, mqtt_port, admin_pw,
 services_off, tls_cert, tls_key, https, simulate_journal, journal_helper) = sys.argv[1:14]
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
# --simulate-journal: der Helper ist ein Skript im Repo, `sudo -n` wird ueber
# einen Shim im PATH (siehe unten) zum direkten Aufruf. Jede andere Aktion
# beantwortet der Fake mit Exit 64, Reboot und Poweroff bleiben wirkungslos.
if simulate_journal == "1":
    config["dashboard"]["system_action_helper"] = journal_helper
if https == "1":
    config["dashboard"]["tls"] = {"cert_file": tls_cert, "key_file": tls_key}
# --installed-services-off: alle sieben Dienste explizit aus, statt den
# Block wegzulassen - so wird genau der Pfad geprueft, den
# 65-dashboard-config.sh nach einer Installation ohne optionale Dienste auf
# dem Node hinterlaesst (Installer-Spec E7), nicht nur das "kein Block =
# alles an"-Verhalten.
if services_off == "1":
    config["installed_services"] = {
        key: False
        for key in (
            "apsystems",
            "automation",
            "battery_soc",
            "shelly",
            "tailscale",
            "trucki",
            "tuya",
        )
    }
open(ziel, "w", encoding="utf-8").write(json.dumps(config, indent=2) + "\n")
PY

# Ohne -ldflags zeigt die Versions-Vorschau in den Einstellungen nur den
# main.go-Default "dev" - das verschleiert, welcher Stand tatsaechlich
# laeuft. Stattdessen dashboard/VERSION mit "-dev" kombinieren, analog zum
# Branch-Suffix in scripts/deploy/deploy_dashboard_to_remote.sh.
DASHBOARD_VERSION="$(tr -d '[:space:]' < "$DASHBOARD_DIR/VERSION")-dev"
(
  cd "$DASHBOARD_DIR"
  if [[ $SIMULATE_UPDATE -eq 1 ]]; then
    export ENERGY_NODE_UPDATES_API_BASE="http://127.0.0.1:$UPDATES_API_PORT"
  fi
  if [[ $SIMULATE_JOURNAL -eq 1 ]]; then
    mkdir -p "$WORK/bin"
    cp "$HERE/fake_sudo.sh" "$WORK/bin/sudo"
    # 15 Minuten Vorlauf, damit die erste Abfrage sofort Zeilen findet.
    python3 -c 'import time; print(time.time() - 900)' > "$WORK/journal-epoch"
    export FAKE_JOURNAL_EPOCH="$WORK/journal-epoch"
    export PATH="$WORK/bin:$PATH"
  fi
  if [[ $SIMULATE_PACKAGE -eq 1 || $SIMULATE_INSTALLED -eq 1 ]]; then
    export ENERGY_NODE_INSTALLER_STATE_DIR="$WORK/installer-state"
  fi
  go run -ldflags "-X main.buildVersion=${DASHBOARD_VERSION}" ./cmd/dashboard --config "$WORK/config.json"
) > "$WORK/dashboard.log" 2>&1 &
DASHBOARD_PID=$!

if [[ $HTTPS -eq 1 ]]; then
  BASE="https://localhost:$HTTP_PORT"
else
  BASE="http://localhost:$HTTP_PORT"
fi
# Jede API-Antwort zaehlt als "erreichbar" - auch 401, denn die Endpunkte
# verlangen eine Anmeldung, die es an dieser Stelle noch nicht gibt.
listening() { [[ "$(curl -s "${CURL_INSECURE[@]}" -m 2 -o /dev/null -w '%{http_code}' "$BASE/api/v1/health")" != "000" ]]; }
for _ in $(seq 1 60); do
  listening && break
  sleep 1
done
if ! listening; then
  echo "Dashboard nicht erreichbar:" >&2
  cat "$WORK/dashboard.log" >&2
  exit 1
fi

# --fake-companion: Gast-Peer wie die Home-Assistant-Integration. Das
# Dashboard holt die Stundenwerte ab, sobald der Peer sein Angebot macht.
if [[ $FAKE_COMPANION -eq 1 ]]; then
  python3 "$HERE/fake_companion.py" "$BASE" > "$WORK/fake-companion.log" 2>&1 &
  FAKE_COMPANION_PID=$!
  echo "Fake-Companion laeuft (PID $FAKE_COMPANION_PID), Log: $WORK/fake-companion.log"
fi

COOKIES="$WORK/cookies.txt"
curl -sf "${CURL_INSECURE[@]}" -m 5 -c "$COOKIES" -H "$SECURE_HEADER" -H 'Content-Type: application/json' \
  -X POST "$BASE/api/v1/auth/login" \
  -d "{\"username\":\"admin\",\"password\":\"$PASSWORD\"}" > /dev/null
api() { curl -sf "${CURL_INSECURE[@]}" -m 5 -b "$COOKIES" -H "$SECURE_HEADER" "$@"; }

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

# Dienststatus: minibroker.py --service-status meldet je Datei die SHA-256,
# die auch das Dashboard berechnet. Erwartet wird der Status genau dieser
# Datei - so prueft die Konfigurationsseite ihn nach dem Oeffnen und Speichern.
CONFIG_URL="$BASE/api/v1/configurations/battery_soc_devices"
BATTERY_STATE="ok"
for name in "${REJECT_CONFIGS[@]}"; do
  [[ "$name" == "battery_soc_devices" ]] && BATTERY_STATE="rejected"
done
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

# --preset shelly-ht: die Unterstuetzung schlafender H&T-Geraete end-to-end.
# Die Presets kommen unveraendert aus services/shelly - faellt dort das
# sleepy-Flag weg, legt das Dashboard neue H&T wieder ohne Gnadenfrist an.
if [[ "$(basename "$FIXTURE")" == "shelly-ht.json" ]]; then
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
if [[ -f "$WORK/data/energy.json" ]] && grep -q '"groups"' "$WORK/data/energy.json"; then
  check "Energiegruppen und Kategorien sind im Snapshot" \
    "len(data.get('groups', [])) > 0 and len(data.get('categories', {})) > 0" \
    "$BASE/api/v1/energy"
fi
check "Eigenes Geraet energy_node ist ueber den Broker zurueckgekommen" \
  "any(d.get('id') == 'energy_node' for d in (data if isinstance(data, list) else data.get('devices', [])))" \
  "$BASE/api/v1/devices"
if [[ "$(basename "$FIXTURE")" == "alle-funktionen.json" ]]; then
  check "Datenfluss liefert Regeln" \
    "any(n['virtual_id'].startswith('rule:') for n in data['nodes'])" \
    "$BASE/api/v1/device/map/flows"
  check "Automations-Geraet fuehrt die Regeln aus" \
    "any(e['details'].get('kind') == 'executes' and e['from'].get('device_id') == 'automation' for e in data['edges'])" \
    "$BASE/api/v1/device/map/flows"
fi
# Die Bank-A-Kante kommt aus battery_soc_devices.json mit bank_a_voltage_topic.
# Die bringt nur fixtures/devices/alle-funktionen mit, docs-screenshots teilt
# die Fixture, nicht die Geraete.
if [[ " ${EXTRA_DEVICES[*]-} " == *" $HERE/fixtures/devices/alle-funktionen "* ]]; then
  check "Datenfluss liefert eine Bank-A-Kante" \
    "any(e['details'].get('field') == 'bank_a_voltage_topic' for e in data['edges'])" \
    "$BASE/api/v1/device/map/flows"
fi
if [[ -n "$THEME" ]]; then
  check "Farbschema aus --theme ist aktiv" \
    "data['theme'] == '$THEME'" \
    "$BASE/api/v1/settings"
fi

# --installed-services-off: die Praesenz-Proben lesen echtes HTML statt
# JSON, deshalb kein check() (das erwartet einen JSON-Body). Geprueft wird
# derselbe End-zu-Ende-Weg wie auf dem Node: config.json -> Go-Template ->
# gerenderte Seite - nicht nur die Unit-Tests der einzelnen Handler.
if [[ $INSTALLED_SERVICES_OFF -eq 1 ]]; then
  html_lacks() { # name, verbotener-string, url
    local name="$1" verboten="$2" url="$3"
    if api "$url" | grep -qF -- "$verboten"; then
      echo "  FEHL $name (\"$verboten\" steht trotzdem im HTML)"; FAILED=1
    else
      echo "  OK   $name"
    fi
  }
  html_lacks "Automationen-Tab fehlt bei installed_services=aus" \
    'id="tab-automations"' "$BASE/"
  html_lacks "Automationen-Fragment liefert bei installed_services=aus keinen Inhalt" \
    'x-data="automationsPanel()"' "$BASE/?fragment=panel&panel=automations"
  html_lacks "Tailscale-Unterseite fehlt bei installed_services=aus" \
    'aria-controls="settings-tailscale"' "$BASE/?fragment=panel&panel=settings"
  html_lacks "TinyTuya-Unterseite fehlt bei installed_services=aus" \
    'aria-controls="settings-tiny-tuya"' "$BASE/?fragment=panel&panel=settings"
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

# --simulate-update zeigt den Checker per ENERGY_NODE_UPDATES_API_BASE auf
# fake_github_releases.py statt der echten API; main.go prueft einmal sofort
# beim Start, wenn der Cache noch leer ist, also reicht kurzes Polling.
if [[ $SIMULATE_UPDATE -eq 1 ]]; then
  echo "==> Update-Simulation: /api/v1/updates/status"
  update_available=0
  for _ in $(seq 1 10); do
    status="$(api "$BASE/api/v1/updates/status" || true)"
    if echo "$status" | grep -q '"available":true' && echo "$status" | grep -q '"latest":"9.9.9"'; then
      update_available=1
      break
    fi
    sleep 1
  done
  if [[ $update_available -eq 1 ]]; then
    echo "  OK   Update v9.9.9 wird als verfuegbar gemeldet"
  else
    echo "  FEHL Update-Status meldet kein verfuegbares v9.9.9: ${status:-<keine Antwort>}" >&2
    FAILED=1
  fi
fi

# --simulate-package: derselbe Weg wie im Browser. Die Oberflaeche ruft
# POST /redeploy/api/run mit mode "prepare", das Dashboard laedt das Bundle vom
# Fake-GitHub in redeploy-candidate/, danach liest die Vorschau dessen Schritte
# (GET /redeploy/api/plan). Der zweite Lauf muss den Download ueberspringen.
# Am Ende wird das Verzeichnis wieder geleert: bei --keep soll der erste Klick
# auf "Aktualisieren" im Browser das Herunterladen zeigen, nicht ein fertiges Paket.
if [[ $SIMULATE_PACKAGE -eq 1 ]]; then
  CANDIDATE="$WORK/data/redeploy-candidate"
  # Wie der Browser: den Token aus der ausgelieferten Seite nehmen (data-token)
  # und als X-Installer-Token schicken. Ein Token aus /api/v1/auth/session zu
  # holen haette verdeckt, dass die Seite ihn nicht bekam (403 csrf_failed).
  page_token="$(api "$BASE/redeploy/" | sed -n 's/.*data-token="\([^"]*\)".*/\1/p' | head -n 1)"
  if [[ -n "$page_token" ]]; then
    echo "  OK   die Redeploy-Seite traegt den Sitzungs-Token fuer ihre Aufrufe"
  else
    echo "  FEHL die Redeploy-Seite hat keinen Token (data-token ist leer)"; FAILED=1
  fi
  echo "==> Redeploy folgt der Dashboard-Sprache"
  redeploy_lang() { api "$@" "$BASE/redeploy/api/bootstrap" | python3 -c 'import json,sys; print(json.load(sys.stdin)["language"])'; }
  if [[ "$(redeploy_lang)" == "de" && "$(redeploy_lang -H 'Accept-Language: en-GB,en;q=0.9')" == "en" ]]; then
    echo "  OK   Redeploy-Bildschirm ohne Wahl deutsch, mit englischem Browser englisch"
  else
    echo "  FEHL Redeploy-Bildschirm folgt der Sprache nicht"; FAILED=1
  fi
  start_prepare() {
    api -H "X-Installer-Token: $page_token" -H 'Content-Type: application/json' -X POST \
      "$BASE/redeploy/api/run" -d '{"mode":"prepare"}' > /dev/null
  }
  echo "==> Paketbezug: Vorbereiten laedt das Bundle"
  if start_prepare; then
    for _ in $(seq 1 30); do
      [[ -f "$CANDIDATE/manifest.json" ]] && break
      sleep 1
    done
  fi
  if [[ -f "$CANDIDATE/manifest.json" && -f "$CANDIDATE/manifest.json.sig" ]]; then
    echo "  OK   Bundle liegt in redeploy-candidate/"
  else
    echo "  FEHL Bundle wurde nicht heruntergeladen (siehe $WORK/dashboard.log)"; FAILED=1
  fi
  # Der Ereignisstrom bleibt offen, curl bricht nach zwei Sekunden ab.
  events="$(api -m 2 "$BASE/redeploy/api/events?since=0" || true)"
  if grep -q 'package.log.ready' <<<"$events"; then
    echo "  OK   Download-Protokoll kommt als Katalogschlüssel"
  else
    echo "  FEHL Download-Protokoll ohne package.log-Schlüssel"; FAILED=1
  fi
  check "Vorschau nennt Version und alle Schritte des heruntergeladenen Bundles" \
    "data['bundle_version'] == 'v9.9.9' and [s['id'] for s in data['steps']] == ['10','20','30','40','50','60','65','70','81','82']" \
    "$BASE/redeploy/api/plan"
  check "Auswahl des Nodes gilt (optionaler Schritt 70 abgewaehlt)" \
    "any(s['id'] == '70' and s['state'] == 'deselected' for s in data['steps'])" \
    "$BASE/redeploy/api/plan"
  check "geaenderter Dienst (battery_soc) steht mit restart=version in der Vorschau" \
    "next(s for s in data['steps'] if s['id'] == '82')['restart'] == 'version'" \
    "$BASE/redeploy/api/plan"
  check "unveraenderter Dienst (apsystems) hat keinen Neustartgrund" \
    "not next(s for s in data['steps'] if s['id'] == '81').get('restart')" \
    "$BASE/redeploy/api/plan"
  check "Was ist neu: Changelog des Pakets neben den installierten Versionen" \
    "data['bundle_version'] == 'v9.9.9' and data['installed']['dashboard'] == '9.9.7' and data['installed']['service:battery_soc'] == 'v0.4.0' and len(data['document']['components']) == 3" \
    "$BASE/redeploy/api/changelog"
  check "Was ist neu: Highlight-Markierung kommt unveraendert an" \
    "sorted({e['highlight'] for c in data['document']['components'] for r in c['releases'] for g in r['groups'] for e in g['entries']}) == [False, True]" \
    "$BASE/redeploy/api/changelog"
  check "Die Oberflaeche kennt jetzt das bereitliegende Paket" \
    "data['auto_prepare'] is True and data['bundle_version'] == 'v9.9.9'" \
    "$BASE/redeploy/api/bootstrap"
  # Zweiter Lauf: gleiche Version, also kein zweiter Download.
  before="$(stat -c %Y "$CANDIDATE/manifest.json")"
  sleep 1
  start_prepare || true
  sleep 2
  if [[ "$(stat -c %Y "$CANDIDATE/manifest.json")" == "$before" ]]; then
    echo "  OK   gleiche Version wird nicht erneut heruntergeladen"
  else
    echo "  FEHL das Bundle wurde trotz gleicher Version neu geschrieben"; FAILED=1
  fi
  rm -rf "$CANDIDATE"
fi

# --simulate-installed: die Versionsseite liest genau die Dateien, die Installer
# und Updater ablegen. Shelly ist in selection.json abgewaehlt (Schritt 83).
if [[ $SIMULATE_INSTALLED -eq 1 ]]; then
  check "Versionen: Paket und laufendes Dashboard" \
    "data['bundle']['version'] == 'v0.7.5' and data['bundle']['arch'] == 'armv6' and data['running']['dashboard'].startswith('v')" \
    "$BASE/api/v1/versions"
  check "Versionen: Komponenten mit Art, Reihenfolge wie im Changelog" \
    "[c['id'] for c in data['components']] == ['dashboard', 'service:battery_soc', 'service:shelly', 'energy_node_common'] and data['has_changelog'] is True" \
    "$BASE/api/v1/versions"
  check "Versionen: abgewaehlter Dienst gilt als nicht installiert" \
    "{c['id']: c['installed'] for c in data['components']} == {'dashboard': True, 'service:battery_soc': True, 'service:shelly': False, 'energy_node_common': True}" \
    "$BASE/api/v1/versions"
  check "Changelog: nach Komponente gefiltert" \
    "[c['id'] for c in data['components']] == ['service:battery_soc']" \
    "$BASE/api/v1/changelog?component=service:battery_soc"
  check "Changelog: Breaking-Eintrag bleibt als solcher erhalten" \
    "any(e['breaking'] for r in data['components'][0]['releases'] for g in r['groups'] for e in g['entries'])" \
    "$BASE/api/v1/changelog?component=dashboard"
  check "Changelog: Highlights und Details markiert" \
    "[e['highlight'] for g in data['components'][0]['releases'][0]['groups'] for e in g['entries']] == [True, False, True, False]" \
    "$BASE/api/v1/changelog?component=dashboard"
  check "Versionen: Vorgaenger gemeldet" \
    "data['previous'] is not None" \
    "$BASE/api/v1/versions"
  check "Neuerungen: Bereich seit dem Vorgaenger" \
    "data['since_previous'] is True and data['from'] != data['to'] and len(data['components']) > 0" \
    "$BASE/api/v1/changelog/highlights"

  # Oberflaeche: die Skripte der Neuerungen und der Update-Leiste werden
  # ausgeliefert, und die Basisseite bindet sie ein (der Neuerungen-Dialog
  # liegt in der Basisseite).
  for asset in js/update-bar.js js/whatsnew.page.js; do
    if api "$BASE/static/$asset" > /dev/null; then
      echo "  OK   Asset $asset wird ausgeliefert"
    else
      echo "  FEHL Asset $asset wird nicht ausgeliefert"; FAILED=1
    fi
  done
  # In eine Variable lesen: grep -q beendet sich beim ersten Treffer und kann
  # curl per SIGPIPE abbrechen (Exit 23), was unter pipefail als Fehler zaehlt.
  base_page="$(api "$BASE/" || true)"
  if grep -q 'whatsnew.page.js' <<<"$base_page" && grep -q 'update-whatsnew-open' <<<"$base_page"; then
    echo "  OK   Basisseite bindet das Neuerungen-Skript und den Dialog ein"
  else
    echo "  FEHL Basisseite bindet whatsnew.page.js oder den Neuerungen-Dialog nicht ein"; FAILED=1
  fi
  settings_js="$(api "$BASE/static/js/settings.page.js" || true)"
  if grep -q "get('page')" <<<"$settings_js"; then
    echo "  OK   settings.page.js wertet den Tiefenlink ?page= aus"
  else
    echo "  FEHL settings.page.js enthaelt die Auswertung von ?page= nicht"; FAILED=1
  fi

  # Fortschritt: ein laufender Auftrag, dann sein Ende. Das Job-Verzeichnis
  # gehoert im Smoke-Test dem Testbenutzer, wie auf dem Pi dem Dashboard-Konto.
  STATE="$WORK/installer-state"
  job_dir="$STATE/job"
  mkdir -p "$job_dir"
  check "Fortschritt: ohne Auftrag idle" "data['state'] == 'idle'" "$BASE/api/v1/updates/progress"
  printf '{"bundle_version":"v9.9.9","mode":"redeploy","steps":["10","60"],"run_id":"smoke-1"}\n' > "$job_dir/current.json"
  printf '%s ##STEP 10 ok\n%s ##STEP 60 begin\n' "$(date +%s%3N)" "$(date +%s%3N)" > "$job_dir/log"
  check "Fortschritt: laeuft mit Schrittnamen" \
    "data['state'] == 'running' and data['done'] == 1 and data['step']['id'] == '60' and data['step']['title'] != ''" \
    "$BASE/api/v1/updates/progress"
  printf '{"result":"ok"}\n' > "$job_dir/status.json"
  check "Fortschritt: abgeschlossen" \
    "data['state'] == 'ok' and data['done'] == 2 and data['finished_at']" \
    "$BASE/api/v1/updates/progress"
  rm -rf "$job_dir"
fi

# --simulate-journal: Service-Logs end-to-end, vom Endpunkt ueber sudo-Shim
# und Fake-Helper bis zu den geparsten Zeilen.
if [[ $SIMULATE_JOURNAL -eq 1 ]]; then
  check "Service-Logs: Einheiten-Auswahl nennt elf Dienste" \
    "len(data['units']) == 11 and data['default_unit'] == 'dashboard'" \
    "$BASE/api/v1/system/logs/units"
  check "Service-Logs: Dashboard liefert Zeilen mit Prioritaet und Einheit" \
    "len(data['entries']) > 100 and data['cursor'].startswith('s=') and all('priority' in e and e['unit'] for e in data['entries'])" \
    "$BASE/api/v1/system/logs?unit=dashboard&since=60&lines=200"
  check "Service-Logs: Suchbegriff filtert, der Cursor rueckt trotzdem vor" \
    "all('health' in e['message'].lower() for e in data['entries']) and data['cursor'].startswith('s=')" \
    "$BASE/api/v1/system/logs?unit=dashboard&since=60&lines=200&q=HEALTH"
  check "Service-Logs: nicht installierter Dienst liefert eine leere Liste" \
    "data['entries'] == [] and data['cursor'] == ''" \
    "$BASE/api/v1/system/logs?unit=caddy&since=60&lines=200"
  code=$(curl -s "${CURL_INSECURE[@]}" -m 5 -o /dev/null -w '%{http_code}' -b "$COOKIES" -H "$SECURE_HEADER" \
    "$BASE/api/v1/system/logs?unit=sshd&since=60&lines=200")
  if [[ "$code" == "400" ]]; then
    echo "  OK   Service-Logs: Dienst ausserhalb der Allowlist abgelehnt (HTTP 400)"
  else
    echo "  FEHL Service-Logs: unbekannter Dienst kam mit HTTP $code durch"; FAILED=1
  fi
fi

# Benutzerverwaltung: ein Konto mit nur view_overview meldet sich ueber HTTPS an.
# Der Admin entzieht die Berechtigung, dieselbe Sitzung sieht sie ohne
# Neustart nicht mehr.
echo "==> Benutzerverwaltung: eingeschraenktes Konto"
ADMIN_CSRF="$(api "$BASE/api/v1/auth/session" | python3 -c 'import json,sys; print(json.load(sys.stdin)["csrf_token"])')"
SMOKE_USER="smoke-user"
SMOKE_PASSWORD="smoke-user-pass-1"
USER_COOKIES="$WORK/smoke-user-cookies.txt"
curl -sf "${CURL_INSECURE[@]}" -m 5 -o /dev/null -H "$SECURE_HEADER" -H "X-CSRF-Token: $ADMIN_CSRF" \
  -H 'Content-Type: application/json' -X POST "$BASE/api/v1/users" \
  -b "$COOKIES" -d "{\"username\":\"$SMOKE_USER\",\"password\":\"$SMOKE_PASSWORD\",\"roles\":[\"view_overview\"]}" || FAILED=1
curl -sf "${CURL_INSECURE[@]}" -m 5 -c "$USER_COOKIES" -H "$SECURE_HEADER" -H 'Content-Type: application/json' \
  -X POST "$BASE/api/v1/auth/login" -d "{\"username\":\"$SMOKE_USER\",\"password\":\"$SMOKE_PASSWORD\"}" > /dev/null || FAILED=1
user_api() { curl -sf "${CURL_INSECURE[@]}" -m 5 -b "$USER_COOKIES" -H "$SECURE_HEADER" "$@"; }
if user_api "$BASE/api/v1/auth/session" | python3 -c '
import json, sys
sys.exit(0 if json.load(sys.stdin)["roles"] == ["view_overview"] else 1)
'; then
  echo "  OK   smoke-user meldet sich mit view_overview an"
else
  echo "  FEHL smoke-user hat nach der Anmeldung nicht genau view_overview"; FAILED=1
fi
code=$(curl -s "${CURL_INSECURE[@]}" -m 5 -o /dev/null -w '%{http_code}' -b "$USER_COOKIES" -H "$SECURE_HEADER" \
  "$BASE/api/v1/system/logs?unit=dashboard&since=60&lines=10")
if [[ "$code" == "403" ]]; then
  echo "  OK   Service-Logs fuer smoke-user abgelehnt (HTTP 403)"
else
  echo "  FEHL Service-Logs fuer smoke-user kam mit HTTP $code durch"; FAILED=1
fi
code=$(curl -s "${CURL_INSECURE[@]}" -m 5 -o /dev/null -w '%{http_code}' -b "$COOKIES" -H "$SECURE_HEADER" \
  -H "X-CSRF-Token: $ADMIN_CSRF" -H 'Content-Type: application/json' -X PUT "$BASE/api/v1/users/$SMOKE_USER" \
  -d '{"roles":[],"disabled":false}')
if [[ "$code" == "200" ]]; then
  echo "  OK   view_overview fuer smoke-user entzogen"
else
  echo "  FEHL Entziehen der Berechtigung kam mit HTTP $code"; FAILED=1
fi
if user_api "$BASE/api/v1/auth/session" | python3 -c '
import json, sys
sys.exit(0 if json.load(sys.stdin)["roles"] == [] else 1)
'; then
  echo "  OK   dieselbe Sitzung hat ohne Neustart keine Berechtigung mehr"
else
  echo "  FEHL dieselbe Sitzung behaelt die entzogene Berechtigung"; FAILED=1
fi

# Ansehen auf Seitenebene: ein Konto mit nur view_stats sieht nur seine Seite.
# Der Admin ergaenzt view_automations, dieselbe Sitzung darf sofort mehr.
echo "==> Ansehen: eingeschraenktes Konto mit view_stats"
create_smoke_user() { # Name, Rollen (JSON), Cookie-Datei
  curl -sf "${CURL_INSECURE[@]}" -m 5 -o /dev/null -H "$SECURE_HEADER" -H "X-CSRF-Token: $ADMIN_CSRF" \
    -H 'Content-Type: application/json' -X POST "$BASE/api/v1/users" \
    -b "$COOKIES" -d "{\"username\":\"$1\",\"password\":\"$SMOKE_PASSWORD\",\"roles\":$2}" || FAILED=1
  curl -sf "${CURL_INSECURE[@]}" -m 5 -c "$3" -H "$SECURE_HEADER" -H 'Content-Type: application/json' \
    -X POST "$BASE/api/v1/auth/login" -d "{\"username\":\"$1\",\"password\":\"$SMOKE_PASSWORD\"}" > /dev/null || FAILED=1
}
# Erwarteter HTTP-Code: eine Zahl oder "!403" fuer "nicht verboten".
check_status() { # Name, erwarteter Code, Cookie-Datei, URL
  local code
  code=$(curl -s "${CURL_INSECURE[@]}" -m 5 -o /dev/null -w '%{http_code}' -b "$3" -H "$SECURE_HEADER" "$4")
  if [[ "$2" == "!403" && "$code" != "403" ]] || [[ "$2" != "!403" && "$code" == "$2" ]]; then
    echo "  OK   $1 (HTTP $code)"
  else
    echo "  FEHL $1 (HTTP $code, erwartet $2)"; FAILED=1
  fi
}
VIEWER_COOKIES="$WORK/smoke-viewer-cookies.txt"
create_smoke_user "smoke-viewer" '["view_stats"]' "$VIEWER_COOKIES"
check_status "Verlaufs-Entitaeten (anyView) fuer smoke-viewer erlaubt" "!403" "$VIEWER_COOKIES" "$BASE/api/v1/history/entities"
check_status "Diagnose fuer smoke-viewer gesperrt" 403 "$VIEWER_COOKIES" "$BASE/api/v1/diagnostics"
if curl -s "${CURL_INSECURE[@]}" -m 5 -b "$VIEWER_COOKIES" -H "$SECURE_HEADER" "$BASE/api/v1/diagnostics" | grep -q '"view_forbidden"'; then
  echo "  OK   Diagnose-Sperre nennt view_forbidden"
else
  echo "  FEHL Diagnose-Sperre nennt nicht view_forbidden"; FAILED=1
fi
check_status "Verlaufs-Panel fuer smoke-viewer gesperrt" 403 "$VIEWER_COOKIES" "$BASE/?fragment=panel&panel=history"
viewer_page="$(curl -sf "${CURL_INSECURE[@]}" -m 5 -b "$VIEWER_COOKIES" -H "$SECURE_HEADER" "$BASE/")"
if grep -q 'id="tab-stats"' <<<"$viewer_page" && ! grep -q 'id="tab-history"' <<<"$viewer_page"; then
  echo "  OK   Startseite zeigt Statistik-Tab, aber keinen Verlaufs-Tab"
else
  echo "  FEHL Startseite von smoke-viewer hat falsche Tabs"; FAILED=1
fi
code=$(curl -s "${CURL_INSECURE[@]}" -m 5 -o /dev/null -w '%{http_code}' -b "$COOKIES" -H "$SECURE_HEADER" \
  -H "X-CSRF-Token: $ADMIN_CSRF" -H 'Content-Type: application/json' -X PUT "$BASE/api/v1/users/smoke-viewer" \
  -d '{"roles":["view_stats","view_automations"],"disabled":false}')
if [[ "$code" == "200" ]]; then
  echo "  OK   view_automations fuer smoke-viewer vergeben"
else
  echo "  FEHL Vergeben von view_automations kam mit HTTP $code"; FAILED=1
fi
check_status "Automations-Zustand ohne Neustart fuer dieselbe Sitzung erlaubt" "!403" "$VIEWER_COOKIES" "$BASE/api/v1/automation/state"

# Verlauf speichern: ein Konto mit nur view_history darf history_views ueber
# PUT /api/v1/settings schreiben, eine Theme-Aenderung bleibt verboten.
echo "==> Ansehen: Konto mit view_history speichert Verlaufs-Sichten"
HISTORY_COOKIES="$WORK/smoke-history-cookies.txt"
create_smoke_user "smoke-history" '["view_history"]' "$HISTORY_COOKIES"
HISTORY_CSRF="$(curl -sf "${CURL_INSECURE[@]}" -m 5 -b "$HISTORY_COOKIES" -H "$SECURE_HEADER" "$BASE/api/v1/auth/session" | python3 -c 'import json,sys; print(json.load(sys.stdin)["csrf_token"])')"
history_settings="$WORK/smoke-history-settings.json"
curl -sf "${CURL_INSECURE[@]}" -m 5 -b "$HISTORY_COOKIES" -H "$SECURE_HEADER" "$BASE/api/v1/settings" \
  | python3 -c '
import json, sys
value = json.load(sys.stdin)
value["history_views"] = value.get("history_views", []) + [{"id": "smoke-history-view", "name": "Smoke Verlauf", "series": [], "range_hours": 24, "aggregate": "avg"}]
json.dump(value, sys.stdout)
' > "$history_settings" || FAILED=1
code=$(curl -s "${CURL_INSECURE[@]}" -m 5 -o /dev/null -w '%{http_code}' -b "$HISTORY_COOKIES" -H "$SECURE_HEADER" \
  -H "X-CSRF-Token: $HISTORY_CSRF" -H 'Content-Type: application/json' -X PUT "$BASE/api/v1/settings" \
  --data-binary "@$history_settings")
if [[ "$code" == "200" ]]; then
  echo "  OK   smoke-history speichert history_views (HTTP 200)"
else
  echo "  FEHL smoke-history kann history_views nicht speichern (HTTP $code)"; FAILED=1
fi
python3 -c '
import json, sys
value = json.load(open(sys.argv[1]))
value["theme"] = "stromblau" if value["theme"] != "stromblau" else "mint"
json.dump(value, open(sys.argv[1], "w"))
' "$history_settings"
code=$(curl -s "${CURL_INSECURE[@]}" -m 5 -o /dev/null -w '%{http_code}' -b "$HISTORY_COOKIES" -H "$SECURE_HEADER" \
  -H "X-CSRF-Token: $HISTORY_CSRF" -H 'Content-Type: application/json' -X PUT "$BASE/api/v1/settings" \
  --data-binary "@$history_settings")
if [[ "$code" == "403" ]]; then
  echo "  OK   Theme-Aenderung fuer smoke-history abgelehnt (HTTP 403)"
else
  echo "  FEHL Theme-Aenderung fuer smoke-history kam mit HTTP $code durch"; FAILED=1
fi

# Update installieren: ein Konto mit nur install_updates startet das angebotene
# Update per POST /api/v1/updates/install. Das braucht ein angebotenes Update
# (fake_github_releases.py, also --simulate-package) und das Zustandsverzeichnis
# des Smoke-Tests. Der Updater laeuft hier nicht, der Auftrag bleibt pending.
if [[ $SIMULATE_PACKAGE -eq 1 ]]; then
  echo "==> Update installieren: Konto mit install_updates"
  UPDATER_COOKIES="$WORK/smoke-updater-cookies.txt"
  create_smoke_user "smoke-updater" '["install_updates"]' "$UPDATER_COOKIES"
  UPDATER_CSRF="$(curl -sf "${CURL_INSECURE[@]}" -m 5 -b "$UPDATER_COOKIES" -H "$SECURE_HEADER" "$BASE/api/v1/auth/session" | python3 -c 'import json,sys; print(json.load(sys.stdin)["csrf_token"])')"
  curl -sf "${CURL_INSECURE[@]}" -m 5 -o /dev/null -b "$UPDATER_COOKIES" -H "$SECURE_HEADER" "$BASE/api/v1/updates/check" || FAILED=1
  code=$(curl -s "${CURL_INSECURE[@]}" -m 5 -o /dev/null -w '%{http_code}' -b "$UPDATER_COOKIES" -H "$SECURE_HEADER" \
    -H "X-CSRF-Token: $UPDATER_CSRF" -H 'Content-Type: application/json' -X POST "$BASE/api/v1/updates/install")
  if [[ "$code" == "202" ]]; then
    echo "  OK   smoke-updater startet das angebotene Update (HTTP 202)"
  else
    echo "  FEHL Update-Installation fuer smoke-updater kam mit HTTP $code"; FAILED=1
  fi
  check_status "Installer-Bildschirm fuer smoke-updater gesperrt" 403 "$UPDATER_COOKIES" "$BASE/redeploy/"
  update_state() { api "$BASE/api/v1/updates/progress" | python3 -c 'import json,sys; print(json.load(sys.stdin).get("state", ""))' 2>/dev/null || true; }
  for _ in $(seq 1 30); do
    [[ "$(update_state)" == "pending" ]] && break
    sleep 1
  done
  if [[ "$(update_state)" == "pending" ]]; then
    echo "  OK   Update-Fortschritt meldet den wartenden Auftrag (pending)"
  else
    echo "  FEHL Update-Fortschritt meldet nicht pending: $(update_state)"; FAILED=1
  fi
  # Den Auftrag entfernen, damit spaetere Pruefungen nicht an ihm haengen.
  rm -rf "$WORK/installer-state/job"
fi

check_history_exchange || FAILED=1

echo
if [[ $KEEP -eq 1 ]]; then
  if [[ $REAL_CHANGELOG -eq 1 ]]; then
    # Erst nach den Fixture-Proben: die Versionsseite liest die Dateien bei
    # jeder Anfrage neu, ein Neustart ist nicht noetig.
    python3 "$HERE/real_changelog.py" "$REPO_DIR" "$WORK/installer-state"
  fi
  # Der Admin bekommt http_login und ein eigenes HTTP-Passwort, damit der
  # Browser sich auch ohne --https anmelden kann (Sitzung dann eingeschraenkt,
  # siehe Benutzerverwaltung). Das Hauptpasswort gilt weiter ueber HTTPS.
  HTTP_PASSWORD="smoketest-http-1234"
  admin_roles="$(api "$BASE/api/v1/users" | python3 -c '
import json, sys
users = json.load(sys.stdin)["users"]
roles = [u for u in users if u["username"] == "admin"][0]["roles"]
print(json.dumps(sorted(set(roles) | {"http_login"})))')"
  api -o /dev/null -H "X-CSRF-Token: $ADMIN_CSRF" -H 'Content-Type: application/json' -X PUT "$BASE/api/v1/users/admin" \
    -d "{\"roles\":$admin_roles,\"disabled\":false}" \
    && api -o /dev/null -H "X-CSRF-Token: $ADMIN_CSRF" -H 'Content-Type: application/json' -X PUT "$BASE/api/v1/users/admin/http-password" \
      -d "{\"password\":\"$HTTP_PASSWORD\"}" \
    || echo "Warnung: HTTP-Login fuer admin konnte nicht eingerichtet werden" >&2
  echo "Dashboard laeuft weiter: $BASE  (Login admin / $PASSWORD)"
  [[ $HTTPS -eq 1 ]] || echo "Ueber HTTP (eingeschraenkte Sitzung): admin / $HTTP_PASSWORD"
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
