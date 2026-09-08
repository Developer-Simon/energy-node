#!/usr/bin/env bash
set -euo pipefail

# fetch_env_from_remote.sh
# Holt den auf dem Zielgeraet gepflegten Stand zurueck ins Repository:
# die zentrale Konfiguration, die Geraetekonfigurationen, die
# Mosquitto-Bridge-Konfiguration und die installierten systemd-Units.
#
# Der Name stammt aus der Zeit der *.env-Dateien; die gibt es nicht mehr.

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "${SCRIPT_DIR}/deploy_lib.sh"

parse_deploy_args "$@"

# Alle nachfolgenden repo-relativen Pfade (services/...) setzen CWD == Repo-Root
# voraus - unabhaengig davon, von wo aus dieses Skript aufgerufen wird.
cd "${REPO_ROOT}"

# Die zentrale Konfiguration wird auf dem Zielgeraet vom Dashboard
# bearbeitet; dieser fetch holt den dortigen Stand als neue Vorlage
# zurueck. Vor dem Commit pruefen, ob geraetespezifische Werte
# (Hostnamen, Pfade) wirklich in die versionierte Vorlage gehoeren.
fetch_with_sudo "${SSH_TARGET}:/etc/energy-node/config.json" "services/energy-node.config.json"

# Geraetekonfigurationen fetchen
fetch "${REMOTE_PREFIX}/devices/apsystems_devices.json" "services/apsystems_ez1/apsystems_devices.json"
fetch "${REMOTE_PREFIX}/devices/battery_soc_devices.json" "services/battery_soc/battery_soc_devices.json"
fetch "${REMOTE_PREFIX}/devices/battery_soc_devices.schema.json" "services/battery_soc/battery_soc_devices.schema.json"
fetch "${REMOTE_PREFIX}/devices/shelly_devices.json" "services/shelly/shelly_devices.json"
fetch "${REMOTE_PREFIX}/devices/shelly_presets.json" "services/shelly/shelly_presets.json"
fetch "${REMOTE_PREFIX}/devices/shelly_presets.schema.json" "services/shelly/shelly_presets.schema.json"
fetch "${REMOTE_PREFIX}/devices/tuya_devices.json" "services/tuya_mqtt/tuya_devices.json"
fetch "${REMOTE_PREFIX}/devices/tuya_devices.schema.json" "services/tuya_mqtt/tuya_devices.schema.json"
fetch "${REMOTE_PREFIX}/devices/trucki_devices.json" "services/trucki/trucki_devices.json"
fetch "${REMOTE_PREFIX}/devices/trucki_devices.schema.json" "services/trucki/trucki_devices.schema.json"

# Systemd-Units fetchen, die in diesem Repository verwaltet werden
for entry in "${SERVICE_TABLE[@]}"; do
  unit_name="$(service_field "${entry}" 1)"
  local_path="${REPO_ROOT}/$(service_field "${entry}" 2)"
  if ssh "${SSH_OPTS[@]}" "${SSH_TARGET}" sudo test -e -- "/etc/systemd/system/${unit_name}"; then
    fetch_with_sudo "${SSH_TARGET}:/etc/systemd/system/${unit_name}" "${local_path}"
  else
    echo "Skipping missing remote service unit: ${unit_name}"
  fi
done

echo
echo "Hinweis: pruefe die zurueckgeholte Vorlage vor dem Commit:"
echo "  ./scripts/deploy/check_tracked_secrets.sh"
