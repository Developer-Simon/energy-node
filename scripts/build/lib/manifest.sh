#!/usr/bin/env bash
#
# Bundle-Manifest schreiben und signieren (E13).
#
# make_bundle.sh legt manifest.head.json mit allen Feldern ausser "files" ab;
# write_bundle_manifest ergaenzt die SHA-256-Summen und macht daraus
# manifest.json. Die Trennung haelt das Hashen frei von allem, was der
# Aufrufer sonst noch weiss.
#
# Dazu bekommt jeder Schritt, dessen Skript seine Eingaben nennt, einen
# Fingerabdruck (steps[].fingerprint). Das Skript nennt sie in Zeilen der Form
#   # step-inputs: bootstrap/lib/render.sh dashboard/ manifest:caddy
# Ein Pfad mit "/" am Ende steht fuer alles darunter, "manifest:<feld>" fuer
# ein Feld des Manifests, das Skript selbst zaehlt immer mit. Der Stempel
# eines Schritts gilt, solange der Fingerabdruck gleich bleibt (lib/step.sh),
# so laeuft ein Update nur die Schritte, deren Eingaben sich geaendert haben.
# Ein Skript ohne step-inputs bekommt keinen Fingerabdruck und laeuft wie
# bisher einmal je Bundle-Version.

# write_bundle_manifest <bundle-dir>
write_bundle_manifest() {
  local bundle="$1"
  if [[ ! -f "${bundle}/manifest.head.json" ]]; then
    echo "${bundle}/manifest.head.json fehlt." >&2
    return 1
  fi
  python3 - "${bundle}" <<'PY'
import hashlib, json, pathlib, sys

root = pathlib.Path(sys.argv[1])
head_path = root / "manifest.head.json"
manifest = json.loads(head_path.read_text(encoding="utf-8"))

# Das Manifest kann sich nicht selbst hashen - genau diese drei Namen laesst
# verify_bundle.sh deshalb auch aus.
skip = {"manifest.json", "manifest.json.sig", "manifest.head.json"}

files = {}
for path in sorted(root.rglob("*")):
    if not path.is_file():
        continue
    rel = path.relative_to(root).as_posix()
    if rel in skip:
        continue
    files[rel] = hashlib.sha256(path.read_bytes()).hexdigest()

if not files:
    sys.exit("%s: keine Dateien zum Hashen gefunden" % root)

manifest["files"] = files


def step_inputs(script):
    inputs = []
    for line in script.read_text(encoding="utf-8").splitlines():
        if line.startswith("# step-inputs:"):
            inputs.extend(line.split(":", 1)[1].split())
    return inputs


def fingerprint(script_rel, inputs):
    lines = ["%s %s" % (script_rel, files[script_rel])]
    for item in inputs:
        if item.startswith("manifest:"):
            value = manifest.get(item.split(":", 1)[1])
            lines.append("%s %s" % (item, json.dumps(value, sort_keys=True)))
            continue
        matched = sorted(rel for rel in files if rel == item or (item.endswith("/") and rel.startswith(item)))
        if not matched:
            sys.exit("%s: step-inputs %s passt auf keine Datei im Bundle" % (script_rel, item))
        lines.extend("%s %s" % (rel, files[rel]) for rel in matched)
    return hashlib.sha256("\n".join(lines).encode("utf-8")).hexdigest()


for step in manifest.get("steps") or []:
    scripts = sorted(root.glob("bootstrap/%s-*.sh" % step.get("id")))
    if len(scripts) != 1:
        continue
    script_rel = scripts[0].relative_to(root).as_posix()
    inputs = step_inputs(scripts[0])
    if inputs:
        step["fingerprint"] = fingerprint(script_rel, inputs)

(root / "manifest.json").write_text(
    json.dumps(manifest, indent=2, ensure_ascii=False) + "\n", encoding="utf-8"
)
head_path.unlink()
print("manifest.json: %d Dateien" % len(files))
PY
}

# sign_bundle_manifest <bundle-dir> <privkey>
#
# -rawin ist fuer ed25519 Pflicht: der Algorithmus signiert die Nachricht
# selbst, nicht deren Digest. Der private Schluessel lebt im CI-Secret des
# Release-Jobs und sonst nirgends.
sign_bundle_manifest() {
  local bundle="$1" key="$2"
  if [[ ! -f "${bundle}/manifest.json" ]]; then
    echo "${bundle}/manifest.json fehlt - erst write_bundle_manifest." >&2
    return 1
  fi
  if [[ ! -f "${key}" ]]; then
    echo "Signaturschluessel ${key} fehlt." >&2
    return 1
  fi
  openssl pkeyutl -sign -inkey "${key}" -rawin \
    -in "${bundle}/manifest.json" -out "${bundle}/manifest.json.sig"
}
