# shellcheck shell=bash
# Gemeinsamer Unterbau fuer Smoke-Test und Demo: Pfade, Port-Pruefung,
# Konfigurationen, Daten, Zertifikat, Paket, Start der Dienste und Prozessverwaltung.
# Wird gesourct, setzt selbst keine Shell-Optionen.
#
# Der Aufrufer setzt vor den env_*-Funktionen WORK, HTTP_PORT, MQTT_PORT,
# UPDATES_API_PORT, COMPANION_PORT, CONSOLE_PORT, FIXTURE, SEED_DATA,
# DEVICE_SOURCES (Array, spaetere Quelle gewinnt), SIMULATE, HTTPS,
# SERVICES_OFF, JOURNAL, INSTALLED, PACKAGE und UPDATES (0 oder 1).
# BROKER_CONTROL_PORT ist optional: mit ihm hat der Broker einen Steuerport
# und die Konsole den Reiter MQTT.
# MQTT_USERNAME, MQTT_PASSWORD (Passwort des Dashboards) und MQTT_BROKER_PASSWORD
# (Passwort des Brokers, dessen Konto MQTT_USERNAME ist) sind optional, Standard leer.

_ENV_LIB_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
DEMO_DIR="$(cd "$_ENV_LIB_DIR/.." && pwd)"
REPO_DIR="$(cd "$DEMO_DIR/.." && pwd)"
DASHBOARD_DIR="$REPO_DIR/dashboard"
# Die Variablen des Kopfs werden von den Aufrufern (Smoke, Demo) genutzt.
# shellcheck disable=SC2034
FAKES="$DEMO_DIR/fakes"
FIXTURES="$DEMO_DIR/fixtures"
# shellcheck disable=SC2034
PASSWORD="smoketest1234"
# isSecureRequest() akzeptiert TLS oder diesen Header - ohne ihn antwortet der
# Login mit "secure_login_required".
# shellcheck disable=SC2034
SECURE_HEADER="X-Forwarded-Proto: https"
ENV_PIDS=()

# Merkt sich einen gestarteten Prozess fuer env_cleanup.
env_track() {
  ENV_PIDS+=("$1")
}

# Beendet alle gemerkten Prozesse: erst kill, bis 5 s warten, dann kill -9.
env_cleanup() {
  local pid alive
  for pid in "${ENV_PIDS[@]+"${ENV_PIDS[@]}"}"; do
    kill "$pid" 2>/dev/null || true
  done
  for _ in $(seq 1 50); do
    alive=0
    for pid in "${ENV_PIDS[@]+"${ENV_PIDS[@]}"}"; do
      kill -0 "$pid" 2>/dev/null && alive=1
    done
    [[ $alive -eq 0 ]] && break
    sleep 0.1
  done
  for pid in "${ENV_PIDS[@]+"${ENV_PIDS[@]}"}"; do
    kill -9 "$pid" 2>/dev/null || true
  done
  ENV_PIDS=()
  return 0
}

# Prueft, ob ein TCP-Port frei ist. Belegt: Meldung mit PID, return 1.
env_require_port() {
  local port="$1" line pid
  line="$(ss -lntpH "sport = :$port" 2>/dev/null)"
  if [[ -z "$line" ]]; then
    return 0
  fi
  pid="$(printf '%s\n' "$line" | grep -o 'pid=[0-9]*' | head -n 1 | cut -d= -f2)"
  echo "Port $port ist belegt (PID ${pid:-?})" >&2
  return 1
}

