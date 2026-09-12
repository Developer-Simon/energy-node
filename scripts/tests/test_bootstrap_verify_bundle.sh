#!/usr/bin/env bash
# Test for scripts/bootstrap/verify_bundle.sh
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
script="$here/../bootstrap/verify_bundle.sh"

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
fail() { echo "FAIL: $1"; [ -n "${2:-}" ] && printf '%s\n' "$2"; exit 1; }

# Echtes ed25519-Schluesselpaar - openssl 3 liegt auf jedem Raspberry Pi OS
# und auf ubuntu-latest, also wird hier nichts nachgebaut.
openssl genpkey -algorithm ed25519 -out "$tmp/key.pem" 2>/dev/null
openssl pkey -in "$tmp/key.pem" -pubout -out "$tmp/pub.pem" 2>/dev/null

bundle="$tmp/bundle"
mkdir -p "$bundle/bootstrap"
printf 'echo hallo\n' > "$bundle/bootstrap/10-apt.sh"

# write_manifest <machine-json> <abi> -- schreibt manifest.json samt Hash
# der einen Bundle-Datei und signiert es.
write_manifest() {
  python3 - "$bundle" "$1" "$2" <<'PY'
import hashlib, json, pathlib, sys
root = pathlib.Path(sys.argv[1])
target = root / "bootstrap" / "10-apt.sh"
digest = hashlib.sha256(target.read_bytes()).hexdigest()
manifest = {
    "version": "v1.0.0",
    "arch": "armv6",
    "uname_machine": json.loads(sys.argv[2]),
    "python_minor": "3.11",
    "python_abi": sys.argv[3],
    "files": {"bootstrap/10-apt.sh": digest},
}
(root / "manifest.json").write_text(json.dumps(manifest, indent=2), encoding="utf-8")
PY
  openssl pkeyutl -sign -inkey "$tmp/key.pem" -rawin \
    -in "$bundle/manifest.json" -out "$bundle/manifest.json.sig"
}

run() { bash "$script" --bundle "$bundle" --pubkey "$tmp/pub.pem" "$@"; }

# Das ABI-Tag des laufenden Interpreters, in der Schreibweise, die auch
# `pip download --abi` erwartet. Nicht aus SOABI ableiten: das liefert
# "cpython-311-x86_64-linux-gnu", nicht "cp311".
abi="$(python3 -c 'import sys; print("cp%d%d" % sys.version_info[:2])')"
machine="$(uname -m)"

# --- heiles Bundle -------------------------------------------------------
write_manifest "[\"$machine\"]" "$abi"
out="$(run)" || fail "heiles Bundle abgelehnt" "$out"
[ "${out##*$'\n'}" = OK ] || fail "kein OK" "$out"

# --- --target akzeptiert die laufende Maschine ---------------------------
out="$(run --target)" || fail "--target lehnt die eigene Maschine ab" "$out"

# --- verfaelschte Datei -> Hash-Fehler -----------------------------------
printf 'echo boese\n' >> "$bundle/bootstrap/10-apt.sh"
set +e
out="$(run)"; rc=$?
set -e
[ "$rc" -eq 1 ] || fail "verfaelschte Datei nicht abgelehnt" "$rc"
[ "${out##*$'\n'}" = "FEHLER BUNDLE_HASH_MISMATCH" ] || fail "falscher Code" "$out"

# --- verfaelschtes Manifest -> Signaturfehler, und zwar VOR dem Hash -----
# Das Manifest wird passend zur veraenderten Datei neu geschrieben, aber
# nicht neu signiert: waere die Reihenfolge falsch herum, meldete das
# Skript hier "alles in Ordnung".
write_manifest "[\"$machine\"]" "$abi"
printf 'x' >> "$bundle/manifest.json"
set +e
out="$(run)"; rc=$?
set -e
[ "$rc" -eq 1 ] || fail "verfaelschtes Manifest nicht abgelehnt" "$rc"
[ "${out##*$'\n'}" = "FEHLER BUNDLE_SIGNATURE_INVALID" ] || fail "falscher Code" "$out"

# --- fremde Architektur --------------------------------------------------
printf 'echo hallo\n' > "$bundle/bootstrap/10-apt.sh"
write_manifest '["sparc64"]' "$abi"
out="$(run)" || fail "ohne --target darf die Architektur egal sein" "$out"
set +e
out="$(run --target)"; rc=$?
set -e
[ "$rc" -eq 1 ] || fail "fremde Architektur nicht abgelehnt" "$rc"
[ "${out##*$'\n'}" = "FEHLER ARCH_MISMATCH" ] || fail "falscher Code" "$out"

# --- fremdes ABI ---------------------------------------------------------
write_manifest "[\"$machine\"]" cp36
set +e
out="$(run --target)"; rc=$?
set -e
[ "$rc" -eq 1 ] || fail "fremdes ABI nicht abgelehnt" "$rc"
[ "${out##*$'\n'}" = "FEHLER PYTHON_ABI_MISMATCH" ] || fail "falscher Code" "$out"

# --- --target-only braucht keinen Schluessel -----------------------------
# Die Signatur ist hier gueltig, aber der Schluessel wird gar nicht erst
# uebergeben: der Schritt auf dem Node soll ohne ihn laufen koennen.
set +e
out="$(bash "$script" --bundle "$bundle" --target-only)"; rc=$?
set -e
[ "$rc" -eq 1 ] || fail "--target-only uebersah das fremde ABI" "$rc"
[ "${out##*$'\n'}" = "FEHLER PYTHON_ABI_MISMATCH" ] || fail "falscher Code" "$out"

write_manifest "[\"$machine\"]" "$abi"
out="$(bash "$script" --bundle "$bundle" --target-only)" \
  || fail "--target-only lehnt das eigene Geraet ab" "$out"
[ "${out##*$'\n'}" = OK ] || fail "--target-only ohne OK" "$out"

# --- --target-only prueft die Signatur NICHT -----------------------------
# Ein zerschossenes manifest.json.sig darf den Schritt auf dem Node nicht
# aufhalten; dafuer ist der Installer zustaendig.
printf 'kaputt' > "$bundle/manifest.json.sig"
out="$(bash "$script" --bundle "$bundle" --target-only)" \
  || fail "--target-only stolpert ueber die Signatur" "$out"

# --- fehlendes Manifest --------------------------------------------------
rm -f "$bundle/manifest.json"
set +e
out="$(run)"; rc=$?
set -e
[ "$rc" -eq 1 ] || fail "fehlendes Manifest nicht gemeldet" "$rc"
[ "${out##*$'\n'}" = "FEHLER BUNDLE_MANIFEST_MISSING" ] || fail "falscher Code" "$out"

# --- kein ##STEP-Marker in der Ausgabe -----------------------------------
grep -q '^##STEP' <<<"$out" && fail "verify_bundle gibt Schritt-Marker aus" "$out"

echo "OK: $(basename "$0")"
