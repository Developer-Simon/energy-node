#!/usr/bin/env bash
# Test for scripts/bootstrap/lib/wheels.sh (Schritt 50 holt fehlende Wheels)
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
lib="$here/../bootstrap/lib/wheels.sh"

tmp="$(mktemp -d)"
srv_pid=""
cleanup() {
  if [ -n "$srv_pid" ]; then kill "$srv_pid" 2>/dev/null || true; fi
  rm -rf "$tmp"
}
trap cleanup EXIT
fail() { echo "FAIL: $1"; [ -n "${2:-}" ] && printf '%s\n' "$2"; exit 1; }

# Lokaler http.server als Ersatz fuer piwheels. Im Test gibt es kein Netz.
srvdir="$tmp/srv"
mkdir -p "$srvdir"
python3 -u -m http.server --bind 127.0.0.1 0 --directory "$srvdir" >"$tmp/srv.log" 2>&1 &
srv_pid=$!
port=""
for _ in $(seq 100); do
  port="$(grep -o 'port [0-9]*' "$tmp/srv.log" | head -n 1 | cut -d' ' -f2 || true)"
  [ -n "$port" ] && break
  sleep 0.1
done
[ -n "$port" ] || fail "http.server startete nicht" "$(cat "$tmp/srv.log")"

# Ein Port, auf dem nichts lauscht: die Verbindung wird abgelehnt.
dead_port="$(python3 -c 'import socket; s = socket.socket(); s.bind(("127.0.0.1", 0)); print(s.getsockname()[1]); s.close()')"

name_a="paho_mqtt-2.1.0-py3-none-any.whl"
printf 'wheel a\n' > "$srvdir/$name_a"
sha_a="$(sha256sum "$srvdir/$name_a" | cut -d' ' -f1)"
bad_sha="$(printf 'anderer Inhalt\n' | sha256sum | cut -d' ' -f1)"
name_e="tinytuya-1.2.0-py3-none-any.whl"
printf 'wheel e\n' > "$srvdir/$name_e"
sha_e="$(sha256sum "$srvdir/$name_e" | cut -d' ' -f1)"

export EN_SUDO=""

# new_case: frischer Node und Bundle. Testvariable nur, wenn der Fall sie braucht.
new_case() {
  rm -rf "$tmp/state" "$tmp/bundle"
  mkdir -p "$tmp/state" "$tmp/bundle/wheels" "$tmp/bundle/bootstrap"
  export EN_STATE_DIR="$tmp/state" EN_BUNDLE_DIR="$tmp/bundle"
  unset EN_WHEELS_DIR EN_WHEEL_HOSTS_TEST
}

# manifest <wheels-json>: Manifest mit genau dieser Festlegungsliste.
manifest() {
  printf '{"version":"v1.0.0","files":{},"wheels":%s}\n' "$1" > "$tmp/bundle/manifest.json"
}

# pin <datei> <adresse> <sha>: eine Festlegung als JSON.
pin() {
  printf '{"file":"%s","url":"%s","sha256":"%s"}' "$1" "$2" "$3"
}

# run_sync: fuehrt wheels_sync so aus wie Schritt 50. Setzt rc und out.
run_sync() {
  rc=0
  out="$(STEP_ID=50 bash -c 'set -euo pipefail; source "$1"; wheels_sync; echo SYNC_DONE' _ "$lib" 2>&1)" || rc=$?
}

expect_ok() {
  if [ "$rc" -ne 0 ] || ! grep -q '^SYNC_DONE$' <<<"$out"; then
    fail "$1" "$out"
  fi
}

expect_fail() {
  if [ "$rc" -eq 0 ] || ! grep -q "^##STEP 50 fail $2\$" <<<"$out"; then
    fail "$1: erwartet ##STEP 50 fail $2" "$out"
  fi
}

# --- vorhandene Datei mit passender Pruefsumme: kein Download ----------------
new_case
mkdir -p "$tmp/state/wheels"
cp "$srvdir/$name_a" "$tmp/state/wheels/$name_a"
manifest "[$(pin "$name_a" "http://127.0.0.1:$dead_port/$name_a" "$sha_a")]"
export EN_WHEEL_HOSTS_TEST=127.0.0.1
run_sync
expect_ok "vorhandenes Wheel mit passender Pruefsumme"

# --- fehlendes Wheel wird geladen, Rechte 0644 bzw. 0755 ----------------------
new_case
export EN_WHEEL_HOSTS_TEST=127.0.0.1
manifest "[$(pin "$name_a" "http://127.0.0.1:$port/$name_a" "$sha_a")]"
run_sync
expect_ok "fehlendes Wheel wird geladen"
[ "$(sha256sum "$tmp/state/wheels/$name_a" | cut -d' ' -f1)" = "$sha_a" ] || fail "geladene Datei hat falsche Pruefsumme"
[ "$(stat -c %a "$tmp/state/wheels/$name_a")" = 644 ] || fail "geladene Datei nicht 0644"
[ "$(stat -c %a "$tmp/state/wheels")" = 755 ] || fail "Wheel-Verzeichnis nicht 0755"
[ ! -e "$tmp/state/wheels/$name_a.part" ] || fail "Teildatei liegengeblieben"

