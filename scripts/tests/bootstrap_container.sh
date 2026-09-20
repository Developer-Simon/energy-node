#!/usr/bin/env bash
#
# Faehrt die gesamte Bootstrap-Kette zweimal und prueft, dass der zweite
# Lauf nichts mehr tut (Abnahmekriterium 3).
#
# Alle Systemkommandos sind Attrappen; bewiesen wird Idempotenz und
# Verdrahtung, nicht das Verhalten auf echtem Raspberry Pi OS.
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
scripts="$(cd "$here/.." && pwd)"

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
fail() { echo "FAIL: $1"; [ -n "${2:-}" ] && printf '%s\n' "$2"; exit 1; }

# Gebaut wird fuer die Architektur und den Interpreter, unter denen dieser
# Test laeuft - 50-python-deps.sh prueft beides gegen das Manifest und
# braeche sonst mit ARCH_MISMATCH ab.
case "$(uname -m)" in
  x86_64) arch=amd64 ;;
  aarch64|arm64) arch=arm64 ;;
  armv6l|armv7l) arch=armv6 ;;
  *) fail "unbekannte Testarchitektur: $(uname -m)" ;;
esac
minor="$(python3 -c 'import sys; print("%d.%d" % sys.version_info[:2])')"

# --- Attrappen fuer alles, was das System anfasst -------------------------
mkdir -p "$tmp/bin"
# shellcheck disable=SC1091
source "$here/lib/fake_system_commands.sh"
install_fake_system_commands "$tmp/bin"
cat > "$tmp/bin/fakepip" <<'SH'
#!/usr/bin/env bash
printf 'pip %s\n' "$*" >> "$CMD_LOG"
exit 0
SH
chmod +x "$tmp/bin/fakepip"
export PATH="$tmp/bin:$PATH" CMD_LOG="$tmp/cmd.log"

# --- Bundle bauen, ohne Netz und ohne Go ----------------------------------
printf '#!/bin/sh\n' > "$tmp/fake-dashboard"
mkdir -p "$tmp/pack/tailscale_1.62.0_arm/systemd"
printf '#!/bin/sh\n' > "$tmp/pack/tailscale_1.62.0_arm/tailscale"
printf '#!/bin/sh\n' > "$tmp/pack/tailscale_1.62.0_arm/tailscaled"
printf '[Unit]\n'    > "$tmp/pack/tailscale_1.62.0_arm/systemd/tailscaled.service"
printf 'FLAGS=""\n'  > "$tmp/pack/tailscale_1.62.0_arm/systemd/tailscaled.defaults"
tar -czf "$tmp/ts.tgz" -C "$tmp/pack" tailscale_1.62.0_arm
printf '#!/bin/sh\n' > "$tmp/fake-caddy"

bash "$scripts/build/make_bundle.sh" \
  --arch "$arch" --python-minor "$minor" \
  --user pruef --base /home/pruef --out "$tmp/dist" \
  --dashboard-binary "$tmp/fake-dashboard" \
  --tailscale-tarball "$tmp/ts.tgz" \
  --caddy-binary "$tmp/fake-caddy" \
  --skip-wheels >"$tmp/build.log" 2>&1 \
  || fail "Bundle-Bau scheiterte" "$(cat "$tmp/build.log")"

bundle="$tmp/bundle"
mkdir -p "$bundle"
tar -xzf "$(find "$tmp/dist" -maxdepth 1 -name 'energy-node-*.tar.gz' | head -n 1)" -C "$bundle"
# Das Caddy-Beipack entpackt sonst der Installer; hier von Hand.
mkdir -p "$bundle/caddy"
tar -xzf "$(find "$tmp/dist" -maxdepth 1 -name 'caddy-*.tar.gz' | head -n 1)" -C "$bundle/caddy"
# --skip-wheels laesst wheels/ leer; 50-python-deps.sh verlangt aber
# mindestens ein Rad. Geprueft wird hier die Verdrahtung, nicht pip - ein
# Platzhalter genuegt und haelt den Test netzfrei.
rm -f "$bundle/wheels/LEER"
printf 'platzhalter\n' > "$bundle/wheels/energy_node_common-0.0.0-py3-none-any.whl"

printf 'geheim\n' > "$tmp/mqtt.pw"; chmod 600 "$tmp/mqtt.pw"
printf 'geheim\n' > "$tmp/admin.pw"; chmod 600 "$tmp/admin.pw"

export EN_BUNDLE_DIR="$bundle" EN_STATE_DIR="$tmp/state" EN_ROOT="$tmp/root"
export EN_SUDO="" EN_TARGET_BASE=/home/pruef EN_TARGET_USER=pruef
export EN_PIP="$tmp/bin/fakepip" EN_TAILSCALE_LOGIN_WAIT=1
EN_BUNDLE_VERSION="$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["version"])' \
  "$bundle/manifest.json")"
export EN_BUNDLE_VERSION