# Kopiert die Konfigurationen aus DEVICE_SOURCES nach $WORK/devices.
# Schema-Dateien werden immer ueberschrieben. Andere *.json-Dateien, die schon
# vor diesem Aufruf im Ziel lagen, bleiben unberuehrt (gespeicherte Konfig).
# Innerhalb eines Aufrufs gewinnt die spaetere Quelle. Danach je Konfiguration
# ein Dienst-Manifest nach $WORK/manifests.
env_prepare_devices() {
  mkdir -p "$WORK/devices" || return 1
  local -A existed=()
  local f name src
  for f in "$WORK/devices"/*.json; do
    [[ -f "$f" ]] || continue
    existed["$(basename "$f")"]=1
  done
  for src in "${DEVICE_SOURCES[@]+"${DEVICE_SOURCES[@]}"}"; do
    for f in "$src"/*.json; do
      [[ -f "$f" ]] || continue
      name="$(basename "$f")"
      if [[ "$name" != *.schema.json && -n "${existed[$name]-}" ]]; then
        continue
      fi
      cp "$f" "$WORK/devices/$name" || return 1
    done
  done
  echo "Konfigurationen: $(find "$WORK/devices" -maxdepth 1 -type f -printf '%f\n' | sort | tr '\n' ' ')"

  # Je Konfiguration ein Dienst-Manifest neben config.json, wie es der Installer
  # unter manifests/ ablegt. Nur fuer die dort genannten Dienste beobachtet das
  # Dashboard outstation/<id>/settings/status.
  mkdir -p "$WORK/manifests" || return 1
  python3 - "$WORK/devices" "$WORK/manifests" <<'PY' || return 1
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
  echo "Dienste: $(find "$WORK/manifests" -maxdepth 1 -type f -printf '%f\n' | sed 's/\.json$//' | sort | tr '\n' ' ')"
}

# Kopiert $SEED_DATA/*.json nach $WORK/data. Vorhandene Dateien bleiben.
env_seed_data() {
  [[ -n "$SEED_DATA" ]] || return 0
  mkdir -p "$WORK/data" || return 1
  local f
  for f in "$SEED_DATA"/*.json; do
    [[ -f "$f" ]] || continue
    [[ -e "$WORK/data/$(basename "$f")" ]] || cp "$f" "$WORK/data/" || return 1
  done
  echo "Seed-Daten: $(find "$WORK/data" -maxdepth 1 -type f -printf '%f\n' | sort | tr '\n' ' ')"
}

# Installer-Zustand aus der Fixture, wenn INSTALLED=1 und noch keiner da ist.
env_installed_state() {
  [[ "${INSTALLED:-0}" == "1" ]] || return 0
  [[ -e "$WORK/installer-state" ]] && return 0
  cp -r "$FIXTURES/installed-state" "$WORK/installer-state" || return 1
}

# Baut das Fake-Update-Paket fuer die Architektur, auf der das Dashboard
# laeuft, und setzt PACKAGE_ARCHIVE. Nur bei PACKAGE=1 und fehlendem Archiv.
env_make_package() {
  [[ "${PACKAGE:-0}" == "1" ]] || return 0
  local arch
  case "$(cd "$DASHBOARD_DIR" && go env GOARCH)" in
    arm) arch="armv6" ;;
    *) arch="$(cd "$DASHBOARD_DIR" && go env GOARCH)" ;;
  esac
  # shellcheck disable=SC2034
  PACKAGE_ARCH="$arch"
  PACKAGE_ARCHIVE="$WORK/package/energy-node-v9.9.9-$arch.tar.gz"
  mkdir -p "$WORK/package" "$WORK/installer-state" || return 1
  [[ -f "$PACKAGE_ARCHIVE" ]] && return 0
  python3 "$DEMO_DIR/lib/make_package.py" "$PACKAGE_ARCHIVE" "$arch" || {
    echo "FEHLER: Paket $PACKAGE_ARCHIVE konnte nicht gebaut werden" >&2
    return 1
  }
}

# Selbstsigniertes Zertifikat fuer localhost bei HTTPS=1, falls keines da ist.
env_write_cert() {
  [[ "${HTTPS:-0}" == "1" ]] || return 0
  [[ -f "$WORK/tls.crt" ]] && return 0
  openssl req -x509 -newkey rsa:2048 -nodes -days 3650 \
    -keyout "$WORK/tls.key" -out "$WORK/tls.crt" \
    -subj "/CN=localhost" -addext "subjectAltName=DNS:localhost,IP:127.0.0.1" \
    2>"$WORK/openssl.log" || {
    echo "FEHLER: Zertifikat konnte nicht erzeugt werden:" >&2
    cat "$WORK/openssl.log" >&2
    return 1
  }
}

# Schreibt auth.pw und config.json (Vorlage services/energy-node.config.json).
# MQTT_USERNAME setzt Konto und Passwortdatei (MQTT_PASSWORD) der MQTT-Verbindung.
env_write_config() {
  printf '%s' "$PASSWORD" > "$WORK/auth.pw" || return 1
  chmod 600 "$WORK/auth.pw" || return 1
  local mqtt_password_file=""
  if [[ -n "${MQTT_USERNAME:-}" ]]; then
    printf '%s' "${MQTT_PASSWORD:-}" > "$WORK/mqtt.pw" || return 1
    chmod 600 "$WORK/mqtt.pw" || return 1
    mqtt_password_file="$WORK/mqtt.pw"
  fi
  local journal_helper=""
  if [[ "${JOURNAL:-0}" == "1" ]]; then
    journal_helper="$FAKES/fake_journal_helper.py"
  fi
  python3 - "$REPO_DIR/services/energy-node.config.json" "$WORK/config.json" \
           "$HTTP_PORT" "$WORK/devices" "$WORK/data" "$MQTT_PORT" "$WORK/auth.pw" \
           "${SERVICES_OFF:-0}" "$WORK/tls.crt" "$WORK/tls.key" "${HTTPS:-0}" \
           "${JOURNAL:-0}" "$journal_helper" "${MQTT_USERNAME:-}" "$mqtt_password_file" <<'PY' || return 1
import json
import sys

(vorlage, ziel, http_port, devices_dir, data_dir, mqtt_port, admin_pw,
 services_off, tls_cert, tls_key, https, simulate_journal, journal_helper,
 mqtt_user, mqtt_pw_file) = sys.argv[1:16]
config = json.loads(open(vorlage, encoding="utf-8").read())
config["mqtt"]["host"] = "127.0.0.1"
config["mqtt"]["port"] = int(mqtt_port)
config["mqtt"]["username"] = mqtt_user
config["mqtt"]["password_file"] = mqtt_pw_file
config["paths"]["devices_dir"] = devices_dir
config["paths"]["data_dir"] = data_dir
config["dashboard"]["port"] = int(http_port)
config["dashboard"]["bind_address"] = "127.0.0.1"
config["dashboard"]["admin_password_file"] = admin_pw
# JOURNAL: der Helper ist ein Skript im Repo, `sudo -n` wird ueber einen Shim
# im PATH (siehe env_start_dashboard) zum direkten Aufruf. Jede andere Aktion
# beantwortet der Fake mit Exit 64, Reboot und Poweroff bleiben wirkungslos.
if simulate_journal == "1":
    config["dashboard"]["system_action_helper"] = journal_helper
if https == "1":
    config["dashboard"]["tls"] = {"cert_file": tls_cert, "key_file": tls_key}
# SERVICES_OFF: alle sieben Dienste explizit aus, statt den Block wegzulassen.
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
}

# Baut das Dashboard nach OUT. Version aus dashboard/VERSION mit "-dev".
env_build_dashboard() {
  local out="$1" version
  version="$(tr -d '[:space:]' < "$DASHBOARD_DIR/VERSION")-dev"
  (
    cd "$DASHBOARD_DIR" && go build -ldflags "-X main.buildVersion=$version" -o "$out" ./cmd/dashboard
  )
}

# Startet den Fake-MQTT-Broker (Log: $WORK/broker.log).
env_start_broker() {
  local args=("$MQTT_PORT" "$FIXTURE" "--service-status" "$WORK/devices")
  [[ "${SIMULATE:-0}" == "1" ]] && args+=("--simulate")
  [[ -n "${MQTT_BROKER_PASSWORD:-}" ]] && args+=("--login" "$MQTT_USERNAME" "$MQTT_BROKER_PASSWORD")
  [[ -n "${BROKER_CONTROL_PORT:-}" ]] && args+=("--control-port" "$BROKER_CONTROL_PORT")
  python3 "$FAKES/minibroker.py" "${args[@]}" > "$WORK/broker.log" 2>&1 &
  env_track $!
  sleep 1
}

# Startet den gefaelschten GitHub-Releases-Server bei UPDATES=1 (Log: $WORK/updates-api.log).
env_start_fake_github() {
  [[ "${UPDATES:-0}" == "1" ]] || return 0
  local args=("$UPDATES_API_PORT" "v9.9.9")
  if [[ "${PACKAGE:-0}" == "1" && -n "${PACKAGE_ARCHIVE:-}" ]]; then
    args+=("$PACKAGE_ARCHIVE")
  fi
  python3 "$FAKES/fake_github_releases.py" "${args[@]}" > "$WORK/updates-api.log" 2>&1 &
  env_track $!
  sleep 1
}

# Startet das Dashboard-Binary BIN mit $WORK/config.json (Log: $WORK/dashboard.log).
# Setzt BASE, CURL_INSECURE, COMPANION_BASE.
env_start_dashboard() {
  local bin="$1"
  if [[ "${HTTPS:-0}" == "1" ]]; then
    BASE="https://localhost:$HTTP_PORT"
    CURL_INSECURE=(-k)
  else
    BASE="http://localhost:$HTTP_PORT"
    CURL_INSECURE=()
  fi
  COMPANION_BASE="https://127.0.0.1:$COMPANION_PORT"
  (
    cd "$DASHBOARD_DIR" || exit 1
    export ENERGY_NODE_COMPANION_PORT="$COMPANION_PORT"
    if [[ "${UPDATES:-0}" == "1" ]]; then
      export ENERGY_NODE_UPDATES_API_BASE="http://127.0.0.1:$UPDATES_API_PORT"
    fi
    if [[ "${JOURNAL:-0}" == "1" ]]; then
      mkdir -p "$WORK/bin"
      cp "$FAKES/fake_sudo.sh" "$WORK/bin/sudo"
      # 15 Minuten Vorlauf, damit die erste Abfrage sofort Zeilen findet.
      python3 -c 'import time; print(time.time() - 900)' > "$WORK/journal-epoch"
      export FAKE_JOURNAL_EPOCH="$WORK/journal-epoch"
      # Jeder Demo-Start beginnt mit normalem Systemzustand.
      rm -f "$WORK/fake-system.json" "$WORK/fake-system-events.jsonl"
      export FAKE_SYSTEM_STATE="$WORK/fake-system.json"
      export FAKE_SYSTEM_EVENTS="$WORK/fake-system-events.jsonl"
      export PATH="$WORK/bin:$PATH"
    fi
    if [[ "${INSTALLED:-0}" == "1" || "${PACKAGE:-0}" == "1" ]]; then
      export ENERGY_NODE_INSTALLER_STATE_DIR="$WORK/installer-state"
    fi
    exec "$bin" --config "$WORK/config.json"
  ) > "$WORK/dashboard.log" 2>&1 &
  DASHBOARD_PID=$!
  env_track "$DASHBOARD_PID"
}

# Wartet bis zu 60 s auf die Health-Antwort. Jede Antwort ausser 000 zaehlt.
env_wait_dashboard() {
  local code
  for _ in $(seq 1 60); do
    code="$(curl -s ${CURL_INSECURE[@]+"${CURL_INSECURE[@]}"} -m 2 -o /dev/null -w '%{http_code}' "$BASE/api/v1/health")"
    [[ "$code" != "000" ]] && return 0
    sleep 1
  done
  echo "Dashboard nicht erreichbar:" >&2
  cat "$WORK/dashboard.log" >&2
  return 1
}

# Admin-Login: setzt COOKIES und ADMIN_CSRF.
env_login() {
  COOKIES="$WORK/cookies.txt"
  curl -sf ${CURL_INSECURE[@]+"${CURL_INSECURE[@]}"} -m 5 -c "$COOKIES" -H "$SECURE_HEADER" -H 'Content-Type: application/json' \
    -X POST "$BASE/api/v1/auth/login" \
    -d "{\"username\":\"admin\",\"password\":\"$PASSWORD\"}" > /dev/null || return 1
  # shellcheck disable=SC2034
  ADMIN_CSRF="$(api "$BASE/api/v1/auth/session" | python3 -c 'import json,sys; print(json.load(sys.stdin)["csrf_token"])')" || return 1
}

# API-Aufruf mit Admin-Cookie: api ARGS...
api() {
  curl -sf ${CURL_INSECURE[@]+"${CURL_INSECURE[@]}"} -m 5 -b "$COOKIES" -H "$SECURE_HEADER" "$@"
}

# Startet den Fake-Companion (Log: $WORK/fake-companion.log).
env_start_fake_companion() {
  python3 "$FAKES/fake_companion.py" "$BASE" > "$WORK/fake-companion.log" 2>&1 &
  env_track $!
}

# Startet die Fake-Konsole (Log: $WORK/fake-console.log). Warnt, wenn sie nicht laeuft.
env_start_console() {
  local args=(--port "$CONSOLE_PORT" --dashboard "$BASE" --companion "$COMPANION_BASE")
  if [[ -n "${BROKER_CONTROL_PORT:-}" ]]; then
    args+=(--broker-control "http://127.0.0.1:$BROKER_CONTROL_PORT")
  fi
  if [[ "${UPDATES:-0}" == "1" ]]; then
    args+=(--updates "http://127.0.0.1:$UPDATES_API_PORT")
  fi
  if [[ "${INSTALLED:-0}" == "1" ]]; then
    args+=(--installer-state "$WORK/installer-state" --installer-fixture "$FIXTURES/installed-state")
  fi
  if [[ "${JOURNAL:-0}" == "1" ]]; then
    args+=(--system-state "$WORK/fake-system.json" --system-events "$WORK/fake-system-events.jsonl"
           --journal-epoch "$WORK/journal-epoch")
  fi
  python3 "$FAKES/fake_console.py" "${args[@]}" \
    > "$WORK/fake-console.log" 2>&1 &
  local pid=$!
  env_track "$pid"
  sleep 1
  if ! kill -0 "$pid" 2>/dev/null; then
    echo "Warnung: Fake-Konsole ist nicht gestartet (Log: $WORK/fake-console.log)" >&2
  fi
}
