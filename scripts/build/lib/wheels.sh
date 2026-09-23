#!/usr/bin/env bash
#
# Wheel-Beschaffung und Architektur-Tabelle fuer den Bundle-Bau (E4, E11).
#
# Die Architektur-Tabelle wohnt hier, obwohl arch_go_env nach make_bundle.sh
# klingt: der schwierige Teil sind die Platform- und ABI-Tags, und die
# gehoeren zu den Wheels. Eine zweite Datei nur fuer GOARCH waere eine
# zweite Stelle, an der eine neue Zielarchitektur nachzutragen waere.

# Drittanbieter-Abhaengigkeiten aller Dienste. paho-mqtt braucht jeder,
# der Rest je nach Bruecke (INSTALLATION.md 3.2).
THIRDPARTY_PACKAGES=(
  "paho-mqtt>=2.0"
  apsystems-ez1
  tinytuya
  requests
)

read -r -a WHEELS_PIP <<< "${EN_PIP:-python3 -m pip}"

arch_unknown() {
  echo "Unbekannte Zielarchitektur: $1 (bekannt: armv6, arm64, amd64)" >&2
  return 1
}

arch_platform_tags() {
  case "$1" in
    armv6) printf 'linux_armv6l\n' ;;
    arm64) printf 'manylinux2014_aarch64\nlinux_aarch64\n' ;;
    amd64) printf 'manylinux2014_x86_64\nlinux_x86_64\n' ;;
    *) arch_unknown "$1" ;;
  esac
}

arch_uname_machines() {
  case "$1" in
    # Ein 32-Bit-Raspberry-Pi-OS meldet je nach Modell armv6l oder armv7l;
    # das armv6-Bundle laeuft auf beiden.
    armv6) printf 'armv6l\narmv7l\n' ;;
    arm64) printf 'aarch64\narm64\n' ;;
    amd64) printf 'x86_64\n' ;;
    *) arch_unknown "$1" ;;
  esac
}

arch_go_env() {
  case "$1" in
    armv6) printf 'GOARCH=arm\nGOARM=6\n' ;;
    arm64) printf 'GOARCH=arm64\n' ;;
    amd64) printf 'GOARCH=amd64\n' ;;
    *) arch_unknown "$1" ;;
  esac
}

# fetch_thirdparty_wheels <ziel> <arch> <python-minor> <abi>
#
# --platform erzwingt --only-binary: pip darf auf dem Bau-Rechner nichts
# uebersetzen, was auf dem Node laufen soll. piwheels ist der Erstindex,
# PyPI der Zweitindex - fuer amd64 liefert piwheels nichts, das faengt der
# Zweitindex ab.
fetch_thirdparty_wheels() {
  local target="$1" arch="$2" minor="$3" abi="$4"
  local tags=() tag args=()

  while IFS= read -r tag; do
    tags+=("${tag}")
  done < <(arch_platform_tags "${arch}") || return 1
  [[ "${#tags[@]}" -gt 0 ]] || return 1

  mkdir -p "${target}"
  for tag in "${tags[@]}"; do
    args+=(--platform "${tag}")
  done

  # pip wertet Umgebungsmarker (python_version < "3.13" ...) mit dem Python
  # des Bau-Rechners aus, nicht mit dem Ziel. Nach dem Download prueft
  # missing_requirements.py die Abhaengigkeiten aller Wheels gegen die
  # Zielmarker und holt nach, was fehlt - bis nichts mehr fehlt.
  local machine missing=()
  machine="$(arch_uname_machines "${arch}" | head -n 1)"
  for _ in 1 2 3 4 5 6; do
    if ! "${WHEELS_PIP[@]}" download \
        --only-binary=:all: \
        --index-url https://www.piwheels.org/simple \
        --extra-index-url https://pypi.org/simple \
        "${args[@]}" \
        --python-version "${minor}" \
        --implementation cp \
        --abi "${abi}" \
        -d "${target}" \
        "${THIRDPARTY_PACKAGES[@]}" "${missing[@]}"; then
      echo "Fuer mindestens eines der Pakete (${THIRDPARTY_PACKAGES[*]} ${missing[*]}) gibt es" >&2
      echo "kein Wheel fuer ${arch}/${abi}. Der Bundle-Bau bricht ab - ein Node" >&2
      echo "darf Abhaengigkeiten nicht selbst aufloesen (E11)." >&2
      return 1
    fi
    missing=()
    while IFS= read -r req; do
      if [[ -n "${req}" ]]; then
        missing+=("${req}")
      fi
    done < <(python3 "$(dirname "${BASH_SOURCE[0]}")/missing_requirements.py" \
               "${target}" "${minor}" "${machine}") || return 1
    if [[ "${#missing[@]}" -eq 0 ]]; then
      return 0
    fi
    echo "Zusaetzlich fuer ${minor}/${machine} noetig: ${missing[*]}" >&2
  done
  echo "Die Abhaengigkeiten der Wheels lassen sich nicht schliessen (fehlt: ${missing[*]})." >&2
  return 1
}


# build_local_wheels <ziel>
#
# Baut die beiden eigenen Pakete. Ein im dist/-Verzeichnis liegendes Rad
# derselben Version wird wiederverwendet.
build_local_wheels() {
  local target="$1"
  local repo_root lib dist version cached built
  repo_root="$(git rev-parse --show-toplevel)"
  mkdir -p "${target}"

  for lib in energy_node_common battery_soc_core; do
    dist="${repo_root}/libs/${lib}/dist"
    version="$(tr -d '[:space:]' < "${repo_root}/libs/${lib}/VERSION")"
    version="${version#v}"

    # dist/ may not exist yet on a checkout that has never built a wheel
    # locally before -- find on a missing directory fails, and pipefail
    # would otherwise carry that failure into the assignment below and kill
    # the script under set -e without printing anything (the real error is
    # thrown away by 2>/dev/null).
    mkdir -p "${dist}"
    cached="$(find "${dist}" -maxdepth 1 -name "${lib}-${version}-*.whl" 2>/dev/null | head -n 1)"
    if [[ -n "${cached}" ]]; then
      echo "Wiederverwendet: $(basename "${cached}")"
      cp "${cached}" "${target}/"
      continue
    fi

    echo "Baue ${lib} ${version}"
    mkdir -p "${dist}"
    rm -f "${dist}"/*.whl
    "${WHEELS_PIP[@]}" wheel "${repo_root}/libs/${lib}" --no-deps --wheel-dir "${dist}" >/dev/null
    built="$(find "${dist}" -maxdepth 1 -name '*.whl' | head -n 1)"
    if [[ -z "${built}" ]]; then
      echo "Kein Wheel fuer ${lib} entstanden." >&2
      return 1
    fi
    case "$(basename "${built}")" in
      "${lib}-${version}-"*) ;;
      *) echo "Gebautes Wheel $(basename "${built}") passt nicht zu VERSION ${version}." >&2
         return 1 ;;
    esac
    cp "${built}" "${target}/"
  done
}
