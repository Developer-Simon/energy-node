#!/usr/bin/env bash
#
# Schritt 82: Batterie-Fuellstand.
#
# Der gesamte Ablauf steckt in lib/service_step.sh - hier steht nur, welcher
# Dienst gemeint ist.
# Eingaben fuer den Fingerabdruck (scripts/build/lib/manifest.sh): solange
# sie gleich bleiben, ueberspringt ein Update diesen Schritt.
# step-inputs: bootstrap/lib/step.sh bootstrap/lib/service_step.sh
# step-inputs: bootstrap/lib/render.sh bootstrap/lib/restart_rule.py
# step-inputs: services/battery_soc/ wheels/
set -euo pipefail
# shellcheck source=scripts/bootstrap/lib/service_step.sh
source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/lib/service_step.sh"

service_step 82 battery_soc battery-soc.service
