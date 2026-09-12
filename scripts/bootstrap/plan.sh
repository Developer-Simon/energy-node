#!/usr/bin/env bash
#
# Vorschau: was wuerde ein Lauf tun?
#
# Liest die Schrittliste aus dem Bundle-Manifest, die vorhandenen Stempel und
# die Auswahl und meldet je Schritt done/pending/deselected sowie je
# Komponente von/nach. Gibt JSON aus und KEINE ##STEP-Marker - dies ist kein
# Schritt, sondern ein Bericht.
set -euo pipefail
# shellcheck source=scripts/bootstrap/lib/step.sh
source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/lib/step.sh"

if [[ ! -f "${EN_BUNDLE_DIR}/manifest.json" ]]; then
  printf 'FEHLER BUNDLE_MANIFEST_MISSING\n'
  exit 1
fi

EN_STATE_DIR="${EN_STATE_DIR}" \
EN_BUNDLE_DIR="${EN_BUNDLE_DIR}" \
EN_BUNDLE_VERSION="${EN_BUNDLE_VERSION}" \
EN_SELECTION="${EN_SELECTION}" \
python3 <<'PY'
import json, os, pathlib, sys

state = pathlib.Path(os.environ["EN_STATE_DIR"])
bundle = pathlib.Path(os.environ["EN_BUNDLE_DIR"])
version = os.environ["EN_BUNDLE_VERSION"]

manifest = json.loads((bundle / "manifest.json").read_text(encoding="utf-8"))

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

steps = []
for entry in manifest.get("steps", []):
    step_id = str(entry.get("id"))
    optional = bool(entry.get("optional"))
    selected = bool(selection.get(step_id, True)) if optional else True
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
