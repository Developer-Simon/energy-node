#!/usr/bin/env bash
# Test for scripts/build/lib/wheels.sh
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
lib="$here/../build/lib/wheels.sh"

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
fail() { echo "FAIL: $1"; [ -n "${2:-}" ] && printf '%s\n' "$2"; exit 1; }

mkdir -p "$tmp/bin"
cat > "$tmp/bin/fakepip" <<'SH'
#!/usr/bin/env bash
printf 'pip %s\n' "$*" >> "$PIP_LOG"
exit "${PIP_RC:-0}"
SH
chmod +x "$tmp/bin/fakepip"
export PIP_LOG="$tmp/pip.log" EN_PIP="$tmp/bin/fakepip"

run() { bash -c 'source "$1"; shift; "$@"' _ "$lib" "$@"; }

# --- Architektur-Tabelle ---------------------------------------------------
[ "$(run arch_platform_tags armv6)" = linux_armv6l ] || fail "armv6-Tag falsch"
[ "$(run arch_uname_machines armv6 | tr '\n' ' ')" = "armv6l armv7l " ] \
  || fail "armv6-uname falsch" "$(run arch_uname_machines armv6)"
[ "$(run arch_go_env armv6 | tr '\n' ' ')" = "GOARCH=arm GOARM=6 " ] \
  || fail "armv6-Go falsch" "$(run arch_go_env armv6)"
run arch_platform_tags arm64 | grep -qx manylinux2014_aarch64 || fail "arm64-Tag fehlt"
run arch_platform_tags amd64 | grep -qx manylinux2014_x86_64 || fail "amd64-Tag fehlt"
[ "$(run arch_uname_machines amd64)" = x86_64 ] || fail "amd64-uname falsch"

set +e
run arch_platform_tags sparc64 2>/dev/null
rc=$?
set -e
[ "$rc" -ne 0 ] || fail "unbekannte Architektur nicht bemaengelt"

# --- fetch_thirdparty_wheels baut den pip-Aufruf richtig ------------------
run fetch_thirdparty_wheels "$tmp/wheels" armv6 3.11 cp311 >/dev/null
log="$(cat "$PIP_LOG")"
grep -q -- '--only-binary=:all:' <<<"$log" || fail "ohne --only-binary" "$log"
grep -q -- '--index-url https://www.piwheels.org/simple' <<<"$log" || fail "ohne piwheels" "$log"
grep -q -- '--extra-index-url https://pypi.org/simple' <<<"$log" || fail "ohne PyPI-Zweitindex" "$log"
grep -q -- '--platform linux_armv6l' <<<"$log" || fail "ohne Platform-Tag" "$log"
grep -q -- '--python-version 3.11' <<<"$log" || fail "ohne Python-Version" "$log"
grep -q -- '--implementation cp' <<<"$log" || fail "ohne Implementation" "$log"
grep -q -- '--abi cp311' <<<"$log" || fail "ohne ABI" "$log"
grep -q -- "-d $tmp/wheels" <<<"$log" || fail "ohne Zielverzeichnis" "$log"
for pkg in paho-mqtt apsystems-ez1 tinytuya requests; do
  grep -q -- "$pkg" <<<"$log" || fail "Paket $pkg fehlt" "$log"
done
[ -d "$tmp/wheels" ] || fail "Zielverzeichnis nicht angelegt"

# --- fehlende Abhaengigkeiten unter den Zielmarkern ------------------------
# pip wertet Marker wie python_version < "3.13" mit dem Interpreter des
# Bau-Rechners aus, nicht mit dem Ziel: Auf Python 3.14 fehlt sonst
# typing_extensions im Bundle, das aiohttp auf dem Node (3.11) braucht.
missing_py="$here/../build/lib/missing_requirements.py"
mk_wheel() { # <ziel> <name-version> <Requires-Dist>...
  local dir="$1" nv="$2"; shift 2
  local name="${nv%%-*}" version="${nv#*-}" meta
  meta="$(mktemp -d)"
  mkdir -p "$meta/${name}-${version}.dist-info"
  {
    printf 'Metadata-Version: 2.1\nName: %s\nVersion: %s\n' "$name" "$version"
    for req in "$@"; do printf 'Requires-Dist: %s\n' "$req"; done
  } > "$meta/${name}-${version}.dist-info/METADATA"
  mkdir -p "$dir"
  (cd "$meta" && python3 -c 'import sys,zipfile,glob; z=zipfile.ZipFile(sys.argv[1],"w"); [z.write(f) for f in glob.glob("*/*")]' \
    "$dir/${name}-${version}-py3-none-any.whl")
  rm -rf "$meta"
}
w="$tmp/closure"
mk_wheel "$w" aiohttp-3.14.3 'typing_extensions>=4.4; python_version < "3.13"' 'attrs>=17' \
  'old_thing; python_version < "3.9"' 'speedups_only; extra == "speedups"' \
  'arm_only; platform_machine == "armv6l"' 'other_arch; platform_machine == "x86_64"'
