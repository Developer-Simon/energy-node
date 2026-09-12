#!/usr/bin/env bash
# shellcheck disable=SC1090  # Testrahmen sourct absichtlich einen dynamischen Pfad, um die Bibliotheksfunktionen im selben Prozess zu pruefen
# Test for scripts/bootstrap/lib/step.sh
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
lib="$here/../bootstrap/lib/step.sh"

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
fail() { echo "FAIL: $1"; [ -n "${2:-}" ] && printf '%s\n' "$2"; exit 1; }

export EN_STATE_DIR="$tmp/state"
export EN_BUNDLE_VERSION="v1.0.0"
export EN_SUDO=""

# --- begin/ok schreiben Marker und Stempel ---------------------------------
out="$( set -euo pipefail; source "$lib"; step_begin 10; step_ok )"
[ "$out" = "##STEP 10 begin
##STEP 10 ok" ] || fail "unerwartete Marker" "$out"
[ -f "$tmp/state/steps/10" ] || fail "kein Stempel geschrieben"
grep -qx "bundle=v1.0.0" "$tmp/state/steps/10" || fail "Bundle-Version fehlt im Stempel"

# --- step_done erkennt den Stempel nur bei gleicher Bundle-Version ---------
( source "$lib"; step_done 10 ) || fail "step_done erkennt eigenen Stempel nicht"
( EN_BUNDLE_VERSION=v2.0.0; source "$lib"; step_done 10 ) && fail "step_done ignoriert Versionswechsel"
( source "$lib"; step_done 99 ) && fail "step_done meldet fremden Schritt als erledigt"

# --- skip und fail ---------------------------------------------------------
out="$( set -euo pipefail; source "$lib"; step_begin 40; step_skip "login ausstehend" )"
[ "${out##*$'\n'}" = "##STEP 40 skip login ausstehend" ] || fail "skip-Marker falsch" "$out"

set +e
out="$( source "$lib"; step_begin 50; step_fail PIP_EXTERNALLY_MANAGED )"
rc=$?
set -e
[ "$rc" -eq 1 ] || fail "step_fail beendet nicht mit 1" "$rc"
[ "${out##*$'\n'}" = "##STEP 50 fail PIP_EXTERNALLY_MANAGED" ] || fail "fail-Marker falsch" "$out"
[ -f "$tmp/state/steps/50" ] && fail "fehlgeschlagener Schritt hat einen Stempel hinterlassen"

# --- step_log landet nicht als Marker --------------------------------------
out="$( source "$lib"; step_log "hallo" )"
case "$out" in '##STEP'*) fail "step_log sieht aus wie ein Marker" "$out" ;; esac

# --- SUDO ist ein Array und bei EN_SUDO="" leer ----------------------------
n="$( EN_SUDO="" bash -c 'source "$1"; echo "${#SUDO[@]}"' _ "$lib" )"
[ "$n" = 0 ] || fail "SUDO nicht leer bei EN_SUDO=''" "$n"
n="$( EN_SUDO="sudo" bash -c 'source "$1"; echo "${SUDO[0]}"' _ "$lib" )"
[ "$n" = sudo ] || fail "SUDO-Vorgabe falsch" "$n"

# --- ohne selection.json ist jeder Schritt gewaehlt ------------------------
export EN_SELECTION="$tmp/selection.json"
( source "$lib"; step_selected 40 ) || fail "ohne Datei nicht gewaehlt"

# --- ein nicht genannter Schritt bleibt gewaehlt ---------------------------
printf '{"steps":{"70":false}}\n' > "$EN_SELECTION"
( source "$lib"; step_selected 40 ) || fail "nicht genannter Schritt abgewaehlt"
( source "$lib"; step_selected 70 ) && fail "abgewaehlter Schritt gilt als gewaehlt"
printf '{"steps":{"70":true}}\n' > "$EN_SELECTION"
( source "$lib"; step_selected 70 ) || fail "true wurde nicht als gewaehlt gelesen"

# --- kaputtes JSON ist nicht stillschweigend "alles an" -------------------
printf 'kein json\n' > "$EN_SELECTION"
set +e
( source "$lib"; step_selected 70 ) 2>/dev/null
rc=$?
set -e
[ "$rc" -ne 0 ] || fail "kaputte selection.json galt als gewaehlt"
rm -f "$EN_SELECTION"
unset EN_SELECTION

# --- EN_TARGET_BASE faellt auf HOME zurueck, bleibt aber ueberschreibbar ---
n="$( HOME=/home/pruef bash -c 'source "$1"; echo "$EN_TARGET_BASE"' _ "$lib" )"
[ "$n" = /home/pruef ] || fail "EN_TARGET_BASE nicht aus HOME" "$n"
n="$( EN_TARGET_BASE=/opt/en bash -c 'source "$1"; echo "$EN_TARGET_BASE"' _ "$lib" )"
[ "$n" = /opt/en ] || fail "EN_TARGET_BASE nicht ueberschreibbar" "$n"

# --- EN_TARGET_USER faellt auf den laufenden Benutzer zurueck -------------
n="$( bash -c 'source "$1"; echo "$EN_TARGET_USER"' _ "$lib" )"
[ "$n" = "$(id -un)" ] || fail "EN_TARGET_USER nicht aus id -un" "$n"
n="$( EN_TARGET_USER=energynode bash -c 'source "$1"; echo "$EN_TARGET_USER"' _ "$lib" )"
[ "$n" = energynode ] || fail "EN_TARGET_USER nicht ueberschreibbar" "$n"

echo "OK: $(basename "$0")"
