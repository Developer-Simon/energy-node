#!/usr/bin/env bash
# Test for scripts/build/lib/wheel_pins.py
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
pins="$here/../build/lib/wheel_pins.py"

tmp="$(mktemp -d)"
srv_pid=""
cleanup() {
  if [[ -n "${srv_pid}" ]]; then kill "${srv_pid}" 2>/dev/null || true; fi
  rm -rf "$tmp"
}
trap cleanup EXIT
fail() { echo "FAIL: $1"; [ -n "${2:-}" ] && printf '%s\n' "$2"; exit 1; }

# Zwei PEP-503-Indizes als statische Verzeichnisse: first (piwheels) und
# second (PyPI). Ein lokaler http.server liefert sie, wie die echten Indizes.
idx="$tmp/idx"
mkdir -p "$idx/first" "$idx/second"
python3 -u -m http.server --bind 127.0.0.1 0 --directory "$idx" >"$tmp/srv.log" 2>&1 &
srv_pid=$!
port=""
for _ in $(seq 100); do
  port="$(grep -o 'port [0-9]*' "$tmp/srv.log" | head -n 1 | cut -d' ' -f2 || true)"
  [ -n "$port" ] && break
  sleep 0.1
done
[ -n "$port" ] || fail "http.server startete nicht" "$(cat "$tmp/srv.log")"
base="http://127.0.0.1:$port"

# make_wheel <name-version-platform.whl> <inhalt>: legt eine Datei an, gibt SHA-256 aus.
make_file() {
  local path="$1" content="$2"
  mkdir -p "$(dirname "$path")"
  printf '%s\n' "$content" > "$path"
  sha256sum "$path" | cut -d' ' -f1
}

# link <index> <normalisierter-name> <datei> <sha>: Eintrag auf der Simple-Seite.
# Der Link ist relativ, wie ihn echte Indizes ausliefern.
link() {
  local index="$1" name="$2" file="$3" sha="$4"
  mkdir -p "$index/$name"
  printf '<a href="../pkgs/%s#sha256=%s">%s</a><br/>\n' "$file" "$sha" "$file" \
    >> "$index/$name/index.html"
}

# --- Treffer in beiden Indizes, Reihenfolge und Sortierung ------------------
w1="$tmp/w1"
sha_paho="$(make_file "$w1/paho_mqtt-2.1.0-py3-none-any.whl" paho)"
sha_req="$(make_file "$w1/requests-2.32.0-py3-none-any.whl" requests)"
mkdir -p "$idx/first/pkgs" "$idx/second/pkgs"
cp "$w1/paho_mqtt-2.1.0-py3-none-any.whl" "$idx/first/pkgs/"
cp "$w1/paho_mqtt-2.1.0-py3-none-any.whl" "$idx/second/pkgs/"
cp "$w1/requests-2.32.0-py3-none-any.whl" "$idx/second/pkgs/"
# piwheels hat den Namen mit einem anderen Hash: der Treffer mit passendem
# Hash in PyPI gewinnt.
link "$idx/first" paho-mqtt paho_mqtt-2.1.0-py3-none-any.whl \
  "1111111111111111111111111111111111111111111111111111111111111111"
link "$idx/second" paho-mqtt paho_mqtt-2.1.0-py3-none-any.whl "$sha_paho"
link "$idx/second" requests requests-2.32.0-py3-none-any.whl "$sha_req"

out="$(python3 -I "$pins" "$w1" "$base/first" "$base/second")" \
  || fail "Treffer in zwei Indizes abgelehnt" "$out"
python3 -I -c '
import json, sys
pins = json.loads(sys.argv[1])
base, sha_paho, sha_req = sys.argv[2], sys.argv[3], sys.argv[4]
assert [p["file"] for p in pins] == [
    "paho_mqtt-2.1.0-py3-none-any.whl",
    "requests-2.32.0-py3-none-any.whl",
], pins
paho, req = pins
assert paho["url"] == base + "/second/pkgs/paho_mqtt-2.1.0-py3-none-any.whl", paho
assert paho["sha256"] == sha_paho, paho
assert req["url"] == base + "/second/pkgs/requests-2.32.0-py3-none-any.whl", req
assert req["sha256"] == sha_req, req
assert "#" not in paho["url"], paho
' "$out" "$base" "$sha_paho" "$sha_req" 2>"$tmp/assert.err" \
  || fail "falsche Festlegungen" "$out $(cat "$tmp/assert.err")"

# --- falsches Fragment: Abbruch mit Dateiname ------------------------------
w2="$tmp/w2"
make_file "$w2/tinytuya-1.17.0-py3-none-any.whl" tinytuya >/dev/null
cp "$w2/tinytuya-1.17.0-py3-none-any.whl" "$idx/first/pkgs/"
link "$idx/first" tinytuya tinytuya-1.17.0-py3-none-any.whl \
  "0000000000000000000000000000000000000000000000000000000000000000"
set +e
err="$(python3 -I "$pins" "$w2" "$base/first" "$base/second" 2>&1 >/dev/null)"
rc=$?
set -e
[ "$rc" -eq 1 ] || fail "falsches Fragment nicht mit Exit 1 abgelehnt" "rc=$rc $err"
grep -q 'tinytuya-1.17.0-py3-none-any.whl' <<<"$err" \
  || fail "Fehlermeldung nennt die Datei nicht" "$err"

# --- fehlende Datei: Abbruch mit Dateiname ---------------------------------
w3="$tmp/w3"
make_file "$w3/apsystems_ez1-2.7.0-py3-none-any.whl" apsystems >/dev/null
set +e
err="$(python3 -I "$pins" "$w3" "$base/first" "$base/second" 2>&1 >/dev/null)"
rc=$?
set -e
[ "$rc" -eq 1 ] || fail "fehlende Datei nicht mit Exit 1 abgelehnt" "rc=$rc $err"
grep -q 'apsystems_ez1-2.7.0-py3-none-any.whl' <<<"$err" \
  || fail "Fehlermeldung nennt die fehlende Datei nicht" "$err"

echo "OK: $(basename "$0")"
