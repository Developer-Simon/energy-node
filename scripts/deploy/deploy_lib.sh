#!/usr/bin/env bash
#
# Gemeinsame Bibliothek der Deploy-Skripte.
#
# Vorher standen SSH_OPTS, RSYNC_SSH, copy(), copy_with_sudo() und fetch()
# nahezu wortgleich in drei Skripten, und die Dienstetabelle sogar dreifach
# in unterschiedlichen Fassungen. Diese Datei ist die einzige Stelle.
#
# Einbinden mit:
#   source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/deploy_lib.sh"

DEPLOY_LIB_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(git -C "${DEPLOY_LIB_DIR}" rev-parse --show-toplevel)"

# Zielbenutzer/-host stehen nicht mehr fest im Skript - das Repo wird
# oeffentlich, und Benutzername/Hostname des eigenen Zielgeraets gehoeren
# nicht in oeffentlichen Code. Sie kommen aus der nicht versionierten
# secrets/deploy-target.env (per .gitignore-Eintrag /secrets/
# ausgeschlossen) und lassen sich weiterhin per --user/--host oder
# vorab exportierten TARGET_USER/TARGET_HOST ueberschreiben.
DEPLOY_TARGET_FILE="${REPO_ROOT}/secrets/deploy-target.env"
if [[ -z "${TARGET_USER:-}" || -z "${TARGET_HOST:-}" ]] && [[ -f "${DEPLOY_TARGET_FILE}" ]]; then
  # shellcheck disable=SC1090
  source "${DEPLOY_TARGET_FILE}"
fi
TARGET_USER="${TARGET_USER:-}"
TARGET_HOST="${TARGET_HOST:-}"
# TARGET_BASE faellt, wenn nicht per --base/Env/secrets/deploy-target.env
# gesetzt, erst in parse_deploy_args auf /home/<TARGET_USER> zurueck -
# TARGET_USER steht an dieser Stelle (vor dem Einlesen von --user) noch
# nicht zwingend fest.
TARGET_BASE="${TARGET_BASE:-}"
DRY_RUN=false
SKIP_RESTART="${SKIP_RESTART:-0}"
FORCE_CONFIG=false

SSH_OPTS=(
  -o ControlMaster=auto
  -o ControlPersist=600
  -o ControlPath="${HOME}/.ssh/ctl-%r@%h:%p"
)

# Login-Passwort des Zielgeraets (lokale Kopie aus secrets/system-ssh.pw,
# per .gitignore nicht versioniert). Ist die Datei vorhanden und sshpass
# installiert, authentifizieren ssh/scp/rsync sich damit automatisch statt
# interaktiv nachzufragen. Ohne Datei oder ohne sshpass verhalten sich
# ssh()/scp() unten wie die echten Kommandos - z.B. wenn stattdessen ein
# SSH-Key hinterlegt ist.
SSH_PASSWORD_FILE="${REPO_ROOT}/secrets/system-ssh.pw"

if [[ -f "${SSH_PASSWORD_FILE}" ]]; then
  if command -v sshpass >/dev/null 2>&1; then
    SSH_AUTH_WRAPPER=(sshpass -f "${SSH_PASSWORD_FILE}")
  else
    echo "Hinweis: ${SSH_PASSWORD_FILE} vorhanden, aber 'sshpass' ist nicht installiert - Passwort-Eingabe bleibt manuell." >&2
    SSH_AUTH_WRAPPER=()
  fi
else
  SSH_AUTH_WRAPPER=()
fi

# Ueberschreibt ssh/scp innerhalb der Deploy-Skripte, damit alle direkten
# Aufrufe (nicht nur copy()/fetch() hier) von SSH_AUTH_WRAPPER profitieren,
# ohne jede einzelne Aufrufstelle in den anderen Skripten anzufassen. Gilt
# nur fuer diesen Shell-Prozess - rsync ruft sein -e-Kommando selbst per
# execvp auf und bekommt den sshpass-Vorspann stattdessen ueber RSYNC_SSH
# in parse_deploy_args.
ssh() { "${SSH_AUTH_WRAPPER[@]}" command ssh "$@"; }
scp() { "${SSH_AUTH_WRAPPER[@]}" command scp "$@"; }

