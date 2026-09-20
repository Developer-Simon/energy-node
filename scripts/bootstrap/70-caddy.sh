#!/usr/bin/env bash
#
# Schritt 70: Caddy als HTTPS-Vorsatz (INSTALLATION.md 8).
#
# Das Binary kommt aus dem getrennten Beipack (E13) und liegt entpackt unter
# ${EN_BUNDLE_DIR}/caddy/caddy. Auf ARMv6 ist das ausdruecklich der Build vom
# Caddy-Projekt, nicht das Distributionspaket.
set -euo pipefail
# shellcheck source=scripts/bootstrap/lib/step.sh
source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/lib/step.sh"

step_begin 70
if ! step_selected 70; then
  step_skip "nicht ausgewaehlt"
  exit 0
fi
if step_done 70; then
  step_skip "bereits erledigt"
  exit 0
fi

binary="${EN_BUNDLE_DIR}/caddy/caddy"
template="${EN_BUNDLE_DIR}/dashboard/Caddyfile"
[[ -f "${binary}" && -f "${template}" ]] || step_fail CADDY_BINARY_MISSING

# Ein vorhandenes Caddy-Binary bleibt stehen, wenn es dem Paketmanager gehoert
# (ein spaeteres apt upgrade tauschte die Datei ohnehin wieder aus) oder
# nicht aelter ist als das im Bundle. Ersetzt wird nur ein eigenes, aelteres.
caddy_bin="${EN_ROOT}/usr/bin/caddy"
install_binary=true
if [[ -x "${caddy_bin}" ]]; then
  installed_version="$("${caddy_bin}" version 2>/dev/null | head -n 1 | cut -d' ' -f1 || true)"
  bundled_version="$("${binary}" version 2>/dev/null | head -n 1 | cut -d' ' -f1 || true)"
  if dpkg -S "${caddy_bin}" >/dev/null 2>&1; then
    install_binary=false
    step_log "Das vorhandene Caddy ${installed_version} gehoert dem Paketmanager und bleibt unangetastet."
  elif step_version_ge "${installed_version}" "${bundled_version}"; then
    install_binary=false
    step_log "Das vorhandene Caddy ${installed_version} ist nicht aelter als das im Bundle (${bundled_version}) und bleibt unangetastet."
  fi
fi

"${SUDO[@]}" mkdir -p "${EN_ROOT}/usr/bin" "${EN_ROOT}/etc/caddy"
if [[ "${install_binary}" == true ]]; then
  "${SUDO[@]}" install -m 0755 "${binary}" "${caddy_bin}"
fi

# Eine vorhandene Caddyfile gehoert dem Betreiber: INSTALLATION.md 8 sagt
# ausdruecklich, dass ein Deploy die Caddy-Konfiguration nicht anfasst.
caddyfile="${EN_ROOT}/etc/caddy/Caddyfile"
if [[ -f "${caddyfile}" ]]; then
  if ! cmp -s "${template}" "${caddyfile}"; then
    step_log "Die vorhandene ${caddyfile} weicht ab und bleibt unangetastet."
  fi
else
  "${SUDO[@]}" install -m 0644 "${template}" "${caddyfile}"
  step_log "Caddyfile aus dem Bundle installiert."
fi

"${SUDO[@]}" caddy validate --config "${caddyfile}" >/dev/null 2>&1 \
  || step_fail CADDY_CONFIG_INVALID

"${SUDO[@]}" systemctl enable --now caddy || step_fail CADDY_START_FAILED
"${SUDO[@]}" systemctl reload caddy || true

step_log "Caddy aktiv. Das Zertifikat stammt aus Caddys lokaler CA ('tls internal')"
step_log "und ist nicht systemweit vertrauenswuerdig - Browser warnen, bis sie ihr vertraut."
step_ok
