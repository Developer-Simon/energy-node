#!/usr/bin/env bash
#
# Shared helpers for the git-hooks/ scripts. Sourced, not executed directly.

COMPONENTS=(
  "dashboard/:dashboard/VERSION"
  "src/:src/VERSION"
  "src/energy_node_common/:src/energy_node_common/VERSION"
  "src/battery_soc_core/:src/battery_soc_core/VERSION"
  "integrations/homeassistant/:integrations/homeassistant/custom_components/battery_soc/manifest.json"
)

current_branch() {
  git symbolic-ref --short -q HEAD || true
}

sanitize_branch() {
  local name="$1"
  printf '%s' "${name}" | sed -E 's#[^A-Za-z0-9_.-]+#-#g; s#-+#-#g; s#^-|-$##g'
}

# component_touched prueft, ob eine Datei-Liste (staged/changed, eine pro
# Zeile) eine Komponente betrifft. Dateien einer verschachtelten Komponente
# (z.B. src/energy_node_common/ unterhalb von src/) zaehlen dabei NICHT als
# den umgebenden Praefix betreffend - ein Commit, der ausschliesslich das
# gemeinsame Modul aendert, soll nur dessen eigenes VERSION bumpen/taggen,
# nicht auch noch src/VERSION.
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
  for other in "${COMPONENTS[@]}"; do
    other_prefix="${other%%:*}"
    if [[ "${other_prefix}" != "${dir_prefix}" && "${other_prefix}" == "${dir_prefix}"* ]]; then
      filtered="$(printf '%s\n' "${filtered}" | grep -v "^${other_prefix}" || true)"
    fi
  done
  [[ -n "${filtered}" ]]
}
