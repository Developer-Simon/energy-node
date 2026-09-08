#!/usr/bin/env bash
set -euo pipefail

# deploy_src_to_remote.sh
# Copy the runtime source files from this repository to the target host.
# Systemd service units are installed/updated with checksum comparison
# (see install_or_update_service_units below).

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "${SCRIPT_DIR}/deploy_lib.sh"
source "${SCRIPT_DIR}/ensure_remote_secrets.sh"
source "${SCRIPT_DIR}/ensure_remote_config.sh"

# Alle nachfolgenden repo-relativen Pfade (services/..., check_tracked_secrets.sh
# ueber SCRIPT_DIR) setzen CWD == Repo-Root voraus - unabhaengig davon, von
# wo aus dieses Skript aufgerufen wird.
cd "${REPO_ROOT}"

RESTART_ONLY=false

# Bekannte Dienste = das remote-Unterverzeichnis (Feld 3) jedes Eintrags aus
# SERVICE_TABLE mit deploy=1 - dieselbe Spalte, die auch
# install_or_update_service_units/restart_services schon verwenden, damit es
# nur eine Quelle fuer "welche Dienste gibt es" gibt.
KNOWN_SERVICES=()
for _entry in "${SERVICE_TABLE[@]}"; do
  [[ "$(service_field "${_entry}" 4)" == "1" ]] || continue
  KNOWN_SERVICES+=("$(service_field "${_entry}" 3)")
done
unset _entry

SELECTED_SERVICES=()

handle_extra_arg() {
  case "$1" in
    --restart-only) RESTART_ONLY=true; CONSUMED_ARGS=1 ;;
    --service)
      if [[ -z "${2:-}" ]]; then
        echo "--service braucht einen Dienstnamen." >&2
        exit 1
      fi
      SELECTED_SERVICES+=("$2")
      CONSUMED_ARGS=2
      ;;
    *) CONSUMED_ARGS=0 ;;
  esac
}
EXTRA_ARG_HANDLER=handle_extra_arg

print_service_usage() {
  cat <<EOF

  --service NAME        Nur diesen Dienst (Quelle, Devices-Config, Unit) ausrollen;
                         wiederholbar fuer mehrere Dienste. Ohne --service werden
                         alle Dienste ausgerollt. Die gemeinsame energy-node-common-
                         Abhaengigkeit wird immer mitgebaut/installiert, da jeder
                         Dienst sie braucht.
                         Bekannte Dienste: ${KNOWN_SERVICES[*]}
EOF
}
EXTRA_USAGE_HOOK=print_service_usage

parse_deploy_args "$@"

if [[ "$RESTART_ONLY" == true && "$SKIP_RESTART" == "1" ]]; then
  echo "--restart-only und --skip-restart schliessen sich aus." >&2
  exit 1
fi

# Ohne --service: alle bekannten Dienste. Mit --service: nur die genannten,
# nach Validierung gegen KNOWN_SERVICES.
if [[ "${#SELECTED_SERVICES[@]}" -eq 0 ]]; then
  EFFECTIVE_SERVICES=("${KNOWN_SERVICES[@]}")
else
  EFFECTIVE_SERVICES=()
  for requested in "${SELECTED_SERVICES[@]}"; do
    known=false
    for candidate in "${KNOWN_SERVICES[@]}"; do
      [[ "${candidate}" == "${requested}" ]] && { known=true; break; }
    done
    if [[ "${known}" != true ]]; then
      echo "Unbekannter Dienst fuer --service: ${requested}" >&2
      echo "Bekannte Dienste: ${KNOWN_SERVICES[*]}" >&2
      exit 1
    fi
    EFFECTIVE_SERVICES+=("${requested}")
  done
fi

service_selected() {
  local name="$1" entry
  for entry in "${EFFECTIVE_SERVICES[@]}"; do
    [[ "${entry}" == "${name}" ]] && return 0
  done
  return 1
}

