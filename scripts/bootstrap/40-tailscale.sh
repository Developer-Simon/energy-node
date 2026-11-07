#!/usr/bin/env bash
#
# Schritt 40: Tailscale aus dem gepinnten Debian-Paket (INSTALLATION.md §3.1).
#
# Das .deb reist im Bundle mit (manifest.packages.tailscale, Pruefsumme vor
# dem Installieren geprueft). Es wird nichts vom Netz geladen - ein frisch
# aufgesetzter Node hat oft noch keinen brauchbaren Egress. Auf amd64 und
# arm64 (update=apt) folgt der Node danach dem Hersteller-Repo; auf armv6
# bleibt die Fassung festgehalten (hold).
# Eingaben fuer den Fingerabdruck (scripts/build/lib/manifest.sh): solange
# sie gleich bleiben, ueberspringt ein Update diesen Schritt.
# step-inputs: bootstrap/lib/step.sh bootstrap/lib/packages.sh bootstrap/keyrings/ manifest:packages.tailscale
set -euo pipefail
# shellcheck source=scripts/bootstrap/lib/step.sh
source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/lib/step.sh"
# shellcheck source=scripts/bootstrap/lib/packages.sh
source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/lib/packages.sh"

FORBIDDEN_FLAG='--tun=userspace-networking'

# Nur eine wirksame Zeile zaehlt: Debians Standarddatei und die Vorlage im
# Tarball fuehren das Beispiel auskommentiert (#FLAGS="--tun=...") auf.
has_forbidden_flag() {
  sed 's/#.*//' "$1" | grep -q -- "${FORBIDDEN_FLAG}"
}

# ts_version_ge <a> <b>: a gleich oder neuer als b (dpkg-Regeln). Eine leere
# Version ist unbekannt und nie neuer.
ts_version_ge() {
  [[ -n "$1" ]] || return 1
  dpkg --compare-versions "$1" ge "$2"
}

step_begin 40
if ! step_selected 40; then
  step_skip "nicht ausgewaehlt"
  exit 0
fi
if step_done 40; then
  step_skip "bereits erledigt"
  exit 0
fi

defaults="${EN_ROOT}/etc/default/tailscaled"
unit="${EN_ROOT}/etc/systemd/system/tailscaled.service"
bin_cli="${EN_ROOT}/usr/sbin/tailscale"
bin_daemon="${EN_ROOT}/usr/sbin/tailscaled"

# Vor jeder Aenderung: eine vorhandene Datei mit dem verbotenen Schalter ist
# ein Abbruchgrund, kein Reparaturfall. Der Schalter verhindert, dass
# tailscaled tailscale0 anlegt - der Node kann dann zu keinem Peer routen.
if [[ -f "${defaults}" ]] && has_forbidden_flag "${defaults}"; then
  step_log "In ${defaults} steht ${FORBIDDEN_FLAG}. Bitte entfernen (INSTALLATION.md 3.1)."
  step_fail TAILSCALE_FLAG_INVALID
fi

pin_version="$(pkg_pin tailscale version)"
update_mode="$(pkg_pin tailscale update)"
have_deb=false
if pkg_file tailscale; then
  have_deb=true
fi
deb="${PKG_FILE_PATH}"

# Vorhanden ist Tailscale, wenn dpkg es kennt oder ein Tarball-Binary liegt.
# Ein Tarball-Binary ist nicht per dpkg verwaltet; seine Version steht darin
# (1.98.9-t4fb758c39-... -> 1.98.9).
dpkg_version="$(pkg_installed_version tailscale)"
present=false
managed=false
installed_version=""
if [[ -n "${dpkg_version}" ]]; then
  present=true
  managed=true
  installed_version="${dpkg_version}"
elif [[ -x "${bin_daemon}" ]]; then
  present=true
  installed_version="$("${bin_daemon}" --version 2>/dev/null | head -n 1 || true)"
  installed_version="${installed_version%%-*}"
fi

