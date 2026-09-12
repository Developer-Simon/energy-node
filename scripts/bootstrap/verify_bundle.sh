#!/usr/bin/env bash
#
# Prueft ein entpacktes Bundle (E13 der Spec).
#
# Bewusst KEIN Schritt: kein ##STEP-Marker, kein Stempel, kein step.sh.
# Benutzt wird es von 50-node-install.sh, 60-node-install.sh, plan.sh und
# spaeter vom Installer und der Updater-Unit - alle brauchen dieselbe
# Pruefung, aber keiner von ihnen ist "Schritt verify".
#
# Reihenfolge ist Absicht: erst die Signatur ueber manifest.json, dann die
# Hashes GEGEN dieses geprueft Manifest. Andersherum prueft man Hashes
# gegen eine Liste, die der Angreifer geschrieben hat.
#
# Usage:
#   verify_bundle.sh --bundle <dir> --pubkey <datei> [--target]
#   verify_bundle.sh --bundle <dir> --target-only
#
#   --target       zusaetzlich Architektur und Python-ABI des laufenden
#                  Systems gegen das Manifest pruefen.
#   --target-only  NUR diese Pruefung, ohne Signatur und ohne Schluessel.
#                  Das ist die Form, die ein einzelner Schritt auf dem Node
#                  benutzt: Signatur und Hashes prueft, wer das Bundle
#                  entgegennimmt (Installer, Updater), nicht jeder Schritt
#                  aufs Neue.
#
# Ausgabe: "OK" und Exit 0, oder "FEHLER <CODE>" als letzte Zeile und Exit 1.
set -euo pipefail

BUNDLE=""
PUBKEY=""
CHECK_TARGET=false
TARGET_ONLY=false
while [[ $# -gt 0 ]]; do
  case "$1" in
    --bundle) BUNDLE="${2:-}"; shift 2 ;;
    --pubkey) PUBKEY="${2:-}"; shift 2 ;;
    --target) CHECK_TARGET=true; shift ;;
    --target-only) CHECK_TARGET=true; TARGET_ONLY=true; shift ;;
    *) shift ;;
  esac
done

die() { printf 'FEHLER %s\n' "$1"; exit 1; }

manifest="${BUNDLE}/manifest.json"
signature="${manifest}.sig"
[[ -n "${BUNDLE}" && -f "${manifest}" ]] || die BUNDLE_MANIFEST_MISSING

if [[ "${TARGET_ONLY}" != true ]]; then
  [[ -f "${signature}" ]] || die BUNDLE_MANIFEST_MISSING
  [[ -n "${PUBKEY}" && -f "${PUBKEY}" ]] || die BUNDLE_SIGNATURE_INVALID

  # -rawin ist fuer ed25519 Pflicht: der Algorithmus signiert die Nachricht
  # selbst und nicht deren Digest.
  openssl pkeyutl -verify -pubin -inkey "${PUBKEY}" -rawin \
    -in "${manifest}" -sigfile "${signature}" >/dev/null 2>&1 \
    || die BUNDLE_SIGNATURE_INVALID

python3 - "${BUNDLE}" <<'PY' || die BUNDLE_HASH_MISMATCH
import hashlib, json, pathlib, sys
root = pathlib.Path(sys.argv[1])
files = json.loads((root / "manifest.json").read_text(encoding="utf-8")).get("files", {})
if not isinstance(files, dict) or not files:
    print("manifest.json: 'files' fehlt oder ist leer", file=sys.stderr)
    sys.exit(1)
bad = 0
for rel, want in sorted(files.items()):
    path = root / rel
    if not path.is_file():
        print("fehlt: %s" % rel, file=sys.stderr)
        bad += 1
        continue
    got = hashlib.sha256(path.read_bytes()).hexdigest()
    if got != want:
        print("verfaelscht: %s" % rel, file=sys.stderr)
        bad += 1
sys.exit(1 if bad else 0)
PY
fi

if [[ "${CHECK_TARGET}" == true ]]; then
  python3 - "${BUNDLE}" <<'PY' || exit_code=$?
import json, pathlib, platform, sys
root = pathlib.Path(sys.argv[1])
manifest = json.loads((root / "manifest.json").read_text(encoding="utf-8"))
machines = manifest.get("uname_machine") or []
if platform.machine() not in machines:
    print("Architektur %s, Bundle erwartet %s" % (platform.machine(), ", ".join(machines)),
          file=sys.stderr)
    sys.exit(2)
# Dieselbe Schreibweise wie `pip download --abi`: cp311, nicht cpython-311.
abi = "cp%d%d" % sys.version_info[:2]
want = manifest.get("python_abi", "")
if want and abi != want:
    print("Python-ABI %s, Bundle erwartet %s" % (abi, want), file=sys.stderr)
    sys.exit(3)
PY
  case "${exit_code:-0}" in
    0) ;;
    2) die ARCH_MISMATCH ;;
    3) die PYTHON_ABI_MISMATCH ;;
    *) die BUNDLE_MANIFEST_MISSING ;;
  esac
fi

printf 'OK\n'