restart_services() {
  echo "==> Restarting selected Energy Node services on ${SSH_TARGET}: ${EFFECTIVE_SERVICES[*]}"
  local units=()
  local entry
  for entry in "${SERVICE_TABLE[@]}"; do
    [[ "$(service_field "${entry}" 4)" == "1" ]] || continue
    service_selected "$(service_field "${entry}" 3)" || continue
    units+=("$(service_field "${entry}" 1)")
  done
  ssh "${SSH_OPTS[@]}" "${SSH_TARGET}" "
    set -euo pipefail
    sudo systemctl daemon-reload
    for service in ${units[*]}; do
      echo \"- \$service\"
      if ! sudo systemctl restart \"\$service\"; then
        echo \"Warning: could not restart \$service\" >&2
      fi
    done
    echo 'Done.'
  "
}

if [[ "$RESTART_ONLY" == true ]]; then
  restart_services
  echo "Restart-only run complete."
  exit 0
fi

copy_with_sudo() {
  local src="$1"
  local dst="$2"
  echo "Copying ${src} -> ${dst} (with sudo)"
  rsync "${RSYNC_OPTS[@]}" --rsync-path="sudo rsync" -e "${RSYNC_SSH}" "${src}" "${dst}"
}

# *_devices.json files are edited live through the dashboard on the target,
# so --ignore-existing here means a redeploy never clobbers a user's live
# device configuration; see knowhow/dashboard/automationen-funktionsweise.md.
copy_if_absent() {
  local src="$1"
  local dst="$2"
  echo "Copying ${src} -> ${dst} (only if absent)"
  rsync "${RSYNC_OPTS[@]}" --ignore-existing -e "${RSYNC_SSH}" "${src}" "${dst}"
}

ensure_remote_dirs() {
  echo "Creating remote directories on ${SSH_TARGET}"
  ssh "${SSH_OPTS[@]}" "${SSH_TARGET}" \
    "mkdir -p -- '${TARGET_BASE}/apsystems_ez1' '${TARGET_BASE}/battery_soc' '${TARGET_BASE}/battery_soc_core' '${TARGET_BASE}/shelly' '${TARGET_BASE}/tuya_mqtt' '${TARGET_BASE}/trucki' '${TARGET_BASE}/energy-node' '${TARGET_BASE}/energy_node_common' '${TARGET_BASE}/automation' '${TARGET_BASE}/devices'"
}

# Frueher wurde jede bereits installierte Unit uebersprungen. Beim harten
# Schnitt auf config.json ist das ein Fehler: die EnvironmentFile-Zeilen
# muessen aus den installierten Units verschwinden, sonst startet der
# Dienst mit einer nicht mehr existierenden Datei und scheitert. Deshalb
# jetzt Inhaltsvergleich - so macht es deploy_dashboard_to_remote.sh
# bereits.
install_or_update_service_units() {
  echo "==> Abgleich der systemd-Units auf ${SSH_TARGET}"
  local entry unit_name local_path remote_subdir deploy local_sum remote_sum
  for entry in "${SERVICE_TABLE[@]}"; do
    unit_name="$(service_field "${entry}" 1)"
    local_path="${REPO_ROOT}/$(service_field "${entry}" 2)"
    remote_subdir="$(service_field "${entry}" 3)"
    deploy="$(service_field "${entry}" 4)"
    [[ "${deploy}" == "1" ]] || { echo "  - ${unit_name} wird nicht ausgerollt (Fremddienst)"; continue; }
    if ! service_selected "${remote_subdir}"; then
      echo "  - ${unit_name} uebersprungen (nicht in --service ausgewaehlt)"
      continue
    fi

    # Die Unit wird vor dem Vergleich/Kopieren gerendert: der im Repo
    # hinterlegte Platzhalter-Benutzer "energynode" (User=/WorkingDirectory=/
    # ExecStart=) wird durch den tatsaechlichen Zielbenutzer/-pfad ersetzt,
    # damit auf dem Zielgeraet keine falschen Zugriffsrechte/Pfade landen.
    local render_dir rendered_unit
    render_dir="$(mktemp -d)"
    rendered_unit="${render_dir}/${unit_name}"
    render_service_unit "${local_path}" "${rendered_unit}"

    local_sum="$(sha256sum "${rendered_unit}" | cut -d' ' -f1)"
    remote_sum="$(ssh "${SSH_OPTS[@]}" "${SSH_TARGET}" \
      "if sudo test -f '/etc/systemd/system/${unit_name}'; then sudo sha256sum '/etc/systemd/system/${unit_name}' | cut -d' ' -f1; else printf 'fehlt'; fi")"

    if [[ "${local_sum}" == "${remote_sum}" ]]; then
      echo "  - ${unit_name} unveraendert"
      rm -rf "${render_dir}"
      continue
    fi
    if [[ "$DRY_RUN" == true ]]; then
      echo "  - ${unit_name} weicht ab (${remote_sum}); wuerde installiert werden (dry-run)"
      rm -rf "${render_dir}"
      continue
    fi
    echo "  - ${unit_name} weicht ab; wird installiert"
    copy "${rendered_unit}" "${REMOTE_PREFIX}/${remote_subdir}/"
    rm -rf "${render_dir}"
    ssh "${SSH_OPTS[@]}" "${SSH_TARGET}" "
      set -e
      sudo install -o root -g root -m 0644 '${TARGET_BASE}/${remote_subdir}/${unit_name}' /etc/systemd/system/${unit_name}
      sudo systemctl daemon-reload
      sudo systemctl enable ${unit_name}
    "
  done
}

