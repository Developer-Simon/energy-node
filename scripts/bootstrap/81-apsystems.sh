#!/usr/bin/env bash
#
# Schritt 81: APsystems-EZ1-Bruecke.
#
# Der gesamte Ablauf steckt in lib/service_step.sh - hier steht nur, welcher
# Dienst gemeint ist.
# Eingaben fuer den Fingerabdruck (scripts/build/lib/manifest.sh): solange
# sie gleich bleiben, ueberspringt ein Update diesen Schritt.
# step-inputs: bootstrap/lib/step.sh bootstrap/lib/service_step.sh
# step-inputs: bootstrap/lib/render.sh bootstrap/lib/restart_rule.py
# step-inputs: services/apsystems_ez1/ wheels/
set -euo pipefail
# shellcheck source=scripts/bootstrap/lib/service_step.sh
source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/lib/service_step.sh"

service_step 81 apsystems_ez1 apsystems-ez1.service
