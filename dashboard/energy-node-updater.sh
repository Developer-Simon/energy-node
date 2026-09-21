#!/usr/bin/env bash
#
# energy-node-updater: root oneshot, gestartet durch energy-node-updater.path
# wenn ${EN_UPDATER_JOB_DIR:-/var/lib/energy-node-installer/job}/pending.json
# erscheint (Plan D, Komponente D der Installer-Spec).
#
# Bewusst KEIN zweiter Go-Schritt-Motor: die Bootstrap-Skripte sind laut E1
# fuer sich per Hand ausfuehrbar, und verify_bundle.sh sagt in seinem
# eigenen Kopf bereits, dass die Updater-Unit es benutzt. Dieses Skript
# tut nur, was steps.Run in installer/internal/steps ueber SSH tut, nur
# lokal: die Auftragsdatei lesen, jeden Schritt der Reihe nach starten,
# ##STEP-Marker und Menschentext mit einem Zeitstempel in eine Logdatei
# statt an ein SSH-Stdout schreiben.
#
# Vertrauensgrenze: job.json/pending.json liegen in einem Verzeichnis, das
# dem unprivilegierten Dashboard-Konto gehoert, und sind von der Signatur
# ueber manifest.json NICHT gedeckt. Deshalb steuert der Auftrag nur noch,
# WELCHE Schritte laufen sollen - und selbst diese Liste wird gegen die
# Schrittliste im geprueften Manifest gehalten. Zielbenutzer und Zielbasis
# kommen aus der root-eigenen target.json (von Schritt 60 geschrieben), bei
# einem noch auf einen Benutzer festgelegten Bundle aus dessen geprueftem
# Manifest; die Bundle-Version kommt aus manifest.json.
#
# Umgebungsvariablen (Vorgaben fuer den echten Betrieb, ueberschreibbar fuer
# scripts/tests/test_updater.sh):
#   EN_UPDATER_JOB_DIR    /var/lib/energy-node-installer/job
#   EN_UPDATER_STATE_DIR  /var/lib/energy-node-installer
#   EN_UPDATER_RUN_DIR    <state>/run  (nur root; hierhin wandert das Bundle)
#   EN_UPDATER_VERIFY     /usr/local/lib/energy-node-installer/verify_bundle.sh
#   EN_UPDATER_PUBKEY     /etc/energy-node-updater/signing_key.pub.pem
#   EN_UPDATER_TARGET     /etc/energy-node-updater/target.json  (root-eigenes Ziel des Knotens)
#   EN_ROOT               Praefix vor Systempfaden (wie in lib/step.sh)
set -uo pipefail

JOB_DIR="${EN_UPDATER_JOB_DIR:-/var/lib/energy-node-installer/job}"
STATE_DIR="${EN_UPDATER_STATE_DIR:-/var/lib/energy-node-installer}"
RUN_DIR="${EN_UPDATER_RUN_DIR:-${STATE_DIR}/run}"
VERIFY="${EN_UPDATER_VERIFY:-/usr/local/lib/energy-node-installer/verify_bundle.sh}"
PUBKEY="${EN_UPDATER_PUBKEY:-/etc/energy-node-updater/signing_key.pub.pem}"
TARGET_FILE="${EN_UPDATER_TARGET:-/etc/energy-node-updater/target.json}"
ROOT_PREFIX="${EN_ROOT:-}"

PENDING="${JOB_DIR}/pending.json"
CURRENT="${JOB_DIR}/current.json"
LOG="${JOB_DIR}/log"
STATUS="${JOB_DIR}/status.json"
# Das Bundle laeuft NICHT aus dem Auftragsverzeichnis: das gehoert dem
# Dashboard-Konto und bliebe zwischen Pruefung und Ausfuehrung beschreibbar
# (TOCTOU, und verify_bundle.sh prueft ohnehin nur die im Manifest
# gelisteten Dateien - eine zusaetzlich abgelegte wheels/*.whl faende
# Schritt 50 trotzdem). Wir holen es zuerst in ein nur fuer root
# erreichbares Verzeichnis und pruefen und fahren dort.
BUNDLE="${RUN_DIR}/bundle"

# Kein Auftrag: nichts zu tun. Das .path-Unit startet uns nur, wenn
# pending.json existiert, aber ein doppeltes Anstossen kurz hintereinander
# ist kein Fehler.
[[ -f "${PENDING}" ]] || exit 0

# Erste Aktion ueberhaupt: pending.json entfernen, damit das .path-Unit
# nicht erneut auf dieselbe Datei anspringt, waehrend wir noch laufen (wir
# schreiben selbst in dieses Verzeichnis - log und status.json - und ein
# .path-Unit, das auf das ganze Verzeichnis achtete, wuerde sich dadurch
# selbst retriggern).
mv "${PENDING}" "${CURRENT}"
: > "${LOG}"
rm -f "${STATUS}"

