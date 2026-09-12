#!/usr/bin/env bash
#
# Gemeinsame Basis aller Bootstrap-Schritte (Komponente A der Spec).
#
# Jeder Schritt sourct diese Datei, meldet seinen Fortschritt ueber die
# step_*-Funktionen und laesst sich ueber die EN_*-Variablen umlenken -
# genau das macht die Schritte ohne Root und ohne Container testbar.
#
#   EN_STATE_DIR      Stempelverzeichnis (Vorgabe /var/lib/energy-node-installer)
#   EN_ROOT           Praefix vor allen Systempfaden (Vorgabe leer)
#   EN_BUNDLE_DIR     entpacktes Bundle (Vorgabe /var/lib/energy-node-installer/bundle)
#   EN_BUNDLE_VERSION Version im Stempel; ein Stempel einer anderen Version
#                     gilt als nicht erledigt
#   EN_SUDO           Kommando-Praefix fuer privilegierte Aufrufe. "" = keines.

EN_STATE_DIR="${EN_STATE_DIR:-/var/lib/energy-node-installer}"
EN_ROOT="${EN_ROOT:-}"
EN_BUNDLE_DIR="${EN_BUNDLE_DIR:-${EN_STATE_DIR}/bundle}"
EN_BUNDLE_VERSION="${EN_BUNDLE_VERSION:-unbekannt}"

# SUDO ist bewusst ein Array: als Zeichenkette muesste jede Aufrufstelle
# unquoted expandieren, was bei leerem Wert ein leeres Argument erzeugt.
read -r -a SUDO <<< "${EN_SUDO-sudo}"
: "${SUDO[@]}"  # als benutzt markieren - wird von Skripten verwendet, die diese Datei sourcen

STEP_ID="${STEP_ID:-}"

step_stamp_path() { printf '%s/steps/%s' "${EN_STATE_DIR}" "$1"; }

# Menschentext fuers Log. Nie in Spalte 1 mit ## beginnen - das ist die
# Grenze, an der der Markerparser der Anwendung trennt.
step_log() { printf '%s\n' "$*"; }

step_begin() {
  STEP_ID="$1"
  printf '##STEP %s begin\n' "${STEP_ID}"
}

step_ok() {
  local stamp
  stamp="$(step_stamp_path "${STEP_ID}")"
  mkdir -p "$(dirname "${stamp}")"
  printf 'bundle=%s\nzeit=%s\n' "${EN_BUNDLE_VERSION}" "$(date -Is)" > "${stamp}"
  printf '##STEP %s ok\n' "${STEP_ID}"
}

step_skip() { printf '##STEP %s skip %s\n' "${STEP_ID}" "$*"; }

# Ein fehlgeschlagener Schritt hinterlaesst keinen Stempel - sonst wuerde der
# naechste Lauf ihn ueberspringen und der Fehler waere unsichtbar.
step_fail() {
  printf '##STEP %s fail %s\n' "${STEP_ID}" "$1"
  exit 1
}

step_done() {
  local stamp
  stamp="$(step_stamp_path "$1")"
  [[ -f "${stamp}" ]] || return 1
  grep -qx "bundle=${EN_BUNDLE_VERSION}" "${stamp}"
}
