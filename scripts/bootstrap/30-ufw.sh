#!/usr/bin/env bash
#
# Schritt 30: Firewall-Regeln (INSTALLATION.md §4).
#
# 443 wird auch dann freigegeben, wenn Caddy nicht gewaehlt ist - eine offene
# Regel ohne lauschenden Dienst ist harmlos, eine fehlende Regel nach einem
# spaeteren Hinzuwaehlen von HTTPS dagegen ein stiller Ausfall.
set -euo pipefail
# shellcheck source=scripts/bootstrap/lib/step.sh
source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/lib/step.sh"

RULES=(ssh 1883/tcp 8080/tcp 443/tcp)

step_begin 30
if step_done 30; then
  step_skip "bereits erledigt"
  exit 0
fi

for rule in "${RULES[@]}"; do
  "${SUDO[@]}" ufw allow "${rule}" || step_fail UFW_FAILED
done

# --force, weil `ufw enable` sonst interaktiv nachfragt und die Frage in einer
# nicht-interaktiven SSH-Session nie beantwortet wird.
"${SUDO[@]}" ufw --force enable || step_fail UFW_FAILED

step_log "Firewall aktiv: ${RULES[*]}"
step_ok
