#!/usr/bin/env bash
# Smoke-Test: startet das Dashboard in drei Konfigurationen und prueft je Lauf
# die gewaehlten Gruppen aus checks/. Gemeinsamer Unterbau (Start, Konfiguration,
# Daten, Abbau) liegt in demo/lib/env.sh.
#
#   dashboard/test/smoke/smoke.sh                  # alle drei Laeufe
#   dashboard/test/smoke/smoke.sh --run haupt      # nur einen Lauf, mehrfach angebbar
#
# Laeufe in fester Reihenfolge, jeder frisch in .run/<name>-XXXXXX. Bestanden:
# der Ordner wird geloescht. Fehlgeschlagen: der Ordner bleibt fuer die Analyse.
# Exit 0 nur, wenn alle gewaehlten Laeufe gruen sind.
#
# Lauf haupt: prueft genau die Demo (demo/demo.sh, Standard). Alle Gruppen, weil
#   die Demo dieselbe Fixture, dieselben Seeds und dieselben Dienste zeigt.
# Lauf ohne-dienste: widerspricht der Demo. Alle sieben Dienste sind aus, deshalb
#   nur die Gruppe services_off.
# Lauf bilanz: die Bilanz braucht exakte Werte ohne Simulation. Ueber HTTP laeuft
#   zusaetzlich die Probe "ohne HTTPS-Kennzeichen abgewiesen", die unter TLS
#   uebersprungen wird.

set -uo pipefail

SMOKE_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=demo/lib/env.sh
source "$SMOKE_DIR/../../../demo/lib/env.sh"
# shellcheck source=dashboard/test/smoke/checks/core.sh
source "$SMOKE_DIR/checks/core.sh"
# shellcheck source=dashboard/test/smoke/checks/access.sh
source "$SMOKE_DIR/checks/access.sh"
# shellcheck source=dashboard/test/smoke/checks/config.sh
source "$SMOKE_DIR/checks/config.sh"
# shellcheck source=dashboard/test/smoke/checks/companion.sh
source "$SMOKE_DIR/checks/companion.sh"
# shellcheck source=dashboard/test/smoke/checks/energy.sh
source "$SMOKE_DIR/checks/energy.sh"
# shellcheck source=dashboard/test/smoke/checks/updates.sh
source "$SMOKE_DIR/checks/updates.sh"
# shellcheck source=dashboard/test/smoke/checks/onboarding.sh
source "$SMOKE_DIR/checks/onboarding.sh"
# shellcheck source=dashboard/test/smoke/checks/mqtt.sh
source "$SMOKE_DIR/checks/mqtt.sh"

# Smoke-Ports. Die Demo nutzt 182xx, der Smoke-Test 181xx.
HTTP_PORT="${DASHBOARD_SMOKE_PORT:-18100}"
MQTT_PORT="${DASHBOARD_SMOKE_MQTT_PORT:-18883}"
UPDATES_API_PORT="${DASHBOARD_SMOKE_UPDATES_API_PORT:-18884}"
COMPANION_PORT="${DASHBOARD_SMOKE_COMPANION_PORT:-18101}"
CONSOLE_PORT="${DASHBOARD_SMOKE_CONSOLE_PORT:-18102}"

RUN_DIR="$SMOKE_DIR/.run"
BIN="$RUN_DIR/bin/dashboard"
FAILED=0

trap env_cleanup EXIT
trap 'exit 130' INT TERM

usage() {
  echo "Aufruf: dashboard/test/smoke/smoke.sh [--run NAME]..." >&2
  echo "Laeufe: haupt, ohne-dienste, bilanz, seed, falsches-passwort" >&2
}

