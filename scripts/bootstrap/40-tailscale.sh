#!/usr/bin/env bash
#
# Schritt 40: Tailscale 1.62.0 aus dem Bundle (INSTALLATION.md §3.1).
#
# Der Tarball reist im Bundle mit, statt vom Node geladen zu werden - ein
# frisch aufgesetzter Node hat oft noch keinen brauchbaren Egress, und das
# Herunterladen gehoert laut E4 ohnehin auf den Bau-Rechner.
set -euo pipefail
# shellcheck source=scripts/bootstrap/lib/step.sh
source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/lib/step.sh"

FORBIDDEN_FLAG='--tun=userspace-networking'

# Nur eine wirksame Zeile zaehlt: Debians Standarddatei und die Vorlage im
# Tarball fuehren das Beispiel auskommentiert (#FLAGS="--tun=...") auf.
has_forbidden_flag() {
  sed 's/#.*//' "$1" | grep -q -- "${FORBIDDEN_FLAG}"
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

shopt -s nullglob
tarballs=("${EN_BUNDLE_DIR}"/tailscale/*.tgz)
shopt -u nullglob
[[ "${#tarballs[@]}" -eq 1 ]] || step_fail TAILSCALE_TARBALL_MISSING

defaults="${EN_ROOT}/etc/default/tailscaled"

# Vor jeder Aenderung: eine vorhandene Datei mit dem verbotenen Schalter ist
# ein Abbruchgrund, kein Reparaturfall. Der Schalter verhindert, dass
# tailscaled tailscale0 anlegt - der Node kann dann zu keinem Peer routen.
if [[ -f "${defaults}" ]] && has_forbidden_flag "${defaults}"; then
  step_log "In ${defaults} steht ${FORBIDDEN_FLAG}. Bitte entfernen (INSTALLATION.md 3.1)."
  step_fail TAILSCALE_FLAG_INVALID
fi

# Eine vorhandene Installation, die nicht aelter ist als die im Bundle, bleibt
# unangetastet. Der Tarball ist auf die letzte ARMv6-Fassung (1.62.0)
# festgelegt; ein Node, der per "tailscale update" laengst darueber liegt,
# wuerde sonst bei jedem neuen Bundle zurueckgesetzt - und "tailscale update"
# startet tailscaled neu und kappt damit die Verbindung, ueber die der
# Installer selbst laeuft. Die Bundle-Fassung steht im Tarballnamen.
bundled_version="$(basename "${tarballs[0]}" .tgz)"
bundled_version="${bundled_version#tailscale_}"
bundled_version="${bundled_version%%_*}"
installed_version=""
if [[ -x "${EN_ROOT}/usr/sbin/tailscaled" ]]; then
  installed_version="$("${EN_ROOT}/usr/sbin/tailscaled" --version 2>/dev/null | head -n 1 || true)"
fi
keep_installed=false
if step_version_ge "${installed_version}" "${bundled_version}"; then
  keep_installed=true
  step_log "Vorhanden ist Tailscale ${installed_version}, das Bundle bringt ${bundled_version}: die Installation bleibt unangetastet."
fi

if [[ "${keep_installed}" == false ]]; then
  work="$(mktemp -d)"
  trap 'rm -rf "${work}"' EXIT
  tar -xzf "${tarballs[0]}" -C "${work}" || step_fail TAILSCALE_INSTALL_FAILED

  # Der echte Tarball packt alles unter tailscale_<ver>_<arch>/. Statt den
  # Namen zu raten, wird die Nutzlast an tailscaled festgemacht.
  payload="$(find "${work}" -type f -name tailscaled -print -quit)"
  [[ -n "${payload}" ]] || step_fail TAILSCALE_INSTALL_FAILED
  payload="$(dirname "${payload}")"

  "${SUDO[@]}" mkdir -p "${EN_ROOT}/usr/sbin" "${EN_ROOT}/etc/systemd/system" \
    "${EN_ROOT}/etc/default"
  "${SUDO[@]}" install -m 0755 "${payload}/tailscale" "${payload}/tailscaled" \
    "${EN_ROOT}/usr/sbin/" || step_fail TAILSCALE_INSTALL_FAILED
  "${SUDO[@]}" install -m 0644 "${payload}/systemd/tailscaled.service" \
    "${EN_ROOT}/etc/systemd/system/tailscaled.service" || step_fail TAILSCALE_INSTALL_FAILED

  # Nur anlegen, wenn sie fehlt - eine vorhandene Datei gehoert dem Betreiber.
  if [[ ! -f "${defaults}" ]]; then
    "${SUDO[@]}" install -m 0644 "${payload}/systemd/tailscaled.defaults" \
      "${defaults}" || step_fail TAILSCALE_INSTALL_FAILED
  fi
  # Gegenprobe nach dem Schreiben: auch die mitgelieferte Vorlage darf den
  # Schalter nicht enthalten.
  if has_forbidden_flag "${defaults}"; then
    step_log "Die installierte ${defaults} enthaelt ${FORBIDDEN_FLAG}."
    step_fail TAILSCALE_FLAG_INVALID
  fi

  "${SUDO[@]}" systemctl daemon-reload
fi
"${SUDO[@]}" systemctl enable --now tailscaled || step_fail TAILSCALE_INSTALL_FAILED

if "${SUDO[@]}" tailscale status >/dev/null 2>&1; then
  if [[ "${keep_installed}" == true ]]; then
    step_log "Node ist angemeldet."
  else
    step_log "Node ist angemeldet; aktualisiere auf die aktuelle Fassung."
    "${SUDO[@]}" tailscale update --yes \
      || step_log "tailscale update fehlgeschlagen; Fassung 1.62.0 bleibt aktiv."
  fi
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
