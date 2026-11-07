#!/usr/bin/env bash
#
# Schritt 70: Caddy als HTTPS-Vorsatz (INSTALLATION.md 8) aus dem gepinnten Paket.
#
# Das .deb reist im Bundle mit (manifest.packages.caddy, Pruefsumme vor dem
# Installieren geprueft). Es wird nichts vom Netz geladen. Auf amd64 und arm64
# (update=apt) folgt der Node danach dem Caddy-Repo; auf armv6 bleibt die
# Fassung festgehalten (hold).
# Eingaben fuer den Fingerabdruck (scripts/build/lib/manifest.sh): solange
# sie gleich bleiben, ueberspringt ein Update diesen Schritt.
# step-inputs: bootstrap/lib/step.sh bootstrap/lib/packages.sh bootstrap/keyrings/ dashboard/Caddyfile manifest:packages.caddy
set -euo pipefail
# shellcheck source=scripts/bootstrap/lib/step.sh
source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/lib/step.sh"
# shellcheck source=scripts/bootstrap/lib/packages.sh
source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/lib/packages.sh"

# Die Paketvorgabe der Konfiguration, wie dpkg sie fuehrt (Systempfad, ohne
# EN_ROOT). Leer, wenn das Paket dpkg nicht bekannt ist.
caddy_conffile_md5() {
  dpkg-query -W -f='${Conffiles}' caddy 2>/dev/null \
    | awk '$1 == "/etc/caddy/Caddyfile" { print $2 }' || true
}

step_begin 70
if ! step_selected 70; then
  step_skip "nicht ausgewaehlt"
  exit 0
fi
if step_done 70; then
  step_skip "bereits erledigt"
  exit 0
fi

template="${EN_BUNDLE_DIR}/dashboard/Caddyfile"
caddy_bin="${EN_ROOT}/usr/bin/caddy"
caddyfile="${EN_ROOT}/etc/caddy/Caddyfile"

# Ohne Vorlage und ohne vorhandene Konfiguration hat der Node nichts, woraus
# er eine Caddyfile machen koennte.
if [[ ! -f "${template}" && ! -f "${caddyfile}" ]]; then
  step_fail CADDY_BINARY_MISSING
fi

pin_version="$(pkg_pin caddy version)"
update_mode="$(pkg_pin caddy update)"
have_deb=false
if pkg_file caddy; then
  have_deb=true
fi
deb="${PKG_FILE_PATH}"

# Vorhanden ist Caddy, wenn dpkg es kennt oder ein Binary unter /usr/bin liegt.
# Ein Binary ohne dpkg-Eintrag hat seine Version in `caddy version`
# (v2.11.9 h1:... -> 2.11.9).
dpkg_version="$(pkg_installed_version caddy)"
present=false
managed=false
installed_version=""
if [[ -n "${dpkg_version}" ]]; then
  present=true
  managed=true
  installed_version="${dpkg_version}"
elif [[ -x "${caddy_bin}" ]]; then
  present=true
  installed_version="$("${caddy_bin}" version 2>/dev/null | head -n 1 | cut -d' ' -f1 || true)"
fi

state=absent
if [[ "${present}" == true ]]; then
  if [[ -z "${pin_version}" ]] || step_version_ge "${installed_version}" "${pin_version}"; then
    state=current
  else
    state=older
  fi
fi

install=false
case "${state}" in
  absent)
    [[ "${have_deb}" == true ]] || step_fail CADDY_PACKAGE_MISSING
    install=true
    ;;
  older)
    if [[ "${have_deb}" == false ]]; then
      if [[ "${update_mode}" == apt ]]; then
        step_log "Caddy ${installed_version} ist älter als ${pin_version}. Update über apt."
      else
        step_log "Caddy ${installed_version} ist älter als ${pin_version}. Update beim nächsten Installer-Lauf."
      fi
      step_ok
      exit 0
    fi
    install=true
    ;;
  current)
    if [[ "${managed}" == true ]]; then
      step_log "Vorhanden ist Caddy ${installed_version}, die Festlegung ist ${pin_version:-leer}: die Installation bleibt unangetastet."
      if [[ "${update_mode}" == apt ]]; then
        pkg_setup_repo caddy
      fi
    else
      step_log "Caddy ${installed_version} ist nicht per dpkg verwaltet: die Installation bleibt unangetastet."
    fi
    ;;
esac

if [[ "${install}" == true ]]; then
  if [[ "${managed}" == false && "${state}" == older ]]; then
    step_log "Das Caddy ${installed_version} ohne Paketverwaltung wird durch das Paket ersetzt."
  fi
  # Ein hold aus einem frueheren Lauf (armv6) verweigert die Aenderung durch
  # apt. Freigeben, installieren, danach wieder festhalten.
  pkg_unhold caddy
  pkg_install_deb "${deb}" || step_fail APT_INSTALL_FAILED
  if [[ "${update_mode}" == apt ]]; then
    pkg_setup_repo caddy
  else
    pkg_hold caddy || step_fail APT_FAILED
  fi
fi

# Die Caddyfile gehoert dem Betreiber. Nur was unveraendert der Paketvorgabe
# entspricht, wird nach der Installation durch die Bundle-Vorlage ersetzt.
"${SUDO[@]}" mkdir -p "${EN_ROOT}/etc/caddy"
if [[ ! -f "${caddyfile}" ]]; then
  "${SUDO[@]}" install -m 0644 "${template}" "${caddyfile}"
  step_log "Caddyfile aus dem Bundle installiert."
else
  package_md5="$(caddy_conffile_md5)"
  if [[ "${install}" == true && -n "${package_md5}" \
        && "${package_md5}" == "$(md5sum "${caddyfile}" | cut -d' ' -f1)" ]]; then
    "${SUDO[@]}" install -m 0644 "${template}" "${caddyfile}"
    step_log "Paketvorgabe der Caddyfile durch die Vorlage aus dem Bundle ersetzt."
  elif [[ -f "${template}" ]] && ! cmp -s "${template}" "${caddyfile}"; then
    step_log "Die vorhandene ${caddyfile} weicht ab und bleibt unangetastet."
  fi
fi

"${SUDO[@]}" caddy validate --config "${caddyfile}" >/dev/null 2>&1 \
  || step_fail CADDY_CONFIG_INVALID

"${SUDO[@]}" systemctl enable --now caddy || step_fail CADDY_START_FAILED
"${SUDO[@]}" systemctl reload caddy || true

step_log "Caddy aktiv. Das Zertifikat stammt aus Caddys lokaler CA ('tls internal')"
step_log "und ist nicht systemweit vertrauenswuerdig - Browser warnen, bis sie ihr vertraut."
step_ok
