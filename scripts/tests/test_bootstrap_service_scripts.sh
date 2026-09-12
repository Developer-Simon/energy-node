#!/usr/bin/env bash
# Test for scripts/bootstrap/8x-*.sh
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
boot="$here/../bootstrap"

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
fail() { echo "FAIL: $1"; [ -n "${2:-}" ] && printf '%s\n' "$2"; exit 1; }

mkdir -p "$tmp/bin"
cat > "$tmp/bin/systemctl" <<'SH'
#!/usr/bin/env bash
printf 'systemctl %s\n' "$*" >> "$SYSTEMCTL_LOG"
SH
chmod +x "$tmp/bin/systemctl"
export PATH="$tmp/bin:$PATH"
export SYSTEMCTL_LOG="$tmp/systemctl.log"

# Datei : ID : Verzeichnis : Unit - dieselbe Tabelle wie im Plan.
TABLE=(
  "81-apsystems.sh:81:apsystems_ez1:apsystems-ez1.service"
  "82-battery-soc.sh:82:battery_soc:battery-soc.service"
  "83-shelly.sh:83:shelly:shelly-rpc.service"
  "84-trucki.sh:84:trucki:trucki-http.service"
  "85-tuya.sh:85:tuya_mqtt:tuya.service"
  "88-automation.sh:88:automation:automation.service"
)

bundle="$tmp/bundle"
for entry in "${TABLE[@]}"; do
  dir="$(cut -d: -f3 <<<"$entry")"
  unit="$(cut -d: -f4 <<<"$entry")"
  mkdir -p "$bundle/services/$dir/devices"
  printf 'print(1)\n' > "$bundle/services/$dir/mod.py"
  printf '[Unit]\n'   > "$bundle/services/$dir/$unit"
done

export EN_ROOT="$tmp/root" EN_BUNDLE_DIR="$bundle" EN_BUNDLE_VERSION=v1.0.0
export EN_SUDO="" EN_TARGET_BASE=/home/pruef

for entry in "${TABLE[@]}"; do
  file="$(cut -d: -f1 <<<"$entry")"
  id="$(cut -d: -f2 <<<"$entry")"
  dir="$(cut -d: -f3 <<<"$entry")"
  unit="$(cut -d: -f4 <<<"$entry")"

  export EN_STATE_DIR="$tmp/state-$id"
  out="$(bash "$boot/$file")"
  grep -q "^##STEP $id ok\$" <<<"$out" || fail "$file: kein ok-Marker fuer $id" "$out"
  [ -f "$tmp/root/home/pruef/$dir/mod.py" ] || fail "$file: Quelle aus $dir fehlt"
  [ -f "$tmp/root/etc/systemd/system/$unit" ] || fail "$file: Unit $unit fehlt"

  # Zweiter Lauf: derselbe Stempel, also skip.
  out="$(bash "$boot/$file")"
  grep -q "^##STEP $id skip bereits erledigt\$" <<<"$out" \
    || fail "$file: zweiter Lauf nicht uebersprungen" "$out"
done

# --- jede Datei ist ausfuehrbar und ruft service_step genau einmal --------
for entry in "${TABLE[@]}"; do
  file="$(cut -d: -f1 <<<"$entry")"
  [ -x "$boot/$file" ] || fail "$file ist nicht ausfuehrbar"
  n="$(grep -c '^service_step ' "$boot/$file")"
  [ "$n" = 1 ] || fail "$file ruft service_step $n mal statt einmal"
done

# --- kein Dienst-Skript ohne Eintrag in der Tabelle ----------------------
for file in "$boot"/8?-*.sh; do
  name="$(basename "$file")"
  printf '%s\n' "${TABLE[@]}" | grep -q "^$name:" \
    || fail "unbekanntes Dienst-Skript: $name"
done

echo "OK: $(basename "$0")"
