#!/usr/bin/env bash
# Test for scripts/build/make_bundle.sh: Fremd-Wheels als Festlegung im
# Manifest, nur die eigenen Wheels im Archiv (--embed-wheels legt alle hinein).
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo="$(cd "$here/../.." && pwd)"
script="$repo/scripts/build/make_bundle.sh"
libs=(energy_node_common battery_soc_core)

tmp="$(mktemp -d)"
srv_pid=""
created_dist=()
cleanup() {
  if [[ -n "${srv_pid}" ]]; then kill "${srv_pid}" 2>/dev/null || true; fi
  # Der Stub-pip legt Platzhalter-Wheels in libs/*/dist an. Die verschwinden
  # wieder, damit kein echter Bau sie spaeter als Cache wiederverwendet.
  for dist in "${created_dist[@]}"; do rm -rf "${dist}"; done
  rm -rf "$tmp"
}
trap cleanup EXIT
fail() { echo "FAIL: $1"; [ -n "${2:-}" ] && printf '%s\n' "$2"; exit 1; }

for lib in "${libs[@]}"; do
  dist="$repo/libs/$lib/dist"
  [ -d "$dist" ] || created_dist+=("$dist")
done

# --- Indizes und Fremd-Wheels ----------------------------------------------
# piwheels liefert paho, PyPI liefert requests. Beides sind echte Zip-Wheels
# mit METADATA, damit missing_requirements.py sie lesen kann.
src="$tmp/src"
mkdir -p "$src"
for nv in paho_mqtt-2.1.0 requests-2.32.0; do
  python3 -I -c '
import sys, zipfile
path, nv = sys.argv[1], sys.argv[2]
name, version = nv.split("-")
with zipfile.ZipFile(path, "w") as z:
    z.writestr(f"{name}-{version}.dist-info/METADATA",
               f"Metadata-Version: 2.1\nName: {name}\nVersion: {version}\n")
' "$src/$nv-py3-none-any.whl" "$nv"
done
sha_paho="$(sha256sum "$src/paho_mqtt-2.1.0-py3-none-any.whl" | cut -d' ' -f1)"
sha_req="$(sha256sum "$src/requests-2.32.0-py3-none-any.whl" | cut -d' ' -f1)"

idx="$tmp/idx"
mkdir -p "$idx/piwheels/pkgs" "$idx/pypi/pkgs" "$idx/piwheels/paho-mqtt" "$idx/pypi/requests"
cp "$src/paho_mqtt-2.1.0-py3-none-any.whl" "$idx/piwheels/pkgs/"
cp "$src/requests-2.32.0-py3-none-any.whl" "$idx/pypi/pkgs/"
printf '<a href="../pkgs/paho_mqtt-2.1.0-py3-none-any.whl#sha256=%s">paho</a>\n' "$sha_paho" \
  > "$idx/piwheels/paho-mqtt/index.html"
printf '<a href="../pkgs/requests-2.32.0-py3-none-any.whl#sha256=%s">requests</a>\n' "$sha_req" \
  > "$idx/pypi/requests/index.html"

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

