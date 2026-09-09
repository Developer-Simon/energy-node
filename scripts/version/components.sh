#!/usr/bin/env bash
#
# Shared component metadata for the version tooling. Sourced, not executed
# directly (scripts/version/bump-patch.sh).

COMPONENTS=(
  "dashboard/:dashboard/VERSION"
  "services/:services/VERSION"
  "libs/energy_node_common/:libs/energy_node_common/VERSION"
  "libs/battery_soc_core/:libs/battery_soc_core/VERSION"
  "integrations/homeassistant/:integrations/homeassistant/custom_components/battery_soc/manifest.json"
)

# component_touched prueft, ob eine Datei-Liste (staged/changed, eine pro
# Zeile) eine Komponente betrifft. Dateien einer verschachtelten Komponente
# (z.B. libs/energy_node_common/ unterhalb eines umschliessenden Praefix)
# zaehlen dabei NICHT als den umgebenden Praefix betreffend - ein Commit, der
# ausschliesslich das gemeinsame Modul aendert, soll nur dessen eigenes
# VERSION bumpen, nicht auch noch das der Sammel-Komponente.
# Liest die aktuelle Version einer Komponente normalisiert als "vX.Y.Z".
# Bei .json-Dateien (z.B. manifest.json) wird das "version"-Feld gelesen -
# HACS/hassfest verlangt dort eine nackte Semver ohne "v"-Prefix - und fuer
# die Rueckgabe mit "v" versehen, damit alle Aufrufer dasselbe Format
# vergleichen koennen.
read_component_version() {
  local version_file="$1"
  if [[ "${version_file}" == *.json ]]; then
    local raw
    raw="$(python3 -c 'import json,sys
try:
    print(json.load(open(sys.argv[1])).get("version", ""))
except Exception:
    pass' "${version_file}" 2>/dev/null)"
    [[ -n "${raw}" ]] && printf 'v%s\n' "${raw}"
  else
    tr -d '[:space:]' < "${version_file}"
  fi
}

component_touched() {
  local dir_prefix="$1" version_file="$2" files="$3"
  local filtered other other_prefix
  filtered="$(printf '%s\n' "${files}" | grep "^${dir_prefix}" || true)"
  filtered="$(printf '%s\n' "${filtered}" | grep -v "^${version_file}$" || true)"
  # CHANGELOG.md is a generated artefact (scripts/generate_changelog.sh) - a
  # changelog-only change must not bump the component.
  filtered="$(printf '%s\n' "${filtered}" | grep -v "^${dir_prefix}CHANGELOG.md$" || true)"
  for other in "${COMPONENTS[@]}"; do
    other_prefix="${other%%:*}"
    if [[ "${other_prefix}" != "${dir_prefix}" && "${other_prefix}" == "${dir_prefix}"* ]]; then
      filtered="$(printf '%s\n' "${filtered}" | grep -v "^${other_prefix}" || true)"
    fi
  done
  [[ -n "${filtered}" ]]
}
