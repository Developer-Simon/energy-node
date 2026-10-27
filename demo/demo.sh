#!/usr/bin/env bash
# Persistentes lokales Dashboard mit allen Fakes: Broker, Companion,
# Konsole und GitHub-Releases. Der Zustand (Benutzer, Layout, Verlauf)
# bleibt unter demo/.state erhalten, bis --reset ihn loescht.
#
#   demo/demo.sh                     # Demo starten, Zustand bleibt
#   demo/demo.sh --reset             # Zustand loeschen, dann neu einrichten
#   demo/demo.sh --docs              # Fixture fuer die Doku-Bilder, Zustand in demo/.run/docs
#   demo/demo.sh --no-simulate       # PV, Netz, Batterie und Last bleiben stehen
#   demo/demo.sh --devices DIR       # zusaetzliche Konfigurationen, spaetere Quelle gewinnt
#
# Abbruch mit Strg-C beendet Dashboard und alle Fakes.

set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=demo/lib/env.sh
source "$SCRIPT_DIR/lib/env.sh"

usage() {
  echo "Aufruf: demo/demo.sh [--reset] [--docs] [--no-simulate] [--devices DIR]..." >&2
}

MODE=demo
RESET=0
SIMULATE=1
EXTRA_DEVICES=()
while [[ $# -gt 0 ]]; do
  case "$1" in
    --reset) RESET=1; shift ;;
    --docs) MODE=docs; shift ;;
    --no-simulate) SIMULATE=0; shift ;;
    --devices)
      if [[ $# -lt 2 ]]; then
        echo "FEHLER: --devices braucht ein Verzeichnis" >&2
        exit 2
      fi
      if ! dir="$(cd "$2" 2>/dev/null && pwd)"; then
        echo "FEHLER: Verzeichnis $2 existiert nicht" >&2
        exit 2
      fi
      EXTRA_DEVICES+=("$dir")
      shift 2
      ;;
    -h|--help) usage; exit 0 ;;
    *) echo "FEHLER: unbekannte Option $1" >&2; usage; exit 2 ;;
  esac
done

# Die Variablen dieses Blocks werden von env.sh gelesen.
# shellcheck disable=SC2034
# Demo-Ports. Smoke-Test nutzt 181xx, die Demo 182xx.
HTTP_PORT="${DASHBOARD_DEMO_PORT:-18200}"
COMPANION_PORT="${DASHBOARD_DEMO_COMPANION_PORT:-18201}"
CONSOLE_PORT="${DASHBOARD_DEMO_CONSOLE_PORT:-18202}"
MQTT_PORT="${DASHBOARD_DEMO_MQTT_PORT:-18203}"
UPDATES_API_PORT="${DASHBOARD_DEMO_UPDATES_API_PORT:-18204}"

HTTPS=1
JOURNAL=1
INSTALLED=1
PACKAGE=1
UPDATES=1
SERVICES_OFF=0

if [[ "$MODE" == docs ]]; then
  WORK="$DEMO_DIR/.run/docs"
  FIXTURE="$FIXTURES/alle-funktionen.json"
  SEED_DATA="$FIXTURES/seed/docs-screenshots"
  DEVICE_SOURCES=(
    "$REPO_DIR/services/battery_soc"
    "$REPO_DIR/services/automation"
    "$FIXTURES/devices/docs-screenshots"
  )
else
  WORK="$DEMO_DIR/.state"
  FIXTURE="$FIXTURES/demo.json"
  SEED_DATA="$FIXTURES/seed/demo"
  DEVICE_SOURCES=(
    "$REPO_DIR/services/battery_soc"
    "$REPO_DIR/services/automation"
    "$REPO_DIR/services/shelly"
    "$FIXTURES/devices/demo"
  )
fi
if (( ${#EXTRA_DEVICES[@]} > 0 )); then
  DEVICE_SOURCES+=("${EXTRA_DEVICES[@]}")
fi

# Nichts starten und nichts loeschen, solange einer der Ports belegt ist.
for port in "$HTTP_PORT" "$COMPANION_PORT" "$CONSOLE_PORT" "$MQTT_PORT" "$UPDATES_API_PORT"; do
  env_require_port "$port" || exit 1
done

cleanup_all() {
  env_cleanup
  if [[ "$MODE" == docs ]]; then
    rm -rf "$WORK"
  fi
}
trap cleanup_all EXIT
trap 'exit 130' INT TERM

if [[ "$MODE" == docs ]]; then
  rm -rf "$WORK"
fi
if [[ $RESET -eq 1 ]]; then
  rm -rf "$WORK"
  echo "Demo-Zustand geloescht"
fi

mkdir -p "$WORK/bin" "$WORK/data" || exit 1
if [[ ! -f "$WORK/.initialized" ]]; then
  echo "Erster Start, Zustand wird eingerichtet"
  # Ein abgebrochener Erststart hinterlaesst halbe Daten. Die werden verworfen.
  rm -rf "$WORK/data" "$WORK/installer-state"
  mkdir -p "$WORK/data" || exit 1
fi

# Jeder Start: Seed, Installer-Zustand, Paket und Zertifikat sind idempotent,
# Konfigurationen werden neu geprueft (Schemas immer erneuert).
env_seed_data || exit 1
env_installed_state || exit 1
env_make_package || exit 1
env_write_cert || exit 1
env_prepare_devices || exit 1
env_write_config || exit 1

echo "Baue Dashboard"
env_build_dashboard "$WORK/bin/dashboard" || {
  echo "FEHLER: Dashboard konnte nicht gebaut werden" >&2
  exit 1
}

env_start_broker
env_start_fake_github
env_start_dashboard "$WORK/bin/dashboard"
env_wait_dashboard || exit 1
env_start_fake_companion
COMPANION_PID="${ENV_PIDS[-1]}"
env_start_console

echo "Startcheck:"
check_part() { # Name, Log, Befehl...
  local name="$1" log="$2"
  shift 2
  if "$@" > /dev/null 2>&1; then
    echo "  ok    $name"
  else
    echo "  FEHLT $name (Log: $log)"
  fi
}
check_dashboard() { # Health verlangt eine Anmeldung
  env_login && api "$BASE/api/v1/health" > /dev/null
}
check_broker() {
  [[ -n "$(ss -lntH "sport = :$MQTT_PORT" 2>/dev/null)" ]]
}
check_companion() {
  kill -0 "$COMPANION_PID"
}
check_console() {
  curl -sf -m 5 "http://127.0.0.1:$CONSOLE_PORT/" > /dev/null
}
check_updates() {
  curl -sf -m 5 "http://127.0.0.1:$UPDATES_API_PORT/" > /dev/null
}
check_part "Dashboard" "$WORK/dashboard.log" check_dashboard
check_part "MQTT-Broker" "$WORK/broker.log" check_broker
check_part "Fake-Companion" "$WORK/fake-companion.log" check_companion
check_part "Fake-Konsole" "$WORK/fake-console.log" check_console
check_part "Fake-GitHub" "$WORK/updates-api.log" check_updates

touch "$WORK/.initialized"

echo ""
echo "Dashboard:    $BASE"
echo "Login admin / $PASSWORD"
echo "Konsole:      http://127.0.0.1:$CONSOLE_PORT"
echo "Datenordner:  $WORK"
echo "Selbstsigniertes Zertifikat im Browser bestaetigen."
echo "Nach Codeaenderung neu starten."
echo "Abbruch mit Strg-C."

wait "$DASHBOARD_PID"