# Clean up local build artifacts that might have been created by earlier
# local or remote installs.  This prevents root-owned __pycache__ / egg-info
# directories from being copied to the remote host.
find libs/energy_node_common -type d \( \
  -name '__pycache__' -o \
  -name '*.egg-info' -o \
  -name '.venv' -o \
  -name '.tox' -o \
  -name 'dist' -o \
  -name 'build' -o \
  -name '.pytest_cache' \
\) -prune -exec rm -rf {} +

# Ensure required directories exist on the remote host.
ensure_remote_dirs

echo "==> Preflight: keine Zugangsdaten in versionierten Dateien"
"${SCRIPT_DIR}/check_tracked_secrets.sh"

echo "==> Preflight: zentrale Zugangsdaten auf dem Zielgeraet"
ensure_remote_secrets "${SSH_TARGET}" "${SSH_OPTS[@]}"

echo "==> Preflight: zentrale Konfiguration auf dem Zielgeraet"
ensure_remote_config "${SSH_TARGET}" "${FORCE_CONFIG}" "${SSH_OPTS[@]}"

# services/VERSION beschreibt alle Python-Dienste als Ganzes (siehe
# git-hooks/pre-commit). Es landet im gemeinsamen devices_dir, den Python und
# Go-Dashboard schon fuer *_devices.json teilen; das Dashboard zeigt es an,
# wenn paths.services_version_file in config.json darauf zeigt (Standard in
# services/energy-node.config.json).
copy "services/VERSION" "${REMOTE_PREFIX}/devices/"

