#!/usr/bin/env bash
#
# Liefert die Dienst-Manifeste (services/<name>/manifest.json) auf das
# Zielgeraet aus: nach /etc/energy-node/manifests/<service_id>.json - dem
# Ort, aus dem sowohl das Go-Dashboard (internal/nodeagent.LoadServiceIDs)
# als auch die Python-Bruecken (energy_node_common.appconfig._load_manifests)
# den aktiven Dienstumfang lesen. Die Python-Seite ist fail-closed: fehlt
# das Verzeichnis oder ist es leer, startet keine Bruecke.
#
# PR #12 hat Format und Konsumenten gebaut, die Auslieferung aber
# ausgeklammert; ohne diesen Schritt kommt ein von werkstatt-IoT migrierter
# Node nicht hoch.
#
# Der volle Satz wird immer ausgerollt - unabhaengig von --service -, weil
# energy_node_common den Manifest-Satz gegen den services-Block der
# config.json in beide Richtungen prueft. Ein Teilsatz waere ein Fehler.
#
# Usage (nach `source`):
#   ensure_remote_manifests "<ssh_target>" "${SSH_OPTS[@]}"

ENSURE_MANIFESTS_LIB_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ENSURE_MANIFESTS_REPO_ROOT="$(git -C "${ENSURE_MANIFESTS_LIB_DIR}" rev-parse --show-toplevel)"

# stage_manifests <services_dir> <staging_dir>
# Kopiert jedes <services_dir>/*/manifest.json nach
# <staging_dir>/<service_id>.json. Exit 1 bei fehlender/leerer service_id,
# doppelter service_id oder wenn kein Manifest gefunden wird.
stage_manifests() {
  local services_dir="$1" staging_dir="$2"
  local manifest service_id
  local seen=" "
  local found=0

  for manifest in "${services_dir}"/*/manifest.json; do
    [[ -f "${manifest}" ]] || continue
    found=1
    service_id="$(python3 -c '
import json, sys
try:
    value = json.load(open(sys.argv[1])).get("service_id")
except Exception as exc:  # noqa: BLE001 - Meldung reicht
    sys.exit("%s: %s" % (sys.argv[1], exc))
if not isinstance(value, str) or not value:
    sys.exit("%s: service_id fehlt oder ist leer" % sys.argv[1])
print(value)
' "${manifest}")" || return 1

    case "${seen}" in
      *" ${service_id} "*)
        echo "stage_manifests: service_id '${service_id}' doppelt vergeben (${manifest})" >&2
        return 1
        ;;
    esac
    seen="${seen}${service_id} "
    cp "${manifest}" "${staging_dir}/${service_id}.json"
  done

  if [[ "${found}" -eq 0 ]]; then
    echo "stage_manifests: keine Manifeste unter ${services_dir}/*/manifest.json" >&2
    return 1
  fi
}
