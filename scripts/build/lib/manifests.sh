#!/usr/bin/env bash
#
# Die Dienstetabelle aus den Dienst-Manifesten (services/<dir>/manifest.json).
#
# Bis zur Vorarbeit stand sie als Doppelpunkt-Zeichenkette in deploy_lib.sh.
# Seit es die Manifeste gibt, waere das eine zweite Wahrheit neben Go
# (nodeagent.LoadServiceIDs) und Python (appconfig._load_manifests) - deshalb
# liest die Bash jetzt dieselbe Quelle. Plan B ersetzt diese Datei durch den
# Generator aus internal/bundle.
#
# Einbinden mit:
#   source "<repo>/scripts/build/lib/manifests.sh"
#   load_service_table "<repo>/services"
#
# Danach stehen bereit:
#   SERVICE_TABLE  "unit:pfad:verzeichnis:deploy" je Dienst
#   SERVICE_STEPS  "verzeichnis:schritt-id" je Dienst

# shellcheck disable=SC2034  # beide Arrays sind fuer die Aufrufer, nicht fuer uns
load_service_table() {
  local services_dir="$1"
  local line dir service_id unit step

  SERVICE_TABLE=()
  SERVICE_STEPS=()

  local rows
  rows="$(python3 - "${services_dir}" <<'PY'
import json, pathlib, sys

services_dir = pathlib.Path(sys.argv[1])
rows = []
seen_ids = {}
seen_steps = {}

for manifest_path in sorted(services_dir.glob("*/manifest.json")):
    directory = manifest_path.parent.name
    try:
        data = json.loads(manifest_path.read_text(encoding="utf-8"))
    except (OSError, ValueError) as exc:
        sys.exit("%s: nicht lesbar: %s" % (manifest_path, exc))
    for field in ("service_id", "unit", "bootstrap_step"):
        value = data.get(field)
        if not isinstance(value, str) or not value:
            sys.exit("%s: %s fehlt oder ist leer" % (manifest_path, field))
    service_id = data["service_id"]
    unit = data["unit"]
    step = data["bootstrap_step"]
    if service_id in seen_ids:
        sys.exit("%s: service_id %r auch in %s" % (manifest_path, service_id, seen_ids[service_id]))
    if step in seen_steps:
        sys.exit("%s: bootstrap_step %r auch in %s" % (manifest_path, step, seen_steps[step]))
    if not (manifest_path.parent / unit).is_file():
        sys.exit("%s: Unit %s liegt nicht neben dem Manifest" % (manifest_path, unit))
    seen_ids[service_id] = manifest_path
    seen_steps[step] = manifest_path
    rows.append("\t".join([directory, service_id, unit, step]))

if not rows:
    sys.exit("%s: kein Dienst-Manifest gefunden" % services_dir)
print("\n".join(rows))
PY
  )" || return 1

  while IFS=$'\t' read -r dir service_id unit step; do
    [[ -n "${dir}" ]] || continue
    # deploy=1: Unit installieren und Dienst neu starten. Das vierte Feld
    # bleibt erhalten, damit service_field und alle Aufrufer unveraendert
    # weiterlaufen.
    SERVICE_TABLE+=("${unit}:services/${dir}/${unit}:${dir}:1")
    SERVICE_STEPS+=("${dir}:${step}")
  done <<< "${rows}"
}
