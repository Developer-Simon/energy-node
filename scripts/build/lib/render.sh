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

# render_unit_as <src> <dst> <user> <base>
#
# Erst der /home/energynode-Pfad, dann das blanke Token: andersherum machte
# die Token-Ersetzung die Pfad-Ersetzung vorher kaputt.
render_unit_as() {
  local src="$1" dst="$2" user="$3" base="$4"
  sed -e "s#/home/energynode#${base}#g" \
      -e "s/\benergynode\b/${user}/g" \
      "${src}" > "${dst}"
}

# Bequemlichkeitsform fuer scripts/deploy/*, die TARGET_USER und TARGET_BASE
# ohnehin gesetzt haben.
render_service_unit() {
  render_unit_as "$1" "$2" "${TARGET_USER}" "${TARGET_BASE}"
}
