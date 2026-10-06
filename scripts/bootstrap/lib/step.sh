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
#   EN_BUNDLE_VERSION Version im Stempel; ohne Fingerabdruck im Manifest gilt
#                     ein Stempel einer anderen Version als nicht erledigt
#   EN_SUDO           Kommando-Praefix fuer privilegierte Aufrufe. "" = keines.

EN_STATE_DIR="${EN_STATE_DIR:-/var/lib/energy-node-installer}"
EN_ROOT="${EN_ROOT:-}"
EN_BUNDLE_DIR="${EN_BUNDLE_DIR:-${EN_STATE_DIR}/bundle}"
EN_BUNDLE_VERSION="${EN_BUNDLE_VERSION:-unbekannt}"
EN_SELECTION="${EN_SELECTION:-${EN_STATE_DIR}/selection.json}"
# Heimatverzeichnis der Python-Dienste. Die Units im Bundle sind bereits
# darauf gerendert, also muss der Node die Quellen an dieselbe Stelle legen.
EN_TARGET_BASE="${EN_TARGET_BASE:-${HOME}}"
# Derselbe Benutzer als Name; wird Gruppe der Dateien unter /etc/energy-node,
# die die Dienste selbst lesen muessen.
EN_TARGET_USER="${EN_TARGET_USER:-$(id -un)}"

# SUDO ist bewusst ein Array: als Zeichenkette muesste jede Aufrufstelle
# unquoted expandieren, was bei leerem Wert ein leeres Argument erzeugt.
read -r -a SUDO <<< "${EN_SUDO-sudo}"
: "${SUDO[@]}"  # als benutzt markieren - wird von Skripten verwendet, die diese Datei sourcen

STEP_ID="${STEP_ID:-}"

step_stamp_path() { printf '%s/steps/%s' "${EN_STATE_DIR}" "$1"; }

# step_fingerprint <id>: der Fingerabdruck des Schritts aus dem Manifest des
# Bundles (siehe scripts/build/lib/manifest.sh), leer ohne einen.
step_fingerprint() {
  [[ -f "${EN_BUNDLE_DIR}/manifest.json" ]] || return 0
  python3 - "${EN_BUNDLE_DIR}/manifest.json" "$1" 2>/dev/null <<'PY' || true
import json, sys
try:
    steps = json.load(open(sys.argv[1], encoding="utf-8")).get("steps") or []
except (OSError, ValueError):
    sys.exit(0)
for step in steps:
    if str(step.get("id")) == sys.argv[2]:
        print(step.get("fingerprint") or "")
PY
}

step_write_stamp() {
  local stamp fingerprint
  stamp="$(step_stamp_path "$1")"
  fingerprint="$(step_fingerprint "$1")"
  mkdir -p "$(dirname "${stamp}")"
  {
    printf 'bundle=%s\nzeit=%s\n' "${EN_BUNDLE_VERSION}" "$(date -Is)"
    [[ -z "${fingerprint}" ]] || printf 'fingerprint=%s\n' "${fingerprint}"
  } > "${stamp}"
}

# Menschentext fuers Log. Nie in Spalte 1 mit ## beginnen - das ist die
# Grenze, an der der Markerparser der Anwendung trennt.
step_log() { printf '%s\n' "$*"; }

step_begin() {
  STEP_ID="$1"
  printf '##STEP %s begin\n' "${STEP_ID}"
}

step_ok() { step_ok_with ""; }

# step_ok_with <zusatz>: wie step_ok, der Zusatz ist ein festes Wort fuer
# die Oberflaeche (15: "neustart noetig"), kein Menschentext. Eine eigene
# Funktion statt eines optionalen Arguments von step_ok - shellcheck 0.9
# meldet sonst SC2119 an jedem Aufruf ohne Argument.
step_ok_with() {
  step_write_stamp "${STEP_ID}"
  if [[ -n "$1" ]]; then
    printf '##STEP %s ok %s\n' "${STEP_ID}" "$1"
  else
    printf '##STEP %s ok\n' "${STEP_ID}"
  fi
}

step_skip() { printf '##STEP %s skip %s\n' "${STEP_ID}" "$*"; }