now_ms() { date +%s%3N; }
log_line() { printf '%s %s\n' "$(now_ms)" "$1" >> "${LOG}"; }

# write_status baut das JSON mit python3 statt mit printf: "code" stammt
# aus der Ausgabe des Pruefers oder eines Schrittskripts, und ein einzelnes
# Anfuehrungszeichen darin ergaebe sonst unlesbares JSON - was
# updaterjob.ReadStatus als generischen BACKEND_ERROR meldete statt des
# echten Fehlercodes.
write_status() {
  UPD_RESULT="$1" UPD_STEP="$2" UPD_CODE="$3" python3 - "${STATUS}" <<'PY'
import json, os, sys
doc = {
    "result": os.environ["UPD_RESULT"],
    "step": os.environ["UPD_STEP"],
    "code": os.environ["UPD_CODE"],
}
with open(sys.argv[1], "w", encoding="utf-8") as handle:
    json.dump(doc, handle, separators=(",", ":"))
    handle.write("\n")
PY
}

# Ab hier ist der Auftrag beansprucht: current.json existiert, status.json
# nicht. Stirbt der Prozess jetzt (Signal, Stromausfall mitten in Schritt
# 60), bliebe updaterjob.InFlight fuer immer wahr und Stage() lehnte jeden
# weiteren Auftrag ab - das Auftragsverzeichnis waere dauerhaft verriegelt.
# Der Trap sorgt dafuer, dass es in jedem Fall einen Endzustand gibt; hat
# der regulaere Weg schon einen geschrieben, ruehrt er ihn nicht an.
# shellcheck disable=SC2329,SC2317 # wird ueber trap aufgerufen
finish_interrupted() {
  local rc="$?"
  if [[ ! -f "${STATUS}" ]]; then
    log_line "FAIL UPDATER_INTERRUPTED"
    write_status fail "" UPDATER_INTERRUPTED
  fi
  exit "${rc}"
}
trap finish_interrupted EXIT

fail_job() {
  write_status "$1" "$2" "$3"
  exit 1
}

# --- Das Bundle aus dem beschreibbaren Auftragsverzeichnis herausholen ----
# Verschieben, nicht kopieren: danach kann das Dashboard-Konto nichts mehr
# nachlegen, und Pruefung wie Ausfuehrung sehen garantiert dieselben Dateien.
rm -rf "${RUN_DIR}"
if [[ "$(id -u)" -eq 0 ]]; then
  install -d -m 0700 -o root -g root "${RUN_DIR}"
else
  # Nur der Test laeuft ohne root; dort gibt es kein root-Eigentum zu setzen.
  install -d -m 0700 "${RUN_DIR}"
fi || fail_job fail "" BUNDLE_CLAIM_FAILED
mv "${JOB_DIR}/bundle" "${BUNDLE}" || fail_job fail "" BUNDLE_CLAIM_FAILED

# --- E13: erst die Signatur, dann die Hashes -- verify_bundle.sh selbst
# haelt diese Reihenfolge ein; wir rufen nur die persistente, schon vor
# diesem Auftrag installierte Kopie auf, nie die im Auftrag selbst
# mitgelieferte -- sonst koennte ein Angreifer mit Schreibrecht auf
# JOB_DIR seinen eigenen, immer-erfolgreichen Pruefer mitschicken.
if ! verify_out=$(bash "${VERIFY}" --bundle "${BUNDLE}" --pubkey "${PUBKEY}" --target 2>&1); then
  code="$(printf '%s\n' "${verify_out}" | tail -1 | awk '{print $2}')"
  code="${code//[^A-Za-z0-9_]/}"
  [[ -n "${code}" ]] || code="BUNDLE_SIGNATURE_INVALID"
  log_line "REJECT ${code}"
  fail_job "rejected" "" "${code}"
fi

# --- Ab hier nur noch geprueftes Manifest --------------------------------
manifest="${BUNDLE}/manifest.json"
if ! manifest_fields="$(python3 - "${manifest}" <<'PY'
import json, sys
doc = json.load(open(sys.argv[1], encoding="utf-8"))
print(doc.get("version", ""))
print(doc.get("target_user", ""))
print(doc.get("target_base", ""))
print(" ".join(str(step["id"]) for step in doc.get("steps", [])))
PY
)"; then
  fail_job fail "" MANIFEST_MALFORMED
fi
{
  IFS= read -r bundle_version
  IFS= read -r target_user
  IFS= read -r target_base
  IFS= read -r manifest_step_ids
} <<< "${manifest_fields}"