# --- falsche Pruefsumme nach dem Download ---------------------------------------
new_case
export EN_WHEEL_HOSTS_TEST=127.0.0.1
manifest "[$(pin "$name_a" "http://127.0.0.1:$port/$name_a" "$bad_sha")]"
run_sync
expect_fail "falsche Pruefsumme nach dem Download" WHEEL_FILE_INVALID
if [ -e "$tmp/state/wheels/$name_a" ] || [ -e "$tmp/state/wheels/$name_a.part" ]; then
  fail "verworfene Datei liegt noch im Verzeichnis"
fi

# --- Server weg: Netzfehler mit Dateiname im Log --------------------------------
new_case
export EN_WHEEL_HOSTS_TEST=127.0.0.1
manifest "[$(pin "$name_a" "http://127.0.0.1:$dead_port/$name_a" "$sha_a")]"
run_sync
expect_fail "Server weg" WHEELS_DOWNLOAD_FAILED
grep -q "$name_a" <<<"$out" || fail "Dateiname fehlt im Log" "$out"

# --- fremder Host wird abgelehnt, bevor es eine Verbindung gibt ------------------
new_case
manifest "[$(pin "$name_a" "https://evil.example.invalid/$name_a" "$sha_a")]"
run_sync
expect_fail "fremder Host" WHEELS_DOWNLOAD_FAILED
grep -q 'nicht erlaubt' <<<"$out" || fail "Ablehnung nicht im Log" "$out"

# --- http ist ohne Testvariable nicht erlaubt; es gibt keine Anfrage -------------
new_case
manifest "[$(pin "$name_e" "http://127.0.0.1:$port/$name_e" "$sha_e")]"
run_sync
expect_fail "http ohne Testvariable" WHEELS_DOWNLOAD_FAILED
if grep -q "GET /$name_e" "$tmp/srv.log"; then
  fail "http-Adresse ohne Testvariable wurde trotzdem abgefragt"
fi

# --- veraltete Datei wird geloescht, festgelegte bleibt ------------------------
new_case
mkdir -p "$tmp/state/wheels"
cp "$srvdir/$name_a" "$tmp/state/wheels/$name_a"
printf 'alt\n' > "$tmp/state/wheels/old_pkg-0.9.0-py3-none-any.whl"
manifest "[$(pin "$name_a" "http://127.0.0.1:$dead_port/$name_a" "$sha_a")]"
run_sync
expect_ok "veraltete Datei"
[ ! -e "$tmp/state/wheels/old_pkg-0.9.0-py3-none-any.whl" ] || fail "veraltete Datei nicht geloescht"
[ -f "$tmp/state/wheels/$name_a" ] || fail "festgelegte Datei fehlt nach dem Aufraeumen"

# --- untergeschobene .whl ohne Festlegung wird entfernt, auch bei leerer Liste ---
new_case
mkdir -p "$tmp/state/wheels"
printf 'evil\n' > "$tmp/state/wheels/evil-1.0-py3-none-any.whl"
manifest "[]"
run_sync
expect_ok "leere Festlegungsliste"
[ ! -e "$tmp/state/wheels/evil-1.0-py3-none-any.whl" ] || fail "untergeschobene .whl nicht entfernt"

# --- eingebettetes Wheel wird kopiert, nicht geladen -----------------------------
new_case
cp "$srvdir/$name_a" "$tmp/bundle/wheels/$name_a"
manifest "[$(pin "$name_a" "http://127.0.0.1:$dead_port/$name_a" "$sha_a")]"
run_sync
expect_ok "eingebettetes Wheel"
cmp -s "$srvdir/$name_a" "$tmp/state/wheels/$name_a" || fail "eingebettete Datei nicht kopiert"

# --- eingebettetes Wheel mit falscher Pruefsumme -> WHEEL_FILE_INVALID -----------
new_case
printf 'verfaelscht\n' > "$tmp/bundle/wheels/$name_a"
manifest "[$(pin "$name_a" "http://127.0.0.1:$dead_port/$name_a" "$sha_a")]"
run_sync
expect_fail "eingebettetes Wheel mit falscher Pruefsumme" WHEEL_FILE_INVALID

# --- ungueltige Festlegung -> MANIFEST_PARSE_FAILED ------------------------------
new_case
manifest "[$(pin "$name_a" "http://127.0.0.1:$dead_port/$name_a" "nicht-hex")]"
run_sync
expect_fail "ungueltige Pruefsumme in der Festlegung" MANIFEST_PARSE_FAILED

# --- wheels_declared: nur eine Liste im Manifest zaehlt --------------------------
new_case
manifest "[]"
bash -c 'set -euo pipefail; source "$1"; wheels_declared' _ "$lib" || fail "leere Liste gilt nicht als Festlegung"
printf '{"version":"v1.0.0","files":{}}\n' > "$tmp/bundle/manifest.json"
if bash -c 'set -euo pipefail; source "$1"; wheels_declared' _ "$lib"; then
  fail "altes Manifest ohne wheels gilt als Festlegung"
fi

echo "OK: $(basename "$0")"