# Stub-pip: download kopiert die Fremd-Wheels, wheel legt ein eigenes Rad an.
cat > "$tmp/fakepip" <<'SH'
#!/usr/bin/env bash
printf '%s\n' "$*" >> "$PIP_LOG"
case "$1" in
  download)
    dir=""
    while [ $# -gt 0 ]; do
      case "$1" in -d) dir="$2"; shift 2 ;; *) shift ;; esac
    done
    cp "$WHEEL_SRC"/*.whl "$dir/"
    ;;
  wheel)
    src="$2"; dir=""; shift 2
    while [ $# -gt 0 ]; do
      case "$1" in --wheel-dir) dir="$2"; shift 2 ;; *) shift ;; esac
    done
    name="$(basename "$src")"
    version="$(tr -d '[:space:]' < "$src/VERSION")"; version="${version#v}"
    : > "$dir/${name}-${version}-py3-none-any.whl"
    ;;
esac
exit 0
SH
chmod +x "$tmp/fakepip"

fake_dashboard="$tmp/fake-dashboard"
printf '#!/bin/sh\necho dashboard\n' > "$fake_dashboard"

export EN_PIP="$tmp/fakepip" PIP_LOG="$tmp/pip.log" WHEEL_SRC="$src"
export EN_WHEEL_INDEXES="$base/piwheels $base/pypi"

build() { # <out> [schalter...]
  local out="$1"; shift
  bash "$script" --arch armv6 --python-minor 3.11 --abi cp311 \
    --out "$out" --dashboard-binary "$fake_dashboard" "$@" >"$out.log" 2>&1 \
    || fail "make_bundle.sh scheiterte ($*)" "$(cat "$out.log")"
  archive="$(find "$out" -maxdepth 1 -name 'energy-node-*-armv6.tar.gz' | head -n 1)"
  [ -n "$archive" ] || fail "kein Archiv in $out" "$(ls "$out")"
  rm -rf "$out.x"
  mkdir -p "$out.x"
  tar -xzf "$archive" -C "$out.x"
}

# Die Wheel-Versionen kommen aus den VERSION-Dateien, auf der Prerelease-Linie
# tragen sie ein -pre.N.
soc_whl="battery_soc_core-$(sed 's/^v//' "$repo/libs/battery_soc_core/VERSION")-py3-none-any.whl"
common_whl="energy_node_common-$(sed 's/^v//' "$repo/libs/energy_node_common/VERSION")-py3-none-any.whl"

# --- Standard: Archiv nur mit eigenen Wheels, Manifest mit Festlegungen ----
build "$tmp/dist1"
find "$tmp/dist1.x/wheels" -maxdepth 1 -type f -printf '%f\n' | sort > "$tmp/own.txt"
[ "$(cat "$tmp/own.txt")" = "$soc_whl
$common_whl" ] \
  || fail "Archiv enthaelt nicht nur die eigenen Wheels" "$(cat "$tmp/own.txt")"

python3 -I -c '
import json, sys
m = json.load(open(sys.argv[1]))
wheels = m["wheels"]
assert isinstance(wheels, list), wheels
assert [w["file"] for w in wheels] == [
    "paho_mqtt-2.1.0-py3-none-any.whl",
    "requests-2.32.0-py3-none-any.whl",
], wheels
paho, req = wheels
base, sha_paho, sha_req = sys.argv[2], sys.argv[3], sys.argv[4]
assert paho == {"file": "paho_mqtt-2.1.0-py3-none-any.whl",
                "url": base + "/piwheels/pkgs/paho_mqtt-2.1.0-py3-none-any.whl",
                "sha256": sha_paho}, paho
assert req == {"file": "requests-2.32.0-py3-none-any.whl",
               "url": base + "/pypi/pkgs/requests-2.32.0-py3-none-any.whl",
               "sha256": sha_req}, req
' "$tmp/dist1.x/manifest.json" "$base" "$sha_paho" "$sha_req" 2>"$tmp/assert.err" \
  || fail "manifest.wheels passt nicht" "$(cat "$tmp/assert.err")"

# --- --embed-wheels: beide Sorten im Archiv -----------------------------------
build "$tmp/dist2" --embed-wheels
find "$tmp/dist2.x/wheels" -maxdepth 1 -type f -printf '%f\n' | sort > "$tmp/all.txt"
for f in paho_mqtt-2.1.0-py3-none-any.whl requests-2.32.0-py3-none-any.whl \
         "$common_whl" "$soc_whl"; do
  grep -qx "$f" "$tmp/all.txt" || fail "--embed-wheels legt $f nicht ins Archiv" "$(cat "$tmp/all.txt")"
done
python3 -I -c '
import json, sys
assert len(json.load(open(sys.argv[1]))["wheels"]) == 2
' "$tmp/dist2.x/manifest.json" || fail "--embed-wheels aendert manifest.wheels"

# --- --skip-wheels: keine Festlegungen ----------------------------------------
build "$tmp/dist3" --skip-wheels
python3 -I -c '
import json, sys
assert json.load(open(sys.argv[1]))["wheels"] == []
' "$tmp/dist3.x/manifest.json" || fail "--skip-wheels laesst manifest.wheels nicht leer"

echo "OK: $(basename "$0")"
