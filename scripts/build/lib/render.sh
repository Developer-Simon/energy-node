#!/usr/bin/env bash
#
# Platzhalter-Ersetzung in systemd-Units, System-Action-Helfern und
# Sudoers-Regeln.
#
# Im oeffentlichen Repo stehen alle diese Dateien auf den generischen
# Benutzer "energynode" und den Pfad /home/energynode. Vor der Auslieferung
# wird beides durch den tatsaechlichen Zielbenutzer/-pfad ersetzt - frueher
# nur beim Deploy, jetzt auch beim Bundle-Bau (E4: das gehoert auf den
# Bau-Rechner).

# shellcheck source=scripts/bootstrap/lib/render.sh
source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/../../bootstrap/lib/render.sh"

# render_service_unit <src> <dst>
#
# Bequemlichkeitsform fuer scripts/deploy/*, die TARGET_USER und TARGET_BASE
# ohnehin gesetzt haben.
render_service_unit() {
  render_unit_as "$1" "$2" "${TARGET_USER}" "${TARGET_BASE}"
}
