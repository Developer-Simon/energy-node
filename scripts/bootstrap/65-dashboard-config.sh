#!/usr/bin/env bash
#
# Schritt 65: schreibt den Block installed_services in die config.json des
# Dashboards, aus der Dienstauswahl in selection.json (E7 der Spec).
#
# Kern-Schritt, aber ohne die uebliche Stempel/skip-Pruefung: die Auswahl
# kann sich bei einem Re-Deploy ("Dienste aendern") aendern, ohne dass sich
# die Bundle-Version aendert, und der Block muss das bei jedem Lauf
# widerspiegeln - deshalb schreibt dieser Schritt immer, nie skip.
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
trap 'rm -f "${manifest_rows}"' EXIT
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

SELECTED_ROWS="${selected_rows}" python3 - "${cfg}" <<'PY'
import json, os, sys

path = sys.argv[1]
tmp_path = path + ".tmp"
doc = json.loads(open(path, encoding="utf-8").read())

installed = {}
for row in os.environ["SELECTED_ROWS"].splitlines():
    if not row.strip():
        continue
    key, selected = row.split("\t")
    installed[key] = selected == "true"

doc["installed_services"] = installed
with open(tmp_path, "w", encoding="utf-8") as handle:
    json.dump(doc, handle, indent=2, sort_keys=True, ensure_ascii=False)
    handle.write("\n")
os.replace(tmp_path, path)
PY

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
printf '##STEP %s ok\n' "65"
