#!/usr/bin/env bash
#
# Gemeinsame Helfer fuer die festgelegten Debian-Pakete (Caddy, Tailscale).
#
# Die Pins kommen aus manifest.packages.<name> im Bundle (je Architektur aus
# scripts/build/packages.json). Die .deb-Dateien liegen unter EN_PACKAGES_DIR
# und werden gegen die Pruefsumme aus dem Manifest geprueft, bevor sie
# installiert werden. Systempfade tragen EN_ROOT als Praefix, Befehle mit
# Rechten laufen ueber "${SUDO[@]}", damit alles ohne Root testbar bleibt.
#
# Die Funktionen sind fuer Schritte gedacht, die bereits step.sh sourcen.
# ACHTUNG: pkg_file beendet bei falscher Pruefsumme den Prozess ueber
# step_fail. Der Aufruf darf deshalb nicht in einer Befehlsersetzung stehen,
# deren Fehlerausgabe verloren ginge - sonst fehlt der Marker. Der Pfad kommt
# deshalb nicht ueber die Ausgabe, sondern in PKG_FILE_PATH:
#   pkg_file name || ...; deb="${PKG_FILE_PATH}"

PACKAGES_LIB_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=scripts/bootstrap/lib/step.sh
source "${PACKAGES_LIB_DIR}/step.sh"

EN_PACKAGES_DIR="${EN_PACKAGES_DIR:-${EN_STATE_DIR}/packages}"

# pkg_pin <name> <feld>: ein Feld aus manifest.packages.<name>, leer ohne
# Manifest oder ohne Eintrag.
pkg_pin() {
  [[ -f "${EN_BUNDLE_DIR}/manifest.json" ]] || return 0
  python3 - "${EN_BUNDLE_DIR}/manifest.json" "$1" "$2" 2>/dev/null <<'PY' || true
import json, sys
try:
    packages = json.load(open(sys.argv[1], encoding="utf-8")).get("packages") or {}
except (OSError, ValueError):
    sys.exit(0)
entry = packages.get(sys.argv[2])
value = entry.get(sys.argv[3]) if isinstance(entry, dict) else None
print("" if value is None else value)
PY
}

# pkg_file <name>: setzt PKG_FILE_PATH auf die .deb, wenn sie zur Festlegung
# passt. Fehlt sie oder fehlt die Festlegung, Rueckgabe 1 (PKG_FILE_PATH leer).
# Liegt sie mit falscher Pruefsumme vor, ist das ein Fehler des Bundles oder
# der Datei: step_fail.
pkg_file() {
  local file sha path actual
  PKG_FILE_PATH=""
  file="$(pkg_pin "$1" file)"
  sha="$(pkg_pin "$1" sha256)"
  [[ -n "${file}" && -n "${sha}" ]] || return 1
  path="${EN_PACKAGES_DIR}/${file}"
  [[ -f "${path}" ]] || return 1
  actual="$(sha256sum "${path}" | cut -d' ' -f1)"
  if [[ "${actual}" != "${sha}" ]]; then
    step_fail PACKAGE_FILE_INVALID
  fi
  # shellcheck disable=SC2034  # gelesen vom Aufrufer, siehe Kopfkommentar
  PKG_FILE_PATH="${path}"
}

# pkg_installed_version <name>: installierte Version, leer wenn nicht installiert.
pkg_installed_version() {
  dpkg-query -W -f='${Version}' "$1" 2>/dev/null || true
}

# pkg_install_deb <pfad>: installiert eine geprueft lokale .deb. Kein
# apt-get update - die Datei kommt aus dem Bundle, nicht aus einem Index.
# DEBIAN_FRONTEND steht hinter sudo, env_reset verwirft es sonst.
pkg_install_deb() {
  "${SUDO[@]}" env DEBIAN_FRONTEND=noninteractive \
    apt-get install -y --no-install-recommends -o Dpkg::Options::=--force-confold "$1"
}

# pkg_setup_repo <name>: Keyring und apt-Quelle des Herstellers anlegen und
# einen etwaigen hold freigeben, damit ein spaeteres apt upgrade greift.
pkg_setup_repo() {
  local name="$1" src keyring list line codename

  case "${name}" in
    tailscale)
      codename="$(grep -E '^VERSION_CODENAME=' "${EN_ROOT}/etc/os-release" 2>/dev/null \
        | head -n 1 | cut -d= -f2 | tr -d '"' || true)"
      [[ -n "${codename}" ]] || step_fail APT_FAILED
      line="deb [signed-by=/usr/share/keyrings/tailscale-archive-keyring.gpg] https://pkgs.tailscale.com/stable/debian ${codename} main"
      ;;
    caddy)
      line="deb [signed-by=/usr/share/keyrings/caddy-archive-keyring.gpg] https://dl.cloudsmith.io/public/caddy/stable/deb/debian any-version main"
      ;;
    *)
      step_log "Unbekannte Paketquelle: ${name}"
      return 1
      ;;
  esac

  src="${EN_BUNDLE_DIR}/bootstrap/keyrings/${name}.gpg"
  keyring="${EN_ROOT}/usr/share/keyrings/${name}-archive-keyring.gpg"
  list="${EN_ROOT}/etc/apt/sources.list.d/${name}.list"
  [[ -f "${src}" ]] || step_fail APT_FAILED

  "${SUDO[@]}" mkdir -p "${EN_ROOT}/usr/share/keyrings" "${EN_ROOT}/etc/apt/sources.list.d"
  "${SUDO[@]}" install -m 0644 "${src}" "${keyring}"
  printf '%s\n' "${line}" | "${SUDO[@]}" tee "${list}" >/dev/null
  # Ein Hold aus einem frueheren Lauf wuerde das Upgrade auf die Pin-Version
  # blockieren. Fehlt das Paket, meldet apt-mark nur, deshalb tolerant.
  "${SUDO[@]}" apt-mark unhold "${name}" >/dev/null 2>&1 || true
}

# pkg_hold <name>: haelt das Paket auf dem installierten Stand fest, damit
# ein apt upgrade es nicht ueber den Pin hinaus verschiebt.
pkg_hold() {
  "${SUDO[@]}" apt-mark hold "$1" >/dev/null
}
