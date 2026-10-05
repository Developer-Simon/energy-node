#!/usr/bin/env bash
#
# Schritt 15: Systempakete aktualisieren (optional, Vorgabe an).
#
# Prueft mit apt-get update, welche installierten Pakete neuere Versionen
# haben, und spielt sie mit apt-get upgrade ein. upgrade entfernt nie ein
# Paket und installiert keine neuen Abhaengigkeiten - zurueckgehaltene
# Pakete bleiben stehen. Auf einem unbeaufsichtigten Node ist das gewollt.
#
# Abgewaehlt prueft der Schritt nur, nennt die Zahl im Log und laesst den
# Lauf auch ohne Netz weiterlaufen. Er hinterlaesst dann keinen Stempel.
#
# Kommt ein neuer Kernel oder neue Firmware, legt der Schritt Debians
# Markierung /run/reboot-required an (tmpfs, weg nach dem Neustart).
# diagnose.sh meldet sie, der Marker-Zusatz "neustart noetig" bringt sie
# auf den Ergebnis-Bildschirm. Der Schritt startet nie selbst neu.
set -euo pipefail
# shellcheck source=scripts/bootstrap/lib/step.sh
source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/lib/step.sh"

# Lock-Timeout: ein laufendes apt (von Hand, unattended-upgrades) soll den
# Schritt warten lassen statt ihn abzubrechen. confdef/confold: geaenderte
# Konfigurationsdateien (mosquitto.conf) bleiben, ohne Rueckfrage.
APT_OPTS=(
  -o DPkg::Lock::Timeout=600
  -o Dpkg::Options::=--force-confdef
  -o Dpkg::Options::=--force-confold
)
# Pakete, nach deren Update erst ein Neustart wirkt.
REBOOT_PACKAGES='^(linux-image-|raspberrypi-kernel|raspberrypi-bootloader|raspi-firmware)'
REBOOT_FLAG="${EN_ROOT}/run/reboot-required"

# DEBIAN_FRONTEND muss hinter sudo stehen - env_reset verwirft es sonst.
apt_get() {
  "${SUDO[@]}" env DEBIAN_FRONTEND=noninteractive apt-get "${APT_OPTS[@]}" "$@"
}

# Namen der Pakete, die apt-get upgrade jetzt aktualisieren wuerde.
pending_packages() {
  apt_get -s upgrade 2>/dev/null | sed -n 's/^Inst \([^ ]*\) .*/\1/p'
}

step_begin 15

if ! step_selected 15; then
  if apt_get -qq update; then
    count="$(pending_packages | grep -c . || true)"
    step_log "Systempakete geprueft: ${count} Pakete koennten aktualisiert werden."
  else
    step_log "apt-get update ist fehlgeschlagen, die Pruefung entfaellt."
  fi
  step_skip "nicht ausgewaehlt"
  exit 0
fi
if step_done 15; then
  step_skip "bereits erledigt"
  exit 0
fi

apt_get -qq update || step_fail APT_UPDATE_FAILED
mapfile -t packages < <(pending_packages)

if [[ "${#packages[@]}" -eq 0 ]]; then
  step_log "Alle Systempakete sind aktuell."
else
  step_log "Aktualisiere ${#packages[@]} Pakete: ${packages[*]}"
  apt_get -y upgrade || step_fail APT_UPGRADE_FAILED
  reboot_packages="$(printf '%s\n' "${packages[@]}" | grep -E "${REBOOT_PACKAGES}" || true)"
  if [[ -n "${reboot_packages}" ]]; then
    "${SUDO[@]}" mkdir -p "$(dirname "${REBOOT_FLAG}")"
    printf '%s\n' "${reboot_packages}" | "${SUDO[@]}" tee -a "${REBOOT_FLAG}.pkgs" >/dev/null
    "${SUDO[@]}" touch "${REBOOT_FLAG}"
  fi
fi

if [[ -f "${REBOOT_FLAG}" ]]; then
  step_log "Ein Neustart des Node ist noetig, damit alle Updates wirken."
  step_ok "neustart noetig"
else
  step_ok
fi
