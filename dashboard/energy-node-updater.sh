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
# Umgebungsvariablen (Vorgaben fuer den echten Betrieb, ueberschreibbar fuer
# scripts/tests/test_updater.sh):
#   EN_UPDATER_JOB_DIR    /var/lib/energy-node-installer/job
#   EN_UPDATER_STATE_DIR  /var/lib/energy-node-installer
#   EN_UPDATER_VERIFY     /usr/local/lib/energy-node-installer/verify_bundle.sh
#   EN_UPDATER_PUBKEY     /etc/energy-node-updater/signing_key.pub.pem
set -uo pipefail

JOB_DIR="${EN_UPDATER_JOB_DIR:-/var/lib/energy-node-installer/job}"
STATE_DIR="${EN_UPDATER_STATE_DIR:-/var/lib/energy-node-installer}"
VERIFY="${EN_UPDATER_VERIFY:-/usr/local/lib/energy-node-installer/verify_bundle.sh}"
PUBKEY="${EN_UPDATER_PUBKEY:-/etc/energy-node-updater/signing_key.pub.pem}"

PENDING="${JOB_DIR}/pending.json"
CURRENT="${JOB_DIR}/current.json"
LOG="${JOB_DIR}/log"
STATUS="${JOB_DIR}/status.json"
BUNDLE="${JOB_DIR}/bundle"

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
write_status() {
  printf '{"result":"%s","step":"%s","code":"%s"}\n' "$1" "$2" "$3" > "${STATUS}"
}

# --- E13: erst die Signatur, dann die Hashes -- verify_bundle.sh selbst
# haelt diese Reihenfolge ein; wir rufen nur die persistente, schon vor
# diesem Auftrag installierte Kopie auf, nie die im Auftrag selbst
# mitgelieferte -- sonst koennte ein Angreifer mit Schreibrecht auf
# JOB_DIR seinen eigenen, immer-erfolgreichen Pruefer mitschicken.
if ! verify_out=$(bash "${VERIFY}" --bundle "${BUNDLE}" --pubkey "${PUBKEY}" --target 2>&1); then
  code="$(printf '%s\n' "${verify_out}" | tail -1 | awk '{print $2}')"
  [[ -n "${code}" ]] || code="BUNDLE_SIGNATURE_INVALID"
  log_line "REJECT ${code}"
  write_status "rejected" "" "${code}"
  exit 1
fi

manifest="${BUNDLE}/manifest.json"
bundle_version="$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1])).get("version",""))' "${manifest}")"

job_json="${CURRENT}"
target_user="$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1])).get("target_user",""))' "${job_json}")"
target_base="$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1])).get("target_base",""))' "${job_json}")"

mapfile -t step_ids < <(python3 -c 'import json,sys; [print(s) for s in json.load(open(sys.argv[1])).get("steps",[])]' "${job_json}")

for id in "${step_ids[@]}"; do
  shopt -s nullglob
  matches=("${BUNDLE}/bootstrap/${id}"-*.sh)
  shopt -u nullglob
  if [[ "${#matches[@]}" -ne 1 ]]; then
    log_line "##STEP ${id} fail STEP_SCRIPT_MISSING"
    write_status "fail" "${id}" "STEP_SCRIPT_MISSING"
    exit 1
  fi
  script="${matches[0]}"

  terminal="" code=""
  while IFS= read -r line; do
    log_line "${line}"
    case "${line}" in
      "##STEP ${id} ok"*|"##STEP ${id} skip"*) terminal=ok ;;
      "##STEP ${id} fail"*)
        terminal=fail
        code="$(printf '%s\n' "${line}" | awk '{print $4}')"
        ;;
    esac
  done < <(EN_STATE_DIR="${STATE_DIR}" EN_SELECTION="${STATE_DIR}/selection.json" \
            EN_BUNDLE_DIR="${BUNDLE}" EN_BUNDLE_VERSION="${bundle_version}" \
            EN_TARGET_USER="${target_user}" EN_TARGET_BASE="${target_base}" \
            bash "${script}" 2>&1)

  if [[ "${terminal}" != ok ]]; then
    [[ -n "${code}" ]] || code="STEP_ENDED_WITHOUT_MARKER"
    write_status "fail" "${id}" "${code}"
    exit 1
  fi
done

write_status "ok" "" ""
exit 0