mk_wheel "$w" attrs-24.1.0
mk_wheel "$w" new_enough-1.0 'attrs>=99'
out="$(python3 "$missing_py" "$w" 3.11 armv6l)" || fail "missing_requirements.py schlug fehl" "$out"
[ "$(sort <<<"$out" | tr '\n' ' ')" = "arm-only attrs>=99 typing-extensions>=4.4 " ] \
  || fail "falsche fehlende Abhaengigkeiten" "$out"
out="$(python3 "$missing_py" "$w" 3.14 x86_64)" || fail "missing_requirements.py (3.14) schlug fehl"
grep -q typing-extensions <<<"$out" && fail "typing_extensions unter 3.14 nicht noetig" "$out"
grep -qx other-arch <<<"$out" || fail "x86_64-Marker nicht ausgewertet" "$out"
[ -z "$(python3 "$missing_py" "$tmp/leer" 3.11 armv6l)" ] || fail "leeres Verzeichnis meldet etwas"

# --- arm64 reicht beide Platform-Tags durch -------------------------------
: > "$PIP_LOG"
run fetch_thirdparty_wheels "$tmp/wheels64" arm64 3.11 cp311 >/dev/null
grep -q -- '--platform manylinux2014_aarch64' "$PIP_LOG" || fail "arm64-Tag fehlt" "$(cat "$PIP_LOG")"
grep -q -- '--platform linux_aarch64' "$PIP_LOG" || fail "zweiter arm64-Tag fehlt" "$(cat "$PIP_LOG")"

# --- ein fehlendes Rad bricht den Bau ab ----------------------------------
set +e
out="$(PIP_RC=1 run fetch_thirdparty_wheels "$tmp/w2" armv6 3.11 cp311 2>&1)"
rc=$?
set -e
[ "$rc" -ne 0 ] || fail "pip-Fehler nicht weitergereicht"
grep -qi 'kein Wheel' <<<"$out" || fail "Meldung nennt das Problem nicht" "$out"

# --- build_local_wheels auf einem frischen Checkout ohne dist/ -----------
# find auf einem fehlenden Verzeichnis schlaegt fehl; unter set -e/pipefail
# darf das cached=$(find ...) nicht das ganze Skript stumm abbrechen (es tat
# das vor dem Fix, weil 2>/dev/null die einzige Fehlermeldung verschluckte).
repo="$tmp/repo"
mkdir -p "$repo/libs/energy_node_common" "$repo/libs/battery_soc_core"
(cd "$repo" && git init -q)
printf 'v1.2.3\n' > "$repo/libs/energy_node_common/VERSION"
printf 'v0.1.0\n' > "$repo/libs/battery_soc_core/VERSION"
cat > "$tmp/bin/fakepip" <<'SH'
#!/usr/bin/env bash
printf 'pip %s\n' "$*" >> "$PIP_LOG"
if [ "$1" = wheel ]; then
  src="$2" dir=""
  shift
  while [ $# -gt 0 ]; do
    case "$1" in --wheel-dir) dir="$2"; shift 2 ;; *) shift ;; esac
  done
  name="$(basename "$src")"
  version="$(tr -d '[:space:]' < "$src/VERSION")"; version="${version#v}"
  : > "$dir/${name}-${version}-py3-none-any.whl"
fi
exit "${PIP_RC:-0}"
SH
chmod +x "$tmp/bin/fakepip"
: > "$PIP_LOG"
# make_bundle.sh selbst laeuft mit set -euo pipefail - das muss hier auch
# gelten, sonst prueft der Test nicht denselben Fehlerpfad (ein einfacher
# "source; call" ohne -e haette den Bug nicht gezeigt).
out="$(cd "$repo" && bash -c 'set -euo pipefail; source "$1"; shift; "$@"' _ "$lib" build_local_wheels "$tmp/localwheels" 2>&1)" \
  || fail "build_local_wheels brach auf einem Checkout ohne dist/ stumm ab" "$out"
[ -d "$repo/libs/energy_node_common/dist" ] || fail "dist/ wurde nicht angelegt"
grep -q 'Baue energy_node_common 1.2.3' <<<"$out" || fail "Bauhinweis fehlt" "$out"

echo "OK: $(basename "$0")"