# Zielbenutzer und Zielbasis steuern mkdir/install/chown und die Sudoers-Regel
# als root. Sie kamen frueher aus job.json (dashboard-beschreibbar, nicht
# signiert) und dann nur aus dem signierten Manifest. Ein user-unabhaengiges
# Bundle traegt kein Ziel; das steht dann in /etc/energy-node-updater/
# target.json, das Schritt 60 als root schreibt. Ein noch festgelegtes Bundle
# muss zu dieser Datei passen. Ein Rueckgriff auf installed-manifest.json gibt
# es absichtlich nicht: es liegt im Zustandsverzeichnis, und das gehoert dem
# SSH-Benutzer.
if ! target_out="$(python3 - "${TARGET_FILE}" "${target_user}" "${target_base}" <<'PY'
import json, os, re, sys

path, man_user, man_base = sys.argv[1:4]
USER = re.compile(r"^[a-z_][a-z0-9_-]{0,31}$")
BASE = re.compile(r"^/[A-Za-z0-9._/-]{1,200}$")


# fullmatch statt match mit $: $ passt in Python auch vor einem abschliessenden
# Zeilenumbruch, und der liesse aus "user\n" zwei Ausgabezeilen werden.
def valid(user, base):
    return bool(USER.fullmatch(user)) and bool(BASE.fullmatch(base)) \
        and base != "/" and ".." not in base.split("/")


def reject(code):
    print(code)
    sys.exit(1)


if os.path.isfile(path):
    try:
        with open(path, encoding="utf-8") as handle:
            doc = json.load(handle)
        user, base = str(doc["user"]), str(doc["base"])
    except (OSError, ValueError, KeyError, TypeError):
        reject("TARGET_INVALID")
    if not valid(user, base):
        reject("TARGET_INVALID")
    if (man_user or man_base) and (man_user != user or man_base != base):
        reject("TARGET_MISMATCH")
else:
    user, base = man_user, man_base
    if not user or not base:
        reject("TARGET_UNKNOWN")
    if not valid(user, base):
        reject("TARGET_INVALID")
print(user)
print(base)
PY
)"; then
  code="${target_out//[^A-Za-z0-9_]/}"
  [[ -n "${code}" ]] || code="TARGET_INVALID"
  log_line "FAIL ${code}"
  fail_job fail "" "${code}"
fi
{ IFS= read -r target_user; IFS= read -r target_base; } <<< "${target_out}"

# --- Schrittliste aus dem Auftrag, gehalten gegen das Manifest -----------
# Der Exit-Code von python3 wird ausgewertet: ohne ihn ergaebe eine kaputte
# oder leere job.json still eine leere Liste, der Schleifenrumpf liefe nie,
# und das Skript meldete am Ende "ok" fuer einen Auftrag, der nichts tat.
if ! job_steps="$(python3 - "${CURRENT}" <<'PY'
import json, sys
job = json.load(open(sys.argv[1], encoding="utf-8"))
steps = job.get("steps")
if not isinstance(steps, list):
    raise SystemExit("job.json: 'steps' fehlt oder ist keine Liste")
for step in steps:
    print(step)
PY
)"; then
  log_line "FAIL JOB_MALFORMED"
  fail_job fail "" JOB_MALFORMED
fi
if [[ -z "${job_steps}" ]]; then
  log_line "FAIL JOB_NO_STEPS"
  fail_job fail "" JOB_NO_STEPS
fi
mapfile -t step_ids <<< "${job_steps}"

# step_known haelt eine Kennung aus job.json gegen das gepruefte Manifest.
# Ohne diese Pruefung baute eine Kennung wie "../../etc/cron.d/x" den
# Glob unten aus dem Bootstrap-Verzeichnis heraus, und root fuehrte aus,
# worauf er zeigte - E13 waere vollstaendig umgangen.
step_known() {
  local wanted="$1" known
  # shellcheck disable=SC2086 # absichtliche Wortzerlegung der Kennungsliste
  for known in ${manifest_step_ids}; do
    [[ "${known}" == "${wanted}" ]] && return 0
  done
  return 1
}

# --- Schritt 20 braucht Argumente, die schon auf dem Knoten stehen -------
# 20-mosquitto.sh verlangt --user/--password-file und scheitert sonst mit
# MOSQUITTO_ARGS_MISSING, noch vor seiner eigenen step_done-Pruefung - ein
# Re-Deploy ohne diese Argumente scheitert also selbst dann, wenn gar
# nichts zu tun waere. Beides steht bereits auf dem Knoten: der Benutzer in
# der config.json des Dashboards, das Passwort in der 0600-Datei, auf die
# sie zeigt. Root liest die Datei direkt von ihrem bekannten Pfad; sie
# fliesst nie durch job.json (Global Constraint: der Auftrag traegt keine
# Geheimnisse).
read_mqtt_config() {
  local src="$1"
  [[ -f "${src}" ]] || return 1
  python3 - "${src}" <<'PY'
import json, sys
mqtt = json.load(open(sys.argv[1], encoding="utf-8")).get("mqtt", {})
print(mqtt.get("username", ""))
print(mqtt.get("password_file", "/etc/energy-node/mqtt.pw"))
PY
}

