#!/usr/bin/env bash
#
# Platzhalter-Ersetzung in systemd-Units, System-Action-Helfern und
# Sudoers-Regeln.
#
# Im oeffentlichen Repo stehen alle diese Dateien auf den generischen
# Benutzer "energynode" und den Pfad /home/energynode. Vor der Auslieferung
# wird beides beim Bundle-Bau durch den tatsaechlichen Zielbenutzer/-pfad
# ersetzt (E4: das gehoert auf den Bau-Rechner).

# shellcheck source=scripts/bootstrap/lib/render.sh
source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/../../bootstrap/lib/render.sh"
