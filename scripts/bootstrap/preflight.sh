#!/usr/bin/env bash
#
# Vorpruefung: meldet die Tatsachen des Node als eine Zeile JSON und
# veraendert nichts. Kein Schritt - kein ##STEP-Marker, kein Stempel.
#
# Diese Datei reist allein, vor dem Bundle: der Installer laedt sie per SFTP
# hoch, damit eine falsche Architektur auffaellt, bevor 50 MB ueber eine
# SFTP-Verbindung zum Pi 1 gehen. Sie darf deshalb nichts aus dem Bundle
# brauchen.
#
#   EN_ROOT       Praefix vor allen Systempfaden (Vorgabe leer, Tests setzen es)
#   EN_STATE_DIR  Stempelverzeichnis (Vorgabe /var/lib/energy-node-installer)
set -euo pipefail

EN_ROOT="${EN_ROOT:-}"
EN_STATE_DIR="${EN_STATE_DIR:-/var/lib/energy-node-installer}"

os_id=""
os_version_id=""
os_pretty_name=""
if [[ -r "${EN_ROOT}/etc/os-release" ]]; then
  # shellcheck disable=SC1091
  os_id="$(sed -n 's/^ID=//p' "${EN_ROOT}/etc/os-release" | tr -d '"' | head -n 1)"
  os_version_id="$(sed -n 's/^VERSION_ID=//p' "${EN_ROOT}/etc/os-release" | tr -d '"' | head -n 1)"
  os_pretty_name="$(sed -n 's/^PRETTY_NAME=//p' "${EN_ROOT}/etc/os-release" | tr -d '"' | head -n 1)"
fi

arch="$(uname -m)"

python_version=""
python_abi=""
if command -v python3 >/dev/null 2>&1; then
  python_version="$(python3 -c 'import sys; print("%d.%d.%d" % sys.version_info[:3])' 2>/dev/null || true)"
  # Dieselbe Schreibweise wie `pip download --abi` und verify_bundle.sh: cp311.
  python_abi="$(python3 -c 'import sys; print("cp%d%d" % sys.version_info[:2])' 2>/dev/null || true)"
fi

# Freier Platz dort, wo Bundle und Wheels landen. Faellt das Verzeichnis noch
# nicht an, zaehlt sein naechster vorhandener Vorfahre.
probe="${EN_STATE_DIR}"
while [[ -n "${probe}" && ! -d "${probe}" ]]; do
  probe="$(dirname "${probe}")"
done
disk_line="$(df -Pm "${probe:-/}" 2>/dev/null | awk 'NR==2 {print $2" "$4}')"
disk_total_mb="${disk_line%% *}"
disk_free_mb="${disk_line##* }"
disk_total_mb="${disk_total_mb:-0}"
disk_free_mb="${disk_free_mb:-0}"

sudo_nopasswd=false
if sudo -n true >/dev/null 2>&1; then
  sudo_nopasswd=true
fi

# Netz: ein HEAD gegen den Debian-Spiegel, kurz abgeschnitten. Kein Ping -
# ICMP ist in vielen Netzen gefiltert und saegte eine funktionierende
# Installation faelschlich als offline ab.
internet=false
if command -v curl >/dev/null 2>&1; then
  if curl -sSf --max-time 5 -o /dev/null -I https://deb.debian.org/ >/dev/null 2>&1; then
    internet=true
  fi
elif command -v wget >/dev/null 2>&1; then
  if wget -q --spider --timeout=5 https://deb.debian.org/ >/dev/null 2>&1; then
    internet=true
  fi
fi

# Zeitzone: /etc/timezone (Debian), sonst das Ziel des localtime-Links.
timezone=""
if [[ -r "${EN_ROOT}/etc/timezone" ]]; then
  timezone="$(head -n 1 "${EN_ROOT}/etc/timezone" | tr -d '[:space:]')"
elif [[ -L "${EN_ROOT}/etc/localtime" ]]; then
  timezone="$(readlink "${EN_ROOT}/etc/localtime" | sed 's|.*/zoneinfo/||')"
fi

installed=false
installed_bundle_version=""
if [[ -d "${EN_STATE_DIR}/steps" ]] && compgen -G "${EN_STATE_DIR}/steps/*" >/dev/null; then
  installed=true
  for stamp in "${EN_STATE_DIR}"/steps/*; do
    [[ -f "${stamp}" ]] || continue
    version="$(sed -n 's/^bundle=//p' "${stamp}" | head -n 1)"
    if [[ -n "${version}" ]]; then
      installed_bundle_version="${version}"
    fi
  done
fi

OS_PRETTY_NAME="${os_pretty_name}" TIMEZONE="${timezone}" python3 - <<PY
import json, os
print(json.dumps({
    "os_id": "${os_id}",
    "os_version_id": "${os_version_id}",
    "os_pretty_name": os.environ["OS_PRETTY_NAME"],
    "arch": "${arch}",
    "python_abi": "${python_abi}",
    "python_version": "${python_version}",
    "disk_free_mb": int("${disk_free_mb}"),
    "disk_total_mb": int("${disk_total_mb}"),
    "sudo_nopasswd": ${sudo_nopasswd^},
    "internet": ${internet^},
    "installed": ${installed^},
    "installed_bundle_version": "${installed_bundle_version}",
    "timezone": os.environ["TIMEZONE"],
}, ensure_ascii=False))
PY
