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

  if ! "${WHEELS_PIP[@]}" download \
      --only-binary=:all: \
      --index-url https://www.piwheels.org/simple \
      --extra-index-url https://pypi.org/simple \
      "${args[@]}" \
      --python-version "${minor}" \
      --implementation cp \
      --abi "${abi}" \
      -d "${target}" \
      "${THIRDPARTY_PACKAGES[@]}"; then
    echo "Fuer mindestens eines der Pakete (${THIRDPARTY_PACKAGES[*]}) gibt es" >&2
    echo "kein Wheel fuer ${arch}/${abi}. Der Bundle-Bau bricht ab - ein Node" >&2
    echo "darf Abhaengigkeiten nicht selbst aufloesen (E11)." >&2
    return 1
  fi
}

# build_local_wheels <ziel>
#
# Baut die beiden eigenen Pakete. Ein im dist/-Verzeichnis liegendes Rad
# derselben Version wird wiederverwendet - dasselbe Verfahren, das
# deploy_src_to_remote.sh schon benutzt, nur ohne Gegenstelle.
build_local_wheels() {
  local target="$1"
  local repo_root lib dist version cached built
  repo_root="$(git rev-parse --show-toplevel)"
  mkdir -p "${target}"

  for lib in energy_node_common battery_soc_core; do
    dist="${repo_root}/libs/${lib}/dist"
    version="$(tr -d '[:space:]' < "${repo_root}/libs/${lib}/VERSION")"
    version="${version#v}"

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