RSYNC_OPTS=(
  --archive
  --verbose
  --compress
  --exclude='__pycache__/'
  --exclude='*.egg-info/'
  --exclude='.venv/'
  --exclude='.tox/'
  --exclude='dist/'
  --exclude='build/'
  --exclude='.pytest_cache/'
)

# Dienstetabelle: unit:lokaler-pfad:remote-unterverzeichnis:deploy
#
# deploy=1 bedeutet: Unit installieren/aktualisieren und Dienst neu starten.
# Ein anderer Wert (aktuell ungenutzt) markiert eine Unit, die nur
# zurueckgeholt (fetch_env_from_remote.sh), aber nie ausgerollt wird.
SERVICE_TABLE=(
  "apsystems-ez1.service:services/apsystems_ez1/apsystems-ez1.service:apsystems_ez1:1"
  "battery-soc.service:services/battery_soc/battery-soc.service:battery_soc:1"
  "shelly-rpc.service:services/shelly/shelly-rpc.service:shelly:1"
  "trucki-http.service:services/trucki/trucki-http.service:trucki:1"
  "tuya.service:services/tuya_mqtt/tuya.service:tuya_mqtt:1"
  "energy-node.service:services/energy-node/energy-node.service:energy-node:1"
  "automation.service:services/automation/automation.service:automation:1"
)

service_field() {
  local entry="$1" index="$2"
  printf '%s' "${entry}" | cut -d: -f"${index}"
}

deploy_usage() {
  cat <<EOF
Optionen (gemeinsam fuer alle Deploy-Skripte):
  --host HOST          Zielhost (Standard: ${TARGET_HOST:-aus secrets/deploy-target.env})
  --user USER          Zielbenutzer (Standard: ${TARGET_USER:-aus secrets/deploy-target.env})
  --base PATH          Basisverzeichnis (Standard: ${TARGET_BASE:-/home/<Zielbenutzer>})
  --dry-run, -n        Nur anzeigen, nichts uebertragen
  --skip-restart       Dienste nicht neu starten
  --force-config       Vorhandene /etc/energy-node/config.json ueberschreiben
  --help, -h           Diese Hilfe
EOF
}

