#!/usr/bin/env bash
#
# Schritt 84: Trucki-HTTP-Bruecke.
#
# Der gesamte Ablauf steckt in lib/service_step.sh - hier steht nur, welcher
# Dienst gemeint ist.
set -euo pipefail
# shellcheck source=scripts/bootstrap/lib/service_step.sh
source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/lib/service_step.sh"

service_step 84 trucki trucki-http.service
