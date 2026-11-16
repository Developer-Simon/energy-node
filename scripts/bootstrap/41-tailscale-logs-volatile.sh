#!/usr/bin/env bash
#
# Schritt 41: tailscaled schreibt seine Logs nur in den Arbeitsspeicher (Opt-in, Vorgabe an).
#
# tailscaled legt seinen Logpuffer standardmaessig unter /var/lib/tailscale ab
# und schreibt ihn dort laufend fort. Auf einer kleinen SD-Karte kostet das
# Schreibzyklen und Platz. Mit TS_LOGS_DIR zeigt der Puffer auf
# /run/tailscale-logs, also ins RAM. Das Verzeichnis legt systemd mit
# RuntimeDirectory bei jedem Start neu an.
#
# Bei einem Neustart geht nur der Teil des Puffers verloren, der noch nicht zur
# Tailscale-Gegenstelle hochgeladen ist. tailscaled.log.conf bleibt liegen, dort
# steht die Log-ID des Knotens, die weiter gebraucht wird.
#
# Der Schritt ist optional und standardmaessig an: ein fehlender Eintrag in
# selection.json zaehlt als ja. Er braucht Schritt 40, weil Tailscale die
# Voraussetzung ist. Ist Tailscale nicht installiert, wird der Schritt ohne
# Stempel uebersprungen und laeuft bei einem spaeteren Lauf.
#
# Der Fingerabdruck (step-inputs unten) sorgt dafuer, dass nicht jedes Update
# tailscaled neu startet. Der Schritt laeuft nur, wenn sich seine Eingaben
# aendern.
#
# step-inputs: bootstrap/lib/step.sh
set -euo pipefail
# shellcheck source=scripts/bootstrap/lib/step.sh
source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/lib/step.sh"

step_begin 41

dropin_dir="${EN_ROOT}/etc/systemd/system/tailscaled.service.d"
dropin="${dropin_dir}/90-energy-node-logs.conf"
state_dir="${EN_ROOT}/var/lib/tailscale"
marker='# von energy-node-installer verwaltet - Aenderungen gehen verloren'

if ! step_selected 41 || ! step_selected 40; then
  if [[ -f "${dropin}" ]] && grep -qxF "${marker}" "${dropin}"; then
    "${SUDO[@]}" rm -f "${dropin}"
    "${SUDO[@]}" systemctl daemon-reload || step_fail TAILSCALE_LOGS_FAILED
    "${SUDO[@]}" systemctl try-restart tailscaled || step_fail TAILSCALE_LOGS_FAILED
    step_log "tailscaled schreibt seine Logs wieder nach /var/lib/tailscale."
  fi
  rm -f "$(step_stamp_path 41)"
  step_skip "nicht ausgewaehlt"
  exit 0
fi
if step_done 41; then
  step_skip "bereits erledigt"
  exit 0
fi
# Ohne Stempel: wird Tailscale spaeter installiert, laeuft der Schritt dann.
if ! systemctl cat tailscaled.service >/dev/null 2>&1; then
  step_skip "Tailscale nicht installiert"
  exit 0
fi

if [[ -f "${dropin}" ]] && ! grep -qxF "${marker}" "${dropin}"; then
  step_log "In ${dropin} steht eine fremde Konfiguration."
  step_fail TAILSCALE_LOGS_CONF_FOREIGN
fi

"${SUDO[@]}" mkdir -p "${dropin_dir}" || step_fail TAILSCALE_LOGS_FAILED
"${SUDO[@]}" tee "${dropin}" >/dev/null <<CONF || step_fail TAILSCALE_LOGS_FAILED
${marker}
[Service]
RuntimeDirectory=tailscale-logs
Environment=TS_LOGS_DIR=/run/tailscale-logs
CONF

"${SUDO[@]}" systemctl daemon-reload || step_fail TAILSCALE_LOGS_FAILED
"${SUDO[@]}" systemctl restart tailscaled || step_fail TAILSCALE_LOGS_FAILED

# Die bisherigen Puffer auf der Karte schreibt niemand mehr fort.
if [[ -d "${state_dir}" ]]; then
  "${SUDO[@]}" find "${state_dir}" -maxdepth 1 -type f -name 'tailscaled.log*.txt' -delete \
    || step_fail TAILSCALE_LOGS_FAILED
fi

step_log "tailscaled puffert seine Logs nur noch im Arbeitsspeicher (/run/tailscale-logs)."
step_ok
