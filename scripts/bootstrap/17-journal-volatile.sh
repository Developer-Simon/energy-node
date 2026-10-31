#!/usr/bin/env bash
#
# Schritt 17: Journal nur im Arbeitsspeicher (Opt-in).
#
# systemd-journald schreibt Logs standardmaessig nach /var/log/journal, wenn
# das Verzeichnis existiert (auf Raspberry Pi OS der Fall). Auf einer kleinen
# SD-Karte kostet das Schreibzyklen und Platz. Mit Storage=volatile landen die
# Logs unter /run/log/journal, also im RAM, und sind nach einem Neustart weg.
# Das trifft auch die Dienstlogs, die das Dashboard anzeigt. Darum ist der
# Schritt ein Opt-in (Manifest-Vorgabe "aus", step_opted_in: ein fehlender
# Eintrag in selection.json zaehlt als "nein").
#
# RuntimeMaxUse deckelt den RAM, den das Journal belegen darf.
#
# Gewaehlt legt der Schritt ein Drop-in an und raeumt das bisherige Journal auf
# der Karte weg (es waere sonst weiter belegt und nicht mehr lesbar-relevant).
# Abgewaehlt nimmt er das Drop-in wieder zurueck und loescht seinen Stempel.
#
# Kein step-inputs: der Schritt laeuft je Bundle-Version, damit eine
# Abwahl auch bei einem Update greift.
set -euo pipefail
# shellcheck source=scripts/bootstrap/lib/step.sh
source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/lib/step.sh"

step_begin 17

dropin_dir="${EN_ROOT}/etc/systemd/journald.conf.d"
dropin="${dropin_dir}/90-energy-node-volatile.conf"
persistent="${EN_ROOT}/var/log/journal"
# Kopfzeile, an der wir unsere eigene Datei wiedererkennen.
marker='# von energy-node-installer verwaltet - Aenderungen gehen verloren'

if ! step_opted_in 17; then
  if [[ -f "${dropin}" ]] && grep -qxF "${marker}" "${dropin}"; then
    "${SUDO[@]}" rm -f "${dropin}"
    "${SUDO[@]}" systemctl restart systemd-journald || step_fail JOURNALD_RESTART_FAILED
    step_log "Journal wieder wie vom System vorgesehen."
  fi
  rm -f "$(step_stamp_path 17)"
  step_skip "nicht ausgewaehlt"
  exit 0
fi
if step_done 17; then
  step_skip "bereits erledigt"
  exit 0
fi

if [[ -f "${dropin}" ]] && ! grep -qxF "${marker}" "${dropin}"; then
  step_log "In ${dropin} steht eine fremde Konfiguration."
  step_fail JOURNAL_CONF_FOREIGN
fi

"${SUDO[@]}" mkdir -p "${dropin_dir}"
"${SUDO[@]}" tee "${dropin}" >/dev/null <<CONF || step_fail JOURNAL_FAILED
${marker}
[Journal]
Storage=volatile
RuntimeMaxUse=50M
CONF

"${SUDO[@]}" systemctl restart systemd-journald || step_fail JOURNALD_RESTART_FAILED

# Das bisherige Journal auf der Karte belegt weiter Platz und wird nicht mehr
# fortgeschrieben.
if [[ -d "${persistent}" ]]; then
  "${SUDO[@]}" rm -rf "${persistent:?}"/* || step_fail JOURNAL_FAILED
  step_log "Bisheriges Journal auf der Karte entfernt."
fi

step_log "Journal liegt nur noch im Arbeitsspeicher (bis 50M), nach einem Neustart ist es leer."
step_ok