# Ein fehlgeschlagener Schritt hinterlaesst keinen Stempel - sonst wuerde der
# naechste Lauf ihn ueberspringen und der Fehler waere unsichtbar.
step_fail() {
  printf '##STEP %s fail %s\n' "${STEP_ID}" "$1"
  exit 1
}

# step_done: traegt der Schritt im Manifest einen Fingerabdruck, gilt sein
# Stempel, solange der Fingerabdruck darin gleich ist, auch ueber ein Update
# hinweg. Der Stempel wird dann auf die neue Bundle-Version gehoben, damit
# Vorpruefung und Diagnose sehen, dass der Schritt zu ihr passt. Ohne
# Fingerabdruck zaehlt wie bisher nur ein Stempel derselben Bundle-Version.
step_done() {
  local stamp fingerprint
  stamp="$(step_stamp_path "$1")"
  [[ -f "${stamp}" ]] || return 1
  fingerprint="$(step_fingerprint "$1")"
  if [[ -z "${fingerprint}" ]]; then
    grep -qx "bundle=${EN_BUNDLE_VERSION}" "${stamp}"
    return
  fi
  grep -qx "fingerprint=${fingerprint}" "${stamp}" || return 1
  grep -qx "bundle=${EN_BUNDLE_VERSION}" "${stamp}" || step_write_stamp "$1"
}

# step_selected entscheidet fuer optionale Schritte, ob sie laufen.
# Fehlende Datei oder nicht genannter Schritt = gewaehlt (Vorgabe "an",
# E7). Eine unlesbare Datei ist dagegen ein Fehler und keine Zustimmung -
# sonst installierte ein Tippfehler in der Auswahl stillschweigend alles.
step_selected() {
  local id="$1"
  [[ -f "${EN_SELECTION}" ]] || return 0
  python3 - "${EN_SELECTION}" "${id}" <<'PY'
import json, sys
try:
    with open(sys.argv[1], encoding="utf-8") as handle:
        data = json.load(handle)
except (OSError, ValueError) as exc:
    sys.exit("selection.json nicht lesbar: %s" % exc)
steps = data.get("steps")
if not isinstance(steps, dict):
    sys.exit("selection.json: 'steps' fehlt oder ist kein Objekt")
sys.exit(0 if steps.get(sys.argv[2], True) else 1)
PY
}

# step_opted_in ist das Gegenstueck fuer Opt-in-Schritte (Manifest-Vorgabe
# "aus"): nur ein ausdrueckliches true in selection.json zaehlt. Fehlende
# Datei oder nicht genannter Schritt = nicht gewaehlt - sonst braechte ein
# Update auf einem Node mit aelterer Auswahl den Schritt ungefragt mit. Eine
# unlesbare Datei ist wie bei step_selected ein Fehler.
step_opted_in() {
  local id="$1"
  [[ -f "${EN_SELECTION}" ]] || return 1
  python3 - "${EN_SELECTION}" "${id}" <<'PY'
import json, sys
try:
    with open(sys.argv[1], encoding="utf-8") as handle:
        data = json.load(handle)
except (OSError, ValueError) as exc:
    sys.exit("selection.json nicht lesbar: %s" % exc)
steps = data.get("steps")
if not isinstance(steps, dict):
    sys.exit("selection.json: 'steps' fehlt oder ist kein Objekt")
sys.exit(0 if steps.get(sys.argv[2]) is True else 1)
PY
}

# step_version_ge <a> <b>: Erfolg, wenn Version a gleich oder neuer als b ist.
# Ein fuehrendes "v" und alles ab dem ersten Zeichen, das nicht zu einer
# Punktversion gehoert (1.98.9-t4fb758c39-g200941d74), zaehlen nicht. Eine
# leere oder nicht lesbare Version ist "unbekannt" und nie gleich oder
# neuer: wer damit einen Schritt ueberspringt, tut es nur bei sicherem
# Wissen.
step_version_ge() {
  local a="${1#v}" b="${2#v}"
  a="${a%%[!0-9.]*}"
  b="${b%%[!0-9.]*}"
  [[ "${a}" =~ ^[0-9]+(\.[0-9]+)*$ && "${b}" =~ ^[0-9]+(\.[0-9]+)*$ ]] || return 1
  [[ "$(printf '%s\n%s\n' "${a}" "${b}" | sort -V | tail -n 1)" == "${a}" ]]
}