# Copy source files - ein Block je Dienst, nur ausgefuehrt wenn der Dienst in
# EFFECTIVE_SERVICES steht (Standard: alle). remote_subdir (siehe
# SERVICE_TABLE Feld 3) ist der Schluessel, denselben Namen nimmt --service
# entgegen.
copy_apsystems_ez1() {
  copy "services/apsystems_ez1/apsystems_ez1_mqtt.py" "${REMOTE_PREFIX}/apsystems_ez1/"
  copy_if_absent "services/apsystems_ez1/apsystems_devices.json" "${REMOTE_PREFIX}/devices/"
  copy "services/apsystems_ez1/apsystems_devices.schema.json" "${REMOTE_PREFIX}/devices/"
}
copy_battery_soc() {
  copy "services/battery_soc/battery_soc_mqtt.py" "${REMOTE_PREFIX}/battery_soc/"
  copy "services/battery_soc/soc_config.py" "${REMOTE_PREFIX}/battery_soc/"
  copy "services/battery_soc/state_store.py" "${REMOTE_PREFIX}/battery_soc/"
  copy "services/battery_soc/mqtt_inputs.py" "${REMOTE_PREFIX}/battery_soc/"
  copy "services/battery_soc/mqtt_discovery.py" "${REMOTE_PREFIX}/battery_soc/"
  copy_if_absent "services/battery_soc/battery_soc_devices.json" "${REMOTE_PREFIX}/devices/"
  copy "services/battery_soc/battery_soc_devices.schema.json" "${REMOTE_PREFIX}/devices/"
}
copy_automation() {
  copy "services/automation/automation_mqtt.py" "${REMOTE_PREFIX}/automation/"
  copy "services/automation/ha_template.py" "${REMOTE_PREFIX}/automation/"
  copy "services/automation/automation_rules.schema.json" "${REMOTE_PREFIX}/devices/"
  copy_if_absent "services/automation/automation_rules.json" "${REMOTE_PREFIX}/devices/"
}
copy_shelly() {
  copy "services/shelly/shelly_rpc_mqtt.py" "${REMOTE_PREFIX}/shelly/"
  copy_if_absent "services/shelly/shelly_devices.json" "${REMOTE_PREFIX}/devices/"
  copy "services/shelly/shelly_devices.schema.json" "${REMOTE_PREFIX}/devices/"
  copy "services/shelly/shelly_presets.json" "${REMOTE_PREFIX}/devices/"
  copy "services/shelly/shelly_presets.schema.json" "${REMOTE_PREFIX}/devices/"
}
copy_tuya_mqtt() {
  copy "services/tuya_mqtt/tuya_mqtt.py" "${REMOTE_PREFIX}/tuya_mqtt/"
  copy "services/tuya_mqtt/tinytuya_probe.py" "${REMOTE_PREFIX}/tuya_mqtt/"
  copy_if_absent "services/tuya_mqtt/tuya_devices.json" "${REMOTE_PREFIX}/devices/"
  copy "services/tuya_mqtt/tuya_devices.schema.json" "${REMOTE_PREFIX}/devices/"
}
copy_trucki() {
  copy "services/trucki/trucki_http_mqtt.py" "${REMOTE_PREFIX}/trucki/"
  copy_if_absent "services/trucki/trucki_devices.json" "${REMOTE_PREFIX}/devices/"
  copy "services/trucki/trucki_devices.schema.json" "${REMOTE_PREFIX}/devices/"
}
copy_energy_node() {
  copy "services/energy-node/energy_node_mqtt.py" "${REMOTE_PREFIX}/energy-node/"
}

for service in "${EFFECTIVE_SERVICES[@]}"; do
  echo "==> Kopiere Quelldateien fuer: ${service}"
  case "${service}" in
    apsystems_ez1) copy_apsystems_ez1 ;;
    battery_soc) copy_battery_soc ;;
    automation) copy_automation ;;
    shelly) copy_shelly ;;
    tuya_mqtt) copy_tuya_mqtt ;;
    trucki) copy_trucki ;;
    energy-node) copy_energy_node ;;
    *)
      echo "Kein Copy-Schritt fuer Dienst '${service}' hinterlegt." >&2
      exit 1
      ;;
  esac
done

install_or_update_service_units

# Build and deploy the energy-node-common package as a wheel. The target's
# installed version (pip show) is checked first: if it already matches
# VERSION_FILE (bumped per commit by git-hooks/pre-commit, like
# dashboard/VERSION and services/VERSION - see COMPONENTS in git-hooks/lib.sh),
# nothing is built and nothing is installed. Otherwise a wheel cached in the
# package's dist directory is reused as long as its version matches
# VERSION_FILE, and only rebuilt when it does not. Building locally avoids
# permission problems with root-owned build/egg-info directories on the
# remote host.
# Ein einziger EXIT-trap fuer beide Wheel-Bloecke (energy-node-common und
# battery-soc-core weiter unten) - ein zweiter `trap ... EXIT` wuerde den
# ersten sonst überschreiben statt sich zu ihm zu addieren, und Bau-Verzeichnisse
# des jeweils anderen Blocks blieben liegen. Leere Variablen sind fuer `rm -rf`
# unschaedlich.
WHEEL_BUILD_DIR=""
COMMON_BUILD_DIR=""
CORE_WHEEL_BUILD_DIR=""
CORE_BUILD_DIR=""
trap 'rm -rf "${WHEEL_BUILD_DIR}" "${COMMON_BUILD_DIR}" "${CORE_WHEEL_BUILD_DIR}" "${CORE_BUILD_DIR}"' EXIT