# parse_deploy_args wertet die gemeinsamen Schalter aus. Ein Skript mit
# eigenen Schaltern setzt vorher EXTRA_ARG_HANDLER auf den Namen einer
# Funktion, die ein unbekanntes Argument entgegennimmt, CONSUMED_ARGS auf die
# Anzahl der verbrauchten Argumente setzt (0 = nicht zustaendig) und bei
# Bedarf eigene Variablen setzt. Wichtig: die Funktion wird DIREKT aufgerufen,
# nicht per Kommando-Substitution ("$(...)") - letztere laeuft in einer
# Subshell, in der gesetzte Variablen beim Zurueckkehren verloren gehen.
# EXTRA_USAGE_HOOK ist optional der Name einer Funktion, die bei --help nach
# deploy_usage zusaetzliche, skriptspezifische Zeilen ausgibt.
parse_deploy_args() {
  while [[ $# -gt 0 ]]; do
    case "$1" in
      --host) TARGET_HOST="$2"; shift 2 ;;
      --user) TARGET_USER="$2"; shift 2 ;;
      --base) TARGET_BASE="$2"; shift 2 ;;
      --dry-run|-n) DRY_RUN=true; shift ;;
      --skip-restart) SKIP_RESTART=1; shift ;;
      --force-config) FORCE_CONFIG=true; shift ;;
      --help|-h)
        deploy_usage
        [[ -n "${EXTRA_USAGE_HOOK:-}" ]] && "${EXTRA_USAGE_HOOK}"
        exit 0
        ;;
      *)
        CONSUMED_ARGS=0
        if [[ -n "${EXTRA_ARG_HANDLER:-}" ]]; then
          "${EXTRA_ARG_HANDLER}" "$@"
        fi
        local consumed="${CONSUMED_ARGS}"
        if [[ "${consumed}" -gt 0 ]]; then
          shift "${consumed}"
        else
          echo "Unbekanntes Argument: $1" >&2
          deploy_usage
          exit 1
        fi
        ;;
    esac
  done

  if [[ -z "${TARGET_USER}" || -z "${TARGET_HOST}" ]]; then
    echo "Kein Zielbenutzer/-host gesetzt." >&2
    echo "Entweder --user/--host angeben, TARGET_USER/TARGET_HOST exportieren," >&2
    echo "oder ${DEPLOY_TARGET_FILE} anlegen mit:" >&2
    echo "  TARGET_USER=<benutzer>" >&2
    echo "  TARGET_HOST=<host>" >&2
    exit 1
  fi

  # Erst hier steht TARGET_USER endgueltig fest (--user/Env/secrets-Datei
  # koennen es bis zu diesem Punkt noch veraendert haben) - deshalb faellt
  # TARGET_BASE erst jetzt auf /home/<TARGET_USER> zurueck, nicht schon beim
  # Laden von deploy_lib.sh.
  TARGET_BASE="${TARGET_BASE:-/home/${TARGET_USER}}"

  if [[ "$DRY_RUN" == true ]]; then
    RSYNC_OPTS+=(--dry-run)
  fi
  REMOTE_PREFIX="${TARGET_USER}@${TARGET_HOST}:${TARGET_BASE}"
  SSH_TARGET="${TARGET_USER}@${TARGET_HOST}"
  if [[ "${#SSH_AUTH_WRAPPER[@]}" -gt 0 ]]; then
    RSYNC_SSH="sshpass -f ${SSH_PASSWORD_FILE} ssh ${SSH_OPTS[*]}"
  else
    RSYNC_SSH="ssh ${SSH_OPTS[*]}"
  fi
}

# render_service_unit ersetzt im generischen Platzhalter "energynode"
# (siehe services/*/*.service, dashboard/energy-node-dashboard*), unter dem alle
# Service-Units/System-Action-Skripte/Sudoers-Regeln im oeffentlichen Repo
# hinterlegt sind, durch den tatsaechlichen Zielbenutzer/-pfad, bevor die
# Datei auf das Zielgeraet kopiert wird. Erst der /home/energynode-Pfad,
# dann das blanke energynode-Token (sonst wuerde die Pfad-Ersetzung durch
# die Token-Ersetzung vorher kaputtgehen).
render_service_unit() {
  local src="$1" dst="$2"
  sed -e "s#/home/energynode#${TARGET_BASE}#g" \
      -e "s/\benergynode\b/${TARGET_USER}/g" \
      "${src}" > "${dst}"
}

copy() {
  echo "Copying $1 -> $2"
  rsync "${RSYNC_OPTS[@]}" -e "${RSYNC_SSH}" "$1" "$2"
}

copy_with_sudo() {
  echo "Copying $1 -> $2 (with sudo)"
  rsync "${RSYNC_OPTS[@]}" --rsync-path="sudo rsync" -e "${RSYNC_SSH}" "$1" "$2"
}

fetch() {
  echo "Fetching $1 -> $2"
  rsync "${RSYNC_OPTS[@]}" -e "${RSYNC_SSH}" "$1" "$2"
}

fetch_with_sudo() {
  echo "Fetching $1 -> $2 (with sudo)"
  rsync "${RSYNC_OPTS[@]}" --rsync-path="sudo rsync" -e "${RSYNC_SSH}" "$1" "$2"
}