mqtt_args=()
prepare_mqtt_args() {
  local out user password_file
  # Erst die Konfiguration des laufenden Knotens, dann - fuer die
  # Erstinstallation, bei der es sie noch nicht gibt - die Vorlage aus dem
  # geprueften Bundle, aus der Schritt 60 sie ohnehin anlegen wuerde.
  out="$(read_mqtt_config "${ROOT_PREFIX}/etc/energy-node/config.json")" \
    || out="$(read_mqtt_config "${BUNDLE}/config/config.json")" \
    || return 1
  { IFS= read -r user; IFS= read -r password_file; } <<< "${out}"
  [[ -n "${user}" && -n "${password_file}" ]] || return 1
  password_file="${ROOT_PREFIX}${password_file}"
  [[ -f "${password_file}" ]] || return 1
  mqtt_args=(--user "${user}" --password-file "${password_file}")
}

for id in "${step_ids[@]}"; do
  if [[ ! "${id}" =~ ^[0-9]+$ ]] || ! step_known "${id}"; then
    log_line "##STEP ${id} fail STEP_NOT_IN_MANIFEST"
    fail_job fail "${id}" STEP_NOT_IN_MANIFEST
  fi

  shopt -s nullglob
  matches=("${BUNDLE}/bootstrap/${id}"-*.sh)
  shopt -u nullglob
  if [[ "${#matches[@]}" -ne 1 ]]; then
    log_line "##STEP ${id} fail STEP_SCRIPT_MISSING"
    fail_job fail "${id}" STEP_SCRIPT_MISSING
  fi
  script="${matches[0]}"

  step_args=()
  if [[ "${id}" == "20" ]]; then
    if ! prepare_mqtt_args; then
      log_line "##STEP ${id} fail MQTT_CONFIG_UNREADABLE"
      fail_job fail "${id}" MQTT_CONFIG_UNREADABLE
    fi
    step_args=("${mqtt_args[@]}")
  fi

  terminal="" code=""
  while IFS= read -r line; do
    log_line "${line}"
    case "${line}" in
      "##STEP ${id} ok"*|"##STEP ${id} skip"*) terminal=ok ;;
      "##STEP ${id} fail"*)
        terminal=fail
        code="$(printf '%s\n' "${line}" | awk '{print $4}')"
        code="${code//[^A-Za-z0-9_]/}"
        ;;
    esac
    # EN_SUDO="" statt der Vorgabe "sudo" aus lib/step.sh: diese Unit laeuft
    # bereits als root, und ein zusaetzliches sudo haenge den Lauf an eine
    # gesunde sudoers-Konfiguration und strippte Umgebungsvariablen, auf die
    # einzelne Schritte bauen (DEBIAN_FRONTEND in 10-apt.sh).
  done < <(EN_STATE_DIR="${STATE_DIR}" EN_SELECTION="${STATE_DIR}/selection.json" \
            EN_BUNDLE_DIR="${BUNDLE}" EN_BUNDLE_VERSION="${bundle_version}" \
            EN_TARGET_USER="${target_user}" EN_TARGET_BASE="${target_base}" \
            EN_SUDO="" \
            bash "${script}" "${step_args[@]}" 2>&1)

  if [[ "${terminal}" != ok ]]; then
    [[ -n "${code}" ]] || code="STEP_ENDED_WITHOUT_MARKER"
    fail_job fail "${id}" "${code}"
  fi
done

# Erst jetzt gilt das Bundle als vollstaendig angewandt: plan.sh liest diese
# Kopie als "von" der Komponenten, und ein halb angewandtes Bundle darf dort
# nicht als Ausgangsstand stehen. Atomar (.tmp + mv), damit ein Abbruch
# mitten im Schreiben nie eine halbe Datei hinterlaesst. Scheitert nur das
# Ablegen, laufen die Schritte trotzdem als erledigt (sie sind idempotent);
# der Auftrag soll deshalb nicht als fehlgeschlagen gelten.
installed="${STATE_DIR}/installed-manifest.json"
if ! { install -m 0644 "${BUNDLE}/manifest.json" "${installed}.tmp" \
       && mv -f "${installed}.tmp" "${installed}"; }; then
  rm -f "${installed}.tmp"
  log_line "WARN INSTALLED_MANIFEST_FAILED"
fi

write_status "ok" "" ""
exit 0
