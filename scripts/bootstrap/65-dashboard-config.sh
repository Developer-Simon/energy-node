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
source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/lib/step.sh"

step_begin 65

cfg="${EN_ROOT}/etc/energy-node/config.json"
manifest="${EN_BUNDLE_DIR}/manifest.json"

[[ -f "${cfg}" ]] || step_fail "CONFIG_JSON_MISSING"
[[ -f "${manifest}" ]] || step_fail "MANIFEST_MISSING"

# step_selected liest EN_SELECTION selbst (lib/step.sh); wir reichen fuer
# jeden Schritt mit dashboard_key nur die Ja/Nein-Antwort als Umgebung durch,
# damit der Python-Teil unten keine zweite Auswahl-Logik braucht.
selected_rows=""
while IFS=$'\t' read -r step_id dashboard_key; do
  [[ -n "${step_id}" ]] || continue
  if step_selected "${step_id}"; then
    selected="true"
  else
    selected="false"
  fi
  selected_rows+="${dashboard_key}"$'\t'"${selected}"$'\n'
done < <(python3 - "${manifest}" <<'PY'
import json, sys
manifest = json.loads(open(sys.argv[1], encoding="utf-8").read())
for step in manifest.get("steps", []):
    key = step.get("dashboard_key")
    if key:
        print("%s\t%s" % (step["id"], key))
PY
)

SELECTED_ROWS="${selected_rows}" python3 - "${cfg}" <<'PY'
import json, os, sys

path = sys.argv[1]
doc = json.loads(open(path, encoding="utf-8").read())

installed = {}
for row in os.environ["SELECTED_ROWS"].splitlines():
    if not row.strip():
        continue
    key, selected = row.split("\t")
    installed[key] = selected == "true"

doc["installed_services"] = installed
with open(path, "w", encoding="utf-8") as handle:
    json.dump(doc, handle, indent=2, sort_keys=True, ensure_ascii=False)
    handle.write("\n")
PY

step_log "installed_services aktualisiert."
printf '##STEP %s ok\n' "65"
