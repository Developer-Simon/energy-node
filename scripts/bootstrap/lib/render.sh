#!/usr/bin/env bash
#
# Platzhalter-Ersetzung fuer Vorlagen, die den Zielbenutzer oder dessen
# Heimatverzeichnis enthalten (systemd-Units, System-Action-Helfer,
# Sudoers-Regel, Konfigurationsvorlage).
#
# Im oeffentlichen Repo stehen diese Dateien auf dem generischen Benutzer
# "energynode" und dem Pfad /home/energynode. Ein allgemeines Bundle liefert
# sie unveraendert aus; gerendert wird erst auf dem Node, fuer den Benutzer
# und die Basis, die dort gelten (EN_TARGET_USER, EN_TARGET_BASE). Dieselbe
# Datei benutzen scripts/build/lib/render.sh (Bau, Deploy-Skripte) und die
# Schritte 60 und 81-88.

# target_is_valid <user> <base>
#
# Benutzer und Basis landen in Units, in einer sudoers-Regel und in Pfaden,
# die root anlegt. Deshalb nur die engen Formen, die ein Linux-Konto und ein
# normaler Installationspfad haben.
target_is_valid() {
  local user="$1" base="$2"
  [[ "${user}" =~ ^[a-z_][a-z0-9_-]{0,31}$ ]] || return 1
  [[ "${base}" =~ ^/[A-Za-z0-9._/-]{1,200}$ ]] || return 1
  [[ "${base}" != "/" ]] || return 1
  [[ "${base}/" != *"/../"* ]] || return 1
  return 0
}

# render_unit_as <src> <dst> <user> <base>
#
# Erst der /home/energynode-Pfad, dann das blanke Token: andersherum machte
# die Token-Ersetzung die Pfad-Ersetzung vorher kaputt. Bei einem
# ungueltigen Ziel wird nichts geschrieben.
render_unit_as() {
  local src="$1" dst="$2" user="$3" base="$4"
  target_is_valid "${user}" "${base}" || return 1
  sed -e "s#/home/energynode#${base}#g" \
      -e "s/\benergynode\b/${user}/g" \
      "${src}" > "${dst}"
}