# Setzt alle Schalter auf den Stand "aus". Jeder Lauf schaltet nur sein Noetiges an.
setze_grundstand() {
  FIXTURE=""
  SEED_DATA=""
  DEVICE_SOURCES=()
  SIMULATE=0
  HTTPS=0
  JOURNAL=0
  INSTALLED=0
  PACKAGE=0
  UPDATES=0
  SERVICES_OFF=0
  PACKAGE_ARCHIVE=""
  SEED_JSON=""
  MQTT_USERNAME=""
  MQTT_PASSWORD=""
  MQTT_BROKER_PASSWORD=""
}

# Lauf seed: Vorgaben des Installers als onboarding-seed.json neben auth.pw.
konfiguriere_seed() {
  setze_demo_quellen
  SEED_JSON='{"version":1,"theme":"stromblau","users":[{"name":"smoke-seed","template":"view","password":"smoke-seed-pass-1"}],"later":["guest"]}'
}

# Quellen und Fixture wie die Demo (demo/demo.sh, Standard).
setze_demo_quellen() {
  FIXTURE="$FIXTURES/demo.json"
  SEED_DATA="$FIXTURES/seed/demo"
  DEVICE_SOURCES=(
    "$REPO_DIR/services/battery_soc"
    "$REPO_DIR/services/automation"
    "$REPO_DIR/services/shelly"
    "$FIXTURES/devices/demo"
  )
}

# Lauf falsches-passwort: der Broker verlangt ein Passwort, das Dashboard schickt ein anderes.
konfiguriere_falsches_passwort() {
  setze_demo_quellen
  MQTT_USERNAME="smoke-mqtt"
  MQTT_PASSWORD="smoke-falsch-1"
  MQTT_BROKER_PASSWORD="smoke-richtig-1"
}

konfiguriere_haupt() {
  setze_demo_quellen
  HTTPS=1
  SIMULATE=1
  JOURNAL=1
  INSTALLED=1
  PACKAGE=1
  UPDATES=1
  SERVICES_OFF=0
}

konfiguriere_ohne_dienste() {
  setze_demo_quellen
  SERVICES_OFF=1
}

konfiguriere_bilanz() {
  FIXTURE="$FIXTURES/energie-ueberschuss.json"
  SEED_DATA="$FIXTURES/seed/energie-alle-karten"
  DEVICE_SOURCES=("$REPO_DIR/services/battery_soc")
}

# Eine Gruppe aus checks/ ausfuehren. Gruppen setzen FAILED=1 bei Fehlern.
gruppe() {
  case "$1" in
    topics) checks_topics ;;
    battery_config) checks_battery_config ;;
    shelly) checks_shelly ;;
    seed) checks_seed ;;
    dataflow) checks_dataflow ;;
    live_push) checks_live_push ;;
    update_offer) checks_update_offer ;;
    package) checks_package ;;
    installed) checks_installed ;;
    journal) checks_journal ;;
    users) checks_users ;;
    onboarding) checks_onboarding ;;
    onboarding_seed) checks_onboarding_seed ;;
    mqtt_falsch) checks_mqtt_falsch ;;
    update_install) checks_update_install ;;
    companion) checks_companion ;;
    history_exchange) checks_history_exchange ;;
    services_off) checks_services_off ;;
    energy_balance) checks_energy_balance ;;
    *)
      echo "FEHLER: unbekannte Gruppe $1" >&2
      return 1
      ;;
  esac
}

# Start der Dienste und Ausfuehrung der Gruppen. Gibt 1 zurueck, wenn der Start
# scheitert oder eine Gruppe fehlschlaegt. WORK bleibt dann erhalten.
starte_und_pruefe() {
  local group
  env_seed_data || return 1
  env_installed_state || return 1
  env_make_package || return 1
  env_write_cert || return 1
  env_prepare_devices || return 1
  env_write_config || return 1
  if [[ -n "$SEED_JSON" ]]; then
    printf '%s' "$SEED_JSON" > "$WORK/onboarding-seed.json" || return 1
  fi
  env_start_broker
  env_start_fake_github
  env_start_dashboard "$BIN"
  env_wait_dashboard || return 1
  env_login || {
    echo "FEHLER: Admin-Anmeldung fehlgeschlagen" >&2
    return 1
  }
  env_start_fake_companion
  env_start_console

  FAILED=0
  for group in "$@"; do
    gruppe "$group" || FAILED=1
  done
  [[ $FAILED -eq 0 ]]
}

