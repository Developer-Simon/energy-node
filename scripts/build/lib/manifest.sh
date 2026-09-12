#!/usr/bin/env bash
#
# Bundle-Manifest schreiben und signieren (E13).
#
# make_bundle.sh legt manifest.head.json mit allen Feldern ausser "files" ab;
# write_bundle_manifest ergaenzt die SHA-256-Summen und macht daraus
# manifest.json. Die Trennung haelt das Hashen frei von allem, was der
# Aufrufer sonst noch weiss.

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