state=absent
if [[ "${present}" == true ]]; then
  if [[ -z "${pin_version}" ]] || ts_version_ge "${installed_version}" "${pin_version}"; then
    state=current
  else
    state=older
  fi
fi

install=false
case "${state}" in
  absent)
    [[ "${have_deb}" == true ]] || step_fail TAILSCALE_PACKAGE_MISSING
    install=true
    ;;
  older)
    if [[ "${have_deb}" == false ]]; then
      if [[ "${update_mode}" == apt ]]; then
        step_log "Tailscale ${installed_version} ist älter als ${pin_version}. Update über apt."
      else
        step_log "Tailscale ${installed_version} ist älter als ${pin_version}. Update beim nächsten Installer-Lauf."
      fi
      step_ok
      exit 0
    fi
    install=true
    ;;
  current)
    if [[ "${managed}" == true ]]; then
      step_log "Vorhanden ist Tailscale ${installed_version}, die Festlegung ist ${pin_version:-leer}: die Installation bleibt unangetastet."
      if [[ "${update_mode}" == apt ]]; then
        pkg_setup_repo tailscale
      fi
    else
      step_log "Tailscale ${installed_version} ist nicht per dpkg verwaltet: die Installation bleibt unangetastet."
    fi
    ;;
esac

if [[ "${install}" == true ]]; then
  # Ein Tarball-Node wird umgestellt: die Dateien des Tarballs weg, sonst
  # verdeckt die alte Unit das Paket. /etc/default/tailscaled bleibt, das
  # Paket installiert mit --force-confold.
  if [[ "${managed}" == false ]]; then
    if [[ "${state}" == older ]]; then
      step_log "Tarball-Installation ${installed_version} wird durch das Paket ersetzt."
    fi
    "${SUDO[@]}" rm -f "${bin_cli}" "${unit}"
  fi
  # Ein hold aus einem frueheren Lauf (armv6) verweigert die Aenderung durch
  # apt. Freigeben, installieren, danach wieder festhalten.
  pkg_unhold tailscale
  pkg_install_deb "${deb}" || step_fail TAILSCALE_INSTALL_FAILED
  "${SUDO[@]}" systemctl daemon-reload
  # Gegenprobe nach der Installation: auch die Paketvorgabe darf den Schalter
  # nicht enthalten.
  if [[ -f "${defaults}" ]] && has_forbidden_flag "${defaults}"; then
    step_log "Die installierte ${defaults} enthaelt ${FORBIDDEN_FLAG}."
    step_fail TAILSCALE_FLAG_INVALID
  fi
  if [[ "${update_mode}" == apt ]]; then
    pkg_setup_repo tailscale
  else
    pkg_hold tailscale || step_fail TAILSCALE_INSTALL_FAILED
  fi
fi
"${SUDO[@]}" systemctl enable --now tailscaled || step_fail TAILSCALE_INSTALL_FAILED

if "${SUDO[@]}" tailscale status >/dev/null 2>&1; then
  step_log "Node ist angemeldet."
  step_ok
  exit 0
fi

# tailscale up blockiert bis zum Browser-Login. Im Hintergrund anstossen, die
# Login-Adresse als Menschentext melden - und KEINEN Stempel setzen, sonst
# gaelte ein nie angemeldeter Node beim naechsten Lauf als fertig.
mkdir -p "${EN_STATE_DIR}"
login_log="${EN_STATE_DIR}/tailscale-up.log"
: > "${login_log}"
"${SUDO[@]}" tailscale up >>"${login_log}" 2>&1 &
url=""
for _ in $(seq 1 "${EN_TAILSCALE_LOGIN_WAIT:-15}"); do
  url="$(grep -o 'https://login\.tailscale\.com/[^[:space:]]*' "${login_log}" | head -n 1 || true)"
  [[ -n "${url}" ]] && break
  sleep 1
done
if [[ -n "${url}" ]]; then
  step_log "Anmeldung noetig. Diese Adresse im Browser oeffnen: ${url}"
else
  step_log "Anmeldung noetig. Die Login-Adresse steht in ${login_log}."
fi
step_skip "login ausstehend"