COMMON_DIR="libs/energy_node_common"
WHEEL_CACHE_DIR="${COMMON_DIR}/dist"
VERSION_FILE="${COMMON_DIR}/VERSION"

wheel_version() {
  python3 -c "import os, re, sys; m = re.match(r'energy_node_common-(.+?)-py', os.path.basename(sys.argv[1])); print(m.group(1) if m else '')" "$1"
}

if [[ -f "$VERSION_FILE" ]]; then
  CURRENT_VERSION="$(tr -d '[:space:]' < "$VERSION_FILE")"
  CURRENT_VERSION="${CURRENT_VERSION#v}"

  # Zuerst pruefen, welche Version das Zielsystem bereits installiert hat.
  # Stimmt sie mit VERSION_FILE ueberein, gibt es weder etwas zu bauen noch
  # zu installieren. Frueher lief der (teure) Wheel-Build bei jedem Deploy,
  # sobald die lokale dist/-Kopie fehlte - auch dann, wenn das Zielsystem
  # schon die passende Version hatte.
  REMOTE_VERSION="$(ssh "${SSH_OPTS[@]}" "${SSH_TARGET}" \
    "sudo python3 -m pip show energy-node-common 2>/dev/null | sed -n 's/^Version: //p'")"

  if [[ "${REMOTE_VERSION}" == "${CURRENT_VERSION}" ]]; then
    echo "==> ${SSH_TARGET} hat energy-node-common ${CURRENT_VERSION} bereits installiert; Build und Installation uebersprungen"
  else
    echo "==> ${SSH_TARGET} hat energy-node-common ${REMOTE_VERSION:-<nicht installiert>}, benoetigt wird ${CURRENT_VERSION}"

    mapfile -t CACHED_WHEELS < <(
      find "$WHEEL_CACHE_DIR" -maxdepth 1 -type f \
        -name 'energy_node_common-*.whl' -print 2>/dev/null
    )

    WHEEL_FILE=""
    if [[ "${#CACHED_WHEELS[@]}" -eq 1 ]]; then
      CACHED_VERSION="$(wheel_version "${CACHED_WHEELS[0]}")"
      if [[ "$CACHED_VERSION" == "$CURRENT_VERSION" ]]; then
        WHEEL_FILE="${CACHED_WHEELS[0]}"
        WHEEL_BASENAME=$(basename "$WHEEL_FILE")
        echo "==> Reusing cached energy-node-common wheel: ${WHEEL_BASENAME}"
      fi
    elif [[ "${#CACHED_WHEELS[@]}" -gt 1 ]]; then
      echo "ERROR: Expected at most one cached energy-node-common wheel in ${WHEEL_CACHE_DIR}, found ${#CACHED_WHEELS[@]}" >&2
      exit 1
    fi

    if [[ -z "$WHEEL_FILE" ]]; then
      echo "==> ${VERSION_FILE} (${CURRENT_VERSION}) does not match cached wheel; building wheel"
      WHEEL_BUILD_DIR=$(mktemp -d)
      COMMON_BUILD_DIR=$(mktemp -d)
      # Aufraeumen der temporaeren Bau-Verzeichnisse übernimmt der EXIT-trap
      # weiter oben (gemeinsam mit dem battery-soc-core-Block unten).

      cp -a "${COMMON_DIR}/." "${COMMON_BUILD_DIR}/"
      (cd "${COMMON_BUILD_DIR}" && python3 -m pip wheel . --no-deps --wheel-dir "${WHEEL_BUILD_DIR}")

      WHEEL_FILE=$(find "${WHEEL_BUILD_DIR}" -maxdepth 1 -name '*.whl' | head -n 1)
      if [[ -z "$WHEEL_FILE" ]]; then
        echo "ERROR: No wheel file produced for energy-node-common" >&2
        exit 1
      fi

      WHEEL_BASENAME=$(basename "$WHEEL_FILE")
      BUILT_VERSION="$(wheel_version "$WHEEL_FILE")"
      if [[ "$BUILT_VERSION" != "$CURRENT_VERSION" ]]; then
        echo "ERROR: Built wheel version ${BUILT_VERSION} does not match ${VERSION_FILE} (${CURRENT_VERSION})" >&2
        exit 1
      fi
      echo "Built wheel version: ${BUILT_VERSION}"

      mkdir -p "$WHEEL_CACHE_DIR"
      rm -f "${WHEEL_CACHE_DIR}"/*.whl
      cp "$WHEEL_FILE" "${WHEEL_CACHE_DIR}/${WHEEL_BASENAME}"
      WHEEL_FILE="${WHEEL_CACHE_DIR}/${WHEEL_BASENAME}"
    fi

    echo "==> Copying wheel to ${SSH_TARGET}"
    # Remove stale wheels on the target so pip cannot pick up an older file.
    ssh "${SSH_OPTS[@]}" "${SSH_TARGET}" "rm -f '${TARGET_BASE}/energy_node_common/'*.whl"
    copy "${WHEEL_FILE}" "${REMOTE_PREFIX}/energy_node_common/"

    echo "==> Installing wheel on ${SSH_TARGET}"
    ssh "${SSH_OPTS[@]}" "${SSH_TARGET}" "
      set -e
      sudo python3 -m pip install \
        --force-reinstall \
        --no-cache-dir \
        --no-deps \
        --break-system-packages \
        '${TARGET_BASE}/energy_node_common/${WHEEL_BASENAME}'
      echo 'Installed version:'
      sudo python3 -m pip show energy-node-common | grep '^Version:'
    "
  fi
fi

# Dasselbe Wheel-Verfahren fuer battery-soc-core, das transport-freie
# SoC-Kernpaket, das battery_soc_mqtt.py seit der Core-Extraktion nutzt.
# Nur gebaut/installiert, wenn battery_soc ueberhaupt ausgerollt wird - kein
# anderer Dienst haengt davon ab.
if service_selected "battery_soc"; then
  CORE_DIR="libs/battery_soc_core"
  CORE_WHEEL_CACHE_DIR="${CORE_DIR}/dist"
  CORE_VERSION_FILE="${CORE_DIR}/VERSION"

  core_wheel_version() {
    python3 -c "import os, re, sys; m = re.match(r'battery_soc_core-(.+?)-py', os.path.basename(sys.argv[1])); print(m.group(1) if m else '')" "$1"
  }

  if [[ -f "$CORE_VERSION_FILE" ]]; then
    CORE_CURRENT_VERSION="$(tr -d '[:space:]' < "$CORE_VERSION_FILE")"
    CORE_CURRENT_VERSION="${CORE_CURRENT_VERSION#v}"

    CORE_REMOTE_VERSION="$(ssh "${SSH_OPTS[@]}" "${SSH_TARGET}" \
      "sudo python3 -m pip show battery-soc-core 2>/dev/null | sed -n 's/^Version: //p'")"

    if [[ "${CORE_REMOTE_VERSION}" == "${CORE_CURRENT_VERSION}" ]]; then
      echo "==> ${SSH_TARGET} hat battery-soc-core ${CORE_CURRENT_VERSION} bereits installiert; Build und Installation uebersprungen"
    else
      echo "==> ${SSH_TARGET} hat battery-soc-core ${CORE_REMOTE_VERSION:-<nicht installiert>}, benoetigt wird ${CORE_CURRENT_VERSION}"

      mapfile -t CORE_CACHED_WHEELS < <(
        find "$CORE_WHEEL_CACHE_DIR" -maxdepth 1 -type f \
          -name 'battery_soc_core-*.whl' -print 2>/dev/null
      )

      CORE_WHEEL_FILE=""
      if [[ "${#CORE_CACHED_WHEELS[@]}" -eq 1 ]]; then
        CORE_CACHED_VERSION="$(core_wheel_version "${CORE_CACHED_WHEELS[0]}")"
        if [[ "$CORE_CACHED_VERSION" == "$CORE_CURRENT_VERSION" ]]; then
          CORE_WHEEL_FILE="${CORE_CACHED_WHEELS[0]}"
          CORE_WHEEL_BASENAME=$(basename "$CORE_WHEEL_FILE")
          echo "==> Reusing cached battery-soc-core wheel: ${CORE_WHEEL_BASENAME}"
        fi
      elif [[ "${#CORE_CACHED_WHEELS[@]}" -gt 1 ]]; then
        echo "ERROR: Expected at most one cached battery-soc-core wheel in ${CORE_WHEEL_CACHE_DIR}, found ${#CORE_CACHED_WHEELS[@]}" >&2
        exit 1
      fi

      if [[ -z "$CORE_WHEEL_FILE" ]]; then
        echo "==> ${CORE_VERSION_FILE} (${CORE_CURRENT_VERSION}) does not match cached wheel; building wheel"
        CORE_WHEEL_BUILD_DIR=$(mktemp -d)
        CORE_BUILD_DIR=$(mktemp -d)
        # Aufraeumen uebernimmt der gemeinsame EXIT-trap weiter oben.

        cp -a "${CORE_DIR}/." "${CORE_BUILD_DIR}/"
        (cd "${CORE_BUILD_DIR}" && python3 -m pip wheel . --no-deps --wheel-dir "${CORE_WHEEL_BUILD_DIR}")

        CORE_WHEEL_FILE=$(find "${CORE_WHEEL_BUILD_DIR}" -maxdepth 1 -name '*.whl' | head -n 1)
        if [[ -z "$CORE_WHEEL_FILE" ]]; then
          echo "ERROR: No wheel file produced for battery-soc-core" >&2
          exit 1
        fi

        CORE_WHEEL_BASENAME=$(basename "$CORE_WHEEL_FILE")
        CORE_BUILT_VERSION="$(core_wheel_version "$CORE_WHEEL_FILE")"
        if [[ "$CORE_BUILT_VERSION" != "$CORE_CURRENT_VERSION" ]]; then
          echo "ERROR: Built wheel version ${CORE_BUILT_VERSION} does not match ${CORE_VERSION_FILE} (${CORE_CURRENT_VERSION})" >&2
          exit 1
        fi
        echo "Built wheel version: ${CORE_BUILT_VERSION}"

        mkdir -p "$CORE_WHEEL_CACHE_DIR"
        rm -f "${CORE_WHEEL_CACHE_DIR}"/*.whl
        cp "$CORE_WHEEL_FILE" "${CORE_WHEEL_CACHE_DIR}/${CORE_WHEEL_BASENAME}"
        CORE_WHEEL_FILE="${CORE_WHEEL_CACHE_DIR}/${CORE_WHEEL_BASENAME}"
      fi

      echo "==> Copying wheel to ${SSH_TARGET}"
      # Remove stale wheels on the target so pip cannot pick up an older file.
      ssh "${SSH_OPTS[@]}" "${SSH_TARGET}" "rm -f '${TARGET_BASE}/battery_soc_core/'*.whl"
      copy "${CORE_WHEEL_FILE}" "${REMOTE_PREFIX}/battery_soc_core/"

      echo "==> Installing wheel on ${SSH_TARGET}"
      ssh "${SSH_OPTS[@]}" "${SSH_TARGET}" "
        set -e
        sudo python3 -m pip install \
          --force-reinstall \
          --no-cache-dir \
          --no-deps \
          --break-system-packages \
          '${TARGET_BASE}/battery_soc_core/${CORE_WHEEL_BASENAME}'
        echo 'Installed version:'
        sudo python3 -m pip show battery-soc-core | grep '^Version:'
      "
    fi
  fi
fi

if [[ "${SKIP_RESTART}" == "1" ]]; then
  echo "SKIP_RESTART=1, skipping remote service restart."
else
  restart_services
fi

echo "Deployment complete."
