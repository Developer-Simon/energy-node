#!/usr/bin/env bash
#
# Schritt 65: schreibt den Block installed_services in die config.json des
# Dashboards, aus der Dienstauswahl in selection.json (E7 der Spec).
#
# Kern-Schritt, aber ohne die uebliche Skip-Pruefung: die Auswahl kann sich
# bei einem Re-Deploy ("Dienste aendern") aendern, ohne dass sich die
# Bundle-Version aendert, und der Block muss das bei jedem Lauf widerspiegeln
# - deshalb prueft dieser Schritt (anders als die anderen Kern-Schritte)
# seinen eigenen Stempel nie und schreibt immer. Er setzt am Ende trotzdem
# step_ok statt eines bloss lokalen "ok"-Markers: der Stempel selbst hat
# hier keine Skip-Wirkung (das oben beschriebene "nie skip" bleibt), aber
# plan.sh (Bericht fuer die Installer-Oberflaeche) liest denselben Stempel,
# um done/pending zu melden - ohne ihn bliebe dieser Schritt dort fuer immer
# "pending", obwohl er jedes Mal erfolgreich lief.
set -euo pipefail
# shellcheck source=scripts/bootstrap/lib/step.sh
source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/lib/step.sh"

step_begin 65

cfg="${EN_ROOT}/etc/energy-node/config.json"
manifest="${EN_BUNDLE_DIR}/manifest.json"

[[ -f "${cfg}" ]] || step_fail "CONFIG_JSON_MISSING"
[[ -f "${manifest}" ]] || step_fail "MANIFEST_MISSING"

# Eine vorhandene, aber kaputte selection.json faellt step_selected auf "nicht
# gewaehlt" zurueck (siehe lib/step.sh) - fuer die meisten Schritte harmlos,
# hier aber gefaehrlich: das schaltete bei einem Tippfehler in der Auswahl
# stillschweigend jeden Dienst als "nicht installiert" ein. Deshalb prueft
# dieser Schritt die Datei vorab selbst und bricht laut ab, statt die
# Fehlinterpretation von step_selected zu erben (E7).
if [[ -f "${EN_SELECTION}" ]] && ! python3 -c "import json,sys; json.load(open(sys.argv[1]))" "${EN_SELECTION}" 2>/dev/null; then
  step_fail SELECTION_UNREADABLE
fi

# step_selected liest EN_SELECTION selbst (lib/step.sh); wir reichen fuer
# jeden Schritt mit dashboard_key nur die Ja/Nein-Antwort als Umgebung durch,
# damit der Python-Teil unten keine zweite Auswahl-Logik braucht.
#
# Der Python-Teil landet zuerst in einer Datei statt in einer
# Prozesssubstitution: "done < <(python3 ...)" liefe in einer eigenen
# Subshell, deren Exit-Code set -euo pipefail nie zu sehen bekommt - ein
# kaputtes manifest.json ergaebe dann still null Zeilen statt eines
# Fehlschlags.
manifest_rows="$(mktemp)"
new_cfg="$(mktemp)"
trap 'rm -f "${manifest_rows}" "${new_cfg}"' EXIT
python3 - "${manifest}" > "${manifest_rows}" <<'PY' || step_fail MANIFEST_PARSE_FAILED
import json, sys
manifest = json.loads(open(sys.argv[1], encoding="utf-8").read())
for step in manifest.get("steps", []):
    key = step.get("dashboard_key")
    if key:
        print("%s\t%s" % (step["id"], key))
PY

selected_rows=""
while IFS=$'\t' read -r step_id dashboard_key; do
  [[ -n "${step_id}" ]] || continue
  if step_selected "${step_id}"; then
    selected="true"
  else
    selected="false"
  fi
  selected_rows+="${dashboard_key}"$'\t'"${selected}"$'\n'
done < "${manifest_rows}"

# Das neue JSON entsteht in einer Datei des Aufrufers, nicht neben der
# config.json: der Installer fuehrt diesen Schritt als SSH-Benutzer aus, und
# /etc/energy-node gehoert root:<Benutzer> mit 0755 - dort laesst sich keine
# Temp-Datei anlegen.
SELECTED_ROWS="${selected_rows}" python3 - "${cfg}" > "${new_cfg}" <<'PY' || step_fail CONFIG_WRITE_FAILED
import json, os, sys

doc = json.loads(open(sys.argv[1], encoding="utf-8").read())

installed = {}
for row in os.environ["SELECTED_ROWS"].splitlines():
    if not row.strip():
        continue
    key, selected = row.split("\t")
    installed[key] = selected == "true"

doc["installed_services"] = installed
json.dump(doc, sys.stdout, indent=2, sort_keys=True, ensure_ascii=False)
sys.stdout.write("\n")
PY

# Ersetzt wird ueber sudo: cp -p uebernimmt Besitzer und Rechte der alten
# Datei (root:<Benutzer> 0664 - das Dashboard bearbeitet sie live und muss
# weiter schreiben duerfen), tee legt den neuen Inhalt hinein, mv tauscht sie
# atomar aus.
"${SUDO[@]}" cp -p "${cfg}" "${cfg}.tmp" || step_fail CONFIG_WRITE_FAILED
if ! "${SUDO[@]}" tee "${cfg}.tmp" < "${new_cfg}" >/dev/null; then
  "${SUDO[@]}" rm -f "${cfg}.tmp"
  step_fail CONFIG_WRITE_FAILED
fi
"${SUDO[@]}" mv "${cfg}.tmp" "${cfg}" || step_fail CONFIG_WRITE_FAILED

# Ohne Neustart bliebe ein bei diesem Lauf abgewaehlter Dienst im laufenden
# Prozess sichtbar, bis irgendwann etwas anderes das Dashboard neu startet -
# das Akzeptanzkriterium ("Tab verschwindet ohne Handarbeit an config.json")
# waere sonst nur auf dem Papier erfuellt. try-restart statt restart: ist der
# Dienst bewusst gestoppt (oder, ganz am Anfang, noch nie gestartet), soll
# dieser Schritt ihn nicht erst hochziehen. "|| true": ein Neustart-Fehler
# darf Schritt 65 nicht scheitern lassen, die config.json ist bereits
# geschrieben.
"${SUDO[@]}" systemctl try-restart "${BINARY_NAME:-energy-node-dashboard}.service" 2>/dev/null || true

step_log "installed_services aktualisiert."
step_ok