# Ein Lauf: Ports pruefen, frisch starten, Gruppen, Abbau, Aufraeumen.
# Aufruf: fuehre_lauf NAME GRUPPE...
fuehre_lauf() {
  local name="$1" ok=0 port
  shift
  echo "=== Lauf $name ==="
  for port in "$HTTP_PORT" "$MQTT_PORT" "$UPDATES_API_PORT" "$COMPANION_PORT" "$CONSOLE_PORT"; do
    if ! env_require_port "$port"; then
      echo "Lauf $name: FEHLGESCHLAGEN (Port $port belegt)"
      return 1
    fi
  done

  WORK="$(mktemp -d "$RUN_DIR/$name-XXXXXX")" || return 1
  ENV_PIDS=()
  starte_und_pruefe "$@" || ok=1
  env_cleanup

  if [[ $ok -eq 0 ]]; then
    rm -rf "$WORK"
    echo "Lauf $name: bestanden"
    return 0
  fi
  echo "Lauf $name: FEHLGESCHLAGEN ($WORK)"
  return 1
}

# Legt den Binary-Ordner an und baut das Dashboard genau einmal je Aufruf.
vorbereiten() {
  mkdir -p "$RUN_DIR/bin" || return 1
  echo "Baue Dashboard nach $BIN"
  env_build_dashboard "$BIN" || {
    echo "FEHLER: Dashboard konnte nicht gebaut werden" >&2
    return 1
  }
}

WANTED=()
while [[ $# -gt 0 ]]; do
  case "$1" in
    --run)
      if [[ $# -lt 2 ]]; then
        echo "FEHLER: --run braucht einen Namen" >&2
        usage
        exit 2
      fi
      case "$2" in
        haupt|ohne-dienste|bilanz|seed|falsches-passwort) WANTED+=("$2") ;;
        *)
          echo "FEHLER: unbekannter Lauf $2" >&2
          usage
          exit 2
          ;;
      esac
      shift 2
      ;;
    -h|--help) usage; exit 0 ;;
    *) echo "FEHLER: unbekannte Option $1" >&2; usage; exit 2 ;;
  esac
done
if (( ${#WANTED[@]} == 0 )); then
  WANTED=(haupt ohne-dienste bilanz seed falsches-passwort)
fi

vorbereiten || exit 1

# Reihenfolge fest, egal in welcher Reihenfolge --run angegeben wurde.
# Gruppen: haupt prueft die Demo, ohne-dienste prueft abgeschaltete Dienste,
# bilanz prueft exakte Energiewerte ohne Simulation.
RESULT=0
for name in haupt ohne-dienste bilanz seed falsches-passwort; do
  wanted=0
  for w in "${WANTED[@]}"; do
    [[ "$w" == "$name" ]] && wanted=1
  done
  [[ $wanted -eq 1 ]] || continue

  setze_grundstand
  case "$name" in
    haupt)
      konfiguriere_haupt
      fuehre_lauf haupt topics battery_config shelly seed dataflow live_push \
        update_offer package installed journal users onboarding update_install companion \
        history_exchange || RESULT=1
      ;;
    ohne-dienste)
      konfiguriere_ohne_dienste
      fuehre_lauf ohne-dienste services_off || RESULT=1
      ;;
    bilanz)
      konfiguriere_bilanz
      fuehre_lauf bilanz seed energy_balance companion || RESULT=1
      ;;
    seed)
      konfiguriere_seed
      fuehre_lauf seed onboarding_seed || RESULT=1
      ;;
    falsches-passwort)
      konfiguriere_falsches_passwort
      fuehre_lauf falsches-passwort mqtt_falsch || RESULT=1
      ;;
  esac
done

exit "$RESULT"
