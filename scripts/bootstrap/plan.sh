#!/usr/bin/env bash
#
# Vorschau: was wuerde ein Lauf tun?
#
# Liest die Schrittliste aus dem Bundle-Manifest, die vorhandenen Stempel und
# die Auswahl und meldet je Schritt done/pending/deselected sowie je
# Komponente von/nach. Gibt JSON aus und KEINE ##STEP-Marker - dies ist kein
# Schritt, sondern ein Bericht.
set -euo pipefail
SCRIPT_LIB_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/lib"
# shellcheck source=scripts/bootstrap/lib/step.sh
source "${SCRIPT_LIB_DIR}/step.sh"

if [[ ! -f "${EN_BUNDLE_DIR}/manifest.json" ]]; then
  printf 'FEHLER BUNDLE_MANIFEST_MISSING\n'
  exit 1
fi

EN_STATE_DIR="${EN_STATE_DIR}" \
EN_BUNDLE_DIR="${EN_BUNDLE_DIR}" \
EN_BUNDLE_VERSION="${EN_BUNDLE_VERSION}" \
EN_SELECTION="${EN_SELECTION}" \
EN_PLAN_LIB_DIR="${SCRIPT_LIB_DIR}" \
python3 <<'PY'
import json, os, pathlib, sys

state = pathlib.Path(os.environ["EN_STATE_DIR"])
bundle = pathlib.Path(os.environ["EN_BUNDLE_DIR"])
version = os.environ["EN_BUNDLE_VERSION"]

sys.path.insert(0, os.environ["EN_PLAN_LIB_DIR"])
import restart_rule

manifest = json.loads((bundle / "manifest.json").read_text(encoding="utf-8"))

installed_doc = None
_installed_path = state / "installed-manifest.json"
if _installed_path.is_file():
    try:
        installed_doc = json.loads(_installed_path.read_text(encoding="utf-8"))
    except ValueError:
        installed_doc = None

# Auswahl: fehlende Datei oder nicht genannter Schritt = gewaehlt.
selection = {}
sel_path = pathlib.Path(os.environ["EN_SELECTION"])
if sel_path.is_file():
    try:
        selection = json.loads(sel_path.read_text(encoding="utf-8")).get("steps") or {}
    except ValueError as exc:
        sys.exit("selection.json nicht lesbar: %s" % exc)

def stamped(step_id):
    """Wie step_done: ein Stempel einer anderen Bundle-Version zaehlt nicht."""
    path = state / "steps" / str(step_id)
    if not path.is_file():
        return False
    return ("bundle=%s" % version) in path.read_text(encoding="utf-8").splitlines()

def chosen(entry):
    if not entry.get("optional"):
        return True
    # Nicht genannt = Manifest-Vorgabe: "an" fuer die ueblichen optionalen
    # Schritte (E7), "aus" fuer Opt-in-Schritte wie 35 (step_opted_in).
    return bool(selection.get(str(entry.get("id")), entry.get("default", True)))

by_id = {str(entry.get("id")): entry for entry in manifest.get("steps", [])}

steps = []
for entry in manifest.get("steps", []):
    step_id = str(entry.get("id"))
    optional = bool(entry.get("optional"))
    selected = chosen(entry)
    # requires: ohne den benoetigten Schritt laeuft dieser nicht (35 -> 83).
    needed = entry.get("requires")
    if selected and needed and not chosen(by_id.get(str(needed), {"optional": True, "default": False})):
        selected = False
    if not selected:
        state_name = "deselected"
    elif stamped(step_id):
        state_name = "done"
    else:
        state_name = "pending"
    item = {"id": step_id, "optional": optional, "selected": selected, "state": state_name}
    for extra in ("service_id", "dir", "unit"):
        if entry.get(extra):
            item[extra] = entry[extra]
    if entry.get("dir"):
        old = next((s for s in (installed_doc or {}).get("steps") or [] if str(s.get("id")) == step_id), None)
        item["von"] = (old or {}).get("version")
        item["nach"] = entry.get("version")
        if state_name == "pending":
            item["restart"] = restart_rule.restart_reason(manifest, installed_doc, step_id)
    steps.append(item)

# von: das Manifest des zuletzt vollstaendig angewandten Bundles. Diese
# Kopie legt der Installer ab; ohne sie ist jedes "von" null.
previous = {}
installed = state / "installed-manifest.json"
if installed.is_file():
    try:
        previous = json.loads(installed.read_text(encoding="utf-8")).get("components") or {}
    except ValueError:
        previous = {}

components = {
    name: {"von": previous.get(name), "nach": value}
    for name, value in (manifest.get("components") or {}).items()
}

print(json.dumps({
    "bundle_version": manifest.get("version", version),
    "steps": steps,
    "components": components,
}, indent=2, ensure_ascii=False))
PY