# Die Schrittliste kommt aus dem Manifest - nicht aus einer Kopie hier.
mapfile -t STEP_IDS < <(python3 -c '
import json, sys
data = json.load(open(sys.argv[1]))
for entry in sorted(data["steps"], key=lambda e: e["id"]):
    print(entry["id"])
' "$bundle/manifest.json")
[ "${#STEP_IDS[@]}" -ge 13 ] || fail "Schrittliste zu kurz" "${STEP_IDS[*]}"

script_for() {
  local id="$1" match
  match="$(find "$bundle/bootstrap" -maxdepth 1 -name "${id}-*.sh" | head -n 1)"
  [ -n "$match" ] || fail "kein Skript fuer Schritt $id"
  printf '%s' "$match"
}

run_chain() {
  local id script out
  : > "$1"
  for id in "${STEP_IDS[@]}"; do
    script="$(script_for "$id")"
    case "$id" in
      20) out="$(bash "$script" --user knoten --password-file "$tmp/mqtt.pw")" ;;
      60) out="$(bash "$script" --mqtt-password-file "$tmp/mqtt.pw" \
                                --admin-password-file "$tmp/admin.pw")" ;;
      *)  out="$(bash "$script")" ;;
    esac
    printf '%s\n' "$out" >> "$1"
  done
}

# --- erster Lauf: jeder Schritt meldet ok ---------------------------------
run_chain "$tmp/lauf1.log"
for id in "${STEP_IDS[@]}"; do
  grep -q "^##STEP $id ok\$" "$tmp/lauf1.log" \
    || fail "Schritt $id im ersten Lauf nicht ok" "$(cat "$tmp/lauf1.log")"
done
grep -q '^##STEP .* fail ' "$tmp/lauf1.log" \
  && fail "Fehlschlag im ersten Lauf" "$(cat "$tmp/lauf1.log")"

# --- Zustand einfrieren ----------------------------------------------------
before="$(find "$tmp/root" -type f -exec sha256sum {} + | sort)"

# --- zweiter Lauf: jeder Schritt meldet skip, ausser 65 -------------------
# 65-dashboard-config.sh ist bewusst kein Skip-Schritt (siehe die Datei
# selbst): die Dienstauswahl kann sich per Re-Deploy aendern, ohne dass die
# Bundle-Version steigt, also schreibt er bei jedem Lauf neu und stoesst am
# Ende einen (hier per Attrappe abgefangenen) systemctl try-restart an.
: > "$CMD_LOG"
run_chain "$tmp/lauf2.log"
for id in "${STEP_IDS[@]}"; do
  [ "$id" = "65" ] && continue
  grep -q "^##STEP $id skip bereits erledigt\$" "$tmp/lauf2.log" \
    || fail "Schritt $id im zweiten Lauf nicht uebersprungen" "$(cat "$tmp/lauf2.log")"
done
grep -q '^##STEP 65 ok$' "$tmp/lauf2.log" \
  || fail "Schritt 65 im zweiten Lauf nicht erneut ok" "$(cat "$tmp/lauf2.log")"
grep -v '^##STEP 65 ' "$tmp/lauf2.log" | grep -q '^##STEP .* ok$' \
  && fail "ein anderer Schritt als 65 lief im zweiten Lauf erneut" "$(cat "$tmp/lauf2.log")"
if [ -s "$CMD_LOG" ]; then
  [ "$(cat "$CMD_LOG")" = "systemctl try-restart energy-node-dashboard.service" ] \
    || fail "zweiter Lauf hat unerwartete Systemkommandos aufgerufen" "$(cat "$CMD_LOG")"
fi

after="$(find "$tmp/root" -type f -exec sha256sum {} + | sort)"
[ "$before" = "$after" ] || fail "der zweite Lauf hat Dateien veraendert" \
  "$(diff <(printf '%s\n' "$before") <(printf '%s\n' "$after") || true)"

# --- die Berichtsskripte laufen ueber denselben Zustand -------------------
bash "$bundle/bootstrap/plan.sh" > "$tmp/plan.json" || fail "plan.sh scheiterte"
python3 -c '
import json, sys
data = json.load(open(sys.argv[1]))
offen = [s["id"] for s in data["steps"] if s["state"] != "done"]
if offen:
    sys.exit("plan.sh meldet nach zwei Laeufen noch offene Schritte: %s" % offen)
' "$tmp/plan.json" || fail "plan.sh meldet offene Schritte" "$(cat "$tmp/plan.json")"

bash "$bundle/bootstrap/diagnose.sh" > "$tmp/diagnose.json" || fail "diagnose.sh scheiterte"
python3 -c 'import json,sys; json.load(open(sys.argv[1]))' "$tmp/diagnose.json" \
  || fail "diagnose.sh liefert kein gueltiges JSON" "$(cat "$tmp/diagnose.json")"

echo "OK: $(basename "$0") - Kette zweimal gefahren, zweiter Lauf ohne Wirkung"
